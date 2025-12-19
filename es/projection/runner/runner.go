// Package runner provides optional tooling for running multiple projections and scaling them safely.
// This package is designed to be explicit, deterministic, and CLI-friendly without imposing
// framework behavior or automatic scheduling.
package runner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/getpup/pupsourcing/es/projection"
	"github.com/getpup/pupsourcing/es/store"
)

var (
	// ErrNoProjections indicates that no projections were provided to run.
	ErrNoProjections = errors.New("no projections provided")

	// ErrInvalidPartitionConfig indicates invalid partition configuration.
	ErrInvalidPartitionConfig = errors.New("invalid partition configuration")
)

// ProjectionConfig defines configuration for a single projection.
type ProjectionConfig struct {
	// Projection is the projection to run
	Projection projection.Projection

	// ProcessorConfig is the configuration for the projection processor
	ProcessorConfig projection.ProcessorConfig
}

// Runner orchestrates multiple projections, optionally with partitioning.
// It is designed to be explicit and deterministic, with no magic auto-discovery.
// The runner works with context cancellation and remains transaction-agnostic.
//
// Example:
//
//	runner := runner.New(db, eventReader)
//	err := runner.Run(ctx, []ProjectionConfig{
//	    {Projection: &MyProjection{}, ProcessorConfig: config1},
//	    {Projection: &MyOtherProjection{}, ProcessorConfig: config2},
//	})
type Runner struct {
	db          *sql.DB
	eventReader store.EventReader
}

// New creates a new projection runner.
func New(db *sql.DB, eventReader store.EventReader) *Runner {
	return &Runner{
		db:          db,
		eventReader: eventReader,
	}
}

// Run runs multiple projections concurrently until the context is cancelled.
// Each projection runs in its own goroutine with its specified configuration.
// Returns when the context is cancelled or when any projection returns an error.
//
// If a projection returns an error, all other projections are cancelled and the error
// is returned. This ensures fail-fast behavior.
//
// This method is safe to call from CLIs and does not assume single-process ownership.
// Coordination happens via database checkpoints.
func (r *Runner) Run(ctx context.Context, configs []ProjectionConfig) error {
	if len(configs) == 0 {
		return ErrNoProjections
	}

	// Validate configurations
	for i, config := range configs {
		if config.Projection == nil {
			return fmt.Errorf("projection at index %d is nil", i)
		}
		if config.ProcessorConfig.PartitionKey < 0 {
			return fmt.Errorf("%w: partition key must be >= 0", ErrInvalidPartitionConfig)
		}
		if config.ProcessorConfig.TotalPartitions < 1 {
			return fmt.Errorf("%w: total partitions must be >= 1", ErrInvalidPartitionConfig)
		}
		if config.ProcessorConfig.PartitionKey >= config.ProcessorConfig.TotalPartitions {
			return fmt.Errorf("%w: partition key must be < total partitions", ErrInvalidPartitionConfig)
		}
	}

	// Create a context that we can cancel if any projection fails
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errChan := make(chan error, len(configs))

	// Start each projection in its own goroutine
	for _, config := range configs {
		wg.Add(1)
		go func(cfg ProjectionConfig) {
			defer wg.Done()

			processor := projection.NewProcessor(r.db, r.eventReader, cfg.ProcessorConfig)
			err := processor.Run(ctx, cfg.Projection)

			// Only report errors that aren't from context cancellation
			if err != nil && !errors.Is(err, context.Canceled) {
				errChan <- fmt.Errorf("projection %q failed: %w", cfg.Projection.Name(), err)
			}
		}(config)
	}

	// Wait for all projections to complete or for an error
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Return the first error, or nil if context was cancelled
	select {
	case err := <-errChan:
		if err != nil {
			cancel() // Cancel all other projections
			return err
		}
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunProjectionPartitions is a helper that runs a single projection with N partitions
// in the same process. This is useful for simple horizontal scaling without needing
// to deploy multiple processes.
//
// Each partition runs in its own goroutine. All partitions share the same database
// connection pool but maintain separate checkpoints in the database.
//
// IMPORTANT: The projection instance is shared across all workers. If your projection
// maintains state, it MUST be thread-safe (use sync.Mutex, atomic operations, or channels).
// Alternatively, the projection can be stateless and only update database tables.
//
// Example:
//
//	err := runner.RunProjectionPartitions(ctx, db, store, myProjection, 4)
//
// This creates 4 workers processing different subsets of events in parallel.
// Events for the same aggregate always go to the same partition, maintaining ordering.
func RunProjectionPartitions(
	ctx context.Context,
	db *sql.DB,
	eventReader store.EventReader,
	proj projection.Projection,
	totalPartitions int,
) error {
	if totalPartitions < 1 {
		return fmt.Errorf("%w: total partitions must be >= 1", ErrInvalidPartitionConfig)
	}

	configs := make([]ProjectionConfig, totalPartitions)
	for i := 0; i < totalPartitions; i++ {
		config := projection.DefaultProcessorConfig()
		config.PartitionKey = i
		config.TotalPartitions = totalPartitions

		configs[i] = ProjectionConfig{
			Projection:      proj,
			ProcessorConfig: config,
		}
	}

	runner := New(db, eventReader)
	return runner.Run(ctx, configs)
}

// RunMultipleProjections is a helper that runs multiple projections, each with its own
// partitioning configuration. This is useful when you want to run different projections
// with different scaling characteristics in the same process.
//
// Example:
//
//	err := runner.RunMultipleProjections(ctx, db, store, []runner.ProjectionConfig{
//	    {
//	        Projection: &FastProjection{},
//	        ProcessorConfig: projection.ProcessorConfig{
//	            BatchSize: 1000,
//	            PartitionKey: 0,
//	            TotalPartitions: 1,
//	        },
//	    },
//	    {
//	        Projection: &SlowProjection{},
//	        ProcessorConfig: projection.ProcessorConfig{
//	            BatchSize: 10,
//	            PartitionKey: workerID,
//	            TotalPartitions: 4,
//	        },
//	    },
//	})
func RunMultipleProjections(
	ctx context.Context,
	db *sql.DB,
	eventReader store.EventReader,
	configs []ProjectionConfig,
) error {
	runner := New(db, eventReader)
	return runner.Run(ctx, configs)
}
