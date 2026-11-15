package runtime

import (
	"context"
	"database/sql"
)

type Store interface {
	Ping(ctx context.Context) error
}

type Memory struct{}

func (Memory) Ping(context.Context) error { return nil }

type SQLite struct {
	db *sql.DB
}

func NewSQLite(db *sql.DB) *SQLite { return &SQLite{db: db} }

func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
