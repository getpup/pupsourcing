// Package sqlite provides a SQLite adapter for event sourcing.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/store"
)

const (
	// sqliteDateTimeFormat is the format used for timestamp storage/parsing in SQLite
	sqliteDateTimeFormat = "2006-01-02 15:04:05.999999"
)

// StoreConfig contains configuration for the SQLite event store.
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

// Store is a SQLite-backed event store implementation.
type Store struct {
	config StoreConfig
}

// NewStore creates a new SQLite event store with the given configuration.
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

	err := tx.QueryRowContext(ctx, query, firstEvent.AggregateType, firstEvent.AggregateID.String()).Scan(&currentVersion)
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

		// Convert UUIDs to strings for SQLite
		var traceID, correlationID, causationID interface{}
		if event.TraceID.Valid {
			traceID = event.TraceID.UUID.String()
		}
		if event.CorrelationID.Valid {
			correlationID = event.CorrelationID.UUID.String()
		}
		if event.CausationID.Valid {
			causationID = event.CausationID.UUID.String()
		}

		result, err := tx.ExecContext(ctx, insertQuery,
			event.AggregateType,
			event.AggregateID.String(),
			aggregateVersion,
			event.EventID.String(),
			event.EventType,
			event.EventVersion,
			event.Payload,
			traceID,
			correlationID,
			causationID,
			event.Metadata,
			event.CreatedAt.Format(sqliteDateTimeFormat),
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

	// Update aggregate_heads with the new version (UPSERT pattern for SQLite)
	latestVersion := nextVersion + int64(len(events)) - 1
	upsertQuery := fmt.Sprintf(`
		INSERT INTO %s (aggregate_type, aggregate_id, aggregate_version, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT (aggregate_type, aggregate_id)
		DO UPDATE SET aggregate_version = ?, updated_at = datetime('now')
	`, s.config.AggregateHeadsTable)

	_, err = tx.ExecContext(ctx, upsertQuery, firstEvent.AggregateType, firstEvent.AggregateID.String(), latestVersion, latestVersion)
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

// IsUniqueViolation checks if an error is a SQLite unique constraint violation.
// This is exported for testing purposes.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	// SQLite error messages for unique constraint violations
	errMsg := err.Error()
	return strings.Contains(errMsg, "UNIQUE constraint failed") ||
		strings.Contains(errMsg, "unique constraint") ||
		strings.Contains(errMsg, "constraint failed")
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
		var aggregateID, eventID string
		var traceID, correlationID, causationID sql.NullString
		var createdAt string

		err := rows.Scan(
			&e.GlobalPosition,
			&e.AggregateType,
			&aggregateID,
			&e.AggregateVersion,
			&eventID,
			&e.EventType,
			&e.EventVersion,
			&e.Payload,
			&traceID,
			&correlationID,
			&causationID,
			&e.Metadata,
			&createdAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Parse UUIDs
		e.AggregateID, err = uuid.Parse(aggregateID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse aggregate ID: %w", err)
		}
		e.EventID, err = uuid.Parse(eventID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse event ID: %w", err)
		}

		if traceID.Valid {
			parsedUUID, err := uuid.Parse(traceID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse trace ID: %w", err)
			}
			e.TraceID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}
		if correlationID.Valid {
			parsedUUID, err := uuid.Parse(correlationID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse correlation ID: %w", err)
			}
			e.CorrelationID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}
		if causationID.Valid {
			parsedUUID, err := uuid.Parse(causationID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse causation ID: %w", err)
			}
			e.CausationID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}

		// Parse timestamp
		e.CreatedAt, err = parseTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
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
	args = append(args, aggregateType, aggregateID.String())

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
		var aggID, eventID string
		var traceID, correlationID, causationID sql.NullString
		var createdAt string

		err := rows.Scan(
			&e.GlobalPosition,
			&e.AggregateType,
			&aggID,
			&e.AggregateVersion,
			&eventID,
			&e.EventType,
			&e.EventVersion,
			&e.Payload,
			&traceID,
			&correlationID,
			&causationID,
			&e.Metadata,
			&createdAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Parse UUIDs
		e.AggregateID, err = uuid.Parse(aggID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse aggregate ID: %w", err)
		}
		e.EventID, err = uuid.Parse(eventID)
		if err != nil {
			return nil, fmt.Errorf("failed to parse event ID: %w", err)
		}

		if traceID.Valid {
			parsedUUID, err := uuid.Parse(traceID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse trace ID: %w", err)
			}
			e.TraceID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}
		if correlationID.Valid {
			parsedUUID, err := uuid.Parse(correlationID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse correlation ID: %w", err)
			}
			e.CorrelationID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}
		if causationID.Valid {
			parsedUUID, err := uuid.Parse(causationID.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse causation ID: %w", err)
			}
			e.CausationID = uuid.NullUUID{UUID: parsedUUID, Valid: true}
		}

		// Parse timestamp
		e.CreatedAt, err = parseTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at: %w", err)
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

// sqliteDateTimeFormats lists common SQLite datetime formats for parsing
var sqliteDateTimeFormats = []string{
	sqliteDateTimeFormat,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999Z",
	"2006-01-02T15:04:05Z",
	time.RFC3339,
	time.RFC3339Nano,
}

// parseTimestamp parses SQLite datetime strings to time.Time
func parseTimestamp(s string) (time.Time, error) {
	for _, format := range sqliteDateTimeFormats {
		t, err := time.Parse(format, s)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", s)
}
