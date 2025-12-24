// Package runner provides optional tooling for running multiple projections and scaling them safely.
// This package is designed to be explicit, deterministic, and CLI-friendly without imposing
// framework behavior or automatic scheduling.
package runner

import (
"context"
"errors"
"fmt"
"sync"

"github.com/getpup/pupsourcing/es/projection"
)

var (
// ErrNoProjections indicates that no projections were provided to run.
ErrNoProjections = errors.New("no projections provided")

// ErrInvalidPartitionConfig indicates invalid partition configuration.
ErrInvalidPartitionConfig = errors.New("invalid partition configuration")
)

// ProjectionRunner pairs a projection with its processor.
// The processor is adapter-specific (postgres.Processor, mysql.Processor, etc.)
// and knows how to manage transactions and checkpoints for that storage type.
type ProjectionRunner struct {
Projection projection.Projection
Processor  projection.ProcessorRunner
}

// Runner orchestrates multiple projections concurrently.
// It is storage-agnostic and works with any processor implementation.
//
// Example with PostgreSQL:
//
//store := postgres.NewStore(postgres.DefaultStoreConfig())
//processor1 := postgres.NewProcessor(db, store, &config1)
//processor2 := postgres.NewProcessor(db, store, &config2)
//
//runner := runner.New()
//err := runner.Run(ctx, []runner.ProjectionRunner{
//    {Projection: &MyProjection{}, Processor: processor1},
//    {Projection: &MyOtherProjection{}, Processor: processor2},
//})
type Runner struct{}

// New creates a new projection runner.
func New() *Runner {
return &Runner{}
}

// Run runs multiple projections concurrently until the context is canceled.
// Each projection runs in its own goroutine with its processor.
// Returns when the context is canceled or when any projection returns an error.
//
// If a projection returns an error, all other projections are canceled and the error
// is returned. This ensures fail-fast behavior.
//
// This method is safe to call from CLIs and does not assume single-process ownership.
// Coordination happens via the processor's checkpoint management.
func (r *Runner) Run(ctx context.Context, runners []ProjectionRunner) error {
if len(runners) == 0 {
return ErrNoProjections
}

// Validate configurations
for i, runner := range runners {
if runner.Projection == nil {
return fmt.Errorf("projection at index %d is nil", i)
}
if runner.Processor == nil {
return fmt.Errorf("processor at index %d is nil", i)
}
}

// Create a context that we can cancel if any projection fails
ctx, cancel := context.WithCancel(ctx)
defer cancel()

var wg sync.WaitGroup
errChan := make(chan error, len(runners))

// Start each projection in its own goroutine
for _, runner := range runners {
wg.Add(1)
go func(pr ProjectionRunner) {
defer wg.Done()

err := pr.Processor.Run(ctx, pr.Projection)

// Only report errors that aren't from context cancellation
if err != nil && !errors.Is(err, context.Canceled) {
errChan <- fmt.Errorf("projection %q failed: %w", pr.Projection.Name(), err)
}
}(runner)
}

// Wait for all projections to complete or for an error
go func() {
wg.Wait()
close(errChan)
}()

// Return the first error, or nil if context was canceled
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
