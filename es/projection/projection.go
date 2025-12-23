// Package projection provides projection processing capabilities.
package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/store"
)

var (
	// ErrProjectionStopped indicates the projection was stopped due to an error.
	ErrProjectionStopped = errors.New("projection stopped")
)

// Projection defines the interface for event projection handlers.
type Projection interface {
	// Name returns the unique name of this projection.
	// This name is used for checkpoint tracking.
	Name() string

	// Handle processes a single event.
	// Return an error to stop projection processing.
	// Event is passed by value to enforce immutability (events are value objects).
	// Large data (Payload, Metadata byte slices) share references to their backing arrays,
	// so the actual payload/metadata data is not deep-copied.
	//
	//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
	Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error
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
//	type UserReadModelProjection struct {}
//
//	func (p *UserReadModelProjection) Name() string {
//	    return "user_read_model"
//	}
//
//	func (p *UserReadModelProjection) AggregateTypes() []string {
//	    return []string{"User"}
//	}
//
//	func (p *UserReadModelProjection) Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error {
//	    // Only receives User aggregate events
//	    return nil
//	}
//
// Example - Global integration publisher:
//
//	type WatermillPublisher struct {}
//
//	func (p *WatermillPublisher) Name() string {
//	    return "system.integration.watermill.v1"
//	}
//
//	func (p *WatermillPublisher) Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error {
//	    // Receives ALL events for publishing to message broker
//	    return nil
//	}
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

	// EventsTable is the name of the events table
	EventsTable string

	// CheckpointsTable is the name of the checkpoints table
	CheckpointsTable string

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
		EventsTable:       "events",
		CheckpointsTable:  "projection_checkpoints",
		BatchSize:         100,
		PartitionKey:      0,
		TotalPartitions:   1,
		PartitionStrategy: HashPartitionStrategy{},
		Logger:            nil, // No logging by default
	}
}

// Processor processes events for projections.
type Processor struct {
	eventReader store.EventReader
	db          *sql.DB
	config      *ProcessorConfig
}

// NewProcessor creates a new projection processor.
//
// Note: In v1.1.0, this function was changed to accept *ProcessorConfig (pointer)
// instead of ProcessorConfig (value) to reduce memory overhead. This is a breaking
// change - update your code to pass &config instead of config.
func NewProcessor(db *sql.DB, eventReader store.EventReader, config *ProcessorConfig) *Processor {
	return &Processor{
		config:      config,
		eventReader: eventReader,
		db:          db,
	}
}

// Run processes events for the given projection until the context is canceled.
// It reads events in batches, applies the partition filter, and updates checkpoints.
// Returns ErrProjectionStopped if the projection handler returns an error.
func (p *Processor) Run(ctx context.Context, projection Projection) error {
	if p.config.Logger != nil {
		p.config.Logger.Info(ctx, "projection processor starting",
			"projection", projection.Name(),
			"partition_key", p.config.PartitionKey,
			"total_partitions", p.config.TotalPartitions,
			"batch_size", p.config.BatchSize)
	}

	// Build aggregate type filter once for the projection (not per batch)
	aggregateTypeFilter := buildAggregateTypeFilter(projection)

	for {
		select {
		case <-ctx.Done():
			if p.config.Logger != nil {
				p.config.Logger.Info(ctx, "projection processor stopped",
					"projection", projection.Name(),
					"reason", ctx.Err())
			}
			return ctx.Err()
		default:
		}

		// Process batch in transaction
		err := p.processBatch(ctx, projection, aggregateTypeFilter)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || err.Error() == "no events in batch" {
				// No events available, continue polling
				continue
			}
			if p.config.Logger != nil {
				p.config.Logger.Error(ctx, "projection processor error",
					"projection", projection.Name(),
					"error", err)
			}
			return fmt.Errorf("%w: %v", ErrProjectionStopped, err)
		}
	}
}

// buildAggregateTypeFilter builds a filter map for scoped projections.
// Returns nil if the projection is not scoped or has an empty aggregate types list.
func buildAggregateTypeFilter(projection Projection) map[string]bool {
	scopedProj, ok := projection.(ScopedProjection)
	if !ok {
		return nil
	}

	types := scopedProj.AggregateTypes()
	if len(types) == 0 {
		return nil
	}

	filter := make(map[string]bool, len(types))
	for _, aggType := range types {
		filter[aggType] = true
	}
	return filter
}

