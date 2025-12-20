// Package mysql provides a MySQL/MariaDB adapter for event sourcing.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/store"
)

// StoreConfig contains configuration for the MySQL event store.
// Configuration is immutable after construction.
type StoreConfig struct {
	// Logger is an optional logger for observability.
	// If nil, logging is disabled (zero overhead).
	Logger es.Logger

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
		Logger:              nil, // No logging by default
	}
}

// Store is a MySQL-backed event store implementation.
type Store struct {
	config StoreConfig
}

// NewStore creates a new MySQL event store with the given configuration.
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
//
//nolint:gocyclo // Cyclomatic complexity of 16 is acceptable here - comes from necessary logging and validation checks
func (s *Store) Append(ctx context.Context, tx es.DBTX, events []es.Event) ([]int64, error) {
	if len(events) == 0 {
		return nil, store.ErrNoEvents
	}

	if s.config.Logger != nil {
		s.config.Logger.Debug(ctx, "append starting", "event_count", len(events))
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
		WHERE aggregate_type = ? AND aggregate_id = ?
	`, s.config.AggregateHeadsTable)

	// Convert UUID to binary for MySQL
	aggregateIDBytes, err := firstEvent.AggregateID.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal aggregate ID: %w", err)
	}

	err = tx.QueryRowContext(ctx, query, firstEvent.AggregateType, aggregateIDBytes).Scan(&currentVersion)
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

	if s.config.Logger != nil {
		if currentVersion.Valid {
			s.config.Logger.Debug(ctx, "version calculated",
				"aggregate_type", firstEvent.AggregateType,
				"aggregate_id", firstEvent.AggregateID,
				"current_version", currentVersion.Int64,
				"next_version", nextVersion)
		} else {
			s.config.Logger.Debug(ctx, "version calculated",
				"aggregate_type", firstEvent.AggregateType,
				"aggregate_id", firstEvent.AggregateID,
				"current_version", "none",
				"next_version", nextVersion)
		}
	}

	// Insert events with auto-assigned versions and collect global positions
	globalPositions := make([]int64, len(events))
	insertQuery := fmt.Sprintf(`
		INSERT INTO %s (
			aggregate_type, aggregate_id, aggregate_version,
			event_id, event_type, event_version,
			payload, trace_id, correlation_id, causation_id,
			metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.config.EventsTable)

	for i := range events {
		event := &events[i]
		aggregateVersion := nextVersion + int64(i)

		// Convert UUIDs to binary for MySQL
		eventIDBytes, err := event.EventID.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal event ID: %w", err)
		}

		var traceID, correlationID, causationID interface{}
		if event.TraceID.Valid {
			traceIDBytes, err := event.TraceID.UUID.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal trace ID: %w", err)
			}
			traceID = traceIDBytes
		}
		if event.CorrelationID.Valid {
			correlationIDBytes, err := event.CorrelationID.UUID.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal correlation ID: %w", err)
			}
			correlationID = correlationIDBytes
		}
		if event.CausationID.Valid {
			causationIDBytes, err := event.CausationID.UUID.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal causation ID: %w", err)
			}
			causationID = causationIDBytes
		}

		result, err := tx.ExecContext(ctx, insertQuery,
			event.AggregateType,
			aggregateIDBytes,
			aggregateVersion,
			eventIDBytes,
			event.EventType,
			event.EventVersion,
			event.Payload,
			traceID,
			correlationID,
			causationID,
			event.Metadata,
			event.CreatedAt,
		)

		if err != nil {
			// Check if this is a unique constraint violation (optimistic concurrency failure)
			if IsUniqueViolation(err) {
				if s.config.Logger != nil {
					s.config.Logger.Error(ctx, "optimistic concurrency conflict",
						"aggregate_type", event.AggregateType,
						"aggregate_id", event.AggregateID,
						"aggregate_version", aggregateVersion)
				}
				return nil, store.ErrOptimisticConcurrency
			}
			return nil, fmt.Errorf("failed to insert event %d: %w", i, err)
		}

		globalPos, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("failed to get last insert id: %w", err)
		}
		globalPositions[i] = globalPos
	}

	// Update aggregate_heads with the new version (UPSERT pattern for MySQL)
	latestVersion := nextVersion + int64(len(events)) - 1
	upsertQuery := fmt.Sprintf(`
		INSERT INTO %s (aggregate_type, aggregate_id, aggregate_version, updated_at)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE aggregate_version = ?, updated_at = NOW()
	`, s.config.AggregateHeadsTable)

	_, err = tx.ExecContext(ctx, upsertQuery, firstEvent.AggregateType, aggregateIDBytes, latestVersion, latestVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to update aggregate head: %w", err)
	}

	if s.config.Logger != nil {
		s.config.Logger.Info(ctx, "events appended",
			"aggregate_type", firstEvent.AggregateType,
			"aggregate_id", firstEvent.AggregateID,
			"event_count", len(events),
			"version_range", fmt.Sprintf("%d-%d", nextVersion, latestVersion),
			"positions", globalPositions)
	}

	return globalPositions, nil
}

