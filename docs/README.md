# pupsourcing Documentation

Production-ready infrastructure for event sourcing in Go applications.

## Documentation

1. **[Getting Started](./getting-started.md)** - Installation, setup, and first steps
2. **[Core Concepts](./core-concepts.md)** - Event sourcing fundamentals
3. **[Database Adapters](./adapters.md)** - PostgreSQL, SQLite, and MySQL/MariaDB
4. **[Projections & Scaling](./scaling.md)** - Horizontal scaling and production patterns
5. **[Event Mapping Code Generation](./eventmap-gen.md)** - Type-safe domain event mapping
6. **[Observability](./observability.md)** - Logging, tracing, and monitoring
7. **[API Reference](./api-reference.md)** - Complete API documentation
8. **[Industry Alignment](./industry-alignment.md)** - Comparison with other systems
9. **[Deployment Guide](./deployment.md)** - Production deployment and operations

## Quick Navigation

### For New Users
- [Installation and Setup](./getting-started.md#quick-start)
- [Basic Examples](../examples/single-worker/)
- [Core Concepts Overview](./core-concepts.md)

### For Production Use
- [Database Selection](./adapters.md#adapter-comparison)
- [Projection Scaling](./scaling.md)
- [Monitoring and Observability](./observability.md)
- [Deployment Patterns](./deployment.md)

### For Advanced Topics
- [Horizontal Partitioning](./scaling.md#partitioning-strategy)
- [Performance Tuning](./scaling.md#performance-tuning)
- [Complete API Reference](./api-reference.md)

## Design Philosophy

pupsourcing is built on these principles:

1. **Library, Not Framework** - Explicit control over all operations
2. **Clean Architecture** - Core interfaces remain database-agnostic
3. **Transaction Control** - Callers manage transaction boundaries
4. **Production Ready** - Designed for real-world production use
5. **Minimal Dependencies** - Go standard library plus database driver

## Examples

Complete runnable examples are available in the [`examples/`](../examples/) directory:

- **[Single Worker](../examples/single-worker/)** - Basic projection pattern
- **[Multiple Projections](../examples/multiple-projections/)** - Concurrent projections
- **[Worker Pool](../examples/worker-pool/)** - In-process horizontal scaling
- **[Partitioned](../examples/partitioned/)** - Multi-process horizontal scaling
- **[With Logging](../examples/with-logging/)** - Observability integration

## Community

- **Issues**: [GitHub Issues](https://github.com/getpup/pupsourcing/issues)
- **Discussions**: [GitHub Discussions](https://github.com/getpup/pupsourcing/discussions)
