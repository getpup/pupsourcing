# Core Concepts

Fundamental principles of event sourcing with pupsourcing.

## Table of Contents

- [Event Sourcing Fundamentals](#event-sourcing-fundamentals)
- [Core Components](#core-components)
- [Key Concepts](#key-concepts)
- [Design Principles](#design-principles)
- [Common Patterns](#common-patterns)
- [See Also](#see-also)

## Event Sourcing Fundamentals

### Definition

Event sourcing stores state changes as an immutable sequence of events rather than maintaining only current state. Instead of updating records (CRUD), the system appends events that describe what happened.

**Traditional CRUD:**
```
User table:
| id | email              | name  | status |
| 1  | alice@example.com  | Alice | active |

# UPDATE loses history
UPDATE user SET email='new@email.com' WHERE id=1
```

**Event Sourcing:**
```
Events (append-only):
1. UserCreated(id=1, email=alice@example.com, name=Alice)
2. EmailVerified(id=1)
3. EmailChanged(id=1, old=alice@example.com, new=alice@newdomain.com)
4. UserDeactivated(id=1, reason="account closed")

Current state = Apply events 1-4 in sequence
Historical state = Apply events up to specific point in time
```

### Benefits

1. **Complete Audit Trail** - Full history of all changes for compliance and debugging
2. **Temporal Queries** - Reconstruct state at any point in time
3. **Flexible Read Models** - Build new projections from existing events without migrations
4. **Event Replay** - Reprocess historical events for debugging or new features
5. **Business Intelligence** - Rich analytical capabilities from event history

### Trade-offs

**Advantages:**
- Complete historical record of all state changes
- Flexible read models without migrations
- Natural audit logging for compliance
- Temporal query capabilities
- Effective debugging through event replay

**Considerations:**
- Higher complexity than simple CRUD
- Learning curve for team members
- Projections must handle idempotency
- Eventual consistency in read models
- Storage growth over time (mitigated by snapshots)
- Schema evolution for immutable events

**When to Use:**
- Systems requiring audit trails (financial, healthcare, legal)
- Complex business domains
- Applications needing temporal queries
- Microservices publishing domain events
- Multiple read models from same data

**When to Avoid:**
- Simple CRUD applications
- Prototypes without event sourcing requirements
- Teams lacking event sourcing experience
- Systems requiring strict low-latency everywhere

## Core Components

### 1. Events

Events are immutable facts that have occurred in your system. They represent something that happened in the past and cannot be changed or deleted.

**Key principles:**
- Events are named in past tense: `UserCreated`, `OrderPlaced`, `PaymentProcessed`
- Events are immutable once persisted
- Events contain all data needed to understand what happened
- Events should be domain-focused, not technical

The Event struct represents an immutable domain event before it is stored. When you create events to append to the store, you populate this structure. The store then assigns the AggregateVersion and GlobalPosition when the event is persisted.

```go
type Event struct {
    CreatedAt     time.Time
    AggregateType string          // Type of aggregate (e.g., "User", "Order")
    EventType     string          // Type of event (e.g., "UserCreated", "OrderPlaced")
    AggregateID   string          // Aggregate instance identifier (UUID string, email, or any identifier)
    Payload       []byte          // Event data (typically JSON)
    Metadata      []byte          // Additional metadata (typically JSON)
    EventVersion  int             // Schema version of this event type (default: 1)
    CausationID   es.NullableString  // ID of event/command that caused this event
    CorrelationID es.NullableString  // Link related events across aggregates
    TraceID       es.NullableString  // Distributed tracing ID
    EventID       uuid.UUID       // Unique event identifier
}
```

Note that AggregateVersion and GlobalPosition are not part of the Event struct because they are assigned by the store during the Append operation. These fields are only present in PersistedEvent.

#### Event vs. PersistedEvent

**Event**: Used when creating new events to append to the store. You populate all fields except AggregateVersion and GlobalPosition, which the store assigns automatically during persistence.

**PersistedEvent**: Returned after events are stored or when reading from the store. Contains all Event fields plus the store-assigned GlobalPosition and AggregateVersion.

```go
type PersistedEvent struct {
    CreatedAt        time.Time
    AggregateType    string
    EventType        string
    AggregateID      string
    Payload          []byte
    Metadata         []byte
    GlobalPosition   int64     // Assigned by store - position in global event log
    AggregateVersion int64     // Assigned by store - version within this aggregate
    EventVersion     int
    CausationID      es.NullableString
    CorrelationID    es.NullableString
    TraceID          es.NullableString
    EventID          uuid.UUID
}
```

This separation ensures events are value objects until persisted. The store assigns both position in the global log and version within the aggregate, guaranteeing consistency.

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
aggregateID := uuid.New().String()

events := []es.Event{
    {
        AggregateType: "User",
        AggregateID:   aggregateID,
        EventType:     "UserCreated",
        EventVersion:  1,
        Payload:       []byte(`{"email":"alice@example.com"}`),
        Metadata:      []byte(`{}`),
        EventID:       uuid.New(),
        CreatedAt:     time.Now(),
    },
    {
        AggregateType: "User",
        AggregateID:   aggregateID,  // Same aggregate
        EventType:     "EmailVerified",
        EventVersion:  1,
        Payload:       []byte(`{}`),
        Metadata:      []byte(`{}`),
        EventID:       uuid.New(),
        CreatedAt:     time.Now(),
    },
}
```

**Key principle:** All events for the same aggregate are processed in order.

### 3. Event Store

The event store is an append-only log of all events, providing atomic append operations with optimistic concurrency control.

```go
type EventStore interface {
    // Append events atomically with version control
    Append(ctx context.Context, tx es.DBTX, expectedVersion es.ExpectedVersion, events []es.Event) ([]int64, error)
}
```

**Properties:**
- Append-only (events are never modified or deleted)
- Globally ordered (via `global_position`)
- Transactional (uses provided transaction)
- Optimistic concurrency via expectedVersion parameter

### 4. Projections

Projections transform events into read models (materialized views), enabling flexible query patterns and eventual consistency.

```go
type Projection interface {
    // Unique name for checkpoint tracking
    Name() string
    
    // Process a single event
    Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error
}
```

**Projection lifecycle:**
1. Read batch of events from store starting from checkpoint
2. Apply partition filter (for horizontal scaling)
3. Call Handle() for each event within a transaction
4. Update checkpoint atomically
5. Commit transaction
6. Repeat until context is cancelled or error occurs

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
store.Append(ctx, tx1, es.Exact(currentVersion), events1)  // Success
tx1.Commit()

// Transaction 2 (concurrent)
tx2, _ := db.BeginTx(ctx, nil)
store.Append(ctx, tx2, es.Exact(currentVersion), events2)  // ErrOptimisticConcurrency
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
    _, err := store.Append(ctx, tx, es.Exact(currentVersion), events)
    
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

Projections must be idempotent because events may be reprocessed during crash recovery or restarts. This ensures that processing the same event multiple times produces the same result as processing it once.

**Non-idempotent (problematic):**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType != "OrderPlaced" {
        return nil
    }
    
    // Problem: Running this twice increments the counter twice for the same event
    _, err := tx.ExecContext(ctx, 
        "UPDATE sales_statistics SET total_orders = total_orders + 1 WHERE date = CURRENT_DATE")
    return err
}
```

**Idempotent approach 1: Track processed events explicitly**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType != "OrderPlaced" {
        return nil
    }
    
    // Check if we've already processed this event
    var exists bool
    err := tx.QueryRowContext(ctx,
        "SELECT EXISTS(SELECT 1 FROM order_events_processed WHERE event_id = $1)",
        event.EventID).Scan(&exists)
    if err != nil {
        return err
    }
    if exists {
        return nil  // Already processed, skip
    }
    
    // Process the event
    _, err = tx.ExecContext(ctx, 
        "UPDATE sales_statistics SET total_orders = total_orders + 1 WHERE date = CURRENT_DATE")
    if err != nil {
        return err
    }
    
    // Mark event as processed
    _, err = tx.ExecContext(ctx,
        "INSERT INTO order_events_processed (event_id, processed_at) VALUES ($1, NOW())",
        event.EventID)
    return err
}
```

**Idempotent approach 2: Use upsert semantics**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType != "UserCreated" {
        return nil
    }
    
    var payload struct {
        Email string `json:"email"`
        Name  string `json:"name"`
    }
    if err := json.Unmarshal(event.Payload, &payload); err != nil {
        return err
    }
    
    // Use INSERT ... ON CONFLICT to make this idempotent
    // If aggregate_id already exists, update with the same values
    _, err := tx.ExecContext(ctx,
        `INSERT INTO users (aggregate_id, email, name, created_at) 
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (aggregate_id) 
         DO UPDATE SET email = EXCLUDED.email, name = EXCLUDED.name`,
        event.AggregateID, payload.Email, payload.Name, event.CreatedAt)
    return err
}
```

**Idempotent approach 3: Use event position as version**
```go
func (p *Projection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    if event.EventType != "InventoryAdjusted" {
        return nil
    }
    
    var payload struct {
        ProductID string `json:"product_id"`
        Quantity  int    `json:"quantity"`
    }
    if err := json.Unmarshal(event.Payload, &payload); err != nil {
        return err
    }
    
    // Only apply if this event's position is greater than last processed position for this product
    result, err := tx.ExecContext(ctx,
        `UPDATE inventory 
         SET quantity = quantity + $1, last_event_position = $2
         WHERE product_id = $3 AND (last_event_position IS NULL OR last_event_position < $2)`,
        payload.Quantity, event.GlobalPosition, payload.ProductID)
    if err != nil {
        return err
    }
    
    // If no rows updated, product doesn't exist yet
    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        _, err = tx.ExecContext(ctx,
            `INSERT INTO inventory (product_id, quantity, last_event_position) 
             VALUES ($1, $2, $3)
             ON CONFLICT (product_id) DO NOTHING`,  // Race condition safety
            payload.ProductID, payload.Quantity, event.GlobalPosition)
    }
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
positions, err := store.Append(ctx, tx, es.NoStream(), events)
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

pupsourcing is designed as a library that you integrate into your application. Your code controls the flow and calls library functions when needed.

**Library approach (pupsourcing):**
```go
// You control when and how to run projections
processor := projection.NewProcessor(db, store, &config)
err := processor.Run(ctx, projection)
```

**Framework approach (not pupsourcing):**
```go
// Framework discovers your code via reflection or annotations
@EventHandler
public void on(UserCreated event) { }
```

Benefits: You maintain full control over program flow, dependencies, and lifecycle management.

### 2. Explicit Dependencies

All dependencies are passed explicitly as parameters. There are no hidden globals, automatic dependency injection, or runtime discovery mechanisms.

**Explicit dependencies (pupsourcing):**
```go
// Every dependency is visible at the call site
runner := runner.New(db, eventReader)
err := runner.Run(ctx, []runner.ProjectionConfig{
    {Projection: proj1, ProcessorConfig: config1},
    {Projection: proj2, ProcessorConfig: config2},
})
```

**Implicit dependencies (not pupsourcing):**
```go
// Where do projections come from? Service locator? Global registry?
runner.Start()  // Unclear what this will execute
```

Benefits: Code is easier to understand, test, and debug when all dependencies are explicit.

### 3. Pull-Based Event Processing

Projections actively read events from the store at their own pace. The library does not use publish-subscribe patterns or push-based delivery.

**Pull-based processing (pupsourcing):**
```go
// Projection reads events on its own schedule
for {
    events := store.ReadEvents(ctx, tx, checkpoint, batchSize)
    for _, event := range events {
        projection.Handle(ctx, tx, event)
    }
    // Update checkpoint and commit
}
```

Benefits:
- Natural backpressure mechanism (slow projections won't overwhelm the system)
- No connection pooling or message broker management required
- Works consistently across different storage backends
- Simple failure recovery (resume from checkpoint)

### 4. Database-Centric Coordination

The database serves as the coordination mechanism. No external distributed systems are required for basic operation.

Database provides:
- **Checkpoints**: Each projection tracks its position via a database row
- **Optimistic concurrency**: Enforced through unique constraints on aggregate versions
- **Transactional consistency**: Operations are atomic within database transactions

This approach keeps infrastructure requirements minimal while providing reliable coordination.

## Common Patterns

### Pattern 1: Read-Your-Writes

Write events and read them back within the same transaction for immediate consistency.

```go
// Write event
tx, _ := db.BeginTx(ctx, nil)
aggregateID := uuid.New().String()
events := []es.Event{
    {
        AggregateType: "User",
        AggregateID:   aggregateID,
        EventType:     "UserCreated",
        EventVersion:  1,
        Payload:       []byte(`{"email":"user@example.com"}`),
        Metadata:      []byte(`{}`),
        EventID:       uuid.New(),
        CreatedAt:     time.Now(),
    },
}
store.Append(ctx, tx, es.NoStream(), events)

// Read immediately within same transaction
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

Rebuild aggregate state from its event history.

```go
type User struct {
    ID     string
    Email  string
    Name   string
    Active bool
}

func (u *User) Apply(event es.PersistedEvent) {
    switch event.EventType {
    case "UserCreated":
        var payload struct {
            Email string `json:"email"`
            Name  string `json:"name"`
        }
        json.Unmarshal(event.Payload, &payload)
        u.Email = payload.Email
        u.Name = payload.Name
        u.Active = true
    case "UserDeactivated":
        u.Active = false
    case "EmailChanged":
        var payload struct {
            NewEmail string `json:"new_email"`
        }
        json.Unmarshal(event.Payload, &payload)
        u.Email = payload.NewEmail
    }
}

func LoadUser(ctx context.Context, tx es.DBTX, store store.AggregateStreamReader, id string) (*User, error) {
    events, err := store.ReadAggregateStream(ctx, tx, "User", id, nil, nil)
    if err != nil {
        return nil, err
    }
    if len(events) == 0 {
        return nil, fmt.Errorf("user not found: %s", id)
    }
    
    user := &User{ID: id}
    for _, event := range events {
        user.Apply(event)
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
        _, err := p.store.Append(ctx, tx, es.NoStream(), []es.Event{inventoryEvent})
        return err
    
    case "InventoryReserved":
        // Charge payment
        paymentEvent := /* ... */
        _, err := p.store.Append(ctx, tx, es.NoStream(), []es.Event{paymentEvent})
        return err
    
    case "PaymentSucceeded":
        // Ship order
        shippingEvent := /* ... */
        _, err := p.store.Append(ctx, tx, es.NoStream(), []es.Event{shippingEvent})
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
