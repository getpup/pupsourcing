# API Reference

Complete API documentation for pupsourcing.

## Table of Contents

1. [Core Types](#core-types)
2. [Observability](#observability)
3. [Event Store](#event-store)
4. [Projections](#projections)
5. [Runner Package](#runner-package)
6. [PostgreSQL Adapter](#postgresql-adapter)

## Core Types

### es.Event

Represents an immutable domain event before persistence.

```go
type Event struct {
    // Identity
    EventID       uuid.UUID      // Unique event identifier
    AggregateType string          // Type of aggregate (e.g., "User", "Order")
    AggregateID   string          // Aggregate instance identifier (UUID string, email, or any identifier)
    EventType     string          // Type of event (e.g., "UserCreated")
    
    // Versioning
    EventVersion  int             // Schema version of this event type (default: 1)
    
    // Data
    Payload    []byte             // Event data (typically JSON)
    Metadata   []byte             // Additional metadata (typically JSON)
    
    // Tracing (optional)
    TraceID       uuid.NullUUID   // Distributed tracing ID
    CorrelationID uuid.NullUUID   // Links related events across aggregates
    CausationID   uuid.NullUUID   // ID of event/command that caused this event
    
    // Timestamp
    CreatedAt time.Time           // When event occurred
}
```

**Note:** `AggregateVersion` and `GlobalPosition` are assigned by the store during `Append`.

### es.ExpectedVersion

Controls optimistic concurrency for aggregate updates.

```go
type ExpectedVersion struct {
    // internal value
}

// Constructors
func Any() ExpectedVersion         // No version check
func NoStream() ExpectedVersion    // Aggregate must not exist
func Exact(version int64) ExpectedVersion  // Aggregate must be at specific version
```

**Usage:**

- **Any()**: Skip version validation. Use when you don't need concurrency control.
- **NoStream()**: Enforce that the aggregate doesn't exist. Use for aggregate creation and uniqueness enforcement.
- **Exact(N)**: Enforce that the aggregate is at version N. Use for normal command handling with optimistic concurrency.

**Examples:**

```go
// Creating a new aggregate
_, err := store.Append(ctx, tx, es.NoStream(), []es.Event{event})

// Updating an existing aggregate at version 5
_, err := store.Append(ctx, tx, es.Exact(5), []es.Event{event})

// No concurrency check
_, err := store.Append(ctx, tx, es.Any(), []es.Event{event})

// Uniqueness enforcement via reservation aggregate
email := "user@example.com"
reservationEvent := es.Event{
    AggregateType: "EmailReservation",
    AggregateID:   email,  // Use email as aggregate ID
    // ... other fields
}
_, err := store.Append(ctx, tx, es.NoStream(), []es.Event{reservationEvent})
// Second attempt with same email will fail with ErrOptimisticConcurrency
```

### es.PersistedEvent

Represents an event that has been stored, including position information.

```go
type PersistedEvent struct {
    // All fields from Event, plus:
    GlobalPosition    int64  // Position in global event log
    AggregateVersion  int64  // Version of aggregate after this event
}
```

### es.DBTX

Database transaction interface used throughout the library.

```go
type DBTX interface {
    ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}
```

Implemented by both `*sql.DB` and `*sql.Tx`.

## Observability

### es.Logger

Optional logging interface for instrumenting the library.

```go
type Logger interface {
    Debug(ctx context.Context, msg string, keyvals ...interface{})
    Info(ctx context.Context, msg string, keyvals ...interface{})
    Error(ctx context.Context, msg string, keyvals ...interface{})
}
```

**Usage:**
```go
type MyLogger struct {
    logger *slog.Logger
}

func (l *MyLogger) Debug(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.DebugContext(ctx, msg, keyvals...)
}

func (l *MyLogger) Info(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.InfoContext(ctx, msg, keyvals...)
}

func (l *MyLogger) Error(ctx context.Context, msg string, keyvals ...interface{}) {
    l.logger.ErrorContext(ctx, msg, keyvals...)
}

// Inject into store
config := postgres.DefaultStoreConfig()
config.Logger = &MyLogger{logger: slog.Default()}
store := postgres.NewStore(config)
```

See the [Observability Guide](./observability.md) for complete documentation and examples.

### es.NoOpLogger

Default logger implementation that does nothing. Used internally when no logger is configured.

```go
type NoOpLogger struct{}

func (NoOpLogger) Debug(_ context.Context, _ string, _ ...interface{}) {}
func (NoOpLogger) Info(_ context.Context, _ string, _ ...interface{}) {}
func (NoOpLogger) Error(_ context.Context, _ string, _ ...interface{}) {}
```

## Event Store

### store.EventStore

Interface for appending events.

```go
type EventStore interface {
    Append(ctx context.Context, tx es.DBTX, expectedVersion es.ExpectedVersion, events []es.Event) ([]int64, error)
}
```

#### Append

Atomically appends events within a transaction with optimistic concurrency control.

```go
func (s *EventStore) Append(ctx context.Context, tx es.DBTX, expectedVersion es.ExpectedVersion, events []es.Event) ([]int64, error)
```

**Parameters:**
- `ctx`: Context for cancellation
- `tx`: Database transaction (you control transaction boundaries)
- `expectedVersion`: Expected aggregate version (Any, NoStream, or Exact)
- `events`: Events to append (must all be for the same aggregate)

**Returns:**
- `[]int64`: Assigned global positions
- `error`: Error if any (including `ErrOptimisticConcurrency`)

**Errors:**
- `store.ErrOptimisticConcurrency`: Version conflict or expectation mismatch
- `store.ErrNoEvents`: Empty events slice

**Example:**
```go
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

// Create a new aggregate
positions, err := store.Append(ctx, tx, es.NoStream(), events)
if errors.Is(err, store.ErrOptimisticConcurrency) {
    // Aggregate already exists
}

// Update existing aggregate at version 3
positions, err := store.Append(ctx, tx, es.Exact(3), events)
if errors.Is(err, store.ErrOptimisticConcurrency) {
    // Version mismatch - another transaction updated the aggregate
    // Reload aggregate state and retry
}

tx.Commit()
```

### store.EventReader

Interface for reading events sequentially.

```go
type EventReader interface {
    ReadEvents(ctx context.Context, tx es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error)
}
```

#### ReadEvents

Reads events starting from a position.

```go
func (s *EventReader) ReadEvents(ctx context.Context, tx es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error)
```

**Parameters:**
- `ctx`: Context
- `tx`: Database transaction
- `fromPosition`: Start position (exclusive - returns events AFTER this position)
- `limit`: Maximum number of events to return

**Returns:**
- `[]es.PersistedEvent`: Events ordered by global_position
- `error`: Error if any

### store.AggregateStreamReader

Interface for reading events for a specific aggregate.

```go
type AggregateStreamReader interface {
    ReadAggregateStream(ctx context.Context, tx es.DBTX, aggregateType string, 
                       aggregateID string, fromVersion, toVersion *int64) ([]es.PersistedEvent, error)
}
```

#### ReadAggregateStream

Reads all events for an aggregate, optionally filtered by version range.

```go
func (s *Store) ReadAggregateStream(ctx context.Context, tx es.DBTX, 
                                   aggregateType string, aggregateID string,
                                   fromVersion, toVersion *int64) ([]es.PersistedEvent, error)
```

**Parameters:**
- `aggregateType`: Type of aggregate (e.g., "User")
- `aggregateID`: Aggregate instance ID (string: UUID, email, or any identifier)
- `fromVersion`: Optional minimum version (inclusive). Pass `nil` for all.
- `toVersion`: Optional maximum version (inclusive). Pass `nil` for all.

**Returns:**
- `[]es.PersistedEvent`: Events ordered by aggregate_version
- `error`: Error if any

**Examples:**
```go
// Read all events for UUID-based aggregate
userID := uuid.New().String()
events, _ := store.ReadAggregateStream(ctx, tx, "User", userID, nil, nil)

// Read from version 5 onwards
from := int64(5)
events, _ := store.ReadAggregateStream(ctx, tx, "User", userID, &from, nil)

// Read specific range
to := int64(10)
events, _ := store.ReadAggregateStream(ctx, tx, "User", userID, &from, &to)

// Read reservation aggregate by email
events, _ := store.ReadAggregateStream(ctx, tx, "EmailReservation", "user@example.com", nil, nil)
```

## Projections

### projection.Projection

Interface for event projection handlers.

```go
type Projection interface {
    Name() string
    Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error
}
```

#### Name

Returns unique projection name used for checkpoint tracking.

```go
func (p *MyProjection) Name() string {
    return "my_projection"
}
```

#### Handle

Processes a single event.

```go
func (p *MyProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    // Process event
    return nil
}
```

**Parameters:**
- `ctx`: Context for cancellation
- `tx`: Database transaction (same as checkpoint update)
- `event`: Event to process (passed by pointer to avoid copying)

**Returns:**
- `error`: Return error to stop projection processing

**Important:** Make projections idempotent - events may be reprocessed on crash recovery.

### projection.Processor

Processes events for a projection.

```go
type Processor struct {
    // unexported fields
}
```

#### NewProcessor

Creates a new projection processor.

```go
func NewProcessor(db *sql.DB, eventReader store.EventReader, config *ProcessorConfig) *Processor
```

**Parameters:**
- `db`: Database connection
- `eventReader`: Event reader implementation
- `config`: Processor configuration (passed by pointer)

**Breaking Change (v1.1.0):** Changed from `ProcessorConfig` (value) to `*ProcessorConfig` (pointer) for better performance.

**Returns:**
- `*Processor`: Processor instance

#### Run

Runs the projection until context is cancelled or an error occurs.

```go
func (p *Processor) Run(ctx context.Context, projection Projection) error
```

**Parameters:**
- `ctx`: Context for cancellation
- `projection`: Projection to run

**Returns:**
- `error`: Error if projection handler fails, or `ctx.Err()` on cancellation

**Example:**
```go
processor := projection.NewProcessor(db, store, config)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

err := processor.Run(ctx, myProjection)
if errors.Is(err, context.Canceled) {
    log.Println("Projection stopped gracefully")
}
```

### projection.ProcessorConfig

Configuration for projection processor.

```go
type ProcessorConfig struct {
    PartitionStrategy PartitionStrategy  // Partitioning strategy
    Logger            es.Logger          // Optional logger (nil = disabled)
    EventsTable       string             // Name of events table
    CheckpointsTable  string             // Name of checkpoints table
    BatchSize         int                // Events per batch
    PartitionKey      int                // This worker's partition (0-indexed)
    TotalPartitions   int                // Total number of partitions
}
```

**Note:** Fields are ordered by size (interfaces/pointers first) for optimal memory layout.

#### DefaultProcessorConfig

Returns default configuration.

```go
func DefaultProcessorConfig() ProcessorConfig {
    return ProcessorConfig{
        EventsTable:       "events",
        CheckpointsTable:  "projection_checkpoints",
        BatchSize:         100,
        PartitionKey:      0,
        TotalPartitions:   1,
        PartitionStrategy: HashPartitionStrategy{},
        Logger:            nil,  // No logging by default
    }
}
```

### projection.PartitionStrategy

Interface for partitioning strategies.

```go
type PartitionStrategy interface {
    ShouldProcess(aggregateID string, partitionKey, totalPartitions int) bool
}
```

### projection.HashPartitionStrategy

Default hash-based partitioning strategy.

```go
type HashPartitionStrategy struct{}

func (HashPartitionStrategy) ShouldProcess(aggregateID string, partitionKey, totalPartitions int) bool
```

Uses FNV-1a hash for deterministic, even distribution.

## Runner Package

### runner.Runner

Orchestrates multiple projections.

```go
type Runner struct {
    // unexported fields
}
```

#### New

Creates a new runner.

```go
func New(db *sql.DB, eventReader store.EventReader) *Runner
```

#### Run

Runs multiple projections concurrently.

```go
func (r *Runner) Run(ctx context.Context, configs []ProjectionConfig) error
```

**Parameters:**
- `ctx`: Context for cancellation
- `configs`: Projection configurations

**Returns:**
- `error`: First error from any projection, or `ctx.Err()`

**Example:**
```go
runner := runner.New(db, store)

configs := []runner.ProjectionConfig{
    {Projection: proj1, ProcessorConfig: config1},
    {Projection: proj2, ProcessorConfig: config2},
}

err := runner.Run(ctx, configs)
```

### runner.ProjectionConfig

Configuration for a single projection in the runner.

```go
type ProjectionConfig struct {
    Projection      projection.Projection
    ProcessorConfig projection.ProcessorConfig
}
```

### runner.RunProjectionPartitions

Helper that runs a projection with N partitions in the same process.

```go
func RunProjectionPartitions(ctx context.Context, db *sql.DB, eventReader store.EventReader,
                            proj projection.Projection, totalPartitions int) error
```

**Parameters:**
- `ctx`: Context
- `db`: Database
- `eventReader`: Event reader
- `proj`: Projection to run
- `totalPartitions`: Number of partitions (workers)

**Returns:**
- `error`: Error if any

**Example:**
```go
// Run 4 workers in same process
err := runner.RunProjectionPartitions(ctx, db, store, projection, 4)
```

### runner.RunMultipleProjections

Helper that runs multiple projections with custom configurations.

```go
func RunMultipleProjections(ctx context.Context, db *sql.DB, eventReader store.EventReader,
                           configs []ProjectionConfig) error
```

**Example:**
```go
configs := []runner.ProjectionConfig{
    {Projection: &Projection1{}, ProcessorConfig: config1},
    {Projection: &Projection2{}, ProcessorConfig: config2},
}

err := runner.RunMultipleProjections(ctx, db, store, configs)
```

## PostgreSQL Adapter

### postgres.Store

PostgreSQL implementation of EventStore, EventReader, and AggregateStreamReader.

```go
type Store struct {
    // unexported fields
}
```

#### NewStore

Creates a new PostgreSQL store.

```go
func NewStore(config StoreConfig) *Store
```

**Parameters:**
- `config`: Store configuration

**Returns:**
- `*Store`: Store instance

**Example:**
```go
store := postgres.NewStore(postgres.DefaultStoreConfig())
```

### postgres.StoreConfig

Configuration for PostgreSQL store.

```go
type StoreConfig struct {
    Logger              es.Logger  // Optional logger (nil = disabled)
    EventsTable         string     // Events table name
    AggregateHeadsTable string     // Aggregate heads table name
    CheckpointsTable    string     // Checkpoints table name
}
```

**Note:** Fields are ordered by size (interfaces/pointers first) for optimal memory layout.

#### DefaultStoreConfig

Returns default configuration.

```go
func DefaultStoreConfig() StoreConfig {
    return StoreConfig{
        EventsTable:         "events",
        AggregateHeadsTable: "aggregate_heads",
        CheckpointsTable:    "projection_checkpoints",
        Logger:              nil,  // No logging by default
    }
}
```

## Error Types

### store.ErrOptimisticConcurrency

Returned when a version conflict is detected.

```go
var ErrOptimisticConcurrency = errors.New("optimistic concurrency conflict")
```

**Example:**
```go
_, err := store.Append(ctx, tx, events)
if errors.Is(err, store.ErrOptimisticConcurrency) {
    // Retry transaction
}
```

### store.ErrNoEvents

Returned when attempting to append zero events.

```go
var ErrNoEvents = errors.New("no events to append")
```

### projection.ErrProjectionStopped

Returned when a projection stops due to handler error.

```go
var ErrProjectionStopped = errors.New("projection stopped")
```

### runner.ErrNoProjections

Returned when no projections are provided.

```go
var ErrNoProjections = errors.New("no projections provided")
```

### runner.ErrInvalidPartitionConfig

Returned when partition configuration is invalid.

```go
var ErrInvalidPartitionConfig = errors.New("invalid partition configuration")
```

## See Also

- [Getting Started](./getting-started.md) - Setup and basic usage
- [Core Concepts](./core-concepts.md) - Understanding the architecture
- [Scaling Guide](./scaling.md) - Production patterns
- [Examples](../examples/) - Working code examples
