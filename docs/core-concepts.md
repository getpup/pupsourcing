# Core Concepts

This guide explains the fundamental concepts of event sourcing with pupsourcing.

## Event Sourcing Basics

### What is Event Sourcing?

Event sourcing is a pattern where state changes are stored as a sequence of events rather than storing only the current state. Instead of updating records in place (CRUD), you append immutable events that describe what happened.

**Why does this matter?**

In traditional databases, you only see the current state. If a user's email changed from alice@example.com to alice@newdomain.com, you lose the history of that change unless you build custom audit tables. With event sourcing, every state change is captured as an event, giving you a complete history by default.

**Traditional approach (CRUD):**
```
User table:
| id | email | name | status |
| 1  | alice@example.com | Alice | active |

# UPDATE user SET email='new@email.com' WHERE id=1
# Old email is lost forever
```

**Event sourcing approach:**
```
Events table (append-only):
1. UserCreated(id=1, email=alice@example.com, name=Alice)
2. EmailVerified(id=1)
3. EmailChanged(id=1, old=alice@example.com, new=alice@newdomain.com)
4. NameChanged(id=1, old=Alice, new=Alice Smith)
5. UserDeactivated(id=1, reason="account closed")

Current state = Apply events 1-5 in order
State at time T = Apply events up to time T
```

### Benefits

1. **Complete Audit Trail** - Know exactly what happened, when it happened, who did it, and why. Essential for compliance, debugging, and business analysis.

2. **Time Travel** - Reconstruct state at any point in history. Useful for debugging production issues: "What did this order look like yesterday before the bug?"

3. **Event Replay** - Build new read models from existing events. Need a new report? Just replay the events through a new projection. No data migration needed.

4. **Debugging** - See exactly what led to current state. Instead of asking "how did we get here?", you can see the exact sequence of events.

5. **Business Insights** - Analyze historical data. How many users changed their email in the last month? Just query the events.

### Trade-offs

**✅ Pros:**
- **Complete history**: Every state change is preserved
- **Flexible read models**: Create new views of data without migrations
- **Natural audit log**: Compliance requirements met by default
- **Temporal queries**: Answer "what if" questions about the past
- **Debugging**: Replay production events in development
- **Business intelligence**: Rich data for analytics

**❌ Cons:**
- **Complexity**: More complex than simple CRUD operations
- **Learning curve**: Team needs to understand ES concepts
- **Idempotency**: Projections must handle duplicate events
- **Eventual consistency**: Read models lag behind write side
- **Storage**: Events accumulate over time (mitigated by snapshots)
- **Query complexity**: Can't simply JOIN tables - need projections
- **Schema evolution**: Events are immutable, must handle old versions

**When to use event sourcing:**
- Systems requiring full audit trails (financial, healthcare, legal)
- Domains with complex business logic
- Applications needing temporal queries
- Microservices needing to publish domain events
- Systems requiring multiple read models from same data

**When NOT to use event sourcing:**
- Simple CRUD applications
- Prototypes or MVPs (unless ES is core requirement)
- Teams unfamiliar with ES and no time to learn
- Systems with strict low-latency requirements everywhere

## Core Components

### 1. Events

Events are immutable facts that have occurred in your system. They represent something that happened in the past and cannot be changed or deleted.

**Key principles:**
- Events are named in past tense: `UserCreated`, `OrderPlaced`, `PaymentProcessed`
- Events are immutable once persisted
- Events contain all data needed to understand what happened
- Events should be domain-focused, not technical

```go
type Event struct {
    // Identity
    EventID       uuid.UUID      // Unique event identifier
    AggregateType string          // Type of aggregate (e.g., "User")
    AggregateID   uuid.UUID      // Aggregate instance identifier
    EventType     string          // Type of event (e.g., "UserCreated")
    
    // Versioning
    EventVersion      int         // Schema version of this event type (for evolution)
    AggregateVersion  int64       // Version AFTER this event (assigned by store)
    GlobalPosition    int64       // Position in global log (assigned by store)
    
    // Data
    Payload    []byte             // Event data (typically JSON)
    Metadata   []byte             // Additional metadata (typically JSON)
    
    // Tracing (optional but recommended for distributed systems)
    TraceID       uuid.NullUUID   // Distributed tracing ID
    CorrelationID uuid.NullUUID   // Link related events across aggregates
    CausationID   uuid.NullUUID   // ID of event/command that caused this event
    
    // Timestamp
    CreatedAt time.Time           // When event occurred
}
```

#### Event vs. PersistedEvent

**Event**: What you create before appending to the store. You don't set `AggregateVersion` or `GlobalPosition` - the store assigns these.

**PersistedEvent**: What you get back after the event is stored or when reading from the store. Includes the assigned `GlobalPosition` and `AggregateVersion`.

**Why the distinction?** Events don't have identity until they're persisted. A PersistedEvent has a position in the global event log and knows its version within the aggregate.

#### Event Design Best Practices

