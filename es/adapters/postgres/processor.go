package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/projection"
)

var (
	// ErrProjectionStopped indicates the projection was stopped due to an error.
	ErrProjectionStopped = errors.New("projection stopped")
)

// Processor processes events for projections using PostgreSQL for checkpointing.
// This is the PostgreSQL-specific implementation that manages transactions internally.
type Processor struct {
	db     *sql.DB
	store  *Store
	config *projection.ProcessorConfig
}

// NewProcessor creates a new PostgreSQL projection processor.
// The processor manages SQL transactions internally and coordinates checkpointing with event processing.
func NewProcessor(db *sql.DB, store *Store, config *projection.ProcessorConfig) *Processor {
	return &Processor{
		db:     db,
		store:  store,
		config: config,
	}
}

// Run processes events for the given projection until the context is canceled.
// It reads events in batches, applies partition and aggregate type filters, and updates checkpoints.
// Returns an error if the projection handler fails.
func (p *Processor) Run(ctx context.Context, proj projection.Projection) error {
	if p.config.Logger != nil {
		p.config.Logger.Info(ctx, "projection processor starting",
			"projection", proj.Name(),
			"partition_key", p.config.PartitionKey,
			"total_partitions", p.config.TotalPartitions,
			"batch_size", p.config.BatchSize)
	}

	// Build aggregate type filter once for the projection (not per batch)
	aggregateTypeFilter := buildAggregateTypeFilter(proj)

	for {
		select {
		case <-ctx.Done():
			if p.config.Logger != nil {
				p.config.Logger.Info(ctx, "projection processor stopped",
					"projection", proj.Name(),
					"reason", ctx.Err())
			}
			return ctx.Err()
		default:
		}

		// Process batch in transaction
		err := p.processBatch(ctx, proj, aggregateTypeFilter)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || err.Error() == "no events in batch" {
				// No events available, continue polling
				continue
			}
			if p.config.Logger != nil {
				p.config.Logger.Error(ctx, "projection processor error",
					"projection", proj.Name(),
					"error", err)
			}
			return fmt.Errorf("%w: %v", ErrProjectionStopped, err)
		}
	}
}

// buildAggregateTypeFilter builds a filter map for scoped projections.
// Returns nil if the projection is not scoped or has an empty aggregate types list.
func buildAggregateTypeFilter(proj projection.Projection) map[string]bool {
	scopedProj, ok := proj.(projection.ScopedProjection)
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

func (p *Processor) processBatch(ctx context.Context, proj projection.Projection, aggregateTypeFilter map[string]bool) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		//nolint:errcheck // Rollback error ignored: expected to fail if commit succeeds
		tx.Rollback()
	}()

	// Get current checkpoint
	checkpoint, err := p.store.GetCheckpoint(ctx, tx, proj.Name())
	if err != nil {
		return fmt.Errorf("failed to get checkpoint: %w", err)
	}

	if p.config.Logger != nil {
		p.config.Logger.Debug(ctx, "processing batch",
			"projection", proj.Name(),
			"checkpoint", checkpoint,
			"batch_size", p.config.BatchSize)
	}

	// Read events
	events, err := p.store.ReadEvents(ctx, tx, checkpoint, p.config.BatchSize)
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

		// Handle event - projection manages its own persistence
		handlerErr := proj.Handle(ctx, event)
		if handlerErr != nil {
			if p.config.Logger != nil {
				p.config.Logger.Error(ctx, "projection handler error",
					"projection", proj.Name(),
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
		err = p.store.UpdateCheckpoint(ctx, tx, proj.Name(), lastPosition)
		if err != nil {
			return fmt.Errorf("failed to update checkpoint: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if p.config.Logger != nil {
		p.config.Logger.Debug(ctx, "batch processed",
			"projection", proj.Name(),
			"processed", processedCount,
			"skipped", skippedCount,
			"checkpoint", lastPosition)
	}

	return nil
}
