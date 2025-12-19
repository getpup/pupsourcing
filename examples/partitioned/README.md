# Partitioned Projection Example

This example demonstrates horizontal scaling using partitioning across multiple processes. Each process handles a subset of events based on aggregate ID hashing.

## What It Does

- Runs the same projection binary multiple times with different partition keys
- Each process handles a disjoint subset of events
- Events for the same aggregate always go to the same partition (ordering guarantee)
- Coordination happens via database checkpoints (no external coordination needed)

## Running the Example

1. Start PostgreSQL:
```bash
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=pupsourcing_example postgres:16
```

2. Run migrations (from the basic example directory):
```bash
cd ../basic
go generate
# Apply the generated migration to your database
```

3. Run multiple instances in separate terminals:

**Terminal 1:**
```bash
PARTITION_KEY=0 go run main.go
```

**Terminal 2:**
```bash
PARTITION_KEY=1 go run main.go
```

**Terminal 3:**
```bash
PARTITION_KEY=2 go run main.go
```

**Terminal 4:**
```bash
PARTITION_KEY=3 go run main.go
```

Alternatively, use command-line flags:
```bash
go run main.go --partition-key=0 --total-partitions=4
```

## Key Concepts

### Partitioning Strategy

- Uses FNV-1a hash of aggregate ID to determine partition
- Deterministic: same aggregate always goes to same partition
- Even distribution across partitions
- Maintains ordering within each aggregate

### Scaling Safely

**To scale from 1 → N workers:**
1. Start worker 0 (processes everything)
2. Add worker 1 (both catch up in parallel)
3. Add worker 2, 3, etc.
4. Each new worker will catch up independently

**To scale from N → M workers:**
- If M > N: Just add new workers with appropriate partition keys
- If M < N: Stop unnecessary workers - no data loss
- Each partition maintains its own checkpoint

### No External Coordination

- No leader election needed
- No distributed locks
- No service discovery
- Just database checkpoints per partition

## Configuration

### Via Environment Variables
```bash
export PARTITION_KEY=0
export DATABASE_URL="postgres://..."
go run main.go
```

### Via Command-Line Flags
```bash
go run main.go --partition-key=0 --total-partitions=4
```

## When to Use This Pattern

- High event volumes (> 1000 events/sec)
- When single-worker projection can't keep up
- When you need better throughput without complex infrastructure
- Production deployments across multiple servers/containers

## Monitoring

Each worker logs:
- Its partition key
- Number of events processed on that partition
- Which specific events it handles

This makes it easy to verify that partitioning is working correctly.

## Production Considerations

1. **Database Connection Pooling**: Each worker has its own connection pool
2. **Restart Safety**: Each worker can restart independently without affecting others
3. **Adding Workers**: Adding new partitions requires reconfiguring all workers
4. **Removing Workers**: Simply stop the worker - checkpoint remains for future restarts

## See Also

- `../worker-pool` - Run multiple partitions in a single process
- `../scaling` - Demonstrates scaling from 1 → N workers
