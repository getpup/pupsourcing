# Examples

This directory contains examples demonstrating how to use the pupsourcing library.

## Basic Example

The [basic](./basic) example demonstrates:
- Connecting to PostgreSQL
- Appending events to the event store
- Processing events with a projection
- Checkpoint-based resumption

To run:
1. Start PostgreSQL:
   ```bash
   docker run -d -p 5432:5432 \
     -e POSTGRES_PASSWORD=postgres \
     -e POSTGRES_DB=pupsourcing_example \
     postgres:16
   ```

2. Generate and apply migrations:
   ```bash
   cd basic
   go generate
   psql -h localhost -U postgres -d pupsourcing_example -f ../../migrations/init.sql
   ```

3. Run the example:
   ```bash
   go run main.go
   ```

## Key Concepts

### Events are Immutable

Events are value objects that become immutable once persisted. They don't have identity until the store assigns a global_position.

### Transaction Control

The library is transaction-agnostic. You control transaction boundaries, allowing you to combine event appending with other database operations atomically.

### Optimistic Concurrency

Version conflicts are detected automatically. If two processes try to append events at the same version, one will fail with `ErrOptimisticConcurrency`.

### Projections

Projections read events sequentially and maintain their progress via checkpoints. They can be stopped and resumed without losing position.

### Horizontal Scaling

Multiple projection processors can run in parallel using hash-based partitioning. Events for the same aggregate always go to the same partition, maintaining ordering.
