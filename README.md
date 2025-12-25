# pupsourcing

[![CI](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml/badge.svg)](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/getpup/pupsourcing)](https://goreportcard.com/report/github.com/getpup/pupsourcing)
[![GoDoc](https://godoc.org/github.com/getpup/pupsourcing?status.svg)](https://godoc.org/github.com/getpup/pupsourcing)

A production-ready Event Sourcing library for Go with clean architecture principles.

## Overview

pupsourcing provides minimal, reliable infrastructure for event sourcing in Go applications. State changes are stored as an immutable sequence of events, providing a complete audit trail and enabling event replay, temporal queries, and flexible read models.

## Key Features

- **Clean Architecture** - Core interfaces are datastore-agnostic; no "infrastructure creep" into your domain model (no annotations, no framework-specific base classes)
- **Multiple Database Adapters** - PostgreSQL, SQLite, and MySQL/MariaDB
- **Optimistic Concurrency** - Automatic conflict detection via database constraints
- **Projection System** - Pull-based event processing with checkpoints
- **Horizontal Scaling** - Hash-based partitioning for projection workers
- **Code Generation** - Optional tool for strongly-typed domain event mapping
- **Minimal Dependencies** - Go standard library plus database driver

## Installation

```bash
go get github.com/getpup/pupsourcing
```

Choose your database driver:
```bash
# PostgreSQL (recommended for production)
go get github.com/lib/pq

# SQLite (embedded, ideal for testing)
go get modernc.org/sqlite

# MySQL/MariaDB
go get github.com/go-sql-driver/mysql
```

## Quick Start

### 1. Generate Database Schema

```bash
go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

Apply the generated migrations using your preferred migration tool.

### 2. (Optional) Generate Event Mapping Code

If you want type-safe mapping between domain events and event sourcing types:

```bash
go run github.com/getpup/pupsourcing/cmd/eventmap-gen \
  -input internal/domain/events \
  -output internal/infrastructure/generated
```

See [Event Mapping Documentation](./docs/eventmap-gen.md) for details.

### 3. Append Events

```go
import (
    "github.com/getpup/pupsourcing/es"
    "github.com/getpup/pupsourcing/es/adapters/postgres"
    "github.com/google/uuid"
)

// Create store
store := postgres.NewStore(postgres.DefaultStoreConfig())

// Create event
event := es.Event{
    AggregateType: "User",
    AggregateID:   uuid.New().String(),  // String-based ID for flexibility
    EventID:       uuid.New(),
    EventType:     "UserCreated",
    EventVersion:  1,
    Payload:       []byte(`{"email":"alice@example.com","name":"Alice"}`),
    Metadata:      []byte(`{}`),
    CreatedAt:     time.Now(),
}

// Append within transaction with optimistic concurrency control
tx, _ := db.BeginTx(ctx, nil)
// Use NoStream() for creating a new aggregate
result, err := store.Append(ctx, tx, es.NoStream(), []es.Event{event})
if err != nil {
    tx.Rollback()
    log.Fatal(err)
}
tx.Commit()

// Access the result
fmt.Printf("Events appended at positions: %v\n", result.GlobalPositions)
fmt.Printf("Aggregate is now at version: %d\n", result.ToVersion())
```

### 4. Read Aggregate Streams

```go
// Read all events for an aggregate
aggregateID := uuid.New().String()
stream, err := store.ReadAggregateStream(ctx, tx, "User", aggregateID, nil, nil)

// Access stream information
fmt.Printf("Stream has %d events\n", stream.Len())
fmt.Printf("Current aggregate version: %d\n", stream.Version())

// Read from a specific version
fromVersion := int64(5)
stream, err = store.ReadAggregateStream(ctx, tx, "User", aggregateID, &fromVersion, nil)

// Process events
for _, event := range stream.Events {
    // Handle event
}
```

### 5. Run Projections

Projections transform events into query-optimized read models. Use **scoped projections** for read models that only care about specific aggregate types, or **global projections** for integration publishers that need all events.

```go
import "github.com/getpup/pupsourcing/es/projection"

// Scoped projection - only receives User aggregate events
type UserReadModelProjection struct{}

func (p *UserReadModelProjection) Name() string {
    return "user_read_model"
}

// AggregateTypes filters events by aggregate type
func (p *UserReadModelProjection) AggregateTypes() []string {
    return []string{"User"}  // Only receives User events
}

func (p *UserReadModelProjection) Handle(ctx context.Context, event es.PersistedEvent) error {
    // Update read model based on User events only
    // Projection manages its own persistence
    return nil
}

// Run projection with adapter-specific processor
store := postgres.NewStore(postgres.DefaultStoreConfig())
config := projection.DefaultProcessorConfig()
processor := postgres.NewProcessor(db, store, &config)
err := processor.Run(ctx, &UserReadModelProjection{})
```

## Documentation

Comprehensive documentation is available in the [`docs/`](./docs) directory:

- **[Getting Started](./docs/getting-started.md)** - Installation, setup, and first steps
- **[Core Concepts](./docs/core-concepts.md)** - Understanding event sourcing principles
- **[Database Adapters](./docs/adapters.md)** - Choosing the right database
- **[Projections & Scaling](./docs/scaling.md)** - Horizontal scaling and production patterns
- **[Event Mapping Code Generation](./docs/eventmap-gen.md)** - Type-safe domain event mapping
- **[Observability](./docs/observability.md)** - Logging, tracing, and monitoring
- **[API Reference](./docs/api-reference.md)** - Complete API documentation

## Examples

Complete runnable examples are available in the [`examples/`](./examples) directory:

- **[Single Worker](./examples/single-worker/)** - Basic projection pattern
- **[Multiple Projections](./examples/multiple-projections/)** - Running different projections concurrently
- **[Worker Pool](./examples/worker-pool/)** - Multiple workers in the same process
- **[Partitioned](./examples/partitioned/)** - Horizontal scaling across processes
- **[With Logging](./examples/with-logging/)** - Observability and debugging

See the [examples README](./examples/README.md) for more details.

## Development

### Running Tests

**Unit tests:**
```bash
make test-unit
```

**Integration tests locally (requires Docker):**
```bash
make test-integration-local
```

This command automatically:
1. Starts PostgreSQL and MySQL containers via `docker compose`
2. Runs all integration tests
3. Cleans up containers

**Manual integration testing:**
```bash
# Start databases
docker compose up -d

# Run integration tests
make test-integration

# Stop databases
docker compose down
```

## Contributing

Contributions are welcome! Please submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
