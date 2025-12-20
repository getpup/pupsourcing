# pupsourcing

[![CI](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml/badge.svg)](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/getpup/pupsourcing)](https://goreportcard.com/report/github.com/getpup/pupsourcing)
[![GoDoc](https://godoc.org/github.com/getpup/pupsourcing?status.svg)](https://godoc.org/github.com/getpup/pupsourcing)

A professional-grade Event Sourcing library for Go, designed with clean architecture principles.

## Overview

`pupsourcing` provides minimal, production-ready infrastructure for event sourcing in Go applications. Event sourcing is a pattern where state changes are stored as a sequence of events, providing a complete audit trail and enabling powerful features like event replay and complex event processing.

## Features

- ✅ **Clean Architecture**: Core interfaces are datastore-agnostic
- ✅ **PostgreSQL Adapter**: Production-ready Postgres implementation
- ✅ **Transaction-Agnostic**: Works with `*sql.DB` and `*sql.Tx`
- ✅ **Optimistic Concurrency**: Built-in version conflict detection
- ✅ **Aggregate Stream Reader**: Read events by aggregate with version filtering
- ✅ **Projection System**: Pull-based event processing with checkpoints
- ✅ **Horizontal Scaling**: Deterministic hash-based partitioning
- ✅ **Migration Generation**: SQL migrations via `go generate`
- ✅ **Observability Hooks**: Optional logging for debugging and monitoring
- ✅ **Zero Dependencies**: Uses only Go standard library (plus database driver)

## Installation

```bash
go get github.com/getpup/pupsourcing
```

For PostgreSQL support:
```bash
go get github.com/lib/pq
```

## Quick Start

### 1. Generate Database Migration

```bash
go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

Or with `go generate`:
```go
//go:generate go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

This creates SQL migration files with:
- Event store table with proper indexes
- Projection checkpoint table
- All necessary constraints

### 2. Append Events

```go
import (
    "github.com/getpup/pupsourcing/es"
    "github.com/getpup/pupsourcing/es/adapters/postgres"
    "github.com/google/uuid"
)

// Create store
store := postgres.NewStore(postgres.DefaultStoreConfig())

// Create events
events := []es.Event{
    {
        AggregateType: "User",
        AggregateID:   uuid.New(),
        EventID:       uuid.New(),
        EventType:     "UserCreated",
        EventVersion:  1,
        Payload:       []byte(`{"email":"user@example.com"}`),
        Metadata:      []byte(`{}`),
        CreatedAt:     time.Now(),
    },
}

// Append within transaction
tx, _ := db.BeginTx(ctx, nil)
positions, err := store.Append(ctx, tx, events)
if err != nil {
    tx.Rollback()
    log.Fatal(err)
}
tx.Commit()
```

### 3. Read Aggregate Streams

```go
// Read all events for an aggregate
aggregateID := uuid.MustParse("...")
events, err := store.ReadAggregateStream(ctx, tx, "User", aggregateID, nil, nil)
if err != nil {
    log.Fatal(err)
}

// Process events in order
for _, event := range events {
    fmt.Printf("Event v%d: %s\n", event.AggregateVersion, event.EventType)
}

// Read from a specific version (e.g., after a snapshot at version 5)
fromVersion := int64(5)
recentEvents, err := store.ReadAggregateStream(ctx, tx, "User", aggregateID, &fromVersion, nil)

// Read a specific version range
toVersion := int64(10)
rangeEvents, err := store.ReadAggregateStream(ctx, tx, "User", aggregateID, &fromVersion, &toVersion)
```

### 4. Process Events with Projections

```go
import (
    "github.com/getpup/pupsourcing/es/projection"
)

// Implement Projection interface
type MyProjection struct {}

func (p *MyProjection) Name() string {
    return "my_projection"
}

func (p *MyProjection) Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error {
    // Process event - update read model, send notifications, etc.
    fmt.Printf("Processing event: %s\n", event.EventType)
    return nil
}

// Run projection
proj := &MyProjection{}
config := projection.DefaultProcessorConfig()
processor := projection.NewProcessor(db, store, &config)

// Run until context is cancelled
err := processor.Run(ctx, proj)
```

## Architecture

### Package Structure

```
es/
├── event.go              # Core event types
├── dbtx.go               # Database transaction abstraction
├── store/                # Event store interfaces
├── projection/           # Projection processing
├── adapters/
│   └── postgres/         # PostgreSQL implementation
└── migrations/           # Migration generation
```

### Design Principles

- **Transaction-Agnostic**: You control transaction boundaries
- **Automatic Versioning**: The library assigns aggregate versions automatically
- **Optimistic Concurrency**: Version conflicts are detected automatically via database constraints
- **Pull-Based Projections**: Read events sequentially by global position
- **Immutable Events**: Events are value objects until persisted
- **Clean Separation**: Infrastructure concerns don't leak into application code

### Optimistic Concurrency

The library handles optimistic concurrency automatically:

1. When you append events, the store fetches the current version from the `aggregate_heads` table (O(1) lookup)
2. It assigns consecutive versions starting from `(current + 1)` to your events
3. The database unique constraint on `(aggregate_type, aggregate_id, aggregate_version)` ensures no conflicts
4. The `aggregate_heads` table is updated atomically with the new version in the same transaction
5. If another transaction commits between the version check and insert, you'll get `store.ErrOptimisticConcurrency`
6. Simply retry your transaction if you encounter a concurrency error

The `aggregate_heads` table tracks the current version of each aggregate, providing O(1) version lookups and eliminating expensive MAX() queries.

## Projections

### Basic Usage

Projections process events sequentially and track progress via checkpoints. They automatically resume from the last processed position.

```go
type UserReadModelProjection struct {
    db *sql.DB
}

func (p *UserReadModelProjection) Name() string {
    return "user_read_model"
}

func (p *UserReadModelProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType == "UserCreated" {
        var user UserCreated
        json.Unmarshal(event.Payload, &user)
        
        // Insert into read model table
        _, err := tx.ExecContext(ctx,
            "INSERT INTO user_read_model (id, name, email, created_at) VALUES ($1, $2, $3, $4)",
            event.AggregateID, user.Name, user.Email, event.CreatedAt)
        return err
    }
    return nil
}
```

### Horizontal Scaling

Scale projections horizontally with hash-based partitioning:

```go
config := projection.DefaultProcessorConfig()
config.TotalPartitions = 4  // Total number of workers
config.PartitionKey = 0     // This worker's partition (0-3)

processor := projection.NewProcessor(db, store, &config)
```

Events for the same aggregate always go to the same partition, maintaining ordering guarantees.

### Projection Orchestration

For running multiple projections or scaling projections across multiple workers, use the `runner` package:

```go
import "github.com/getpup/pupsourcing/es/projection/runner"

// Run a single projection with N workers (in-process scaling)
err := runner.RunProjectionPartitions(ctx, db, store, projection, 4)

// Run multiple projections concurrently
configs := []runner.ProjectionConfig{
    {Projection: &Projection1{}, ProcessorConfig: config1},
    {Projection: &Projection2{}, ProcessorConfig: config2},
}
err := runner.RunMultipleProjections(ctx, db, store, configs)
```

### CLI-Friendly Patterns

Design your projection runners to work as CLI commands:

```go
func main() {
    partitionKey := flag.Int("partition-key", 0, "Partition key")
    totalPartitions := flag.Int("total-partitions", 1, "Total partitions")
    flag.Parse()
    
    // Configure projection with partition info
    config := projection.DefaultProcessorConfig()
    config.PartitionKey = *partitionKey
    config.TotalPartitions = *totalPartitions
    
    processor := projection.NewProcessor(db, store, &config)
    err := processor.Run(ctx, projection)
}
```

Run multiple instances safely:
```bash
# Terminal 1
./myapp projections --partition-key=0 --total-partitions=4

# Terminal 2
./myapp projections --partition-key=1 --total-partitions=4

# Terminal 3-4: same with partition-key=2 and 3
```

### Scaling Patterns

**Single Process, Multiple Workers (Worker Pool)**
```go
// 4 workers in the same process
runner.RunProjectionPartitions(ctx, db, store, projection, 4)
```

**Multiple Processes, Partitioned**
```bash
# Run the same binary with different partition keys
PARTITION_KEY=0 ./myapp &
PARTITION_KEY=1 ./myapp &
PARTITION_KEY=2 ./myapp &
PARTITION_KEY=3 ./myapp &
```

**Mixed: Multiple Projections with Different Scaling**
```go
configs := []runner.ProjectionConfig{
    {
        Projection: &FastProjection{},
        ProcessorConfig: projection.ProcessorConfig{
            BatchSize: 1000,
            PartitionKey: 0,
            TotalPartitions: 1,  // Single worker
        },
    },
    {
        Projection: &SlowProjection{},
        ProcessorConfig: projection.ProcessorConfig{
            BatchSize: 10,
            PartitionKey: workerID,
            TotalPartitions: 4,  // 4 workers
        },
    },
}
```

## Configuration

### Event Store

```go
config := postgres.StoreConfig{
    EventsTable:      "events",
    CheckpointsTable: "projection_checkpoints",
}
store := postgres.NewStore(config)
```

### Projections

```go
config := projection.ProcessorConfig{
    EventsTable:       "events",
    CheckpointsTable:  "projection_checkpoints",
    BatchSize:         100,
    PartitionKey:      0,
    TotalPartitions:   1,
    PartitionStrategy: projection.HashPartitionStrategy{},
}
```

## Observability & Logging

pupsourcing includes optional logging hooks for observability and debugging without forcing logging decisions or dependencies.

### Logger Interface

Implement the minimal `es.Logger` interface with your preferred logging library:

```go
type Logger interface {
    Debug(ctx context.Context, msg string, keyvals ...interface{})
    Info(ctx context.Context, msg string, keyvals ...interface{})
    Error(ctx context.Context, msg string, keyvals ...interface{})
}
```

### Injecting Loggers

**Event Store Logging:**
```go
config := postgres.DefaultStoreConfig()
config.Logger = myLogger  // Your logger implementation
store := postgres.NewStore(config)

// Now store operations will log:
// - Append operations (with event counts, aggregate info, versions, positions)
// - Read operations (with query parameters and result counts)
// - Optimistic concurrency conflicts
```

**Projection Logging:**
```go
config := projection.DefaultProcessorConfig()
config.Logger = myLogger  // Your logger implementation
processor := projection.NewProcessor(db, store, &config)

// Now projection operations will log:
// - Processor start/stop events
// - Batch processing progress (processed/skipped counts)
// - Checkpoint updates
// - Handler errors with event details
```

### Zero-Overhead When Disabled

If you don't set a logger, logging is disabled with **zero overhead**:

```go
// No logger = no logging overhead
config := postgres.DefaultStoreConfig()  // Logger is nil by default
store := postgres.NewStore(config)
```

The library checks `if logger != nil` before any logging operations, ensuring no performance impact when logging is disabled.

### Integration Examples

**With Standard Library log:**
```go
type StdLogger struct{}

func (l *StdLogger) Debug(ctx context.Context, msg string, keyvals ...interface{}) {
    log.Printf("[DEBUG] %s %v", msg, keyvals)
}

func (l *StdLogger) Info(ctx context.Context, msg string, keyvals ...interface{}) {
    log.Printf("[INFO] %s %v", msg, keyvals)
}

func (l *StdLogger) Error(ctx context.Context, msg string, keyvals ...interface{}) {
    log.Printf("[ERROR] %s %v", msg, keyvals)
}
```

**With slog (Go 1.21+):**
```go
type SlogLogger struct {
    logger *slog.Logger
}

func (l *SlogLogger) Debug(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.DebugContext(ctx, msg, keyvals...)
}

func (l *SlogLogger) Info(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.InfoContext(ctx, msg, keyvals...)
}

func (l *SlogLogger) Error(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.ErrorContext(ctx, msg, keyvals...)
}
```

**With zap, zerolog, logrus:** Similar pattern - wrap your logger with the `es.Logger` interface.

See the [with-logging example](./examples/with-logging/) for a complete demonstration.

## Testing

### Unit Tests

```bash
go test ./...
```

### Integration Tests

Integration tests require PostgreSQL:

```bash
# Start PostgreSQL
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=pupsourcing_test postgres:16

# Run integration tests
go test -tags=integration ./es/adapters/postgres/integration_test/... ./es/projection/integration_test/...
```

## Database Schema

### Events Table

```sql
CREATE TABLE events (
    global_position BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    event_id UUID NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    event_version INT NOT NULL DEFAULT 1,
    payload BYTEA NOT NULL,
    trace_id UUID,
    correlation_id UUID,
    causation_id UUID,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (aggregate_type, aggregate_id, aggregate_version)
);
```

### Aggregate Heads Table

```sql
CREATE TABLE aggregate_heads (
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_type, aggregate_id)
);
```

This table tracks the current version of each aggregate, providing O(1) version lookups and eliminating expensive MAX() queries on the events table.

### Key Design Decisions

- **BYTEA for payload**: Supports any serialization format (JSON, Protobuf, etc.)
- **BIGSERIAL for global_position**: Ensures globally ordered event log
- **UUID for event_id**: Guarantees uniqueness in distributed scenarios
- **aggregate_version**: Enables optimistic concurrency control
- **aggregate_heads table**: Efficient O(1) version tracking

## Documentation

Comprehensive documentation is available in the [`docs/`](./docs) directory:

- **[Getting Started](./docs/getting-started.md)** - Installation, setup, and first steps
- **[Core Concepts](./docs/core-concepts.md)** - Understanding event sourcing with pupsourcing
- **[Projections & Scaling](./docs/scaling.md)** - Advanced projection patterns and horizontal scaling
- **[Industry Alignment](./docs/industry-alignment.md)** - Comparison with Kafka, EventStoreDB, Axon
- **[Deployment Guide](./docs/deployment.md)** - Production deployment patterns (coming soon)

## Examples

Complete, runnable examples demonstrating various patterns:

- **[Single Worker](./examples/single-worker/)** - Simplest pattern (1 projection, 1 worker)
- **[Partitioned](./examples/partitioned/)** - Horizontal scaling across multiple processes
- **[Worker Pool](./examples/worker-pool/)** - Multiple workers in the same process
- **[Multiple Projections](./examples/multiple-projections/)** - Running different projections concurrently
- **[Scaling](./examples/scaling/)** - Dynamic scaling from 1→N workers
- **[Stop/Resume](./examples/stop-resume/)** - Checkpoint reliability demonstration
- **[With Logging](./examples/with-logging/)** - Observability and debugging with custom loggers
- **[Basic](./examples/basic/)** - Original simple example

See the [examples README](./examples/README.md) for detailed instructions.

## Project Structure

```
.
├── es/                      # Core event sourcing packages
│   ├── event.go            # Core event types
│   ├── dbtx.go             # Database transaction abstraction
│   ├── store/              # Event store interfaces
│   ├── projection/         # Projection processing
│   │   └── runner/         # Projection orchestration tooling
│   ├── adapters/
│   │   └── postgres/       # PostgreSQL implementation
│   └── migrations/         # Migration generation
├── cmd/
│   └── migrate-gen/        # Migration generator CLI
├── docs/                   # Comprehensive documentation
├── examples/               # Runnable examples (7 patterns)
└── pkg/                    # Public API
```

## What pupsourcing Does NOT Handle

pupsourcing is a **library, not a framework**. It intentionally does not include:

- ❌ Automatic scheduling or background daemons
- ❌ Built-in Kubernetes/systemd integration
- ❌ Global coordination services (leader election, service discovery)
- ❌ Opinionated deployment models
- ❌ Automatic projection discovery
- ❌ Built-in monitoring/metrics (bring your own)

Instead, pupsourcing provides:

- ✅ **Building blocks** for projection orchestration (runner package)
- ✅ **CLI-friendly helpers** for running projections
- ✅ **Database-based coordination** (checkpoints, no external services)
- ✅ **Explicit APIs** - you wire everything together
- ✅ **Flexible deployment** - works with any orchestration system

This design makes pupsourcing:
- Simple to understand and debug
- Easy to integrate into existing applications
- Free from deployment assumptions
- Suitable for any environment (local, Docker, k8s, systemd, etc.)

## Development

### Prerequisites

- Go 1.23 or later
- PostgreSQL 12+ (for integration tests)

### Running Tests

```bash
# Unit tests only
go test ./...

# With integration tests
go test -tags=integration ./...
```

### Linting

```bash
golangci-lint run
```

## Roadmap

Current scope (v1):
- ✅ Event store with PostgreSQL
- ✅ Projection processing
- ✅ Optimistic concurrency
- ✅ Horizontal scaling support
- ✅ Read API for aggregate streams
- ✅ Projection orchestration tooling (runner package)
- ✅ Comprehensive documentation and examples
- ✅ Observability hooks and logging

Future considerations:
- Snapshots for long-lived aggregates
- Additional adapters (MySQL, SQLite)
- Metrics and instrumentation hooks

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
