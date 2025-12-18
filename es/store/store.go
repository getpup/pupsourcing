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
	// Optimistic concurrency is enforced via AggregateVersion:
	// - The first event's version must match the current aggregate version + 1
	// - Subsequent events must have sequential versions
	//
	// Returns ErrOptimisticConcurrency if version check fails.
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
