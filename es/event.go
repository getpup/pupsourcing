// Package es provides core event sourcing interfaces and types.
package es

import (
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/google/uuid"
)

// NullableString represents a string that may be null.
// It implements database/sql Scanner and Valuer interfaces for SQL interop,
// but avoids direct dependency on sql.NullString in public types.
type NullableString struct {
	String string
	Valid  bool // Valid is true if String is not NULL
}

// Scan implements the sql.Scanner interface.
func (ns *NullableString) Scan(value interface{}) error {
	if value == nil {
		ns.String, ns.Valid = "", false
		return nil
	}
	var s sql.NullString
	if err := s.Scan(value); err != nil {
		return err
	}
	ns.String, ns.Valid = s.String, s.Valid
	return nil
}

// Value implements the driver.Valuer interface.
func (ns NullableString) Value() (driver.Value, error) {
	if !ns.Valid {
		return nil, nil
	}
	return ns.String, nil
}

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
	CausationID   NullableString
	CorrelationID NullableString
	TraceID       NullableString
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
	CausationID      NullableString
	CorrelationID    NullableString
	TraceID          NullableString
	EventID          uuid.UUID
}
