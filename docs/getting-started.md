# Getting Started

This guide covers installation, setup, and creating your first event-sourced application with pupsourcing.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Complete Example](#complete-example)
- [Next Steps](#next-steps)
- [Common Patterns](#common-patterns)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- Go 1.23 or later
- PostgreSQL 12+ (or SQLite for development/testing)

## Installation

```bash
go get github.com/getpup/pupsourcing
```

Choose a database driver:
```bash
# PostgreSQL (recommended for production)
go get github.com/lib/pq

# SQLite (ideal for development/testing)
go get modernc.org/sqlite

# MySQL/MariaDB
go get github.com/go-sql-driver/mysql
```

## Quick Start

### 1. Generate Database Schema

Generate SQL migrations for your chosen database:

```bash
go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

Or use `go generate`:

```go
//go:generate go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

This creates SQL migration files with:
- Events table with proper indexes
- Aggregate heads table for version tracking
- Projection checkpoints table

### 2. Apply Schema Migrations

Apply the generated migrations using your preferred migration tool (golang-migrate, goose, etc.).

### 3. Initialize Database Connection

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
)

db, err := sql.Open("postgres", 
    "host=localhost port=5432 user=postgres password=postgres dbname=myapp sslmode=disable")
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### 4. Create Event Store

```go
import (
    "github.com/getpup/pupsourcing/es/adapters/postgres"
)

store := postgres.NewStore(postgres.DefaultStoreConfig())
```

### 5. Append Your First Event

```go
import (
    "github.com/getpup/pupsourcing/es"
    "github.com/google/uuid"
    "time"
)

// Define your event payload
type UserCreated struct {
    Email string `json:"email"`
    Name  string `json:"name"`
}

// Marshal to JSON
payload, _ := json.Marshal(UserCreated{
    Email: "alice@example.com",
    Name:  "Alice Smith",
})

// Create event
aggregateID := uuid.New().String() // In practice, this comes from your domain/business logic
events := []es.Event{
    {
        BoundedContext: "Identity",  // Required: scope events to bounded context
        AggregateType:  "User",
        AggregateID:    aggregateID,
        EventID:        uuid.New(),
        EventType:      "UserCreated",
        EventVersion:   1,
        Payload:        payload,
        Metadata:       []byte(`{}`),
        CreatedAt:      time.Now(),
    },
}

// Append in a transaction
ctx := context.Background()
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

result, err := store.Append(ctx, tx, es.NoStream(), events)
if err != nil {
    log.Fatal(err)
}

if err := tx.Commit(); err != nil {
    log.Fatal(err)
}

fmt.Printf("Event appended at position: %d\n", result.GlobalPositions[0])
fmt.Printf("Aggregate version: %d\n", result.ToVersion())
```

### 6. Read Events

```go
// Read all events for an aggregate
aggregateID := "550e8400-e29b-41d4-a716-446655440000" // Use the actual aggregate ID
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

stream, err := store.ReadAggregateStream(ctx, tx, "Identity", "User", aggregateID, nil, nil)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Aggregate version: %d\n", stream.Version())

for _, event := range stream.Events {
    fmt.Printf("Event: %s (version %d)\n", event.EventType, event.AggregateVersion)
}
```

### 7. Create a Projection

Create a scoped projection that only receives User events:

```go
import (
    "github.com/getpup/pupsourcing/es/projection"
)

type UserCountProjection struct {
    count int
}

func (p *UserCountProjection) Name() string {
    return "user_count"
}

// AggregateTypes makes this a scoped projection
func (p *UserCountProjection) AggregateTypes() []string {
    return []string{"User"}  // Only receives User events
}

// BoundedContexts filters by context - receives only Identity context events
func (p *UserCountProjection) BoundedContexts() []string {
    return []string{"Identity"}
}

func (p *UserCountProjection) Handle(_ context.Context, event es.PersistedEvent) error {
    if event.EventType == "UserCreated" {
        p.count++
        fmt.Printf("User count: %d\n", p.count)
    }
    return nil
}
```

### 8. Run the Projection

```go
proj := &UserCountProjection{}
config := projection.DefaultProcessorConfig()

// Use adapter-specific processor
store := postgres.NewStore(postgres.DefaultStoreConfig())
processor := postgres.NewProcessor(db, store, &config)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Run until context is cancelled
err := processor.Run(ctx, proj)
```

## Complete Example

See the [complete working example](../examples/single-worker/main.go) that ties everything together.

## Next Steps

- Learn [Core Concepts](./core-concepts.md) to understand event sourcing with pupsourcing
- Explore [Projections & Scaling](./scaling.md) to build read models
- See [Scaling Guide](./scaling.md) for production deployments
- Browse [Examples](../examples/) for more patterns

## Common Patterns

### Appending Multiple Events

```go
userID := uuid.New().String()
events := []es.Event{
    {
        BoundedContext: "Identity",
        AggregateType:  "User",
        AggregateID:    userID,
        EventID:        uuid.New(),
        EventType:      "UserCreated",
        EventVersion:   1,
        Payload:        payload1,
        Metadata:       []byte(`{}`),
        CreatedAt:      time.Now(),
    },
    {
        BoundedContext: "Identity",
        AggregateType:  "User",
        AggregateID:    userID,  // Same aggregate
        EventID:        uuid.New(),
        EventType:      "EmailVerified",
        EventVersion:   1,
        Payload:        payload2,
        Metadata:       []byte(`{}`),
        CreatedAt:      time.Now(),
    },
}

// Both events appended atomically
result, err := store.Append(ctx, tx, es.NoStream(), events)
```

### Handling Version Conflicts

```go
result, err := store.Append(ctx, tx, es.Exact(currentVersion), events)
if errors.Is(err, store.ErrOptimisticConcurrency) {
    // Another transaction modified this aggregate
    // Retry the entire operation
    tx.Rollback()
    // ... retry logic
}
```

### Reading Event Ranges

```go
aggregateID := uuid.New().String()

// Read from version 5 onwards (e.g., after loading a snapshot)
fromVersion := int64(5)
stream, err := store.ReadAggregateStream(ctx, tx, "Identity", "User", aggregateID, &fromVersion, nil)

// Read a specific range
toVersion := int64(10)
stream, err := store.ReadAggregateStream(ctx, tx, "Identity", "User", aggregateID, &fromVersion, &toVersion)
```

## Troubleshooting

### Connection Errors

Ensure PostgreSQL is running:
```bash
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16
```

### Migration Issues

Verify migrations were applied:
```sql
\d events
\d aggregate_heads
\d projection_checkpoints
```

### Event Not Appearing

Check transaction was committed:
```go
tx, _ := db.BeginTx(ctx, nil)
store.Append(ctx, tx, es.NoStream(), events)
tx.Commit() // Don't forget this!
```

### Projection Not Processing

Verify events exist:
```sql
SELECT COUNT(*) FROM events;
```

Check projection checkpoint:
```sql
SELECT * FROM projection_checkpoints WHERE projection_name = 'your_projection';
```

## Resources

- [Core Concepts](./core-concepts.md) - Understand the fundamentals
- [API Reference](./api-reference.md) - Complete API documentation
- [Examples](../examples/) - Working code examples