**✅ Good event names:**
- `OrderPlaced` (not `PlaceOrder` - it already happened)
- `PaymentCompleted` (not `Payment` - be specific)
- `UserEmailChanged` (not `UserUpdated` - what exactly changed?)

**❌ Bad event names:**
- `CreateUser` (command, not event)
- `Update` (too generic)
- `UserEvent` (meaningless)

**Event payload guidelines:**
- Include all data needed to understand the event
- Don't include computed values that can be derived
- Use JSON for flexibility and readability
- Version your event schemas (EventVersion field)

Example:
```go
// ✅ Good: Includes all relevant data
{
    "user_id": "123",
    "old_email": "alice@old.com",
    "new_email": "alice@new.com",
    "changed_by": "user_456",
    "reason": "user requested"
}

// ❌ Bad: Missing context
{
    "email": "alice@new.com"
}
```

### 2. Aggregates

An aggregate is a cluster of related domain objects that are treated as a unit for data changes. In event sourcing, an aggregate is the primary unit of consistency.

**Core principles:**
- An aggregate is a consistency boundary
- All events for an aggregate are processed in order
- Aggregates are identified by `AggregateType` + `AggregateID`
- Events within an aggregate are strictly ordered by `AggregateVersion`

**Example: User Aggregate**

```go
// User aggregate - spans multiple events
aggregateID := uuid.New()

events := []es.Event{
    {
        AggregateType: "User",
        AggregateID:   aggregateID,
        EventType:     "UserCreated",
        Payload:       []byte(`{"email":"alice@example.com"}`),
        // ...
    },
    {
        AggregateType: "User",
        AggregateID:   aggregateID,  // Same aggregate
        EventType:     "EmailVerified",
        Payload:       []byte(`{}`),
        // ...
    },
}
```

**Key principle:** All events for the same aggregate are processed in order.

### 3. Event Store

The event store is an append-only log of all events.

```go
type EventStore interface {
    // Append events atomically
    Append(ctx context.Context, tx es.DBTX, events []es.Event) ([]int64, error)
}
```

**Properties:**
- Append-only (events are never modified or deleted)
- Globally ordered (via `global_position`)
- Transactional (uses provided transaction)

### 4. Projections

Projections transform events into read models (materialized views).

```go
type Projection interface {
    // Unique name for checkpoint tracking
    Name() string
    
    // Process a single event
    Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error
}
```

**Projection lifecycle:**
1. Read batch of events from store
2. Apply partition filter
3. Call Handle() for each event
4. Update checkpoint
5. Commit transaction
6. Repeat

### 5. Checkpoints

Checkpoints track where a projection has processed up to.

```sql
CREATE TABLE projection_checkpoints (
    projection_name TEXT PRIMARY KEY,
    last_global_position BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

**Key features:**
- One checkpoint per projection
- Updated atomically with event processing
- Enables resumable processing

## Key Concepts

### Optimistic Concurrency

pupsourcing uses optimistic concurrency control to prevent conflicts.

```go
// Transaction 1
tx1, _ := db.BeginTx(ctx, nil)
store.Append(ctx, tx1, events1)  // Success
tx1.Commit()

// Transaction 2 (concurrent)
tx2, _ := db.BeginTx(ctx, nil)
store.Append(ctx, tx2, events2)  // ErrOptimisticConcurrency
tx2.Rollback()
```

**How it works:**
1. Each aggregate has a current version in `aggregate_heads` table
2. When appending, version is checked (O(1) lookup)
3. New events get consecutive versions
4. Database constraint enforces uniqueness: `(aggregate_type, aggregate_id, aggregate_version)`
5. If another transaction committed between check and insert → conflict

**Handling conflicts:**
```go
for retries := 0; retries < maxRetries; retries++ {
    tx, _ := db.BeginTx(ctx, nil)
    _, err := store.Append(ctx, tx, events)
    
    if errors.Is(err, store.ErrOptimisticConcurrency) {
        tx.Rollback()
        // Reload aggregate, reapply command
        continue
    }
    
    if err != nil {
        tx.Rollback()
        return err
    }
    
    return tx.Commit()
}
```

### Global Position

Every event gets a unique, monotonically increasing position.

```
Event 1 → global_position = 1
Event 2 → global_position = 2
Event 3 → global_position = 3
...
```

**Uses:**
- Checkpoint tracking
- Event replay
- Ordered processing
- Temporal queries

### Aggregate Versioning

Each aggregate has its own version sequence.

```
User ABC:
  Event 1 → aggregate_version = 1 (UserCreated)
  Event 2 → aggregate_version = 2 (EmailVerified)
  Event 3 → aggregate_version = 3 (NameChanged)

User XYZ:
  Event 1 → aggregate_version = 1 (UserCreated)
  Event 2 → aggregate_version = 2 (Deactivated)
