# pupsourcing Documentation

Welcome to the pupsourcing documentation! This library provides minimal, production-ready infrastructure for event sourcing in Go applications.

## Table of Contents

1. [Getting Started](./getting-started.md) - Installation, setup, and your first events
2. [Core Concepts](./core-concepts.md) - Understanding event sourcing with pupsourcing
3. [Database Adapters](./adapters.md) - PostgreSQL, SQLite, and MySQL/MariaDB adapters
4. [Projections Guide](./scaling.md) - Complete guide to projections and scaling
5. [Observability Guide](./observability.md) - Logging, tracing, and metrics
6. [API Reference](./api-reference.md) - Complete API documentation
7. [Industry Alignment](./industry-alignment.md) - Comparison with other event sourcing systems
8. [Deployment Guide](./deployment.md) - Production deployment patterns and operations

## Quick Links

### For New Users
- [Quick Start Guide](./getting-started.md#quick-start)
- [Simple Examples](../examples/single-worker/)
- [Core Concepts](./core-concepts.md)
- [Database Adapters](./adapters.md)

### For Production Use
- [Database Adapter Selection](./adapters.md#adapter-comparison)
- [Scaling Projections](./scaling.md)
- [Observability & Monitoring](./observability.md)
- [Deployment Patterns](./deployment.md)

### For Advanced Users
- [Partitioning Strategy](./scaling.md#partitioning)
- [Custom Projections](./projections.md#advanced-patterns)
- [Performance Tuning](./scaling.md#performance-tuning)

## Philosophy

pupsourcing is designed with these principles:

1. **Library, Not Framework** - Explicit control, no magic
2. **Clean Architecture** - Core interfaces are datastore-agnostic
3. **Transaction Control** - You control transaction boundaries
4. **Production Ready** - Built for real-world use cases
5. **Zero Dependencies** - Only Go standard library (plus database driver)

## Examples

Complete, runnable examples are available in the [`examples/`](../examples/) directory:

- **[Single Worker](../examples/single-worker/)** - Simplest pattern
- **[Partitioned](../examples/partitioned/)** - Horizontal scaling across processes
- **[Multiple Projections](../examples/multiple-projections/)** - Running different projections
- **[With Logging](../examples/with-logging/)** - Observability and debugging
- **[Worker Pool](../examples/worker-pool/)** - Partitions in same process
- **[Scaling](../examples/scaling/)** - Dynamic scaling from 1→N workers
- **[Stop/Resume](../examples/stop-resume/)** - Checkpoint reliability

## Community & Support

- **Issues**: [GitHub Issues](https://github.com/getpup/pupsourcing/issues)
- **Discussions**: [GitHub Discussions](https://github.com/getpup/pupsourcing/discussions)
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

## License

TBD - See [LICENSE](../LICENSE) file
