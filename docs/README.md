# pupsourcing Documentation

Welcome to the pupsourcing documentation! This library provides minimal, production-ready infrastructure for event sourcing in Go applications.

## Table of Contents

1. [Getting Started](./getting-started.md) - Installation, setup, and your first events
2. [Core Concepts](./core-concepts.md) - Understanding event sourcing with pupsourcing
3. [Projections Guide](./projections.md) - Complete guide to event projections
4. [Scaling & Advanced Usage](./scaling.md) - Horizontal scaling and production patterns
5. [API Reference](./api-reference.md) - Complete API documentation
6. [Industry Alignment](./industry-alignment.md) - Comparison with other event sourcing systems
7. [Deployment Guide](./deployment.md) - Production deployment patterns

## Quick Links

### For New Users
- [Quick Start Guide](./getting-started.md#quick-start)
- [Simple Examples](../examples/single-worker/)
- [Core Concepts](./core-concepts.md)

### For Production Use
- [Scaling Projections](./scaling.md)
- [Deployment Patterns](./deployment.md)
- [Monitoring & Operations](./deployment.md#monitoring)

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
- **[Worker Pool](../examples/worker-pool/)** - Partitions in same process
- **[Scaling](../examples/scaling/)** - Dynamic scaling from 1→N workers
- **[Stop/Resume](../examples/stop-resume/)** - Checkpoint reliability

## Community & Support

- **Issues**: [GitHub Issues](https://github.com/getpup/pupsourcing/issues)
- **Discussions**: [GitHub Discussions](https://github.com/getpup/pupsourcing/discussions)
- **Contributing**: See [CONTRIBUTING.md](../CONTRIBUTING.md)

## License

TBD - See [LICENSE](../LICENSE) file
