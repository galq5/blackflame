package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/galq/blackflame/internal/config"
)

//go:embed migrations/*.sql
var migrations embed.FS

func OpenSQLite(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	// Apply pragmas through the DSN so every pooled connection gets them.
	// foreign_keys is per-connection in SQLite; setting it once on the pool is
	// not enough.
	const pragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	dsn := path
	if path == ":memory:" {
		dsn = "file:blackflame?mode=memory&cache=shared&" + pragmas
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		dsn = "file:" + path + separator + pragmas + "&_pragma=journal_mode(WAL)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func EnsureDefaults(ctx context.Context, db *sql.DB) error {
	defaults := config.Default()

	settings := map[string]any{
		"branding":  defaults.Branding,
		"theme":     defaults.Theme,
		"layout":    defaults.Layout,
		"widgets":   defaults.Widgets,
		"search":    defaults.Search,
		"weather":   defaults.Weather,
		"status":    defaults.Status,
		"discovery": defaults.Discovery,
		"icons":     defaults.Icons,
	}
	for key, value := range settings {
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal default %s: %w", key, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value) VALUES (?, ?)`, key, string(raw)); err != nil {
			return fmt.Errorf("seed default %s: %w", key, err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM boards`).Scan(&count); err != nil {
		return fmt.Errorf("count boards: %w", err)
	}
	if count > 0 {
		return nil
	}

	result, err := db.ExecContext(ctx, `INSERT INTO boards(slug, name, is_public, sort_order) VALUES ('home', 'Home', 1, 0)`)
	if err != nil {
		return fmt.Errorf("seed default board: %w", err)
	}
	boardID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("default board id: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sections(board_id, name, is_public, sort_order) VALUES (?, 'Apps', 1, 0), (?, 'Bookmarks', 1, 1)`, boardID, boardID); err != nil {
		return fmt.Errorf("seed default sections: %w", err)
	}
	return nil
}