// shouldProcessEvent checks if an event should be processed based on partition and aggregate type filters.
//
//nolint:gocritic // hugeParam: Intentionally pass by value to match event processing pattern
func (p *Processor) shouldProcessEvent(event es.PersistedEvent, aggregateTypeFilter map[string]bool) bool {
	// Apply partition filter
	if !p.config.PartitionStrategy.ShouldProcess(
		event.AggregateID,
		p.config.PartitionKey,
		p.config.TotalPartitions,
	) {
		return false
	}

	// Apply aggregate type filter if projection is scoped
	if aggregateTypeFilter != nil && !aggregateTypeFilter[event.AggregateType] {
		return false
	}

	return true
}

func (p *Processor) processBatch(ctx context.Context, projection Projection, aggregateTypeFilter map[string]bool) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		//nolint:errcheck // Rollback error ignored: expected to fail if commit succeeds
		tx.Rollback()
	}()

	// Get current checkpoint
	checkpoint, err := p.getCheckpoint(ctx, tx, projection.Name())
	if err != nil {
		return fmt.Errorf("failed to get checkpoint: %w", err)
	}

	if p.config.Logger != nil {
		p.config.Logger.Debug(ctx, "processing batch",
			"projection", projection.Name(),
			"checkpoint", checkpoint,
			"batch_size", p.config.BatchSize)
	}

	// Read events
	events, err := p.eventReader.ReadEvents(ctx, tx, checkpoint, p.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to read events: %w", err)
	}

	if len(events) == 0 {
		return errors.New("no events in batch")
	}

	// Process events with partition filter and aggregate type filter
	// Note: Events are passed by value to projection handlers to enforce immutability.
	// This creates a 232-byte copy per event, but large data (Payload, Metadata) is not deep-copied
	// since slices share references to their backing arrays. The immutability guarantee
	// is more valuable than the minimal copy cost in event processing workloads.
	var lastPosition int64
	var processedCount int
	var skippedCount int
	for i := range events {
		event := events[i]

		// Check if event should be processed
		if !p.shouldProcessEvent(event, aggregateTypeFilter) {
			lastPosition = event.GlobalPosition
			skippedCount++
			continue
		}

		// Handle event
		handlerErr := projection.Handle(ctx, tx, event)
		if handlerErr != nil {
			if p.config.Logger != nil {
				p.config.Logger.Error(ctx, "projection handler error",
					"projection", projection.Name(),
					"position", event.GlobalPosition,
					"aggregate_type", event.AggregateType,
					"aggregate_id", event.AggregateID,
					"event_type", event.EventType,
					"error", handlerErr)
			}
			return fmt.Errorf("projection handler error at position %d: %w", event.GlobalPosition, handlerErr)
		}

		lastPosition = event.GlobalPosition
		processedCount++
	}

	// Update checkpoint
	if lastPosition > 0 {
		err = p.updateCheckpoint(ctx, tx, projection.Name(), lastPosition)
		if err != nil {
			return fmt.Errorf("failed to update checkpoint: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if p.config.Logger != nil {
		p.config.Logger.Debug(ctx, "batch processed",
			"projection", projection.Name(),
			"processed", processedCount,
			"skipped", skippedCount,
			"checkpoint", lastPosition)
	}

	return nil
}

func (p *Processor) getCheckpoint(ctx context.Context, tx es.DBTX, projectionName string) (int64, error) {
	query := fmt.Sprintf(`
		SELECT last_global_position 
		FROM %s 
		WHERE projection_name = $1
	`, p.config.CheckpointsTable)

	var checkpoint int64
	err := tx.QueryRowContext(ctx, query, projectionName).Scan(&checkpoint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return checkpoint, nil
}

func (p *Processor) updateCheckpoint(ctx context.Context, tx es.DBTX, projectionName string, position int64) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (projection_name, last_global_position, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (projection_name)
		DO UPDATE SET 
			last_global_position = EXCLUDED.last_global_position,
			updated_at = EXCLUDED.updated_at
	`, p.config.CheckpointsTable)

	_, err := tx.ExecContext(ctx, query, projectionName, position)
	return err
}
