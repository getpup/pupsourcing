# pupsourcing Examples

This directory contains comprehensive, runnable examples demonstrating how to use pupsourcing in various scenarios.

## Overview

Each example is self-contained and demonstrates a specific pattern or use case. Start with `single-worker` for the basics, then explore more advanced patterns.

## Examples

### 1. [Single Worker](./single-worker/)
**Difficulty:** Beginner  
**Best for:** Getting started, development, low-volume production

The simplest projection pattern - one projection, one worker, no partitioning.

**What you'll learn:**
- Basic projection setup
- Checkpoint tracking
- Graceful shutdown
- Event processing

**Run it:**
```bash
cd single-worker
go run main.go
```

### 2. [Partitioned](./partitioned/)
**Difficulty:** Intermediate  
**Best for:** High-volume production, horizontal scaling

Run the same projection across multiple processes with partitioning.

**What you'll learn:**
- Horizontal scaling across processes
- Hash-based partitioning
- Independent worker operation
- CLI-friendly configuration

**Run it:**
```bash
# Terminal 1
cd partitioned
PARTITION_KEY=0 go run main.go

# Terminal 2
PARTITION_KEY=1 go run main.go

# Terminal 3-4: same with PARTITION_KEY=2 and 3
```

### 3. [Worker Pool](./worker-pool/)
**Difficulty:** Intermediate  
**Best for:** Medium-scale production, single machine

Run multiple partitions of a projection in the same process using goroutines.

**What you'll learn:**
- In-process parallelism
- Thread-safe projections
- Runner package usage
- Resource sharing

**Run it:**
```bash
cd worker-pool
go run main.go --workers=4
```

### 4. [Multiple Projections](./multiple-projections/)
**Difficulty:** Intermediate  
**Best for:** CQRS applications, multiple read models

Run different projections concurrently in the same process.

**What you'll learn:**
- Running multiple projections
- Independent checkpoints
- Different batch sizes per projection
- Runner configuration

**Run it:**
```bash
cd multiple-projections
go run main.go
```

### 5. [Scaling](./scaling/)
**Difficulty:** Advanced  
**Best for:** Understanding scaling mechanics

Demonstrates how to safely scale from 1 worker to N workers dynamically.

**What you'll learn:**
- Adding workers incrementally
- Independent catch-up
- Load distribution
- Production scaling patterns

**Run it:**
```bash
cd scaling

# Append events once
go run main.go --worker-id=0 --append

# Start workers incrementally
WORKER_ID=0 go run main.go  # Terminal 1
WORKER_ID=1 go run main.go  # Terminal 2
WORKER_ID=2 go run main.go  # Terminal 3
WORKER_ID=3 go run main.go  # Terminal 4
```

### 6. [Stop and Resume](./stop-resume/)
**Difficulty:** Beginner  
**Best for:** Understanding checkpoint reliability

Shows that projections can be stopped and resumed without data loss.

**What you'll learn:**
- Checkpoint persistence
- Graceful shutdown
- Resumption behavior
- Status checking

**Run it:**
```bash
cd stop-resume

# Append events
go run main.go --mode=append --events=20

# Process some events, then Ctrl+C
go run main.go --mode=process

# Check status
go run main.go --mode=status

# Resume processing
go run main.go --mode=process
```

### 7. [Basic](./basic/)
**Difficulty:** Beginner  
**Best for:** Initial setup and exploration

The original simple example showing basic event appending and projection.

**Run it:**
```bash
cd basic
go generate  # Generate migrations
go run main.go
```

## Quick Start

### Prerequisites

All examples require:
- Go 1.23 or later
- PostgreSQL 12+ running locally

Start PostgreSQL:
```bash
docker run -d -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=pupsourcing_example \
  postgres:16
```

### Running Examples

1. Navigate to an example directory
2. Generate and apply migrations (first time only):
   ```bash
   cd basic
   go generate
   psql -h localhost -U postgres -d pupsourcing_example -f ../../migrations/init.sql
   ```
3. Run the example:
   ```bash
   go run main.go
   ```

## Learning Path

**New to event sourcing?**
1. Start with [Single Worker](./single-worker/)
2. Read [Core Concepts](../docs/core-concepts.md)
3. Try [Stop and Resume](./stop-resume/)

**Need to scale projections?**
1. Review [Worker Pool](./worker-pool/) for single-machine scaling
2. Study [Partitioned](./partitioned/) for multi-machine scaling
3. Understand [Scaling](./scaling/) for dynamic scaling

**Building a CQRS application?**
1. Explore [Multiple Projections](./multiple-projections/)
2. Read [Scaling Guide](../docs/scaling.md)
3. See [Industry Alignment](../docs/industry-alignment.md)

## Key Concepts

### Events are Immutable

Events are value objects that become immutable once persisted. They don't have identity until the store assigns a `global_position`.

### Transaction Control

You control transaction boundaries. This allows you to combine event appending with other database operations atomically.

### Optimistic Concurrency

Version conflicts are detected automatically via database constraints. If concurrent modifications occur, one will fail with `ErrOptimisticConcurrency`.

### Projections

Projections read events sequentially and maintain progress via checkpoints. They can be stopped and resumed without losing position.

### Horizontal Scaling

Multiple projection processors can run in parallel using hash-based partitioning. Events for the same aggregate always go to the same partition, maintaining ordering.

### Checkpoints

Each projection maintains its own checkpoint in the database. Checkpoints are updated atomically with event processing, ensuring exactly-once semantics.

## Common Patterns

### Pattern: Graceful Shutdown

All examples demonstrate graceful shutdown:
```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

go func() {
    <-sigChan
    cancel()
}()

err := processor.Run(ctx, projection)
```

### Pattern: CLI Configuration

Examples show CLI-friendly configuration:
```go
partitionKey := flag.Int("partition-key", -1, "Partition key")
totalPartitions := flag.Int("total-partitions", 4, "Total partitions")
flag.Parse()

// Also support env vars
if *partitionKey == -1 {
    if envKey := os.Getenv("PARTITION_KEY"); envKey != "" {
        *partitionKey, _ = strconv.Atoi(envKey)
    }
}
```

### Pattern: Idempotent Projections

Make projections safe for reprocessing:
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    _, err := tx.ExecContext(ctx,
        "INSERT INTO read_model (id, data) VALUES ($1, $2)"+
        "ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data",
        id, data)
    return err
}
```

## Troubleshooting

### PostgreSQL Connection Errors

Ensure PostgreSQL is running:
```bash
docker ps  # Check if container is running
psql -h localhost -U postgres -d pupsourcing_example -c "SELECT 1"
```

### Migrations Not Applied

Generate and apply migrations:
```bash
cd basic && go generate
psql -h localhost -U postgres -d pupsourcing_example -f ../../migrations/init.sql
```

### Projection Not Processing

Check events exist:
```sql
SELECT COUNT(*) FROM events;
```

Check projection checkpoint:
```sql
SELECT * FROM projection_checkpoints;
```

## Next Steps

- Read [Getting Started Guide](../docs/getting-started.md)
- Study [Core Concepts](../docs/core-concepts.md)
- Review [Scaling Guide](../docs/scaling.md)
- Explore [API Reference](../docs/api-reference.md)

## Contributing

Found a bug or have an example idea? Please open an issue or submit a PR!
