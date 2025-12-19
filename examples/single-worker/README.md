# Single Worker Example

This example demonstrates the simplest possible projection setup: one projection running on a single worker with no partitioning.

## What It Does

- Runs a single projection that counts user creation events
- Processes events sequentially from the event store
- Gracefully handles shutdown signals (Ctrl+C)
- Automatically resumes from the last checkpoint on restart

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

3. Run the example:
```bash
go run main.go
```

## Key Concepts

- **Checkpoint Tracking**: The projection automatically tracks its position in the event stream
- **Resumability**: Stop and restart the process - it picks up where it left off
- **Simplicity**: This is the baseline pattern - perfect for getting started

## When to Use This Pattern

- Small to medium event volumes (< 1000 events/sec)
- Simple projections that can keep up with event production
- Development and testing environments
- Applications where projection lag is not critical

## Scaling Up

When this pattern is not sufficient, see:
- `../partitioned` - Scale with partitioning in separate processes
- `../worker-pool` - Scale with partitions in the same process
