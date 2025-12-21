# Event Mapping Code Generation

The `eventmap-gen` tool generates strongly-typed mapping code between domain events and pupsourcing event sourcing types (`es.Event` and `es.PersistedEvent`).

## Table of Contents

- [Why This Tool Exists](#why-this-tool-exists)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Versioned Events](#versioned-events)
- [Generated Code](#generated-code)
- [Clean Architecture](#clean-architecture)
- [Advanced Usage](#advanced-usage)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Why This Tool Exists

In event-sourced systems, you typically have:

1. **Domain events** - Pure business logic types in your domain layer (no infrastructure dependencies)
2. **ES events** - Infrastructure types (`es.Event`, `es.PersistedEvent`) for storage and replay

Manually mapping between these layers is error-prone and repetitive. This tool:

- **Generates explicit mapping code** - No runtime reflection, no magic
- **Supports versioned events** - Handle schema evolution over time
- **Maintains clean architecture** - Domain stays pure, generated code lives in infrastructure
- **Type-safe** - Compile-time guarantees with generics
- **Inspectable** - Generated code is readable Go that you can debug

## Installation

```bash
go install github.com/getpup/pupsourcing/cmd/eventmap-gen@latest
```

Or run directly:

```bash
go run github.com/getpup/pupsourcing/cmd/eventmap-gen [flags]
```

## Quick Start

### 1. Organize Your Domain Events

Create domain event structs in a directory:

```
internal/domain/events/
  v1/
    user_registered.go
    user_email_changed.go
```

**Example domain event (`user_registered.go`):**

```go
package v1

// UserRegistered is emitted when a new user registers.
type UserRegistered struct {
    Email string `json:"email"`
    Name  string `json:"name"`
}
```

### 2. Generate Mapping Code

```bash
go run github.com/getpup/pupsourcing/cmd/eventmap-gen \
  -input internal/domain/events \
  -output internal/infrastructure/persistence/generated \
  -package generated
```

### 3. Use Generated Code

```go
package main

import (
    "github.com/getpup/pupsourcing/es"
    "github.com/google/uuid"
    "internal/domain/events/v1"
    "internal/infrastructure/persistence/generated"
)

func main() {
    // Create domain event
    event := v1.UserRegistered{
        Email: "alice@example.com",
        Name:  "Alice",
    }

    // Convert to es.Event
    esEvents, err := generated.ToESEvents(
        "User",                    // aggregate type
        uuid.New().String(),       // aggregate ID
        []any{event},             // domain events
        generated.WithTraceID("trace-123"), // optional metadata
    )

    // Store in event store...
    // Later, retrieve and convert back...

    persistedEvents := []es.PersistedEvent{/* from database */}
    domainEvents, err := generated.FromESEvents[any](persistedEvents)
}
```

## Versioned Events

Event schemas evolve over time. This tool supports versioning through directory structure, similar to protobuf packages.

### Directory Structure

```
events/
  v1/
    user_registered.go    # Initial version
    order_created.go
  v2/
    user_registered.go    # New version with additional fields
    order_created.go      # New version
  v3/
    order_created.go      # Another evolution
```

### Version Rules

1. **Directory name determines version**: `v1/` → `EventVersion = 1`, `v2/` → `EventVersion = 2`
2. **Event type stays the same**: `UserRegistered` is the event type across all versions
3. **Version + Type uniquely identifies schema**: `(UserRegistered, 1)` vs `(UserRegistered, 2)`
4. **Default version is 1**: Events outside version directories get version 1

### Example: Schema Evolution

**Version 1 (`events/v1/user_registered.go`):**

```go
package v1

type UserRegistered struct {
    Email string `json:"email"`
    Name  string `json:"name"`
}
```

**Version 2 (`events/v2/user_registered.go`):**

```go
package v2

type UserRegistered struct {
    Email     string `json:"email"`
    Name      string `json:"name"`
    Country   string `json:"country"`   // New field
    Timestamp int64  `json:"timestamp"` // New field
}
```

### Handling Historical Events

The generated code correctly deserializes events based on their stored version:

```go
// Event stream from database contains mixed versions
persistedEvents := []es.PersistedEvent{
    {EventType: "UserRegistered", EventVersion: 1, Payload: ...},
    {EventType: "UserEmailChanged", EventVersion: 1, Payload: ...},
    {EventType: "UserRegistered", EventVersion: 2, Payload: ...},
}

// Deserialize correctly based on version
domainEvents, err := generated.FromESEvents[any](persistedEvents)
// Result:
// - domainEvents[0] is v1.UserRegistered
// - domainEvents[1] is v1.UserEmailChanged
// - domainEvents[2] is v2.UserRegistered
```

## Generated Code

The tool generates:

### 1. `EventTypeOf` - Type Resolution

```go
func EventTypeOf(e any) (string, error)
```

Returns the event type string for a domain event. The event type is the struct name (without version).

### 2. `ToESEvents` - Domain to ES Conversion

```go
func ToESEvents(
    aggregateType string,
    aggregateID string,
    events []any,
    opts ...Option,
) ([]es.Event, error)
```

Converts domain events to `es.Event` instances with:
- JSON marshaling of payload
- Automatic version assignment
- UUID generation
- Optional metadata (causation/correlation/trace IDs)

### 3. `FromESEvents` - ES to Domain Conversion

```go
func FromESEvents[T any](events []es.PersistedEvent) ([]T, error)
```

Converts persisted events back to domain events using generics. Validates event type and version, then unmarshals JSON payload.

### 4. Type-Safe Helpers

For each event version, generates:

```go
// Convert specific event to ES
func ToUserRegisteredV1(
    aggregateType string,
    aggregateID string,
    e v1.UserRegistered,
    opts ...Option,
) (es.Event, error)

// Convert ES to specific event
func FromUserRegisteredV1(
    pe es.PersistedEvent,
) (v1.UserRegistered, error)
```

### 5. Options Pattern

```go
type Option func(*eventOptions)

func WithCausationID(id string) Option
func WithCorrelationID(id string) Option
func WithTraceID(id string) Option
func WithMetadata(metadata []byte) Option
```

Use options to inject metadata:

```go
esEvents, err := generated.ToESEvents(
    "User", userID, []any{event},
    generated.WithCausationID("command-123"),
    generated.WithCorrelationID("correlation-456"),
    generated.WithTraceID("trace-789"),
)
```

## Clean Architecture

This tool maintains clean architecture boundaries:

```
┌─────────────────────────────────────────┐
│ Domain Layer (Pure Business Logic)     │
│ ┌─────────────────────────────────────┐ │
│ │ Domain Events (NO dependencies)     │ │
│ │ - v1.UserRegistered                 │ │
│ │ - v1.OrderCreated                   │ │
│ │ - v2.UserRegistered                 │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
               ▲
               │ Pure, no framework coupling
               │
┌──────────────┴──────────────────────────┐
│ Infrastructure Layer                    │
│ ┌─────────────────────────────────────┐ │
│ │ Generated Mapping Code              │ │
│ │ (Depends on pupsourcing & domain)   │ │
│ │ - EventTypeOf()                     │ │
│ │ - ToESEvents()                      │ │
│ │ - FromESEvents()                    │ │
│ └─────────────────────────────────────┘ │
│ ┌─────────────────────────────────────┐ │
│ │ Event Store (PostgreSQL/SQLite)     │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### Design Principles

1. **Domain events are pure** - No dependency on pupsourcing or infrastructure
2. **Generated code lives in infrastructure** - Can depend on pupsourcing and domain
3. **No Apply logic** - Generated code only handles marshaling/unmarshaling
4. **No aggregate modification** - Aggregate logic stays in domain layer
5. **Explicit, not magical** - Generated code is readable Go

### DDD / CQRS / Event Sourcing

This tool fits into Domain-Driven Design and CQRS patterns:

- **Events are Facts** - Domain events represent things that happened
- **Immutable** - Events never change after creation
- **Version-Aware** - Schema evolution is explicit and traceable
- **Command-Event Separation** - Commands produce events, events are stored
- **Read Models** - Projections consume events to build read models

## Advanced Usage

### Custom Module Paths

By default, the tool auto-detects your module path from `go.mod`. Override with:

```bash
eventmap-gen \
  -input internal/domain/events \
  -output internal/infra/generated \
  -package generated \
  -module github.com/mycompany/myapp/internal/domain/events
```

### Custom Output Filename

```bash
eventmap-gen \
  -input events \
  -output generated \
  -filename my_events.gen.go
```

### Using with `go generate`

Add to your code:

```go
//go:generate go run github.com/getpup/pupsourcing/cmd/eventmap-gen -input ../../domain/events -output . -package persistence
```

Then run:

```bash
go generate ./...
```

## Best Practices

### 1. Keep Domain Events Simple

✅ **Good:**

```go
package v1

type OrderCreated struct {
    OrderID    string  `json:"order_id"`
    CustomerID string  `json:"customer_id"`
    Amount     float64 `json:"amount"`
}
```

❌ **Avoid:**

```go
package v1

import "github.com/getpup/pupsourcing/es"

// Don't couple domain to infrastructure
type OrderCreated struct {
    es.Event  // Don't embed ES types
    Amount float64
}
```

### 2. Use JSON Tags

Always use JSON tags for explicit field names:

```go
type UserRegistered struct {
    Email string `json:"email"` // ✅ Explicit
    Name  string                // ❌ Will use "Name" by default
}
```

### 3. Version When Schema Changes

Create a new version when:
- Adding required fields
- Changing field types
- Removing fields
- Changing field semantics

Optional fields can sometimes be added to existing versions using `omitempty`.

### 4. Don't Delete Old Versions

Old versions must remain for replaying historical events:

```
events/
  v1/
    user_registered.go  # Keep this even if you move to v2
  v2/
    user_registered.go  # New version
```

### 5. Use Type-Safe Helpers When Possible

```go
// ✅ Type-safe, validates at compile time
esEvent, err := generated.ToUserRegisteredV1("User", userID, event)

// ⚠️ Less safe, uses interface{}
esEvents, err := generated.ToESEvents("User", userID, []any{event})
```

### 6. Document Breaking Changes

Add comments when introducing breaking schema changes:

```go
package v2

// UserRegistered version 2 adds Country (required) and Timestamp.
// Breaking change from v1: Country is now required.
// Use v1.UserRegistered for historical events before 2024-01-15.
type UserRegistered struct {
    Email     string `json:"email"`
    Name      string `json:"name"`
    Country   string `json:"country"`   // New required field
    Timestamp int64  `json:"timestamp"` // Added in v2
}
```

## Troubleshooting

### Error: "no events discovered"

**Cause:** No exported structs found in input directory.

**Solution:** Ensure:
- Structs are exported (capitalized names)
- Files are `.go` files (not `_test.go`)
- Directory path is correct

### Error: "unknown event type"

**Cause:** Trying to deserialize an event type that wasn't in the input directory when code was generated.

**Solution:** 
1. Add the event type to your domain events directory
2. Regenerate the mapping code
3. Redeploy

### Error: "unknown version X for event type Y"

**Cause:** Event store contains a version that wasn't in the input directory when code was generated.

**Solution:**
1. Add the missing version to your domain events directory (e.g., create `vX/event.go`)
2. Regenerate the mapping code
3. Redeploy

### Import Path Issues

If you see import errors in generated code:

1. Check that `-module` flag is set correctly
2. Verify `go.mod` is in the expected location
3. Run `go mod tidy` after generating code

### Generated Code Won't Compile

1. Ensure domain events have exported fields
2. Check for circular imports
3. Verify all types are JSON-serializable
4. Run `go mod tidy` to resolve dependencies

## Examples

### Example 1: User Management Events

```
events/
  v1/
    user_registered.go
    user_email_changed.go
    user_deleted.go
```

```bash
eventmap-gen \
  -input events \
  -output persistence/generated
```

### Example 2: Order Processing with Versioning

```
events/
  v1/
    order_created.go
    order_shipped.go
  v2/
    order_created.go  # Added tax and currency
```

```bash
eventmap-gen \
  -input events \
  -output persistence/generated
```

### Example 3: Multi-Aggregate System

```
events/
  user/
    v1/
      user_registered.go
  order/
    v1/
      order_created.go
      order_fulfilled.go
    v2/
      order_created.go
```

Generate separately for each aggregate:

```bash
eventmap-gen -input events/user -output persistence/user/generated
eventmap-gen -input events/order -output persistence/order/generated
```

## Further Reading

- [Event Sourcing Pattern](https://martinfowler.com/eaaDev/EventSourcing.html)
- [Versioned Domain Events](https://www.eventstore.com/blog/versioning-in-an-event-sourced-system)
- [pupsourcing Core Concepts](../core-concepts.md)
- [pupsourcing API Reference](../api-reference.md)
