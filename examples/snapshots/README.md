# Snapshots Example

This example demonstrates how to use the built-in snapshot projection in pupsourcing.

## Overview

Snapshots provide a way to store the current state of aggregates, allowing for faster reconstitution without replaying all events. The snapshot projection automatically maintains snapshots by listening to all events and updating the snapshot for each aggregate.

## Setup

### 1. Generate Migrations

First, generate the standard event sourcing migration:

```bash
go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations
```

Then, generate the snapshots table migration:

```bash
go run github.com/getpup/pupsourcing/cmd/snapshots-migrate-gen -output migrations
```

This creates two migration files:
- `<timestamp>_init_event_sourcing.sql` - Events and checkpoints tables
- `<timestamp>_add_snapshots.sql` - Snapshots table

### 2. Apply Migrations

Apply both migrations to your database:

```bash
psql -d your_database -f migrations/<timestamp>_init_event_sourcing.sql
psql -d your_database -f migrations/<timestamp>_add_snapshots.sql
```

## Usage

### Running the Snapshot Projection

```go
package main

import (
    "context"
    "database/sql"
    "log"

    _ "github.com/lib/pq"

    "github.com/getpup/pupsourcing/es/adapters/postgres"
    "github.com/getpup/pupsourcing/es/adapters/postgres/projections"
    "github.com/getpup/pupsourcing/es/projection"
)

func main() {
    // Connect to database
    db, err := sql.Open("postgres", "your-connection-string")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Create store
    store := postgres.NewStore(postgres.DefaultStoreConfig())

    // Create snapshot projection
    snapshotProj, err := projections.NewSnapshotProjection(projections.DefaultSnapshotConfig())
    if err != nil {
        log.Fatal(err)
    }

    // Create processor
    processor := projection.NewProcessor(db, store, projection.DefaultProcessorConfig())

    // Run projection (blocks until context is cancelled)
    ctx := context.Background()
    if err := processor.Run(ctx, snapshotProj); err != nil {
        log.Fatal(err)
    }
}
```

### Custom Table Names

You can customize the snapshots table name:

```go
config := projections.SnapshotConfig{
    SnapshotsTable: "custom_snapshots",
}
snapshotProj, err := projections.NewSnapshotProjection(config)
if err != nil {
    log.Fatal(err)
}
```

**Note**: Table names are validated to prevent SQL injection. Only alphanumeric characters and underscores are allowed.

## How It Works

1. **Event Listening**: The snapshot projection listens to all events from the event store
2. **State Updates**: For each event, it upserts a snapshot record for that aggregate
3. **Latest State Only**: The snapshot table maintains only the latest state per aggregate (keyed by `aggregate_type` and `aggregate_id`)
4. **Version Tracking**: Each snapshot tracks which `aggregate_version` it represents
5. **Automatic Updates**: As new events arrive, snapshots are automatically updated

## Snapshot Table Schema

```sql
CREATE TABLE snapshots (
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    payload BYTEA NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_type, aggregate_id)
);
```

## Benefits

- **Faster Reconstitution**: Load aggregate state from snapshot instead of replaying all events
- **Reduced Load**: Fewer database queries when reconstituting aggregates
- **Optional**: Snapshots don't interfere with existing append API or projections
- **Built-in**: No need to implement custom snapshot logic

## Notes

- Snapshots are updated asynchronously by the projection
- There's a small delay between event append and snapshot update
- The snapshot projection can be run alongside other projections
- Each snapshot stores the full aggregate state from the latest event
