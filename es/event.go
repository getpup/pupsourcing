// Package es provides core event sourcing interfaces and types.
package es

import (
	"time"

	"github.com/google/uuid"
)

// Event represents an immutable domain event before persistence.
// Events are value objects without identity until persisted.
// AggregateVersion and GlobalPosition are assigned by the store during Append.
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

	// CausationID identifies the event/command that caused this event (optional)
	CausationID uuid.NullUUID

	// CorrelationID links related events across aggregates (optional)
	CorrelationID uuid.NullUUID

	// TraceID for distributed tracing (optional)
	TraceID uuid.NullUUID

	// EventID is a unique identifier for this event
	EventID uuid.UUID

	// AggregateID uniquely identifies the aggregate instance.
	// This can be any string value, allowing for flexible aggregate identification
	// such as UUIDs, email addresses (for reservation aggregates), or other identifiers.
	AggregateID string
}

// PersistedEvent represents an event that has been stored.
// It includes the GlobalPosition and AggregateVersion assigned by the event store.
type PersistedEvent struct {
	// CreatedAt is when the event was created
	CreatedAt time.Time

	// AggregateType identifies the type of aggregate this event belongs to
	AggregateType string

	// EventType identifies the type of event
	EventType string

	// Payload contains the event data
	Payload []byte

	// Metadata contains additional event metadata as JSON
	Metadata []byte

	// EventVersion is the schema version of this event type
	EventVersion int

	// GlobalPosition is assigned by the store upon persistence
	GlobalPosition int64

	// AggregateVersion is the version of the aggregate after this event was applied
	// Assigned by the store during append
	AggregateVersion int64

	// CausationID identifies the event/command that caused this event (optional)
	CausationID uuid.NullUUID

	// CorrelationID links related events across aggregates (optional)
	CorrelationID uuid.NullUUID

	// TraceID for distributed tracing (optional)
	TraceID uuid.NullUUID

	// EventID is a unique identifier for this event
	EventID uuid.UUID

	// AggregateID uniquely identifies the aggregate instance.
	// This can be any string value, allowing for flexible aggregate identification
	// such as UUIDs, email addresses (for reservation aggregates), or other identifiers.
	AggregateID string
}
