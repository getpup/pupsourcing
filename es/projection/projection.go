// Package projection provides projection processing capabilities.
package projection

import (
"context"
"hash/fnv"

"github.com/getpup/pupsourcing/es"
)

// Projection defines the interface for event projection handlers.
// Projections are storage-agnostic and can write to any destination
// (SQL databases, NoSQL stores, message brokers, search engines, etc.).
type Projection interface {
// Name returns the unique name of this projection.
// This name is used for checkpoint tracking.
Name() string

// Handle processes a single event.
// Return an error to stop projection processing.
//
// Projections are responsible for managing their own persistence.
// For SQL projections, they should manage their own database connections/transactions.
// For non-SQL projections (Elasticsearch, Redis, message brokers), they use appropriate clients.
//
// Event is passed by value to enforce immutability (events are value objects).
// Large data (Payload, Metadata byte slices) share references to their backing arrays,
// so the actual payload/metadata data is not deep-copied.
//
//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
Handle(ctx context.Context, event es.PersistedEvent) error
}

// ScopedProjection is an optional interface that projections can implement to filter
// events by aggregate type. This is useful for read model projections that only care
// about specific aggregate types.
//
// By default, projections implementing only the Projection interface receive all events.
// This ensures that global projections (e.g., integration publishers, audit logs) continue
// to work without modification.
//
// Example - Read model projection:
//
//type UserReadModelProjection struct {}
//
//func (p *UserReadModelProjection) Name() string {
//    return "user_read_model"
//}
//
//func (p *UserReadModelProjection) AggregateTypes() []string {
//    return []string{"User"}
//}
//
//func (p *UserReadModelProjection) Handle(ctx context.Context, event es.PersistedEvent) error {
//    // Only receives User aggregate events
//    return nil
//}
//
// Example - Global integration publisher:
//
//type WatermillPublisher struct {}
//
//func (p *WatermillPublisher) Name() string {
//    return "system.integration.watermill.v1"
//}
//
//func (p *WatermillPublisher) Handle(ctx context.Context, event es.PersistedEvent) error {
//    // Receives ALL events for publishing to message broker
//    return nil
//}
type ScopedProjection interface {
Projection
// AggregateTypes returns the list of aggregate types this projection cares about.
// If empty, the projection receives all events (same as not implementing ScopedProjection).
// If non-empty, only events matching one of these aggregate types are passed to Handle.
AggregateTypes() []string
}

// PartitionStrategy defines how events are partitioned across projection instances.
type PartitionStrategy interface {
// ShouldProcess returns true if this projection instance should process the given event.
// aggregateID is the aggregate ID of the event.
// partitionKey identifies this projection instance (e.g., "0" for first of 4 workers).
// totalPartitions is the total number of projection instances.
ShouldProcess(aggregateID string, partitionKey int, totalPartitions int) bool
}

// HashPartitionStrategy implements deterministic hash-based partitioning.
// Events are distributed across partitions based on a hash of the aggregate ID.
// This ensures:
// - All events for the same aggregate go to the same partition
// - Even distribution across partitions
// - Deterministic assignment (same aggregate always goes to same partition)
//
// This strategy enables horizontal scaling of projection processing while
// maintaining ordering guarantees within each aggregate.
type HashPartitionStrategy struct{}

// ShouldProcess implements PartitionStrategy using FNV-1a hashing.
func (HashPartitionStrategy) ShouldProcess(aggregateID string, partitionKey, totalPartitions int) bool {
if totalPartitions <= 1 {
return true
}

h := fnv.New32a()
h.Write([]byte(aggregateID))
partition := int(h.Sum32()) % totalPartitions
return partition == partitionKey
}

// ProcessorConfig configures a projection processor.
type ProcessorConfig struct {
// PartitionStrategy determines which events this processor handles
PartitionStrategy PartitionStrategy

// Logger is an optional logger for observability.
// If nil, logging is disabled (zero overhead).
Logger es.Logger

// BatchSize is the number of events to read per batch
BatchSize int

// PartitionKey identifies this processor instance (0-indexed)
PartitionKey int

// TotalPartitions is the total number of processor instances
TotalPartitions int
}

// DefaultProcessorConfig returns the default configuration.
func DefaultProcessorConfig() ProcessorConfig {
return ProcessorConfig{
BatchSize:         100,
PartitionKey:      0,
TotalPartitions:   1,
PartitionStrategy: HashPartitionStrategy{},
Logger:            nil, // No logging by default
}
}

// ProcessorRunner is the interface that adapter-specific processors must implement.
// This allows the Runner to orchestrate projections regardless of the underlying
// storage implementation (SQL, NoSQL, message brokers, etc.).
type ProcessorRunner interface {
// Run processes events for the given projection until the context is canceled.
// Returns an error if the projection handler fails.
Run(ctx context.Context, projection Projection) error
}
