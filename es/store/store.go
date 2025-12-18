// Package store provides event store abstractions and implementations.
package store

import (
	"context"
	"errors"

	"github.com/getpup/pupsourcing/es"
)

var (
	// ErrOptimisticConcurrency indicates a version conflict during append.
	ErrOptimisticConcurrency = errors.New("optimistic concurrency conflict")

	// ErrNoEvents indicates an attempt to append zero events.
	ErrNoEvents = errors.New("no events to append")
)

// EventStore defines the interface for appending events.
type EventStore interface {
	// Append atomically appends one or more events within the provided transaction.
	// Events must all belong to the same aggregate instance.
	// Returns the assigned global positions or an error.
	//
	// The store automatically assigns AggregateVersion to each event:
	// - Fetches the current MAX(aggregate_version) for the aggregate
	// - Assigns consecutive versions starting from (max + 1)
	// - The database unique constraint on (aggregate_type, aggregate_id, aggregate_version)
	//   enforces optimistic concurrency
	//
	// Returns ErrOptimisticConcurrency if another transaction commits conflicting events
	// between the version check and insert (detected via unique constraint violation).
	// Returns ErrNoEvents if events slice is empty.
	Append(ctx context.Context, tx es.DBTX, events []es.Event) ([]int64, error)
}

// EventReader defines the interface for reading events sequentially.
type EventReader interface {
	// ReadEvents reads events starting from the given global position.
	// Returns up to limit events.
	// Events are ordered by global_position ascending.
	ReadEvents(ctx context.Context, tx es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error)
}
