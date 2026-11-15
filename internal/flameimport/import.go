package flameimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

type Result struct {
	Apps       int `json:"apps"`
	Categories int `json:"categories"`
	Bookmarks  int `json:"bookmarks"`
	Skipped    int `json:"skipped"`
}

type flameApp struct {
	Name        string
	URL         string
	Icon        string
	Pinned      bool
	Order       int
	Public      bool
	Description string
}

type flameCategory struct {
	ID     int64
	Name   string
	Pinned bool
	Order  int
	Public bool
}

type flameBookmark struct {
	Name       string
	URL        string
	CategoryID int64
	Icon       string
	Public     bool
	Order      int
}

func Import(ctx context.Context, target *sql.DB, flamePath string, replace bool) (Result, error) {
	source, err := sql.Open("sqlite", "file:"+flamePath+"?mode=ro")
	if err != nil {
		return Result{}, fmt.Errorf("open Flame database: %w", err)
	}
	defer source.Close()
	if err := source.PingContext(ctx); err != nil {
		return Result{}, fmt.Errorf("open Flame database: %w", err)
	}
	if err := requireTable(ctx, source, "apps"); err != nil {
		return Result{}, err
	}
	if err := requireTable(ctx, source, "categories"); err != nil {
		return Result{}, err
	}
	if err := requireTable(ctx, source, "bookmarks"); err != nil {
		return Result{}, err
	}

	apps, err := readApps(ctx, source)
	if err != nil {
		return Result{}, err
	}
	categories, err := readCategories(ctx, source)
	if err != nil {
		return Result{}, err
	}
	bookmarks, err := readBookmarks(ctx, source)
	if err != nil {
		return Result{}, err
	}

	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()

	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM boards`); err != nil {
			return Result{}, fmt.Errorf("clear existing content: %w", err)
		}
	}
	boardID, err := ensureHomeBoard(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	appsSection, err := ensureSection(ctx, tx, boardID, "Apps")
	if err != nil {
		return Result{}, err
	}
	bookmarksSection, err := ensureSection(ctx, tx, boardID, "Bookmarks")
	if err != nil {
		return Result{}, err
	}

	result := Result{}
	sectionByCategory := map[int64]int64{}
	for index, category := range categories {
		sectionID, err := ensureSectionWithVisibility(ctx, tx, boardID, category.Name, category.Public, category.Pinned, category.Order)
		if err != nil {
			return Result{}, err
		}
		sectionByCategory[category.ID] = sectionID
		result.Categories++
		_ = index
	}

	for index, app := range apps {
		if !validURL(app.URL) {
			result.Skipped++
			continue
		}
		exists, err := itemExists(ctx, tx, appsSection, "app", app.Name, app.URL)
		if err != nil {
			return Result{}, err
		}
		if exists {
			result.Skipped++
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO items(
				board_id, section_id, kind, name, url, icon, description,
				is_public, is_pinned, source, sort_order
			) VALUES (?, ?, 'app', ?, ?, ?, ?, ?, ?, 'manual', ?)`,
			boardID, appsSection, app.Name, app.URL, app.Icon, app.Description,
			boolInt(app.Public), boolInt(app.Pinned), orderValue(app.Order, index),
		); err != nil {
			return Result{}, fmt.Errorf("import app %q: %w", app.Name, err)
		}
		result.Apps++
	}

	for index, bookmark := range bookmarks {
		if !validURL(bookmark.URL) {
			result.Skipped++
			continue
		}
		sectionID := bookmarksSection
		if mapped, ok := sectionByCategory[bookmark.CategoryID]; ok {
			sectionID = mapped
		}
		exists, err := itemExists(ctx, tx, sectionID, "bookmark", bookmark.Name, bookmark.URL)
		if err != nil {
			return Result{}, err
		}
		if exists {
			result.Skipped++
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO items(
				board_id, section_id, kind, name, url, icon,
				is_public, source, sort_order
			) VALUES (?, ?, 'bookmark', ?, ?, ?, ?, 'manual', ?)`,
			boardID, sectionID, bookmark.Name, bookmark.URL, bookmark.Icon,
			boolInt(bookmark.Public), orderValue(bookmark.Order, index),
		); err != nil {
			return Result{}, fmt.Errorf("import bookmark %q: %w", bookmark.Name, err)
		}
		result.Bookmarks++
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}

func readApps(ctx context.Context, db *sql.DB) ([]flameApp, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, url, COALESCE(icon, ''), COALESCE(isPinned, 0),
		       COALESCE(orderId, 0), COALESCE(isPublic, 1), COALESCE(description, '')
		FROM apps`)
	if err != nil {
		return nil, fmt.Errorf("read Flame apps: %w", err)
	}
	defer rows.Close()
	var apps []flameApp
	for rows.Next() {
		var app flameApp
		var pinned, public int
		if err := rows.Scan(&app.Name, &app.URL, &app.Icon, &pinned, &app.Order, &public, &app.Description); err != nil {
			return nil, fmt.Errorf("scan Flame app: %w", err)
		}
		app.Pinned = pinned == 1
		app.Public = public == 1
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

func readCategories(ctx context.Context, db *sql.DB) ([]flameCategory, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, COALESCE(isPinned, 0), COALESCE(orderId, 0), COALESCE(isPublic, 1)
		FROM categories`)
	if err != nil {
		return nil, fmt.Errorf("read Flame categories: %w", err)
	}
	defer rows.Close()
	var categories []flameCategory
	for rows.Next() {
		var category flameCategory
		var pinned, public int
		if err := rows.Scan(&category.ID, &category.Name, &pinned, &category.Order, &public); err != nil {
			return nil, fmt.Errorf("scan Flame category: %w", err)
		}
		category.Pinned = pinned == 1
		category.Public = public == 1
		categories = append(categories, category)
	}
	return categories, rows.Err()
}

func readBookmarks(ctx context.Context, db *sql.DB) ([]flameBookmark, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, url, categoryId, COALESCE(icon, ''), COALESCE(isPublic, 1), COALESCE(orderId, 0)
		FROM bookmarks`)
	if err != nil {
		return nil, fmt.Errorf("read Flame bookmarks: %w", err)
	}
	defer rows.Close()
	var bookmarks []flameBookmark
	for rows.Next() {
		var bookmark flameBookmark
		var public int
		if err := rows.Scan(&bookmark.Name, &bookmark.URL, &bookmark.CategoryID, &bookmark.Icon, &public, &bookmark.Order); err != nil {
			return nil, fmt.Errorf("scan Flame bookmark: %w", err)
		}
		bookmark.Public = public == 1
		bookmarks = append(bookmarks, bookmark)
	}
	return bookmarks, rows.Err()
}

func requireTable(ctx context.Context, db *sql.DB, name string) error {
	var found string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("Flame database is missing table %q", name)
	}
	if err != nil {
		return fmt.Errorf("inspect Flame database: %w", err)
	}
	return nil
}

