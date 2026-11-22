package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/galq/blackflame/internal/config"
)

func testDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := EnsureDefaults(ctx, db); err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	return db, func() { db.Close() }
}

func TestMigrateAndLoadDefaults(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	cfgStore, err := NewSQLiteStore(ctx, db)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	snapshot := cfgStore.Snapshot()
	if snapshot.Config == nil {
		t.Fatal("expected config")
	}
	if len(snapshot.Config.Boards) != 1 || snapshot.Config.Boards[0].Slug != "home" {
		t.Fatalf("unexpected boards: %+v", snapshot.Config.Boards)
	}
	if len(snapshot.Config.Boards[0].Sections) != 2 {
		t.Fatalf("unexpected sections: %+v", snapshot.Config.Boards[0].Sections)
	}
}

func TestCRUDAndReorder(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	cfgStore, err := NewSQLiteStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	boardID, err := CreateBoard(ctx, db, "work", "Work", true)
	if err != nil {
		t.Fatal(err)
	}
	sectionID, err := CreateSection(ctx, db, boardID, "Apps", true, false)
	if err != nil {
		t.Fatal(err)
	}
	first, err := CreateItem(ctx, db, ItemInput{BoardID: boardID, SectionID: sectionID, Kind: "app", Name: "First", URL: "https://first.example", Public: true, CheckEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateItem(ctx, db, ItemInput{BoardID: boardID, SectionID: sectionID, Kind: "app", Name: "Second", URL: "https://second.example", Public: true, CheckEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReorderItems(ctx, db, sectionID, []int64{second, first}); err != nil {
		t.Fatal(err)
	}
	home := cfgStore.Snapshot().Config.Boards[0]
	otherSection := home.Sections[0].ID
	otherItem, err := CreateItem(ctx, db, ItemInput{BoardID: home.ID, SectionID: otherSection, Kind: "app", Name: "Other", URL: "https://other.example", Public: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{{first, otherItem}, {first, first}, {first}} {
		if err := ReorderItems(ctx, db, sectionID, ids); err == nil {
			t.Fatalf("accepted invalid item order: %v", ids)
		}
	}
	if err := ReorderSections(ctx, db, boardID, []int64{otherSection}); err == nil {
		t.Fatal("moved a section across boards")
	}
	if err := cfgStore.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	var items []config.Item
	for _, board := range cfgStore.Snapshot().Config.Boards {
		if board.Slug != "work" {
			continue
		}
		for _, section := range board.Sections {
			items = append(items, section.Items...)
		}
	}
	if len(items) != 2 || items[0].Name != "Second" {
		t.Fatalf("unexpected item order: %+v", items)
	}
}

func TestForeignKeysCascade(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	boardID, err := CreateBoard(ctx, db, "cascade", "Cascade", true)
	if err != nil {
		t.Fatal(err)
	}
	sectionID, err := CreateSection(ctx, db, boardID, "Apps", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateItem(ctx, db, ItemInput{BoardID: boardID, SectionID: sectionID, Kind: "app", Name: "Item", URL: "https://example.com", Public: true, CheckEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSection(ctx, db, sectionID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE section_id = ?`, sectionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("foreign key cascade did not delete items: %d remain", count)
	}
}

func TestDiscoveryUpsertAndOverride(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()
	if err := UpsertDiscovered(ctx, db, config.SourceDocker, []config.DiscoveredItem{{
		Source:     config.SourceDocker,
		ExternalID: "abc:0",
		Name:       "Discovered",
		URL:        "https://discovered.example",
		Icon:       "docker",
		Group:      "Docker",
	}}); err != nil {
		t.Fatal(err)
	}
	cfgStore, err := NewSQLiteStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var discovered *config.Item
	var boardID, sectionID int64
	for _, board := range cfgStore.Snapshot().Config.Boards {
		for _, section := range board.Sections {
			for i := range section.Items {
				if section.Items[i].ExternalID == "abc:0" {
					discovered = &section.Items[i]
					boardID = board.ID
					sectionID = section.ID
				}
			}
		}
	}
	if discovered == nil || discovered.Source != config.SourceDocker {
		t.Fatalf("discovered item missing: %+v", discovered)
	}
	if err := UpdateItem(ctx, db, discovered.ID, ItemInput{
		BoardID: boardID, SectionID: sectionID, Kind: "app", Name: "Renamed",
		URL: discovered.URL, Public: true, CheckEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertDiscovered(ctx, db, config.SourceDocker, []config.DiscoveredItem{{
		Source:     config.SourceDocker,
		ExternalID: "abc:0",
		Name:       "Discovery Name",
		URL:        discovered.URL,
		Icon:       "docker",
		Group:      "Docker",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cfgStore.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	for _, board := range cfgStore.Snapshot().Config.Boards {
		for _, section := range board.Sections {
			for _, item := range section.Items {
				if item.ExternalID == "abc:0" && item.Name != "Renamed" {
					t.Fatalf("override was overwritten: %+v", item)
				}
			}
		}
	}
}

func TestMergeDiscoveredDoesNotMutateBase(t *testing.T) {
	base := config.Default()
	base.Boards = []config.Board{{Slug: "home", Name: "Home", Sections: []config.Section{{Name: "Apps"}}}}
	merged := MergeDiscovered(base, []config.Item{{Name: "Discovered", URL: "https://d.example", Group: "Docker"}})
	if len(base.Boards) != 1 {
		t.Fatal("base boards mutated")
	}
	if len(merged.Boards) != 2 {
		t.Fatalf("expected synthetic discovered board, got %+v", merged.Boards)
	}
}

func TestDeleteBoardGuards(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	var homeID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM boards WHERE slug = 'home'`).Scan(&homeID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBoard(ctx, db, homeID); !errors.Is(err, ErrLastBoard) {
		t.Fatalf("expected ErrLastBoard, got %v", err)
	}
	if err := DeleteBoard(ctx, db, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	second, err := CreateBoard(ctx, db, "work", "Work", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteBoard(ctx, db, second); err != nil {
		t.Fatalf("delete with two boards: %v", err)
	}
	if err := DeleteBoard(ctx, db, homeID); !errors.Is(err, ErrLastBoard) {
		t.Fatalf("expected ErrLastBoard for survivor, got %v", err)
	}
}

func TestConcurrentDeleteBoardsKeepsOne(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	var homeID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM boards WHERE slug = 'home'`).Scan(&homeID); err != nil {
		t.Fatal(err)
	}
	second, err := CreateBoard(ctx, db, "work", "Work", true)
	if err != nil {
		t.Fatal(err)
	}

	results := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for index, id := range []int64{homeID, second} {
		go func(index int, id int64) {
			defer wg.Done()
			results[index] = DeleteBoard(ctx, db, id)
		}(index, id)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one successful delete, got %d (%v)", succeeded, results)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM boards`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one surviving board, got %d", count)
	}
}

func TestItemRequiresMatchingBoard(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	boardA, err := CreateBoard(ctx, db, "board-a", "A", true)
	if err != nil {
		t.Fatal(err)
	}
	boardB, err := CreateBoard(ctx, db, "board-b", "B", true)
	if err != nil {
		t.Fatal(err)
	}
	sectionA, err := CreateSection(ctx, db, boardA, "Apps", true, false)
	if err != nil {
		t.Fatal(err)
	}
	input := ItemInput{BoardID: boardB, SectionID: sectionA, Kind: "app", Name: "Mismatched", URL: "https://m.example", Public: true}
	if _, err := CreateItem(ctx, db, input); err == nil {
		t.Fatal("accepted an item whose section belongs to another board")
	}
	valid := ItemInput{BoardID: boardA, SectionID: sectionA, Kind: "app", Name: "Matched", URL: "https://ok.example", Public: true}
	itemID, err := CreateItem(ctx, db, valid)
	if err != nil {
		t.Fatal(err)
	}
	mismatched := valid
	mismatched.BoardID = boardB
	if err := UpdateItem(ctx, db, itemID, mismatched); err == nil {
		t.Fatal("update accepted a section from another board")
	}
}

func TestLoadDiscoveredItemsKeepsItemConfig(t *testing.T) {
	db, cleanup := testDB(t)
	defer cleanup()
	ctx := context.Background()

	if err := UpsertDiscovered(ctx, db, config.SourceDocker, []config.DiscoveredItem{{
		Source:     config.SourceDocker,
		ExternalID: "cfg:1",
		Name:       "Configured",
		URL:        "https://cfg.example",
		Icon:       "docker",
		Group:      "Docker",
	}}); err != nil {
		t.Fatal(err)
	}

	var boardID, sectionID, itemID int64
	if err := db.QueryRowContext(ctx, `SELECT board_id, section_id, id FROM items WHERE external_id = 'cfg:1'`).Scan(&boardID, &sectionID, &itemID); err != nil {
		t.Fatal(err)
	}
	if err := UpdateItem(ctx, db, itemID, ItemInput{
		BoardID: boardID, SectionID: sectionID, Kind: "app", Name: "Configured",
		URL: "https://cfg.example", Public: true, CheckEnabled: true,
		Config: map[string]any{"port": 8080},
	}); err != nil {
		t.Fatal(err)
	}

	items, err := LoadDiscoveredItems(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ExternalID != "cfg:1" {
			continue
		}
		found = true
		if item.Config == nil {
			t.Fatal("LoadDiscoveredItems dropped the per-item config")
		}
	}
	if !found {
		t.Fatal("discovered item missing from LoadDiscoveredItems")
	}
}
