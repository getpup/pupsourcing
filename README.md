# pupsourcing

[![CI](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml/badge.svg)](https://github.com/getpup/pupsourcing/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/getpup/pupsourcing)](https://goreportcard.com/report/github.com/getpup/pupsourcing)
[![GoDoc](https://godoc.org/github.com/getpup/pupsourcing?status.svg)](https://godoc.org/github.com/getpup/pupsourcing)

A production-ready Event Sourcing library for Go with clean architecture principles.

## Overview

pupsourcing provides minimal, reliable infrastructure for event sourcing in Go applications. State changes are stored as an immutable sequence of events, providing a complete audit trail and enabling event replay, temporal queries, and flexible read models.

## Key Features

- **Clean Architecture** - Core interfaces are datastore-agnostic
- **Multiple Database Adapters** - PostgreSQL, SQLite, and MySQL/MariaDB
- **Optimistic Concurrency** - Automatic conflict detection via database constraints
- **Projection System** - Pull-based event processing with checkpoints
- **Horizontal Scaling** - Hash-based partitioning for projection workers
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

### 2. Append Events

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
    AggregateID:   uuid.New(),
    EventID:       uuid.New(),
    EventType:     "UserCreated",
    EventVersion:  1,
    Payload:       []byte(`{"email":"alice@example.com","name":"Alice"}`),
    Metadata:      []byte(`{}`),
    CreatedAt:     time.Now(),
}

// Append within transaction
tx, _ := db.BeginTx(ctx, nil)
positions, err := store.Append(ctx, tx, []es.Event{event})
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

// Read from a specific version
fromVersion := int64(5)
events, err = store.ReadAggregateStream(ctx, tx, "User", aggregateID, &fromVersion, nil)
```

### 4. Run Projections

```go
import "github.com/getpup/pupsourcing/es/projection"

// Implement Projection interface
type MyProjection struct{}

func (p *MyProjection) Name() string {
    return "my_projection"
}

func (p *MyProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    // Update read model based on event
    return nil
}

// Run projection
config := projection.DefaultProcessorConfig()
processor := projection.NewProcessor(db, store, &config)
err := processor.Run(ctx, &MyProjection{})
```

## Documentation

Comprehensive documentation is available in the [`docs/`](./docs) directory:

- **[Getting Started](./docs/getting-started.md)** - Installation, setup, and first steps
- **[Core Concepts](./docs/core-concepts.md)** - Understanding event sourcing principles
- **[Database Adapters](./docs/adapters.md)** - Choosing the right database
- **[Projections & Scaling](./docs/scaling.md)** - Horizontal scaling and production patterns
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

## Contributing

Contributions are welcome! Please submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