func ensureHomeBoard(ctx context.Context, tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM boards WHERE slug = 'home'`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO boards(slug, name, is_public, sort_order) VALUES ('home', 'Home', 1, 0)`)
	if err != nil {
		return 0, fmt.Errorf("create home board: %w", err)
	}
	return result.LastInsertId()
}

func ensureSection(ctx context.Context, tx *sql.Tx, boardID int64, name string) (int64, error) {
	return ensureSectionWithVisibility(ctx, tx, boardID, name, true, false, 0)
}

func ensureSectionWithVisibility(ctx context.Context, tx *sql.Tx, boardID int64, name string, public, pinned bool, order int) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM sections WHERE board_id = ? AND name = ?`, boardID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO sections(board_id, name, is_public, is_pinned, sort_order)
		VALUES (?, ?, ?, ?, ?)`, boardID, name, boolInt(public), boolInt(pinned), order)
	if err != nil {
		return 0, fmt.Errorf("create section %q: %w", name, err)
	}
	return result.LastInsertId()
}

func itemExists(ctx context.Context, tx *sql.Tx, sectionID int64, kind, name, url string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM items
		WHERE section_id = ? AND kind = ? AND name = ? AND url = ?
		LIMIT 1`, sectionID, kind, name, url).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check existing item: %w", err)
	}
	return true, nil
}

func validURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func orderValue(order, fallback int) int {
	if order > 0 {
		return order
	}
	return fallback
}
