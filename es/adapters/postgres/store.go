// Package postgres provides a PostgreSQL adapter for event sourcing.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/store"
)

// StoreConfig contains configuration for the Postgres event store.
// Configuration is immutable after construction.
type StoreConfig struct {
	// EventsTable is the name of the events table
	EventsTable string

	// CheckpointsTable is the name of the projection checkpoints table
	CheckpointsTable string

	// AggregateHeadsTable is the name of the aggregate version tracking table
	AggregateHeadsTable string
}

// DefaultStoreConfig returns the default configuration.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		EventsTable:         "events",
		CheckpointsTable:    "projection_checkpoints",
		AggregateHeadsTable: "aggregate_heads",
	}
}

// Store is a PostgreSQL-backed event store implementation.
type Store struct {
	config StoreConfig
}

// NewStore creates a new Postgres event store with the given configuration.
func NewStore(config StoreConfig) *Store {
	return &Store{
		config: config,
	}
}

// Append implements store.EventStore.
// It automatically assigns aggregate versions using the aggregate_heads table for O(1) lookup.
// The database constraint on (aggregate_type, aggregate_id, aggregate_version) enforces
// optimistic concurrency - if another transaction commits between our version check and insert,
// the insert will fail with a unique constraint violation.
func (s *Store) Append(ctx context.Context, tx es.DBTX, events []es.Event) ([]int64, error) {
	if len(events) == 0 {
		return nil, store.ErrNoEvents
	}

	// Validate all events belong to same aggregate
	firstEvent := events[0]
	for i := range events {
		e := &events[i]
		if e.AggregateType != firstEvent.AggregateType {
			return nil, fmt.Errorf("event %d: aggregate type mismatch", i)
		}
		if e.AggregateID != firstEvent.AggregateID {
			return nil, fmt.Errorf("event %d: aggregate ID mismatch", i)
		}
	}

	// Fetch current version from aggregate_heads table
	var currentVersion sql.NullInt64
	query := fmt.Sprintf(`
		SELECT aggregate_version 
		FROM %s 
		WHERE aggregate_type = $1 AND aggregate_id = $2
	`, s.config.AggregateHeadsTable)

	err := tx.QueryRowContext(ctx, query, firstEvent.AggregateType, firstEvent.AggregateID).Scan(&currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to check current version: %w", err)
	}

	// Determine starting version for new events
	var nextVersion int64
	if currentVersion.Valid {
		nextVersion = currentVersion.Int64 + 1
	} else {
		nextVersion = 1 // First event for this aggregate
	}

	// Insert events with auto-assigned versions and collect global positions
	globalPositions := make([]int64, len(events))
	insertQuery := fmt.Sprintf(`
		INSERT INTO %s (
			aggregate_type, aggregate_id, aggregate_version,
			event_id, event_type, event_version,
			payload, trace_id, correlation_id, causation_id,
			metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING global_position
	`, s.config.EventsTable)

	for i := range events {
		event := &events[i]
		aggregateVersion := nextVersion + int64(i)

		var globalPos int64
		err := tx.QueryRowContext(ctx, insertQuery,
			event.AggregateType,
			event.AggregateID,
			aggregateVersion,
			event.EventID,
			event.EventType,
			event.EventVersion,
			event.Payload,
			event.TraceID,
			event.CorrelationID,
			event.CausationID,
			event.Metadata,
			event.CreatedAt,
		).Scan(&globalPos)

		if err != nil {
			// Check if this is a unique constraint violation (optimistic concurrency failure)
			if IsUniqueViolation(err) {
				return nil, store.ErrOptimisticConcurrency
			}
			return nil, fmt.Errorf("failed to insert event %d: %w", i, err)
		}
		globalPositions[i] = globalPos
	}

	// Update aggregate_heads with the new version (UPSERT pattern)
	latestVersion := nextVersion + int64(len(events)) - 1
	upsertQuery := fmt.Sprintf(`
		INSERT INTO %s (aggregate_type, aggregate_id, aggregate_version, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (aggregate_type, aggregate_id)
		DO UPDATE SET aggregate_version = $3, updated_at = NOW()
	`, s.config.AggregateHeadsTable)

	_, err = tx.ExecContext(ctx, upsertQuery, firstEvent.AggregateType, firstEvent.AggregateID, latestVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to update aggregate head: %w", err)
	}

	return globalPositions, nil
}

// IsUniqueViolation checks if an error is a PostgreSQL unique constraint violation.
// This is exported for testing purposes.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a pq.Error with unique_violation code (23505)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505" // unique_violation
	}

	// Fallback: check error message for common patterns
	errMsg := fmt.Sprintf("%v", err)
	return containsString(errMsg, "duplicate key") || containsString(errMsg, "unique constraint")
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ReadEvents implements store.EventReader.
func (s *Store) ReadEvents(ctx context.Context, tx es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error) {
	query := fmt.Sprintf(`
		SELECT 
			global_position, aggregate_type, aggregate_id, aggregate_version,
			event_id, event_type, event_version,
			payload, trace_id, correlation_id, causation_id,
			metadata, created_at
		FROM %s
		WHERE global_position > $1
		ORDER BY global_position ASC
		LIMIT $2
	`, s.config.EventsTable)

	rows, err := tx.QueryContext(ctx, query, fromPosition, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []es.PersistedEvent
	for rows.Next() {
		var e es.PersistedEvent
		err := rows.Scan(
			&e.GlobalPosition,
			&e.AggregateType,
			&e.AggregateID,
			&e.AggregateVersion,
			&e.EventID,
			&e.EventType,
			&e.EventVersion,
			&e.Payload,
			&e.TraceID,
			&e.CorrelationID,
			&e.CausationID,
			&e.Metadata,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return events, nil
}
