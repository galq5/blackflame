package flameimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/store"
)

func TestImportFlameDatabase(t *testing.T) {
	flamePath := filepath.Join(t.TempDir(), "flame.sqlite")
	flame, err := sql.Open("sqlite", flamePath)
	if err != nil {
		t.Fatal(err)
	}
	defer flame.Close()
	statements := []string{
		`CREATE TABLE apps (name TEXT, url TEXT, icon TEXT, isPinned INTEGER, orderId INTEGER, isPublic INTEGER, description TEXT)`,
		`CREATE TABLE categories (id INTEGER PRIMARY KEY, name TEXT, isPinned INTEGER, orderId INTEGER, isPublic INTEGER)`,
		`CREATE TABLE bookmarks (name TEXT, url TEXT, categoryId INTEGER, icon TEXT, isPublic INTEGER, orderId INTEGER)`,
		`INSERT INTO categories(id, name, isPinned, orderId, isPublic) VALUES (1, 'Media', 0, 0, 1)`,
		`INSERT INTO apps(name, url, icon, isPinned, orderId, isPublic, description) VALUES ('Immich', 'https://photos.example.com', 'immich', 1, 0, 1, 'Photos')`,
		`INSERT INTO bookmarks(name, url, categoryId, icon, isPublic, orderId) VALUES ('Wiki', 'https://wiki.example.com', 1, 'bookstack', 0, 0)`,
	}
	for _, statement := range statements {
		if _, err := flame.Exec(statement); err != nil {
			t.Fatalf("seed Flame database: %v", err)
		}
	}

	target, err := store.OpenSQLite(filepath.Join(t.TempDir(), "blackflame.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	ctx := context.Background()
	if err := store.Migrate(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefaults(ctx, target); err != nil {
		t.Fatal(err)
	}
	result, err := Import(ctx, target, flamePath, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Apps != 1 || result.Categories != 1 || result.Bookmarks != 1 || result.Skipped != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	cfg, err := store.LoadConfig(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	boards, sections, items := cfg.Counts()
	if boards != 1 || sections < 3 || items != 2 {
		t.Fatalf("unexpected counts: %d %d %d", boards, sections, items)
	}
	var foundApp, foundBookmark bool
	for _, board := range cfg.Boards {
		for _, section := range board.Sections {
			for _, item := range section.Items {
				switch item.Name {
				case "Immich":
					foundApp = item.Pinned && item.IsPublic() && item.Description == "Photos"
				case "Wiki":
					foundBookmark = !item.IsPublic() && section.Name == "Media"
				}
			}
		}
	}
	if !foundApp || !foundBookmark {
		t.Fatalf("imported content missing: app=%v bookmark=%v", foundApp, foundBookmark)
	}
	second, err := Import(ctx, target, flamePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Apps != 0 || second.Bookmarks != 0 || second.Skipped != 2 {
		t.Fatalf("merge import was not idempotent: %+v", second)
	}
	cfg, err = store.LoadConfig(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, count := cfg.Counts(); count != 2 {
		t.Fatalf("duplicate items after merge import: %d", count)
	}
}

func TestValidURL(t *testing.T) {
	if !validURL("https://example.com") || validURL("javascript:alert(1)") || validURL("") {
		t.Fatal("URL validation is wrong")
	}
}

func TestConfigDefaults(t *testing.T) {
	if config.CurrentVersion != 1 {
		t.Fatal("unexpected config version")
	}
}
