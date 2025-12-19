# Projections & Scaling Guide

This guide covers everything you need to know about projections in pupsourcing, from basic concepts to production-scale deployments.

## Table of Contents

1. [What Are Projections?](#what-are-projections)
2. [Basic Projections](#basic-projections)
3. [Horizontal Scaling](#horizontal-scaling)
4. [Partitioning Strategy](#partitioning-strategy)
5. [Running Multiple Projections](#running-multiple-projections)
6. [Performance Tuning](#performance-tuning)
7. [Production Patterns](#production-patterns)

## What Are Projections?

Projections transform events into read models. Think of them as materialized views that are incrementally updated as events occur.

### Event Stream → Projection → Read Model

```
Events:               Projection:           Read Model:
  UserCreated    →    Update database   →  users table
  EmailVerified  →    Send notification →  email sent
  UserDeactivated →   Update cache      →  cache updated
```

### Why Projections?

- **CQRS**: Separate read and write models
- **Performance**: Optimized read models for queries
- **Flexibility**: Multiple views of the same data
- **Integration**: Trigger side effects (emails, webhooks)

## Basic Projections

### Implementing a Projection

```go
type UserCountProjection struct {
    db *sql.DB
}

func (p *UserCountProjection) Name() string {
    // Unique name for checkpoint tracking
    return "user_count"
}

func (p *UserCountProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    switch event.EventType {
    case "UserCreated":
        var payload UserCreated
        if err := json.Unmarshal(event.Payload, &payload); err != nil {
            return err
        }
        
        // Update read model in the same transaction
        _, err := tx.ExecContext(ctx,
            "INSERT INTO user_stats (user_id, email, created_at) VALUES ($1, $2, $3)"+
            "ON CONFLICT (user_id) DO NOTHING",
            event.AggregateID, payload.Email, event.CreatedAt)
        return err
        
    case "UserDeactivated":
        _, err := tx.ExecContext(ctx,
            "UPDATE user_stats SET active = false WHERE user_id = $1",
            event.AggregateID)
        return err
    }
    
    return nil
}
```

### Running a Projection

```go
import (
    "github.com/getpup/pupsourcing/es/projection"
)

proj := &UserCountProjection{db: db}
processor := projection.NewProcessor(db, store, projection.DefaultProcessorConfig())

// Run until context is cancelled
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

err := processor.Run(ctx, proj)
```

### Key Features

- **Automatic Checkpointing**: Position is saved after each batch
- **At-Least-Once Delivery**: Events may be reprocessed on crash (make projections idempotent)
- **Transactional**: Event processing and checkpoint update are atomic
- **Resumable**: Stops and resumes without data loss

## Horizontal Scaling

When a single projection worker can't keep up, scale horizontally with partitioning.

### Architecture

```
Event Stream:     Partitions:       Workers:
  Event 1    →    Partition 0  →   Worker 0
  Event 2    →    Partition 1  →   Worker 1
  Event 3    →    Partition 2  →   Worker 2
  Event 4    →    Partition 3  →   Worker 3
  Event 5    →    Partition 0  →   Worker 0
  ...
```

### Hash-Based Partitioning

Events are distributed based on `hash(aggregate_id) % total_partitions`:

```go
config := projection.DefaultProcessorConfig()
config.PartitionKey = 0      // This worker's partition (0-3)
config.TotalPartitions = 4   // Total number of workers

processor := projection.NewProcessor(db, store, config)
```

### Guarantees

✅ **Events for the same aggregate go to the same partition**
  - Maintains ordering within an aggregate
  - Safe for stateful projections

✅ **Even distribution across partitions**
  - Each partition handles ~25% of events (with 4 partitions)
  - Load balancing is automatic

✅ **Deterministic assignment**
  - Same aggregate always goes to same partition
  - Predictable behavior

❌ **No global ordering guarantee**
  - Events across different aggregates may be processed out of order
  - This is usually fine for most use cases

### Scaling Patterns

#### Pattern 1: Separate Processes

Run the same binary multiple times with different partition keys:

```bash
# Terminal 1
PARTITION_KEY=0 TOTAL_PARTITIONS=4 ./myapp process-projections

# Terminal 2
PARTITION_KEY=1 TOTAL_PARTITIONS=4 ./myapp process-projections

# Terminal 3
PARTITION_KEY=2 TOTAL_PARTITIONS=4 ./myapp process-projections

# Terminal 4
PARTITION_KEY=3 TOTAL_PARTITIONS=4 ./myapp process-projections
```

See [partitioned example](../examples/partitioned/) for details.

#### Pattern 2: Worker Pool (Single Process)

Run multiple partitions in the same process using goroutines:

```go
import "github.com/getpup/pupsourcing/es/projection/runner"

// Run 4 partitions in the same process
err := runner.RunProjectionPartitions(ctx, db, store, projection, 4)
```

**⚠️ Thread Safety Warning:** When using worker pools, all workers share the same projection instance. If your projection maintains state, it MUST be thread-safe:

```go
// ✅ Good: Thread-safe using atomic operations
type SafeProjection struct {
    count int64
}

func (p *SafeProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    atomic.AddInt64(&p.count, 1)  // Thread-safe
    return nil
}

// ✅ Good: Stateless (only updates database)
type StatelessProjection struct{}

func (p *StatelessProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    _, err := tx.ExecContext(ctx, "INSERT INTO read_model ...")  // Database handles concurrency
    return err
}

// ❌ Bad: Not thread-safe
type UnsafeProjection struct {
    count int  // Race condition!
}

func (p *UnsafeProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    p.count++  // NOT thread-safe!
    return nil
}
```

See [worker-pool example](../examples/worker-pool/) for details.
```

See [worker-pool example](../examples/worker-pool/) for details.

### When to Use Each Pattern

| Pattern | Best For | Pros | Cons |
|---------|----------|------|------|
| **Single Worker** | < 1K events/sec | Simple | Limited throughput |
| **Worker Pool** | 2-8 partitions, one machine | Easy deployment | Single point of failure |
| **Separate Processes** | > 8 partitions, multiple machines | Better isolation | More complex deployment |

## Partitioning Strategy

### Default: HashPartitionStrategy

Uses FNV-1a hash for deterministic, even distribution:

```go
type HashPartitionStrategy struct{}

func (HashPartitionStrategy) ShouldProcess(aggregateID string, partitionKey, totalPartitions int) bool {
    if totalPartitions <= 1 {
        return true
    }
    h := fnv.New32a()
    h.Write([]byte(aggregateID))
    partition := int(h.Sum32()) % totalPartitions
    return partition == partitionKey
}
```

### Custom Partitioning

Implement `PartitionStrategy` for custom logic:

```go
type RegionPartitionStrategy struct {
    region string
}

func (s RegionPartitionStrategy) ShouldProcess(aggregateID string, partitionKey, totalPartitions int) bool {
    // Custom logic - e.g., based on aggregate prefix
    // "us-" prefix goes to partition 0
    // "eu-" prefix goes to partition 1
    if strings.HasPrefix(aggregateID, "us-") {
        return partitionKey == 0
    } else if strings.HasPrefix(aggregateID, "eu-") {
        return partitionKey == 1
    }
    // Default to hash for others
    return HashPartitionStrategy{}.ShouldProcess(aggregateID, partitionKey, totalPartitions)
}
```

## Running Multiple Projections

### Pattern 1: Separate Processes

Run each projection in its own process for better isolation:

```bash
# Process 1
./myapp projection --name=user_counter

# Process 2
./myapp projection --name=email_sender

# Process 3
./myapp projection --name=analytics
```

### Pattern 2: Same Process

Run multiple projections in the same process:

```go
import "github.com/getpup/pupsourcing/es/projection/runner"

configs := []runner.ProjectionConfig{
    {
        Projection: &UserCounterProjection{},
        ProcessorConfig: projection.DefaultProcessorConfig(),
    },
    {
        Projection: &EmailSenderProjection{},
        ProcessorConfig: projection.DefaultProcessorConfig(),
    },
    {
        Projection: &AnalyticsProjection{},
        ProcessorConfig: projection.DefaultProcessorConfig(),
    },
}

err := runner.RunMultipleProjections(ctx, db, store, configs)
```

See [multiple-projections example](../examples/multiple-projections/) for details.

### Trade-offs

| Approach | Pros | Cons |
|----------|------|------|
| **Separate Processes** | Better isolation, independent scaling | More processes to manage |
| **Same Process** | Simpler deployment, shared resources | One failure affects all |

### When to Mix

You can run:
- Fast projections together
- Slow projections in separate processes with their own partitioning
- Critical projections isolated from non-critical ones

## Performance Tuning

### Batch Size

Controls how many events are processed per transaction:

```go
config := projection.DefaultProcessorConfig()
config.BatchSize = 100  // Default

// Larger batches: better throughput, higher latency
config.BatchSize = 1000

// Smaller batches: lower latency, more transactions
config.BatchSize = 10
```

**Guidelines:**
- Fast projections: 500-1000
- Slow projections: 10-50
- Default (100) works for most cases

### Connection Pooling

Configure database connection pool:

```go
db, _ := sql.Open("postgres", connStr)
db.SetMaxOpenConns(25)        // Limit concurrent connections
db.SetMaxIdleConns(5)         // Idle connections to keep
db.SetConnMaxLifetime(5 * time.Minute)
```

### Checkpoint Frequency

Checkpoint is updated after each batch. To reduce checkpoint writes:

```go
// Process more events per checkpoint
config.BatchSize = 500

// But consider: larger batches = more reprocessing on crash
```

### Monitoring

Track these metrics:

```sql
-- Projection lag
SELECT 
    projection_name,
    (SELECT MAX(global_position) FROM events) - last_global_position as lag
FROM projection_checkpoints;

-- Processing rate
SELECT 
    projection_name,
    last_global_position,
    updated_at
FROM projection_checkpoints
ORDER BY updated_at DESC;
```

## Production Patterns

### Pattern 1: Gradual Scaling

Start with 1 worker, scale up as needed:

```
Day 1: 1 worker (handles 100%)
Day 5: 2 workers (each handles ~50%)
Day 10: 4 workers (each handles ~25%)
Day 30: 8 workers (each handles ~12.5%)
```

See [scaling example](../examples/scaling/) for a demonstration.

### Pattern 2: Projection Prioritization

Run critical projections with more resources:

```go
// Critical: User data (4 workers)
runner.RunProjectionPartitions(ctx, db, store, &UserProjection{}, 4)

// Normal: Analytics (2 workers, separate process)
runner.RunProjectionPartitions(ctx, db, store, &AnalyticsProjection{}, 2)

// Low priority: Reports (1 worker, best-effort)
processor.Run(ctx, &ReportProjection{})
```

### Pattern 3: Hot/Cold Separation

Process recent events quickly, older events more slowly:

```go
// Hot path: Recent events (small batches, low latency)
hotConfig := projection.DefaultProcessorConfig()
hotConfig.BatchSize = 10
hotProcessor := projection.NewProcessor(db, store, hotConfig)

// Cold path: Historical events (large batches, high throughput)
coldConfig := projection.DefaultProcessorConfig()
coldConfig.BatchSize = 1000
coldProcessor := projection.NewProcessor(db, store, coldConfig)
```

### Pattern 4: Idempotent Projections

Always make projections idempotent to handle reprocessing:

```go
// ✅ Good: Idempotent insert
_, err := tx.ExecContext(ctx,
    "INSERT INTO users (id, email) VALUES ($1, $2)"+
    "ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email",
    userID, email)

// ❌ Bad: Non-idempotent increment
_, err := tx.ExecContext(ctx,
    "UPDATE counters SET count = count + 1")

// ✅ Better: Track processed events
_, err := tx.ExecContext(ctx,
    "INSERT INTO processed_events (event_id) VALUES ($1)"+
    "ON CONFLICT (event_id) DO NOTHING",
    eventID)
```

## Advanced Topics

### Projection Rebuilding

To rebuild a projection from scratch:

```sql
-- 1. Delete checkpoint
DELETE FROM projection_checkpoints WHERE projection_name = 'my_projection';

-- 2. Clear read model
TRUNCATE TABLE my_read_model;

-- 3. Restart projection - it will reprocess all events
```

### Snapshot Support

For long-lived aggregates, consider snapshots:

```go
// Read from snapshot position
snapshotVersion := int64(1000)
recentEvents, err := store.ReadAggregateStream(ctx, tx, "User", aggregateID, &snapshotVersion, nil)
```

### Error Handling

```go
func (p *MyProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    // Transient errors: return error to retry
    if err := someOperation(); err != nil {
        return fmt.Errorf("transient error: %w", err)
    }
    
    // Permanent errors: log and skip
    if err := validate(event); err != nil {
        log.Printf("Invalid event %s: %v", event.EventID, err)
        return nil  // Skip this event
    }
    
    return nil
}
```

## See Also

- [Getting Started](./getting-started.md) - Basic setup
- [Scaling Example](../examples/scaling/) - Dynamic scaling demonstration
- [Deployment Guide](./deployment.md) - Production deployment patterns
- [Industry Alignment](./industry-alignment.md) - Comparison with other systems
