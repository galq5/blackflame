package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/galq/blackflame/internal/config"
)

// SQLiteStore is the authoritative content store for UI mode.
type SQLiteStore struct {
	db *sql.DB

	mu   sync.RWMutex
	snap config.Snapshot

	notify chan struct{}
}

func NewSQLiteStore(ctx context.Context, db *sql.DB) (*SQLiteStore, error) {
	store := &SQLiteStore{
		db:     db,
		notify: make(chan struct{}, 1),
	}
	if err := store.Reload(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Snapshot() config.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

func (s *SQLiteStore) Reload(ctx context.Context) error {
	cfg, err := LoadConfig(ctx, s.db)
	now := time.Now().UTC()
	s.mu.Lock()
	if err != nil {
		s.snap.Err = err
		s.mu.Unlock()
		s.signal()
		return err
	}
	s.snap = config.Snapshot{
		Config:   cfg,
		Source:   "sqlite",
		LoadedAt: now,
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *SQLiteStore) Watch() <-chan struct{} { return s.notify }

func (s *SQLiteStore) ReadOnly() bool { return false }

func (s *SQLiteStore) Close() error {
	// The notification channel stays open for the lifetime of the process.
	// Consumers stop via their request or shutdown context.
	return nil
}

func (s *SQLiteStore) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func LoadConfig(ctx context.Context, db *sql.DB) (*config.Config, error) {
	cfg := config.Default()
	if err := loadSettings(ctx, db, cfg); err != nil {
		return nil, err
	}

	type boardRow struct {
		id        int64
		slug      string
		name      string
		isPublic  int
		themeJSON sql.NullString
	}
	type sectionRow struct {
		id        int64
		boardID   int64
		name      string
		isPublic  int
		isPinned  int
		sortOrder int
	}
	type itemRow struct {
		id           int64
		sectionID    int64
		kind         string
		name         string
		url          string
		icon         string
		description  string
		isPublic     int
		isPinned     int
		isHidden     int
		checkEnabled int
		source       string
		externalID   sql.NullString
		group        string
		isOverride   int
		configJSON   sql.NullString
		sortOrder    int
	}

	boardRows, err := queryRows(ctx, db, `
		SELECT id, slug, name, is_public, theme_json
		FROM boards
		ORDER BY sort_order, id`, func(rows *sql.Rows) (boardRow, error) {
		var row boardRow
		err := rows.Scan(&row.id, &row.slug, &row.name, &row.isPublic, &row.themeJSON)
		return row, err
	})
	if err != nil {
		return nil, fmt.Errorf("query boards: %w", err)
	}

	sectionRows, err := queryRows(ctx, db, `
		SELECT id, board_id, name, is_public, is_pinned, sort_order
		FROM sections
		ORDER BY board_id, sort_order, id`, func(rows *sql.Rows) (sectionRow, error) {
		var row sectionRow
		err := rows.Scan(&row.id, &row.boardID, &row.name, &row.isPublic, &row.isPinned, &row.sortOrder)
		return row, err
	})
	if err != nil {
		return nil, fmt.Errorf("query sections: %w", err)
	}

	itemRows, err := queryRows(ctx, db, `
		SELECT section_id, id, kind, name, url, icon, description, is_public, is_pinned,
		       is_hidden, check_enabled, source, external_id, group_name,
		       is_override, config_json, sort_order
		FROM items
		WHERE section_id IS NOT NULL
		ORDER BY section_id, sort_order, id`, func(rows *sql.Rows) (itemRow, error) {
		var row itemRow
		err := rows.Scan(
			&row.sectionID, &row.id, &row.kind, &row.name, &row.url, &row.icon,
			&row.description, &row.isPublic, &row.isPinned, &row.isHidden,
			&row.checkEnabled, &row.source, &row.externalID, &row.group,
			&row.isOverride, &row.configJSON, &row.sortOrder,
		)
		return row, err
	})
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}

	sectionsByBoard := make(map[int64][]config.Section, len(boardRows))
	for _, row := range sectionRows {
		sectionsByBoard[row.boardID] = append(sectionsByBoard[row.boardID], config.Section{
			ID:        row.id,
			Name:      row.name,
			Public:    config.Bool(row.isPublic == 1),
			Pinned:    row.isPinned == 1,
			SortOrder: row.sortOrder,
		})
	}

	itemsBySection := make(map[int64][]config.Item, len(sectionRows))
	for _, row := range itemRows {
		item := config.Item{
			ID:           row.id,
			Kind:         row.kind,
			Name:         row.name,
			URL:          row.url,
			Icon:         row.icon,
			Description:  row.description,
			Public:       config.Bool(row.isPublic == 1),
			Pinned:       row.isPinned == 1,
			Hidden:       row.isHidden == 1,
			CheckEnabled: config.Bool(row.checkEnabled == 1),
			Source:       row.source,
			ExternalID:   row.externalID.String,
			Group:        row.group,
			Override:     row.isOverride == 1,
			SortOrder:    row.sortOrder,
		}
		if row.configJSON.Valid && row.configJSON.String != "" {
			if err := json.Unmarshal([]byte(row.configJSON.String), &item.Config); err != nil {
				return nil, fmt.Errorf("decode item config: %w", err)
			}
		}
		itemsBySection[row.sectionID] = append(itemsBySection[row.sectionID], item)
	}

	for boardID, sections := range sectionsByBoard {
		for i := range sections {
			sections[i].Items = itemsBySection[sections[i].ID]
		}
		sectionsByBoard[boardID] = sections
	}

	cfg.Boards = make([]config.Board, 0, len(boardRows))
	for _, row := range boardRows {
		board := config.Board{
			ID:     row.id,
			Slug:   row.slug,
			Name:   row.name,
			Public: config.Bool(row.isPublic == 1),
		}
		if row.themeJSON.Valid && row.themeJSON.String != "" {
			var theme config.Theme
			if err := json.Unmarshal([]byte(row.themeJSON.String), &theme); err != nil {
				return nil, fmt.Errorf("decode theme for board %s: %w", row.slug, err)
			}
			board.Theme = &theme
		}
		board.Sections = sectionsByBoard[row.id]
		cfg.Boards = append(cfg.Boards, board)
	}
	return cfg, nil
}

func queryRows[T any](ctx context.Context, db *sql.DB, query string, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []T
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func loadSettings(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	settings := []struct {
		key    string
		target any
	}{
		{"branding", &cfg.Branding},
		{"theme", &cfg.Theme},
		{"layout", &cfg.Layout},
		{"widgets", &cfg.Widgets},
		{"search", &cfg.Search},
		{"weather", &cfg.Weather},
		{"status", &cfg.Status},
		{"discovery", &cfg.Discovery},
		{"icons", &cfg.Icons},
	}
	for _, setting := range settings {
		var raw string
		err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, setting.key).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load setting %s: %w", setting.key, err)
		}
		if err := json.Unmarshal([]byte(raw), setting.target); err != nil {
			return fmt.Errorf("decode setting %s: %w", setting.key, err)
		}
	}
	return nil
}

func SaveSettings(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	settings := []struct {
		key   string
		value any
	}{
		{"branding", cfg.Branding},
		{"theme", cfg.Theme},
		{"layout", cfg.Layout},
		{"widgets", cfg.Widgets},
		{"search", cfg.Search},
		{"weather", cfg.Weather},
		{"status", cfg.Status},
		{"discovery", cfg.Discovery},
		{"icons", cfg.Icons},
	}
	for _, setting := range settings {
		raw, err := json.Marshal(setting.value)
		if err != nil {
			return fmt.Errorf("marshal setting %s: %w", setting.key, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO settings(key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, setting.key, string(raw)); err != nil {
			return fmt.Errorf("save setting %s: %w", setting.key, err)
		}
	}
	return nil
}
