package es

import (
	"context"
	"database/sql"
)

// DBTX is a minimal interface for database operations.
// It is implemented by both *sql.DB and *sql.Tx, allowing
// the library to be transaction-agnostic.
//
// This design gives callers full control over transaction boundaries
// while keeping the library focused on event sourcing concerns.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Ensure standard library types implement DBTX
var (
	_ DBTX = (*sql.DB)(nil)
	_ DBTX = (*sql.Tx)(nil)
)
