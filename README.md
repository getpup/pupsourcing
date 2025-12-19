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
- ✅ **Projection System**: Pull-based event processing with checkpoints
- ✅ **Horizontal Scaling**: Deterministic hash-based partitioning
- ✅ **Migration Generation**: SQL migrations via `go generate`
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

### 3. Process Events with Projections

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
processor := projection.NewProcessor(db, store, projection.DefaultProcessorConfig())

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
type EmailNotificationProjection struct {
    mailer *Mailer
}

func (p *EmailNotificationProjection) Name() string {
    return "email_notifications"
}

func (p *EmailNotificationProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType == "UserCreated" {
        var user UserCreated
        json.Unmarshal(event.Payload, &user)
        return p.mailer.SendWelcome(user.Email)
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

processor := projection.NewProcessor(db, store, config)
```

Events for the same aggregate always go to the same partition, maintaining ordering guarantees.

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

## Examples

See the [examples](./examples) directory for complete working examples:

- [Basic Usage](./examples/basic/main.go) - Simple event append and projection

## Project Structure

```
.
├── es/                   # Core event sourcing packages
│   ├── store/           # Event store abstractions
│   ├── projection/      # Projection processing
│   ├── adapters/        # Database adapters
│   │   └── postgres/    # PostgreSQL adapter
│   └── migrations/      # Migration generation
├── cmd/
│   └── migrate-gen/     # Migration generator CLI
├── examples/            # Example applications
└── pkg/                 # Public API
```

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

Future considerations:
- Read API for aggregate streams
- Snapshots for long-lived aggregates
- Additional adapters (MySQL, SQLite)
- Observability hooks

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

TBD
