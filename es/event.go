// Package es provides core event sourcing interfaces and types.
package es

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Event represents an immutable domain event before persistence.
// Events are value objects without identity until persisted.
// AggregateVersion and GlobalPosition are assigned by the store during Append.
type Event struct {
	CreatedAt     time.Time
	AggregateType string
	EventType     string
	AggregateID   string
	Payload       []byte
	Metadata      []byte
	EventVersion  int
	CausationID   sql.NullString
	CorrelationID sql.NullString
	TraceID       sql.NullString
	EventID       uuid.UUID
}

// PersistedEvent represents an event that has been stored.
// It includes the GlobalPosition and AggregateVersion assigned by the event store.
type PersistedEvent struct {
	CreatedAt        time.Time
	AggregateType    string
	EventType        string
	AggregateID      string
	Payload          []byte
	Metadata         []byte
	GlobalPosition   int64
	AggregateVersion int64
	EventVersion     int
	CausationID      sql.NullString
	CorrelationID    sql.NullString
	TraceID          sql.NullString
	EventID          uuid.UUID
}