```

**Uses:**
- Optimistic concurrency
- Event replay
- Aggregate reconstruction

### Idempotency

Projections must be idempotent because events may be reprocessed (e.g., on crash recovery).

**Non-idempotent (bad):**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    // Problem: Running twice increments counter twice
    _, err := tx.ExecContext(ctx, "UPDATE stats SET count = count + 1")
    return err
}
```

**Idempotent (good):**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    // Solution: Use INSERT ... ON CONFLICT
    _, err := tx.ExecContext(ctx,
        "INSERT INTO processed_events (event_id) VALUES ($1)"+
        "ON CONFLICT (event_id) DO NOTHING",
        event.EventID)
    if err != nil {
        return err
    }
    
    // Now safe to update stats
    _, err = tx.ExecContext(ctx, "UPDATE stats SET count = count + 1")
    return err
}
```

### Transaction Boundaries

**You control transactions**, not the library.

```go
// Your responsibility: begin transaction
tx, _ := db.BeginTx(ctx, nil)
defer tx.Rollback()

// Library uses your transaction
positions, err := store.Append(ctx, tx, events)
if err != nil {
    return err  // Rollback happens in defer
}

// Your responsibility: commit
return tx.Commit()
```

**Benefits:**
- Compose operations atomically
- Control isolation levels
- Integrate with existing code

## Design Principles

### 1. Library, Not Framework

pupsourcing is a library. You call it; it doesn't call you.

**Library style (pupsourcing):**
```go
// You're in control
processor := projection.NewProcessor(db, store, config)
err := processor.Run(ctx, projection)
```

**Framework style (not pupsourcing):**
```go
// Framework discovers and calls your code
@EventHandler
public void on(UserCreated event) { }
```

### 2. Explicit Over Magic

No auto-discovery, no hidden globals, no magic.

**Explicit (pupsourcing):**
```go
// Every dependency is explicit
runner := runner.New(db, eventReader)
err := runner.Run(ctx, []runner.ProjectionConfig{
    {Projection: proj1, ProcessorConfig: config1},
    {Projection: proj2, ProcessorConfig: config2},
})
```

**Magic (not pupsourcing):**
```go
// Where do projections come from? Environment variables? Registry?
runner.Start()  // What is it running?
```

### 3. Pull-Based Processing

Projections pull events from the store. No pub/sub, no push.

**Pull-based (pupsourcing):**
```go
// Projection reads at its own pace
for {
    events := store.ReadEvents(ctx, tx, checkpoint, batchSize)
    for _, event := range events {
        projection.Handle(ctx, tx, event)
    }
}
```

**Benefits:**
- Simple backpressure
- No connection management
- Works with any storage

### 4. Database as Coordination

No external coordination needed. Database provides:
- Checkpoints (per projection)
- Optimistic concurrency (via constraints)
- Transactions (atomic operations)

## Common Patterns

### Pattern 1: Read-Your-Writes

```go
// Write event
tx, _ := db.BeginTx(ctx, nil)
store.Append(ctx, tx, events)

// Read immediately (same transaction)
aggregate, _ := store.ReadAggregateStream(ctx, tx, "User", aggregateID, nil, nil)
tx.Commit()
```

### Pattern 2: Event Upcasting

Handle different event versions:

```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    switch event.EventType {
    case "UserCreated":
        switch event.EventVersion {
        case 1:
            return p.handleUserCreatedV1(event)
        case 2:
            return p.handleUserCreatedV2(event)
        }
    }
    return nil
}
```

### Pattern 3: Aggregate Reconstruction

```go
type User struct {
    ID    uuid.UUID
    Email string
    Name  string
}

func LoadUser(ctx context.Context, tx es.DBTX, store EventStore, id uuid.UUID) (*User, error) {
    events, err := store.ReadAggregateStream(ctx, tx, "User", id, nil, nil)
    if err != nil {
        return nil, err
    }
    
    user := &User{ID: id}
    for _, event := range events {
        user.Apply(event)  // Apply each event in order
    }
    return user, nil
}
```

### Pattern 4: Saga/Process Manager

Coordinate across aggregates:

```go
type OrderSagaProjection struct {
    store *postgres.Store
    db    *sql.DB
}

func (p *OrderSagaProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    switch event.EventType {
    case "OrderPlaced":
        // Reserve inventory
        inventoryEvent := /* ... */
        _, err := p.store.Append(ctx, tx, []es.Event{inventoryEvent})
        return err
    
    case "InventoryReserved":
        // Charge payment
        paymentEvent := /* ... */
        _, err := p.store.Append(ctx, tx, []es.Event{paymentEvent})
        return err
    
    case "PaymentSucceeded":
        // Ship order
        shippingEvent := /* ... */
        _, err := p.store.Append(ctx, tx, []es.Event{shippingEvent})
        return err
    }
    return nil
}
```

## See Also

- [Getting Started](./getting-started.md) - Setup and first steps
- [Scaling Guide](./scaling.md) - Production patterns
- [API Reference](./api-reference.md) - Complete API docs
- [Examples](../examples/) - Working code examples