// IsUniqueViolation checks if an error is a MySQL unique constraint violation.
// This is exported for testing purposes.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a MySQL error with duplicate entry code (1062)
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062 // ER_DUP_ENTRY
	}

	// Fallback: check error message for common patterns
	errMsg := err.Error()
	return strings.Contains(errMsg, "Duplicate entry") ||
		strings.Contains(errMsg, "duplicate key") ||
		strings.Contains(errMsg, "unique constraint")
}

// ReadEvents implements store.EventReader.
func (s *Store) ReadEvents(ctx context.Context, tx es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error) {
	if s.config.Logger != nil {
		s.config.Logger.Debug(ctx, "reading events", "from_position", fromPosition, "limit", limit)
	}

	query := fmt.Sprintf(`
		SELECT 
			global_position, aggregate_type, aggregate_id, aggregate_version,
			event_id, event_type, event_version,
			payload, trace_id, correlation_id, causation_id,
			metadata, created_at
		FROM %s
		WHERE global_position > ?
		ORDER BY global_position ASC
		LIMIT ?
	`, s.config.EventsTable)

	rows, err := tx.QueryContext(ctx, query, fromPosition, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []es.PersistedEvent
	for rows.Next() {
		var e es.PersistedEvent
		var aggregateIDBytes, eventIDBytes []byte
		var traceIDBytes, correlationIDBytes, causationIDBytes []byte

		err := rows.Scan(
			&e.GlobalPosition,
			&e.AggregateType,
			&aggregateIDBytes,
			&e.AggregateVersion,
			&eventIDBytes,
			&e.EventType,
			&e.EventVersion,
			&e.Payload,
			&traceIDBytes,
			&correlationIDBytes,
			&causationIDBytes,
			&e.Metadata,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Parse UUIDs from binary
		if err := e.AggregateID.UnmarshalBinary(aggregateIDBytes); err != nil {
			return nil, fmt.Errorf("failed to parse aggregate ID: %w", err)
		}
		if err := e.EventID.UnmarshalBinary(eventIDBytes); err != nil {
			return nil, fmt.Errorf("failed to parse event ID: %w", err)
		}

		if len(traceIDBytes) > 0 {
			var traceUUID uuid.UUID
			if err := traceUUID.UnmarshalBinary(traceIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse trace ID: %w", err)
			}
			e.TraceID = uuid.NullUUID{UUID: traceUUID, Valid: true}
		}
		if len(correlationIDBytes) > 0 {
			var corrUUID uuid.UUID
			if err := corrUUID.UnmarshalBinary(correlationIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse correlation ID: %w", err)
			}
			e.CorrelationID = uuid.NullUUID{UUID: corrUUID, Valid: true}
		}
		if len(causationIDBytes) > 0 {
			var causeUUID uuid.UUID
			if err := causeUUID.UnmarshalBinary(causationIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse causation ID: %w", err)
			}
			e.CausationID = uuid.NullUUID{UUID: causeUUID, Valid: true}
		}

		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if s.config.Logger != nil {
		s.config.Logger.Debug(ctx, "events read", "count", len(events))
	}

	return events, nil
}

// ReadAggregateStream implements store.AggregateStreamReader.
func (s *Store) ReadAggregateStream(ctx context.Context, tx es.DBTX, aggregateType string, aggregateID uuid.UUID, fromVersion, toVersion *int64) ([]es.PersistedEvent, error) {
	if s.config.Logger != nil {
		s.config.Logger.Debug(ctx, "reading aggregate stream",
			"aggregate_type", aggregateType,
			"aggregate_id", aggregateID,
			"from_version", fromVersion,
			"to_version", toVersion)
	}

	// Convert UUID to binary for MySQL
	aggregateIDBytes, err := aggregateID.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal aggregate ID: %w", err)
	}

	// Build the query dynamically based on optional version parameters
	baseQuery := fmt.Sprintf(`
		SELECT 
			global_position, aggregate_type, aggregate_id, aggregate_version,
			event_id, event_type, event_version,
			payload, trace_id, correlation_id, causation_id,
			metadata, created_at
		FROM %s
		WHERE aggregate_type = ? AND aggregate_id = ?
	`, s.config.EventsTable)

	var args []interface{}
	args = append(args, aggregateType, aggregateIDBytes)

	// Add version range filters if specified
	if fromVersion != nil {
		baseQuery += " AND aggregate_version >= ?"
		args = append(args, *fromVersion)
	}

	if toVersion != nil {
		baseQuery += " AND aggregate_version <= ?"
		args = append(args, *toVersion)
	}

	// Always order by aggregate_version ASC
	baseQuery += " ORDER BY aggregate_version ASC"

	rows, err := tx.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregate stream: %w", err)
	}
	defer rows.Close()

	var events []es.PersistedEvent
	for rows.Next() {
		var e es.PersistedEvent
		var aggIDBytes, eventIDBytes []byte
		var traceIDBytes, correlationIDBytes, causationIDBytes []byte

		err := rows.Scan(
			&e.GlobalPosition,
			&e.AggregateType,
			&aggIDBytes,
			&e.AggregateVersion,
			&eventIDBytes,
			&e.EventType,
			&e.EventVersion,
			&e.Payload,
			&traceIDBytes,
			&correlationIDBytes,
			&causationIDBytes,
			&e.Metadata,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Parse UUIDs from binary
		if err := e.AggregateID.UnmarshalBinary(aggIDBytes); err != nil {
			return nil, fmt.Errorf("failed to parse aggregate ID: %w", err)
		}
		if err := e.EventID.UnmarshalBinary(eventIDBytes); err != nil {
			return nil, fmt.Errorf("failed to parse event ID: %w", err)
		}

		if len(traceIDBytes) > 0 {
			var traceUUID uuid.UUID
			if err := traceUUID.UnmarshalBinary(traceIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse trace ID: %w", err)
			}
			e.TraceID = uuid.NullUUID{UUID: traceUUID, Valid: true}
		}
		if len(correlationIDBytes) > 0 {
			var corrUUID uuid.UUID
			if err := corrUUID.UnmarshalBinary(correlationIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse correlation ID: %w", err)
			}
			e.CorrelationID = uuid.NullUUID{UUID: corrUUID, Valid: true}
		}
		if len(causationIDBytes) > 0 {
			var causeUUID uuid.UUID
			if err := causeUUID.UnmarshalBinary(causationIDBytes); err != nil {
				return nil, fmt.Errorf("failed to parse causation ID: %w", err)
			}
			e.CausationID = uuid.NullUUID{UUID: causeUUID, Valid: true}
		}

		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if s.config.Logger != nil {
		s.config.Logger.Debug(ctx, "aggregate stream read",
			"aggregate_type", aggregateType,
			"aggregate_id", aggregateID,
			"event_count", len(events))
	}

	return events, nil
}
