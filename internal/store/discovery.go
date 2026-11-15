package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/galq/blackflame/internal/config"
)

// UpsertDiscovered writes normalized discovery results into the runtime items
// table. Items edited by a user are marked as overrides and are not overwritten.
func UpsertDiscovered(ctx context.Context, db *sql.DB, source string, items []config.DiscoveredItem) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin discovery upsert: %w", err)
	}
	defer tx.Rollback()

	boardID, err := ensureHomeBoard(ctx, tx)
	if err != nil {
		return err
	}

	seen := make([]string, 0, len(items))
	for _, item := range items {
		if item.ExternalID == "" || item.URL == "" {
			continue
		}
		group := strings.TrimSpace(item.Group)
		if group == "" {
			group = "Discovered"
		}
		sectionID, err := ensureSection(ctx, tx, boardID, group)
		if err != nil {
			return err
		}

		var existingID int64
		var overridden int
		err = tx.QueryRowContext(ctx, `
			SELECT id, is_override FROM items
			WHERE source = ? AND external_id = ?`, source, item.ExternalID).Scan(&existingID, &overridden)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			var order int
			_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM items WHERE section_id = ?`, sectionID).Scan(&order)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO items(
					board_id, section_id, kind, name, url, icon, description,
					is_public, is_pinned, is_hidden, check_enabled, source,
					external_id, group_name, is_override, sort_order
				) VALUES (?, ?, 'app', ?, ?, ?, '', 1, 0, ?, 1, ?, ?, ?, 0, ?)`,
				boardID, sectionID, item.Name, item.URL, item.Icon, boolInt(item.Hidden),
				source, item.ExternalID, group, order,
			); err != nil {
				return fmt.Errorf("insert discovered item %s: %w", item.ExternalID, err)
			}
		case err != nil:
			return fmt.Errorf("lookup discovered item %s: %w", item.ExternalID, err)
		case overridden == 1:
			// Preserve the user's edits; only refresh the group label.
			if _, err := tx.ExecContext(ctx, `UPDATE items SET group_name = ? WHERE id = ?`, group, existingID); err != nil {
				return fmt.Errorf("refresh discovered item group: %w", err)
			}
		default:
			if _, err := tx.ExecContext(ctx, `
				UPDATE items SET
					board_id = ?, section_id = ?, name = ?, url = ?, icon = ?,
					is_hidden = ?, group_name = ?,
					updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				WHERE id = ?`,
				boardID, sectionID, item.Name, item.URL, item.Icon, boolInt(item.Hidden), group, existingID,
			); err != nil {
				return fmt.Errorf("update discovered item %s: %w", item.ExternalID, err)
			}
		}
		seen = append(seen, item.ExternalID)
	}

	if err := deleteStaleDiscovered(ctx, tx, source, seen); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit discovery upsert: %w", err)
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
		return 0, fmt.Errorf("find home board: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO boards(slug, name, is_public, sort_order) VALUES ('home', 'Home', 1, 0)`)
	if err != nil {
		return 0, fmt.Errorf("create home board: %w", err)
	}
	return result.LastInsertId()
}

func ensureSection(ctx context.Context, tx *sql.Tx, boardID int64, name string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM sections WHERE board_id = ? AND name = ?`, boardID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find section %s: %w", name, err)
	}
	var order int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM sections WHERE board_id = ?`, boardID).Scan(&order)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO sections(board_id, name, is_public, sort_order)
		VALUES (?, ?, 1, ?)`, boardID, name, order)
	if err != nil {
		return 0, fmt.Errorf("create section %s: %w", name, err)
	}
	return result.LastInsertId()
}

func deleteStaleDiscovered(ctx context.Context, tx *sql.Tx, source string, seen []string) error {
	if len(seen) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM items WHERE source = ? AND is_override = 0`, source)
		return err
	}
	placeholders := make([]string, len(seen))
	args := make([]any, 0, len(seen)+1)
	args = append(args, source)
	for i, id := range seen {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(`DELETE FROM items WHERE source = ? AND is_override = 0 AND external_id NOT IN (%s)`, strings.Join(placeholders, ","))
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// LoadDiscoveredItems returns runtime discovery items for GitOps mode merging.
func LoadDiscoveredItems(ctx context.Context, db *sql.DB) ([]config.Item, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, kind, name, url, icon, description, is_public, is_pinned,
		       is_hidden, check_enabled, source, external_id, group_name,
		       is_override, config_json, sort_order
		FROM items
		WHERE source <> 'manual'
		ORDER BY group_name, sort_order, id`)
	if err != nil {
		return nil, fmt.Errorf("query discovered items: %w", err)
	}
	defer rows.Close()

	var items []config.Item
	for rows.Next() {
		var (
			id           int64
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
		)
		if err := rows.Scan(
			&id, &kind, &name, &url, &icon, &description, &isPublic, &isPinned,
			&isHidden, &checkEnabled, &source, &externalID, &group,
			&isOverride, &configJSON, &sortOrder,
		); err != nil {
			return nil, fmt.Errorf("scan discovered item: %w", err)
		}
		item := config.Item{
			ID:           id,
			Kind:         kind,
			Name:         name,
			URL:          url,
			Icon:         icon,
			Description:  description,
			Public:       config.Bool(isPublic == 1),
			Pinned:       isPinned == 1,
			Hidden:       isHidden == 1,
			CheckEnabled: config.Bool(checkEnabled == 1),
			Source:       source,
			ExternalID:   externalID.String,
			Group:        group,
			Override:     isOverride == 1,
			SortOrder:    sortOrder,
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate discovered items: %w", err)
	}
	return items, nil
}

// MergeDiscovered returns a copy of base with runtime discovery items appended
// under a synthetic "discovered" board. Used by GitOps mode.
func MergeDiscovered(base *config.Config, items []config.Item) *config.Config {
	if base == nil || len(items) == 0 {
		return base
	}
	out := *base
	out.Boards = append([]config.Board(nil), base.Boards...)

	boardIndex := -1
	for i := range out.Boards {
		if out.Boards[i].Slug == "discovered" {
			boardIndex = i
			break
		}
	}
	if boardIndex == -1 {
		out.Boards = append(out.Boards, config.Board{
			Slug:   "discovered",
			Name:   "Discovered",
			Public: config.Bool(true),
		})
		boardIndex = len(out.Boards) - 1
	}
	board := out.Boards[boardIndex]
	board.Sections = make([]config.Section, len(out.Boards[boardIndex].Sections))
	for i, section := range out.Boards[boardIndex].Sections {
		board.Sections[i] = section
		board.Sections[i].Items = append([]config.Item(nil), section.Items...)
	}
	out.Boards[boardIndex] = board

	for _, item := range items {
		group := strings.TrimSpace(item.Group)
		if group == "" {
			group = "Discovered"
		}
		sectionIndex := -1
		for i := range board.Sections {
			if board.Sections[i].Name == group {
				sectionIndex = i
				break
			}
		}
		if sectionIndex == -1 {
			board.Sections = append(board.Sections, config.Section{
				Name:   group,
				Public: config.Bool(true),
			})
			sectionIndex = len(board.Sections) - 1
		}
		board.Sections[sectionIndex].Items = append(board.Sections[sectionIndex].Items, item)
	}
	out.Boards[boardIndex] = board
	return &out
}
