// Package postgres provides a PostgreSQL adapter for event sourcing.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
}

// DefaultStoreConfig returns the default configuration.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		EventsTable:      "events",
		CheckpointsTable: "projection_checkpoints",
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
func (s *Store) Append(ctx context.Context, tx es.DBTX, events []es.Event) ([]int64, error) {
	if len(events) == 0 {
		return nil, store.ErrNoEvents
	}

	// Validate all events belong to same aggregate
	firstEvent := events[0]
	for i, e := range events {
		if e.AggregateType != firstEvent.AggregateType {
			return nil, fmt.Errorf("event %d: aggregate type mismatch", i)
		}
		if e.AggregateID != firstEvent.AggregateID {
			return nil, fmt.Errorf("event %d: aggregate ID mismatch", i)
		}
		expectedVersion := firstEvent.AggregateVersion + int64(i)
		if e.AggregateVersion != expectedVersion {
			return nil, fmt.Errorf("event %d: expected version %d, got %d", i, expectedVersion, e.AggregateVersion)
		}
	}

	// Check current version for optimistic concurrency
	var currentVersion sql.NullInt64
	query := fmt.Sprintf(`
		SELECT MAX(aggregate_version) 
		FROM %s 
		WHERE aggregate_type = $1 AND aggregate_id = $2
	`, s.config.EventsTable)

	err := tx.QueryRowContext(ctx, query, firstEvent.AggregateType, firstEvent.AggregateID).Scan(&currentVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to check current version: %w", err)
	}

	// Verify optimistic concurrency
	expectedCurrentVersion := firstEvent.AggregateVersion - 1
	if currentVersion.Valid && currentVersion.Int64 != expectedCurrentVersion {
		return nil, store.ErrOptimisticConcurrency
	}
	if !currentVersion.Valid && expectedCurrentVersion != 0 {
		return nil, store.ErrOptimisticConcurrency
	}

	// Insert events and collect global positions
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

	for i, event := range events {
		var globalPos int64
		err := tx.QueryRowContext(ctx, insertQuery,
			event.AggregateType,
			event.AggregateID,
			event.AggregateVersion,
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
			return nil, fmt.Errorf("failed to insert event %d: %w", i, err)
		}
		globalPositions[i] = globalPos
	}

	return globalPositions, nil
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
