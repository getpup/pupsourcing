# Industry Alignment - Comparison with Other Event Sourcing Systems

This document explains pupsourcing concepts in terms of other popular event sourcing and event streaming systems, helping users migrate or understand the library from different perspectives.

## Table of Contents

- [Quick Reference](#quick-reference)
- [Apache Kafka](#apache-kafka)
- [EventStoreDB](#eventstoredb)
- [Axon Framework (Java)](#axon-framework-java)
- [Marten (C#/.NET)](#marten-cnet)
- [Comparison Summary](#comparison-summary)
- [Terminology Translation](#terminology-translation)
- [See Also](#see-also)

## Quick Reference

| pupsourcing | Kafka | EventStoreDB | Axon Framework |
|-------------|-------|--------------|----------------|
| Event | Message | Event | Event |
| GlobalPosition | Offset | Position | Sequence Number |
| Projection | Consumer | Subscription | Tracking Event Processor |
| PartitionKey | Partition | - | Segment |
| Checkpoint | Consumer Offset | Checkpoint | Token |
| AggregateID | Key | Stream ID | Aggregate Identifier |
| EventStore | Topic | Stream | Event Store |

## Apache Kafka

### Conceptual Mapping

pupsourcing projections work like **Kafka consumer groups**:

| Kafka Concept | pupsourcing Equivalent |
|---------------|------------------------|
| Topic | Event Store (single, global log) |
| Partition | Projection partition (based on aggregate ID) |
| Consumer Group | Projection name |
| Consumer | Worker with specific `PartitionKey` |
| Offset | `GlobalPosition` in checkpoint |
| Message Key | `AggregateID` |
| Message Value | Event `Payload` |

### Similarities

```go
// Kafka consumer group
consumer := kafka.NewConsumer(&kafka.ConfigMap{
    "group.id": "my-consumer-group",
    "partition.assignment.strategy": "range",
})

// pupsourcing projection (similar concept)
config := projection.ProcessorConfig{
    PartitionKey: 0,           // Like consumer instance
    TotalPartitions: 4,        // Like partition count
}
processor := projection.NewProcessor(db, store, &config)
```

### Key Differences

| Aspect | Kafka | pupsourcing |
|--------|-------|-------------|
| **Partitioning** | Physical topic partitions | Logical (hash-based) |
| **Coordination** | Zookeeper/Raft | Database checkpoints |
| **Rebalancing** | Automatic | Manual (add/remove workers) |
| **Ordering** | Per partition | Per aggregate |
| **Storage** | Log files | PostgreSQL |

### When to Use Each

**Use Kafka when:**
- High throughput (millions of events/sec)
- Multiple heterogeneous consumers
- Event streaming beyond event sourcing
- Existing Kafka infrastructure

**Use pupsourcing when:**
- Simpler deployment (just PostgreSQL)
- Integrated with existing PostgreSQL application
- Event sourcing is primary use case
- Don't need Kafka-level throughput

## EventStoreDB

### Conceptual Mapping

pupsourcing is similar to **EventStoreDB subscriptions**:

| EventStoreDB | pupsourcing |
|--------------|-------------|
| Stream | Aggregate (type + ID) |
| $all stream | Event store (global log) |
| Subscription | Projection |
| Checkpoint | Checkpoint |
| Catch-up subscription | Projection (default behavior) |
| Persistent subscription | Projection with checkpoint |

### Similarities

```csharp
// EventStoreDB subscription
var subscription = await client.SubscribeToAllAsync(
    FromAll.After(checkpoint),
    HandleEvent,
    subscriptionDropped: OnDropped
);

// pupsourcing projection (similar)
processor := projection.NewProcessor(db, store, &config)
err := processor.Run(ctx, projection)
```

### Key Differences

| Aspect | EventStoreDB | pupsourcing |
|--------|--------------|-------------|
| **Storage** | Custom database | PostgreSQL |
| **Projections** | Built-in | Code-based |
| **Clustering** | Built-in | External orchestration |
| **Partitioning** | Not built-in | Hash-based |
| **Query language** | JavaScript | Go code |

### Migration Path

From EventStoreDB to pupsourcing:

1. **Persistent Subscriptions** → Projections with checkpoints
2. **Catch-up Subscriptions** → Default projection behavior
3. **Projection Manager** → Projection runner package
4. **Event linking** → Custom projection logic

## Axon Framework (Java)

### Conceptual Mapping

pupsourcing projections work like **Axon Tracking Event Processors**:

| Axon Framework | pupsourcing |
|----------------|-------------|
| Tracking Event Processor | Projection processor |
| Token Store | Checkpoint table |
| Segment | Partition |
| Event Handler | Projection.Handle() |
| Aggregate | Aggregate (type + ID) |
| Event Store | Event store |

### Similarities

```java
// Axon tracking processor
@ProcessingGroup("my-processor")
@Transactional
public class MyProjection {
    @EventHandler
    public void on(UserCreatedEvent event) {
        // Handle event
    }
}

// pupsourcing projection (similar)
type MyProjection struct {}

func (p *MyProjection) Name() string {
    return "my-processor"
}

func (p *MyProjection) Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error {
    if event.EventType == "UserCreated" {
        // Handle event
    }
    return nil
}
```

### Key Differences

| Aspect | Axon Framework | pupsourcing |
|--------|----------------|-------------|
| **Language** | Java | Go |
| **Discovery** | Annotation-based | Explicit |
| **DI** | Spring | Manual |
| **Sagas** | Built-in | External |
| **CQRS** | Full framework | Library |

### Migration Path

From Axon Framework to pupsourcing:

1. **@EventHandler** → `Projection.Handle()`
2. **@ProcessingGroup** → `Projection.Name()`
3. **Token Store** → Checkpoint table
4. **Segment** → `PartitionKey`/`TotalPartitions`
5. **Event Store** → pupsourcing event store

## Marten (C#/.NET)

### Conceptual Mapping

| Marten | pupsourcing |
|--------|-------------|
| Event Store | Event store |
| Projection | Projection |
| Async Daemon | Projection processor |
| Document Session | Transaction (tx) |
| Inline Projection | Synchronous handling |
| Async Projection | Default projection |

### Similarities

Both use PostgreSQL as the underlying database.

```csharp
// Marten projection
public class UserProjection : MultiStreamProjection<UserReadModel, Guid>
{
    public void Apply(UserCreated @event, UserReadModel model)
    {
        model.Email = @event.Email;
    }
}

// pupsourcing projection
func (p *UserProjection) Handle(ctx context.Context, tx es.DBTX, event es.PersistedEvent) error {
    if event.EventType == "UserCreated" {
        // Update read model
    }
    return nil
}
```

### Key Differences

| Aspect | Marten | pupsourcing |
|--------|--------|-------------|
| **Language** | C#/.NET | Go |
| **Document DB** | Yes | No (manual) |
| **Projections** | Built-in DSL | Code-based |
| **Live Aggregation** | Yes | Manual |

## Comparison Summary

### Architecture Philosophy

| System | Philosophy |
|--------|-----------|
| **Kafka** | Distributed streaming platform |
| **EventStoreDB** | Purpose-built event store |
| **Axon** | Full CQRS/ES framework |
| **Marten** | PostgreSQL document + events |
| **pupsourcing** | Minimal ES library |

### Complexity vs. Features

```
High Features, High Complexity
↑  Axon Framework
│  EventStoreDB with projections
│  Kafka with KSQL
│
│  Marten
│  EventStoreDB (basic)
│
↓  pupsourcing
Low Features, Low Complexity
```

### When to Choose pupsourcing

Choose pupsourcing when you want:

✅ Minimal, library-style approach
✅ PostgreSQL-based solution
✅ Event sourcing without framework lock-in
✅ Full control over projections
✅ Go-native implementation
✅ Simple horizontal scaling

Consider alternatives when you need:

❌ Multi-language support
❌ Built-in projections language
❌ Automatic cluster coordination
❌ Very high throughput (>100K events/sec)
❌ Multiple transport mechanisms
❌ Complex distributed sagas

## Terminology Translation

### For Kafka Users

- **Topic** = Your event store (single, ordered log)
- **Consumer Group** = Projection name
- **Consumer Instance** = Worker with partition key
- **Offset** = GlobalPosition checkpoint
- **Key** = AggregateID
- **Partition** = Logical partition (hash-based)

### For EventStoreDB Users

- **$all Stream** = Event store (global log)
- **Stream** = Aggregate events (filter by type + ID)
- **Persistent Subscription** = Projection with checkpoint
- **Position** = GlobalPosition
- **Event** = Event

### For Axon Users

- **Event Store** = Event store
- **Tracking Processor** = Projection processor
- **Token Store** = Checkpoint table
- **Segment** = Partition key
- **@EventHandler** = Projection.Handle()
- **Processing Group** = Projection name

## See Also

- [Getting Started](./getting-started.md) - Quick start guide
- [Scaling Guide](./scaling.md) - Horizontal scaling patterns
- [Examples](../examples/) - Working code examples
