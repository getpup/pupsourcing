# Scoped Projections Example

This example demonstrates the difference between **scoped projections** (read models) and **global projections** (integration/outbox publishers).

## Concepts

### Scoped Projection (ScopedProjection interface)

A scoped projection only receives events from specific aggregate types. This is ideal for:
- **Read models** that only care about specific domain aggregates
- **Denormalized views** for query optimization
- **Materialized views** for specific use cases

Example:
```go
type UserReadModelProjection struct {
    userCount int
}

func (p *UserReadModelProjection) Name() string {
    return "user_read_model"
}

// AggregateTypes implements ScopedProjection
func (p *UserReadModelProjection) AggregateTypes() []string {
    return []string{"User"}  // Only receives User events
}

func (p *UserReadModelProjection) Handle(ctx context.Context, event es.PersistedEvent) error {
    // Only User events arrive here
    return nil
}
```

### Global Projection (Projection interface only)

A global projection receives ALL events regardless of aggregate type. This is ideal for:
- **Integration publishers** (e.g., Watermill, RabbitMQ, Kafka)
- **Outbox pattern** implementations
- **Audit logs** that need every event
- **Analytics** that aggregate across all domains

Example:
```go
type WatermillIntegrationProjection struct {
    publishedCount int
}

func (p *WatermillIntegrationProjection) Name() string {
    return "system.integration.watermill.v1"
}

// Does NOT implement AggregateTypes() - receives ALL events
func (p *WatermillIntegrationProjection) Handle(ctx context.Context, event es.PersistedEvent) error {
    // ALL events arrive here - publish to message broker
    return nil
}
```

## Running the Example

### Prerequisites
- PostgreSQL running on localhost:5432
- Database migrations applied (see basic example)

### Start PostgreSQL
```bash
docker run -d -p 5432:5432 \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=pupsourcing_example \
  postgres:16
```

### Apply Migrations
```bash
cd ../basic
go run main.go  # This will create the necessary tables
```

### Run the Example
```bash
cd examples/scoped-projections
go run main.go
```

## Expected Output

The example creates:
- 3 User events (UserCreated)
- 3 Order events (OrderPlaced)
- 2 Product events (ProductAdded)

**Total: 8 events**

You should see:
1. **UserReadModel** (scoped) processes only 3 User events
2. **OrderReadModel** (scoped) processes only 3 Order events
3. **WatermillIntegration** (global) processes all 8 events

Example output:
```
[UserReadModel] User created: Alice - Total users: 1
[UserReadModel] User created: Bob - Total users: 2
[UserReadModel] User created: Carol - Total users: 3
[OrderReadModel] Order placed: $99.99 - Total orders: 1, Revenue: $99.99
[OrderReadModel] Order placed: $149.99 - Total orders: 2, Revenue: $249.98
[OrderReadModel] Order placed: $49.99 - Total orders: 3, Revenue: $299.97
[Watermill] Publishing event to message broker: User/UserCreated (count: 1)
[Watermill] Publishing event to message broker: User/UserCreated (count: 2)
[Watermill] Publishing event to message broker: User/UserCreated (count: 3)
[Watermill] Publishing event to message broker: Order/OrderPlaced (count: 4)
[Watermill] Publishing event to message broker: Order/OrderPlaced (count: 5)
[Watermill] Publishing event to message broker: Order/OrderPlaced (count: 6)
[Watermill] Publishing event to message broker: Product/ProductAdded (count: 7)
[Watermill] Publishing event to message broker: Product/ProductAdded (count: 8)
```

## Key Takeaways

1. **Scoped projections** (implementing `ScopedProjection`) filter events by aggregate type
2. **Global projections** (implementing only `Projection`) receive all events
3. Both can run concurrently in the same application
4. Each projection maintains its own checkpoint, allowing them to process at different rates
5. Filtering happens efficiently at the processor level, not in the projection handler

## Use Cases

### When to use ScopedProjection
- Read models for specific aggregates (e.g., user profile view)
- Domain-specific denormalizations (e.g., order summary)
- Search indexes for specific entity types
- Notifications for specific events

### When to use Global Projection
- Message broker integrations (Watermill, Kafka, RabbitMQ)
- Outbox pattern implementations
- Cross-aggregate analytics
- Complete audit trail
- Event forwarding to external systems
- Change data capture (CDC) integrations

## Naming Conventions

For global integration projections, we recommend the naming convention:
```
system.integration.{integration-name}.v{version}
```

Examples:
- `system.integration.watermill.v1`
- `system.integration.kafka.v1`
- `system.integration.rabbitmq.v1`
- `system.integration.webhook.v1`

This makes it clear that the projection is for system integration, not a domain read model.
