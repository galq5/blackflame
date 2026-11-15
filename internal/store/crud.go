package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrLastBoard = errors.New("cannot delete the last board")
	ErrNotFound  = errors.New("not found")
)

type ItemInput struct {
	BoardID      int64
	SectionID    int64
	Kind         string
	Name         string
	URL          string
	Icon         string
	Description  string
	Public       bool
	Pinned       bool
	Hidden       bool
	CheckEnabled bool
	Config       map[string]any
}

func CreateBoard(ctx context.Context, db *sql.DB, slug, name string, public bool) (int64, error) {
	var order int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM boards`).Scan(&order)
	result, err := db.ExecContext(ctx, `
		INSERT INTO boards(slug, name, is_public, sort_order)
		VALUES (?, ?, ?, ?)`, slug, name, boolInt(public), order)
	if err != nil {
		return 0, fmt.Errorf("create board: %w", err)
	}
	return result.LastInsertId()
}

func UpdateBoard(ctx context.Context, db *sql.DB, id int64, slug, name string, public bool) error {
	result, err := db.ExecContext(ctx, `
		UPDATE boards
		SET slug = ?, name = ?, is_public = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, slug, name, boolInt(public), id)
	if err != nil {
		return fmt.Errorf("update board: %w", err)
	}
	return requireAffected(result)
}

func DeleteBoard(ctx context.Context, db *sql.DB, id int64) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM boards`).Scan(&count); err != nil {
		return fmt.Errorf("count boards: %w", err)
	}
	if count <= 1 {
		return ErrLastBoard
	}
	result, err := db.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete board: %w", err)
	}
	return requireAffected(result)
}

func CreateSection(ctx context.Context, db *sql.DB, boardID int64, name string, public, pinned bool) (int64, error) {
	var order int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM sections WHERE board_id = ?`, boardID).Scan(&order)
	result, err := db.ExecContext(ctx, `
		INSERT INTO sections(board_id, name, is_public, is_pinned, sort_order)
		VALUES (?, ?, ?, ?, ?)`, boardID, name, boolInt(public), boolInt(pinned), order)
	if err != nil {
		return 0, fmt.Errorf("create section: %w", err)
	}
	return result.LastInsertId()
}

func UpdateSection(ctx context.Context, db *sql.DB, id int64, name string, public, pinned bool) error {
	result, err := db.ExecContext(ctx, `
		UPDATE sections SET name = ?, is_public = ?, is_pinned = ?
		WHERE id = ?`, name, boolInt(public), boolInt(pinned), id)
	if err != nil {
		return fmt.Errorf("update section: %w", err)
	}
	return requireAffected(result)
}

func DeleteSection(ctx context.Context, db *sql.DB, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM sections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete section: %w", err)
	}
	return requireAffected(result)
}

func CreateItem(ctx context.Context, db *sql.DB, input ItemInput) (int64, error) {
	var order int
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM items WHERE section_id = ?`, input.SectionID).Scan(&order)
	configJSON, err := marshalConfig(input.Config)
	if err != nil {
		return 0, err
	}
	result, err := db.ExecContext(ctx, `
		INSERT INTO items(
			board_id, section_id, kind, name, url, icon, description,
			is_public, is_pinned, is_hidden, check_enabled, source,
			config_json, sort_order
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'manual', ?, ?)`,
		input.BoardID, input.SectionID, input.Kind, input.Name, input.URL, input.Icon,
		input.Description, boolInt(input.Public), boolInt(input.Pinned), boolInt(input.Hidden),
		boolInt(input.CheckEnabled), configJSON, order,
	)
	if err != nil {
		return 0, fmt.Errorf("create item: %w", err)
	}
	return result.LastInsertId()
}

func UpdateItem(ctx context.Context, db *sql.DB, id int64, input ItemInput) error {
	configJSON, err := marshalConfig(input.Config)
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, `
		UPDATE items SET
			board_id = ?, section_id = ?, kind = ?, name = ?, url = ?, icon = ?,
			description = ?, is_public = ?, is_pinned = ?, is_hidden = ?,
			check_enabled = ?, config_json = ?,
			is_override = CASE WHEN source = 'manual' THEN 0 ELSE 1 END,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`,
		input.BoardID, input.SectionID, input.Kind, input.Name, input.URL, input.Icon,
		input.Description, boolInt(input.Public), boolInt(input.Pinned), boolInt(input.Hidden),
		boolInt(input.CheckEnabled), configJSON, id,
	)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return requireAffected(result)
}

func DeleteItem(ctx context.Context, db *sql.DB, id int64) error {
	result, err := db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return requireAffected(result)
}

func ReorderItems(ctx context.Context, db *sql.DB, sectionID int64, ids []int64) error {
	return reorderSiblings(ctx, db, sectionID, ids,
		`UPDATE items SET sort_order = ? WHERE id = ? AND section_id = ?`,
		`SELECT COUNT(*) FROM items WHERE section_id = ?`)
}

func ReorderSections(ctx context.Context, db *sql.DB, boardID int64, ids []int64) error {
	return reorderSiblings(ctx, db, boardID, ids,
		`UPDATE sections SET sort_order = ? WHERE id = ? AND board_id = ?`,
		`SELECT COUNT(*) FROM sections WHERE board_id = ?`)
}

func reorderSiblings(ctx context.Context, db *sql.DB, parentID int64, ids []int64, updateSQL, countSQL string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder: %w", err)
	}
	defer tx.Rollback()

	seen := make(map[int64]struct{}, len(ids))
	for index, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate reorder ID %d", id)
		}
		seen[id] = struct{}{}
		result, err := tx.ExecContext(ctx, updateSQL, index, id, parentID)
		if err != nil {
			return fmt.Errorf("reorder record %d: %w", id, err)
		}
		if err := requireAffected(result); err != nil {
			return fmt.Errorf("reorder record %d in parent %d: %w", id, parentID, err)
		}
	}

	var count int
	if err := tx.QueryRowContext(ctx, countSQL, parentID).Scan(&count); err != nil {
		return fmt.Errorf("count reorder siblings: %w", err)
	}
	if count != len(ids) {
		return fmt.Errorf("reorder requires all %d siblings, got %d", count, len(ids))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder: %w", err)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func marshalConfig(value map[string]any) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal item config: %w", err)
	}
	return string(raw), nil
}
