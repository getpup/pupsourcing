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
	Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error
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
func (HashPartitionStrategy) ShouldProcess(aggregateID string, partitionKey int, totalPartitions int) bool {
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

	// PartitionStrategy determines which events this processor handles
	PartitionStrategy PartitionStrategy
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
	}
}

// Processor processes events for projections.
type Processor struct {
	config      ProcessorConfig
	eventReader store.EventReader
	db          *sql.DB
}

// NewProcessor creates a new projection processor.
func NewProcessor(db *sql.DB, eventReader store.EventReader, config ProcessorConfig) *Processor {
	return &Processor{
		config:      config,
		eventReader: eventReader,
		db:          db,
	}
}

// Run processes events for the given projection until the context is cancelled.
// It reads events in batches, applies the partition filter, and updates checkpoints.
// Returns ErrProjectionStopped if the projection handler returns an error.
func (p *Processor) Run(ctx context.Context, projection Projection) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Process batch in transaction
		err := p.processBatch(ctx, projection)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || err.Error() == "no events in batch" {
				// No events available, continue polling
				continue
			}
			return fmt.Errorf("%w: %v", ErrProjectionStopped, err)
		}
	}
}

func (p *Processor) processBatch(ctx context.Context, projection Projection) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get current checkpoint
	checkpoint, err := p.getCheckpoint(ctx, tx, projection.Name())
	if err != nil {
		return fmt.Errorf("failed to get checkpoint: %w", err)
	}

	// Read events
	events, err := p.eventReader.ReadEvents(ctx, tx, checkpoint, p.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to read events: %w", err)
	}

	if len(events) == 0 {
		return errors.New("no events in batch")
	}

	// Process events with partition filter
	var lastPosition int64
	for _, event := range events {
		// Apply partition filter
		if !p.config.PartitionStrategy.ShouldProcess(
			event.AggregateID.String(),
			p.config.PartitionKey,
			p.config.TotalPartitions,
		) {
			lastPosition = event.GlobalPosition
			continue
		}

		// Handle event
		err := projection.Handle(ctx, tx, event)
		if err != nil {
			return fmt.Errorf("projection handler error at position %d: %w", event.GlobalPosition, err)
		}

		lastPosition = event.GlobalPosition
	}

	// Update checkpoint
	if lastPosition > 0 {
		err = p.updateCheckpoint(ctx, tx, projection.Name(), lastPosition)
		if err != nil {
			return fmt.Errorf("failed to update checkpoint: %w", err)
		}
	}

	return tx.Commit()
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
