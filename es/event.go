// Package es provides core event sourcing interfaces and types.
package es

import (
	"time"

	"github.com/google/uuid"
)

// Event represents an immutable domain event.
// Events are value objects without identity until persisted.
type Event struct {
	// CreatedAt is when the event was created
	CreatedAt time.Time

	// AggregateType identifies the type of aggregate this event belongs to
	AggregateType string

	// EventType identifies the type of event
	EventType string

	// Payload contains the event data
	// Store as BYTEA for flexibility - allows any serialization format
	Payload []byte

	// Metadata contains additional event metadata as JSON
	Metadata []byte

	// EventVersion is the schema version of this event type
	EventVersion int

	// GlobalPosition is assigned by the store upon persistence
	// This field is read-only and set after successful append
	GlobalPosition int64

	// AggregateVersion is the version of the aggregate after this event is applied
	// Used for optimistic concurrency control
	AggregateVersion int64

	// CausationID identifies the event/command that caused this event (optional)
	CausationID uuid.NullUUID

	// CorrelationID links related events across aggregates (optional)
	CorrelationID uuid.NullUUID

	// TraceID for distributed tracing (optional)
	TraceID uuid.NullUUID

	// EventID is a unique identifier for this event
	EventID uuid.UUID

	// AggregateID uniquely identifies the aggregate instance
	AggregateID uuid.UUID
}

// PersistedEvent represents an event that has been stored.
// It includes the GlobalPosition assigned by the event store.
type PersistedEvent struct {
	Event
	// GlobalPosition is guaranteed to be set for persisted events
}
