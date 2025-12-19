// Package store provides event store abstractions and implementations.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"

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
	// - Fetches the current version from the aggregate_heads table (O(1) lookup)
	// - Assigns consecutive versions starting from (current + 1)
	// - Updates aggregate_heads with the new version
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

// AggregateStreamReader defines the interface for reading events for a specific aggregate.
type AggregateStreamReader interface {
	// ReadAggregateStream reads all events for a specific aggregate instance.
	// Events are ordered by aggregate_version ascending.
	//
	// Parameters:
	// - aggregateType: the type of aggregate (e.g., "User", "Order")
	// - aggregateID: the unique identifier of the aggregate instance
	// - fromVersion: optional minimum version (inclusive). Pass nil to read from the beginning.
	// - toVersion: optional maximum version (inclusive). Pass nil to read to the end.
	//
	// Examples:
	// - ReadAggregateStream(ctx, tx, "User", id, nil, nil) - read all events
	// - ReadAggregateStream(ctx, tx, "User", id, ptr(5), nil) - read from version 5 onwards
	// - ReadAggregateStream(ctx, tx, "User", id, nil, ptr(10)) - read up to version 10
	// - ReadAggregateStream(ctx, tx, "User", id, ptr(5), ptr(10)) - read versions 5-10
	//
	// Returns an empty slice if no events match the criteria.
	ReadAggregateStream(ctx context.Context, tx es.DBTX, aggregateType string, aggregateID uuid.UUID, fromVersion, toVersion *int64) ([]es.PersistedEvent, error)
}
