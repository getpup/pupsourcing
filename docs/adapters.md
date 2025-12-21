# Database Adapters

pupsourcing provides production-ready adapters for PostgreSQL, SQLite, and MySQL/MariaDB. All adapters implement identical interfaces, enabling seamless database migration.

## Table of Contents

- [Architecture](#architecture)
- [PostgreSQL Adapter](#postgresql-adapter)
- [SQLite Adapter](#sqlite-adapter)
- [MySQL/MariaDB Adapter](#mysqlmariadb-adapter)
- [Adapter Comparison](#adapter-comparison)
- [Switching Adapters](#switching-adapters)
- [Configuration Options](#configuration-options)
- [Testing Recommendations](#testing-recommendations)
- [Performance Considerations](#performance-considerations)

## Architecture

Each adapter implements three core interfaces:
- **`store.EventStore`** - Append events with optimistic concurrency
- **`store.EventReader`** - Sequential event reading by global position
- **`store.AggregateStreamReader`** - Aggregate-specific event retrieval

This design maintains database-agnostic application code and ensures consistent projection behavior across all adapters.

## PostgreSQL Adapter

**Package:** `github.com/getpup/pupsourcing/es/adapters/postgres`  
**Driver:** `github.com/lib/pq`  
**Status:** Production-ready ✅

### Key Features

- Native UUID type for efficient storage and indexing
- JSONB metadata with advanced querying capabilities
- Excellent concurrent write performance
- O(1) version lookups via `aggregate_heads` table
- Optimistic concurrency via unique constraints

### Data Types

| Field | Type | Purpose |
|-------|------|---------|
| `global_position` | `BIGSERIAL` | Auto-incrementing, globally ordered |
| `aggregate_id` | `TEXT` | String-based aggregate identifier (UUIDs, emails, custom IDs) |
| `event_id` | `UUID` | Unique event identifier |
| `payload` | `BYTEA` | Binary event data |
| `metadata` | `JSONB` | Queryable structured metadata |
| `created_at` | `TIMESTAMPTZ` | Timezone-aware timestamp |

### Advanced Capabilities

- Full-text search on JSONB metadata
- Partial and expression indexes
- LISTEN/NOTIFY for real-time notifications
- Row-level security for multi-tenancy

### Ideal For

- High-availability production systems
- Multi-tenant applications
- High concurrent write workloads
- Advanced metadata querying requirements

### Usage

```go
import (
    "github.com/getpup/pupsourcing/es/adapters/postgres"
    _ "github.com/lib/pq"
)

store := postgres.NewStore(postgres.DefaultStoreConfig())

// Use with *sql.DB or *sql.Tx
db, _ := sql.Open("postgres", connString)
tx, _ := db.BeginTx(ctx, nil)
positions, err := store.Append(ctx, tx, es.NoStream(), events)
tx.Commit()
```
err := migrations.GeneratePostgres(&config)
```

---

### SQLite Adapter

**Package:** `github.com/getpup/pupsourcing/es/adapters/sqlite`  
**Driver:** `modernc.org/sqlite` (pure Go, no CGO required)  
**Status:** Production-ready for embedded use ✅

#### Capabilities

- **Embedded Database**: No separate server process required
- **Zero Configuration**: Works out of the box
- **ACID Transactions**: Full transaction support with WAL mode
- **Concurrent Reads**: Multiple readers with WAL journaling mode
- **Optimistic Concurrency**: Enforced via unique constraints
- **Aggregate Version Tracking**: O(1) version lookups via `aggregate_heads` table

#### Data Types

| Field | SQLite Type | Notes |
|-------|-------------|-------|
| `global_position` | `INTEGER` with `AUTOINCREMENT` | Auto-incrementing primary key |
| `aggregate_id` | `TEXT` | UUID stored as string |
| `event_id` | `TEXT` | UUID stored as string with unique constraint |
| `payload` | `BLOB` | Binary data, supports any serialization format |
| `metadata` | `TEXT` | JSON as text (SQLite 3.38+ has JSON functions) |
| `created_at` | `TEXT` | ISO 8601 datetime strings |

#### Unique Features

- **Single file database** - Easy backup and deployment
- **Cross-platform** - Works on all platforms Go supports
- **In-memory mode** - Excellent for testing (`":memory:"` database)
- **JSON1 extension** - Built-in JSON query functions
- **Pure Go driver** - No CGO dependencies with modernc.org/sqlite

#### Best For

- Testing and development environments
- CI/CD pipelines (no external database required)
- Embedded applications
- Desktop applications
- Small to medium deployments
- Local-first applications
- Edge computing scenarios

#### Limitations

- **Write concurrency**: Limited to one writer at a time (even with WAL mode)
- **Network access**: Requires file system access, no network protocol
- **Scalability**: Best for single-instance deployments
- **Aggregate ID storage**: Stored as TEXT (36+ bytes for UUIDs, suitable for all string identifiers)

#### Example Usage

```go
import (
    "github.com/getpup/pupsourcing/es/adapters/sqlite"
    _ "modernc.org/sqlite"
)

// Create store
store := sqlite.NewStore(sqlite.DefaultStoreConfig())

// Use with file-based database
db, _ := sql.Open("sqlite", "events.db")

// Enable WAL mode for better concurrency
db.Exec("PRAGMA journal_mode = WAL;")

// Use with transactions
tx, _ := db.BeginTx(ctx, nil)
positions, err := store.Append(ctx, tx, es.NoStream(), events)
tx.Commit()
```

#### Migration Generation

```go
import "github.com/getpup/pupsourcing/es/migrations"

config := migrations.DefaultConfig()
err := migrations.GenerateSQLite(&config)
```

#### Performance Tips

- **Enable WAL mode**: `PRAGMA journal_mode = WAL;` for better concurrency
- **Increase cache size**: `PRAGMA cache_size = -64000;` (64MB cache)
- **Use synchronous=NORMAL**: `PRAGMA synchronous = NORMAL;` for better write performance
- **Batch writes**: Commit multiple events in a single transaction

---

### MySQL/MariaDB Adapter

**Package:** `github.com/getpup/pupsourcing/es/adapters/mysql`  
**Driver:** `github.com/go-sql-driver/mysql`  
**Status:** Production-ready ✅

#### Capabilities

- **Binary UUID Storage**: Efficient `BINARY(16)` storage for UUIDs
- **JSON Metadata**: Native `JSON` type with indexing support
- **InnoDB Engine**: ACID transactions with MVCC concurrency
- **High Availability**: Supports replication and clustering
- **Optimistic Concurrency**: Enforced via unique constraints
- **Aggregate Version Tracking**: O(1) version lookups via `aggregate_heads` table

#### Data Types

| Field | MySQL Type | Notes |
|-------|-----------|-------|
| `global_position` | `BIGINT AUTO_INCREMENT` | Auto-incrementing primary key |
| `aggregate_id` | `VARCHAR(255)` | String-based aggregate identifier (UUIDs, emails, custom IDs) |
| `event_id` | `BINARY(16)` | UUID stored as 16-byte binary with unique constraint |
| `payload` | `BLOB` | Binary data, supports any serialization format |
| `metadata` | `JSON` | Native JSON type with validation |
| `created_at` | `TIMESTAMP(6)` | Microsecond precision timestamps |

#### Unique Features

- **JSON functions**: Rich set of JSON query and manipulation functions
- **Replication**: Built-in master-slave and group replication
- **Galera Cluster**: Multi-master synchronous replication
- **InnoDB**: Row-level locking for better concurrency
- **Flexible Identifiers**: Supports UUID strings, emails, and custom aggregate IDs

#### Best For

- Production applications with existing MySQL infrastructure
- Applications requiring high availability and replication
- Multi-region deployments
- Systems with high read/write loads
- Applications needing standard SQL compatibility

#### Limitations

- **Statement separation**: Requires executing SQL statements one at a time (no multi-statement exec)
- **JSON indexing**: Less flexible than PostgreSQL's JSONB

#### Example Usage

```go
import (
    "github.com/getpup/pupsourcing/es/adapters/mysql"
    _ "github.com/go-sql-driver/mysql"
)

// Create store
store := mysql.NewStore(mysql.DefaultStoreConfig())

// Use with connection string
dsn := "user:password@tcp(localhost:3306)/dbname?parseTime=true"
db, _ := sql.Open("mysql", dsn)

// Use with transactions
tx, _ := db.BeginTx(ctx, nil)
positions, err := store.Append(ctx, tx, es.NoStream(), events)
tx.Commit()
```

#### Migration Generation

```go
import "github.com/getpup/pupsourcing/es/migrations"

config := migrations.DefaultConfig()
err := migrations.GenerateMySQL(&config)
```

#### Important Notes

- **parseTime parameter**: Always include `?parseTime=true` in DSN to handle timestamps correctly
- **Statement execution**: The adapter handles UUID binary conversion automatically
- **Migration execution**: Migrations must be executed statement-by-statement (adapter handles this)

---

## Adapter Comparison

| Feature | PostgreSQL | SQLite | MySQL/MariaDB |
|---------|-----------|--------|---------------|
| **Production Ready** | ✅ | ⚠️ Limited | ✅ |
| **Server Required** | Yes | No (embedded) | Yes |
| **Concurrent Writes** | Excellent | Limited | Excellent |
| **Aggregate ID** | TEXT | TEXT | VARCHAR(255) |
| **Event ID** | UUID (16 bytes) | TEXT (36 bytes) | BINARY (16 bytes) |
| **JSON Support** | JSONB (indexed) | TEXT + functions | JSON type |
| **Timestamp Precision** | Microseconds | Seconds | Microseconds |
| **Setup Complexity** | Medium | Minimal | Medium |
| **Replication** | Built-in | File-level | Built-in |
| **HA Support** | Excellent | Manual | Excellent |
| **Best For** | Production | Testing/Embedded | Production |
| **License** | PostgreSQL | Public Domain | GPL/MIT |

## Switching Adapters

Switching between adapters requires only changing the import and constructor. All other code remains identical:

```go
// PostgreSQL
import "github.com/getpup/pupsourcing/es/adapters/postgres"
store := postgres.NewStore(postgres.DefaultStoreConfig())

// SQLite  
import "github.com/getpup/pupsourcing/es/adapters/sqlite"
store := sqlite.NewStore(sqlite.DefaultStoreConfig())

// MySQL
import "github.com/getpup/pupsourcing/es/adapters/mysql"
store := mysql.NewStore(mysql.DefaultStoreConfig())

// All adapters support the same operations
positions, err := store.Append(ctx, tx, es.NoStream(), events)
events, err := store.ReadEvents(ctx, tx, fromPosition, limit)
events, err := store.ReadAggregateStream(ctx, tx, aggregateType, aggregateID, nil, nil)
```

## Configuration Options

All adapters support the same configuration options:

```go
type StoreConfig struct {
    // Logger for observability (optional)
    Logger es.Logger
    
    // Table names (customizable)
    EventsTable         string // Default: "events"
    CheckpointsTable    string // Default: "projection_checkpoints"
    AggregateHeadsTable string // Default: "aggregate_heads"
}
```

## Testing Recommendations

- **Development**: Use SQLite for quick iteration without server setup
- **Integration Tests**: Use SQLite or Docker containers for PostgreSQL/MySQL
- **Production**: Match your production database in staging environments
- **CI/CD**: SQLite requires no setup; PostgreSQL/MySQL need service containers

## Performance Considerations

### Write Performance

- **PostgreSQL**: Excellent with high concurrency, benefits from connection pooling
- **SQLite**: Limited by single-writer restriction, use batching
- **MySQL**: Excellent with InnoDB, configure `innodb_flush_log_at_trx_commit` appropriately

### Read Performance

- **PostgreSQL**: Excellent for complex queries, leverage JSONB indexes
- **SQLite**: Fast for simple queries, leverage in-memory mode for testing
- **MySQL**: Good query performance, benefits from proper indexing

### Projection Performance

All adapters provide identical projection performance characteristics since projections use the `EventReader` interface which reads events sequentially by global position.

## Migration Strategy

When migrating between adapters:

1. **Generate new migrations** for the target database
2. **Export events** from source database (use `ReadEvents` with pagination)
3. **Import events** to target database (use `Append` in batches)
4. **Update projection checkpoints** if needed
5. **Verify aggregate versions** match in `aggregate_heads` table

## Support Matrix

| Go Version | PostgreSQL | SQLite | MySQL |
|-----------|-----------|--------|-------|
| 1.23+ | ✅ | ✅ | ✅ |
| 1.24+ | ✅ | ✅ | ✅ |
| 1.25+ | ✅ | ✅ | ✅ |

| Database Version | Support Status |
|-----------------|---------------|
| PostgreSQL 12+ | ✅ Fully supported |
| PostgreSQL 11- | ⚠️ Not tested |
| SQLite 3.35+ | ✅ Fully supported |
| SQLite 3.34- | ⚠️ May work but not tested |
| MySQL 8.0+ | ✅ Fully supported |
| MySQL 5.7 | ⚠️ May work but not tested |
| MariaDB 10.5+ | ✅ Fully supported |

## Examples

Complete working examples for each adapter are available in the `examples/` directory:

- **PostgreSQL**: `examples/basic/` - Full-featured example with projections
- **SQLite**: `examples/sqlite-basic/` - Embedded database example
- **MySQL**: `examples/mysql-basic/` - MySQL/MariaDB example

## Contributing

When implementing new adapters:

1. Implement all three interfaces: `EventStore`, `EventReader`, `AggregateStreamReader`
2. Handle optimistic concurrency via database constraints
3. Maintain the `aggregate_heads` table for O(1) version lookups
4. Add comprehensive integration tests
5. Create an example application
6. Document capabilities and limitations
7. Update this documentation

See existing adapters for reference implementations.
