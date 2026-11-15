package store

import (
	"context"
	"database/sql"
	"path/filepath"
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
