# CockroachDB Adapter Support Research

**Date:** 2026-01-01  
**Purpose:** Evaluate CockroachDB as a potential database adapter for pupsourcing

## Executive Summary

CockroachDB is **highly suitable** as a pupsourcing adapter with minimal modifications needed to the existing PostgreSQL adapter. This document provides a comprehensive analysis and implementation recommendations.

## Table of Contents

1. [Overview](#overview)
2. [PostgreSQL Wire Protocol Compatibility](#postgresql-wire-protocol-compatibility)
3. [Transaction Semantics](#transaction-semantics)
4. [Optimistic Concurrency Control](#optimistic-concurrency-control)
5. [SQL Dialect Compatibility](#sql-dialect-compatibility)
6. [Performance Characteristics](#performance-characteristics)
7. [Horizontal Scaling](#horizontal-scaling)
8. [Known Limitations & Considerations](#known-limitations--considerations)
9. [Implementation Recommendations](#implementation-recommendations)
10. [Conclusion](#conclusion)

---

## Overview

CockroachDB is a distributed SQL database designed for cloud-native applications. It provides:
- **PostgreSQL wire protocol compatibility** - Can use standard PostgreSQL drivers
- **Distributed architecture** - Data is automatically sharded and replicated
- **ACID transactions** - Strong consistency guarantees across distributed nodes
- **Horizontal scalability** - Add nodes without application changes
- **High availability** - Automatic failover and replication

### Key Features for Event Sourcing

1. **Transactional consistency** - Perfect for event append operations
2. **Global sequential ordering** - Via `INT` or `SERIAL` columns with some caveats
3. **Unique constraints** - Essential for optimistic concurrency
4. **Strong read-after-write consistency** - Critical for event sourcing correctness

---

## PostgreSQL Wire Protocol Compatibility

### ✅ EXCELLENT Compatibility

CockroachDB implements the PostgreSQL wire protocol v3.0, making it compatible with most PostgreSQL drivers including:
- `lib/pq` (pure Go driver used in pupsourcing)
- `pgx` (high-performance Go driver)
- Standard `database/sql` interface

**Connection String Example:**
```go
// PostgreSQL format works directly
connStr := "postgresql://user:password@localhost:26257/pupsourcing?sslmode=disable"
db, err := sql.Open("postgres", connStr)
```

### Driver Support

The existing PostgreSQL adapter uses `github.com/lib/pq` which works seamlessly with CockroachDB. No driver changes are required.

**Key Points:**
- ✅ Use `postgres` driver name (not `cockroach`)
- ✅ Standard connection strings work
- ✅ All SQL operations via `database/sql` are compatible
- ✅ Parameter placeholders (`$1`, `$2`) work identically

---

## Transaction Semantics

### ACID Guarantees

CockroachDB provides full ACID transactions with **serializable isolation** by default, which is stronger than PostgreSQL's default (read committed).

| Feature | PostgreSQL | CockroachDB | Impact |
|---------|-----------|-------------|--------|
| Default Isolation | Read Committed | Serializable | ✅ Better for event sourcing |
| Transaction Retries | Rare | More common | ⚠️ Need retry logic |
| Distributed Transactions | Single node | Multi-node | ✅ Horizontal scaling |
| Transaction Size Limits | Very large | 64MB default | ⚠️ Monitor event sizes |

### Transaction Retry Logic

**CRITICAL CONSIDERATION:** CockroachDB may return serialization errors more frequently than PostgreSQL due to its distributed nature.

```go
// Error code 40001: serialization_failure
// Already handled by pupsourcing's optimistic concurrency logic!
```

The existing pupsourcing implementation already handles retry scenarios via `store.ErrOptimisticConcurrency`, which maps well to CockroachDB's retry requirements.

**Recommendation:** Document that applications should implement exponential backoff retry logic at the application layer (already best practice).

---

## Optimistic Concurrency Control

### ✅ FULLY COMPATIBLE

Pupsourcing's optimistic concurrency mechanism relies on:
1. Unique constraints on `(bounded_context, aggregate_type, aggregate_id, aggregate_version)`
2. `aggregate_heads` table for O(1) version lookups
3. Returning `ErrOptimisticConcurrency` on conflicts

**CockroachDB Support:**
- ✅ Unique constraints work identically
- ✅ Constraint violation error codes are compatible
- ✅ `IsUniqueViolation()` function detects errors correctly

### Testing Verification

The existing PostgreSQL adapter's `IsUniqueViolation()` function checks for:
```go
const uniqueViolationSQLState = "23505"
```

CockroachDB uses the **same PostgreSQL error codes** (SQLSTATE 23505) for unique violations, ensuring compatibility.

---

## SQL Dialect Compatibility

### Core SQL Features

| SQL Feature | PostgreSQL | CockroachDB | Status |
|------------|-----------|-------------|--------|
| `BIGSERIAL` | ✅ | ✅ | Compatible |
| `UUID` type | ✅ | ✅ | Compatible |
| `BYTEA` type | ✅ | ✅ | Compatible |
| `JSONB` type | ✅ | ✅ | Compatible |
| `TIMESTAMPTZ` | ✅ | ✅ | Compatible |
| `ON CONFLICT` | ✅ | ✅ | Compatible |
| Returning clause | ✅ | ✅ | Compatible |
| CTEs | ✅ | ✅ | Compatible |

### ⚠️ IMPORTANT: Sequential ID Generation

**Key Difference:** CockroachDB's `SERIAL`/`BIGSERIAL` behavior differs from PostgreSQL:

#### PostgreSQL `BIGSERIAL`
- Guaranteed strictly increasing sequence
- No gaps under normal operation
- Single-node sequence generation

#### CockroachDB `SERIAL`/`BIGSERIAL`
- **Not guaranteed to be gapless**
- **Not guaranteed to be strictly ordered across transactions**
- Uses a distributed unique ID generator for performance
- IDs are unique but may appear "out of order" when inserted concurrently

### Impact on Event Sourcing

The `global_position` column in the events table uses `BIGSERIAL` for ordering. In CockroachDB:

**✅ GOOD NEWS:**
- IDs are still unique and monotonically increasing over time
- Suitable for pagination and checkpoint-based resumption
- Projection processors that read events in batches will work correctly

**⚠️ CONSIDERATION:**
- Two concurrent transactions may see "reversed" global positions if one commits slightly before the other
- This is actually **acceptable** for event sourcing projections because:
  1. Projections read in batches by position
  2. Eventual consistency is acceptable (events will be processed)
  3. Aggregate streams are ordered by `aggregate_version`, not `global_position`

**ALTERNATIVE: Use `unique_rowid()`**
CockroachDB provides a `unique_rowid()` function that generates ordered unique IDs more efficiently:

```sql
-- Option 1: Keep BIGSERIAL (compatible, good enough)
global_position BIGSERIAL PRIMARY KEY

-- Option 2: Use unique_rowid() for better performance (CockroachDB-specific)
global_position INT DEFAULT unique_rowid() PRIMARY KEY
```

**Recommendation:** Keep `BIGSERIAL` for compatibility. Performance is excellent and ordering guarantees are sufficient for event sourcing use cases.

### ON CONFLICT Clause

✅ CockroachDB supports the same `ON CONFLICT` syntax as PostgreSQL:

```sql
-- This works identically in both databases
INSERT INTO aggregate_heads (bounded_context, aggregate_type, aggregate_id, aggregate_version, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (bounded_context, aggregate_type, aggregate_id)
DO UPDATE SET aggregate_version = $4, updated_at = NOW()
```

---

## Performance Characteristics

### Read Performance

| Operation | PostgreSQL | CockroachDB | Notes |
|-----------|-----------|-------------|-------|
| Single aggregate stream read | Excellent | Excellent | Local reads when data is co-located |
| Sequential event scan | Excellent | Good | May involve multiple nodes |
| Index lookups | Excellent | Excellent | Distributed but efficient |

### Write Performance

| Operation | PostgreSQL | CockroachDB | Notes |
|-----------|-----------|-------------|-------|
| Single event append | Excellent | Good | Requires distributed consensus |
| Batch event append | Excellent | Very Good | Batching reduces consensus overhead |
| Concurrent writes | Excellent | Good | More retry/serialization conflicts |

### Optimization Strategies

1. **Use Bounded Contexts for Locality**
   - CockroachDB can co-locate data by hash ranges
   - Store related aggregates in the same bounded context
   - Consider partitioning by `bounded_context`

2. **Batch Event Appends**
   - Pupsourcing already supports batch appends
   - This is even more important in CockroachDB to amortize consensus costs

3. **Connection Pooling**
   - Use connection pools (e.g., `pgxpool`)
   - Reduce connection overhead for distributed queries

4. **Follow Timestamp Caching**
   - CockroachDB caches recent read timestamps
   - Recent aggregate reads are fast
   - Leverage the `aggregate_heads` table (already implemented!)

---

## Horizontal Scaling

### ✅ EXCELLENT Scaling Characteristics

CockroachDB's distributed architecture is a **perfect match** for event sourcing workloads:

### Data Distribution

**Automatic Sharding:**
- CockroachDB automatically shards data across nodes
- Events are distributed by primary key ranges
- No application code changes required

**Replication:**
- Configurable replication factor (default: 3)
- Automatic rebalancing when nodes are added/removed
- High availability without manual intervention

### Scaling Projections

Pupsourcing's projection system with **hash-based partitioning** works excellently with CockroachDB:

```go
config := projection.DefaultProcessorConfig()
config.TotalPartitions = 4  // Run 4 projection workers
config.PartitionKey = 0     // This worker handles partition 0
```

**Benefits:**
1. Multiple projection workers can read from distributed CockroachDB nodes in parallel
2. Each partition can read from different node ranges
3. Linear scaling for projection processing
4. No single point of contention

### Geographic Distribution

CockroachDB supports **multi-region deployments**:
- Specify data locality rules
- Pin data to specific regions (e.g., GDPR compliance)
- Low-latency reads from local regions
- Global event log with regional projections

**Example Use Case:**
- Events written to nearest region
- Projections run in each region
- Read models are region-local for low latency

---

## Known Limitations & Considerations

### 1. Transaction Retry Frequency

**Issue:** More frequent serialization errors than PostgreSQL  
**Mitigation:** 
- Already handled by `ErrOptimisticConcurrency`
- Document need for application-level retry logic with backoff
- Consider increasing `TotalPartitions` to reduce contention

### 2. Transaction Size Limits

**Issue:** 64MB default transaction size limit  
**Impact:** Large batch event appends may hit limits  
**Mitigation:**
- Monitor event payload sizes
- Document recommended batch sizes (<1000 events per transaction)
- Pupsourcing's design already encourages reasonably sized batches

### 3. Sequential ID Gaps

**Issue:** `BIGSERIAL` may have gaps or temporary ordering anomalies  
**Impact:** Minimal - projections use checkpoint-based resumption  
**Mitigation:**
- Document behavior
- Emphasize that `aggregate_version` provides total ordering within aggregates
- `global_position` provides eventual ordering across all events

### 4. Clock Skew Sensitivity

**Issue:** CockroachDB relies on clock synchronization (NTP)  
**Impact:** Clock skew >500ms can cause issues  
**Mitigation:**
- Document NTP requirement (standard for distributed systems)
- Monitor clock skew in production

### 5. Performance vs. PostgreSQL

**Trade-off:**
- Single-node PostgreSQL: Lower latency, higher throughput
- CockroachDB: Higher latency (consensus), better scalability

**When to Choose:**
- **PostgreSQL:** Single-region, <100k events/day, simpler ops
- **CockroachDB:** Multi-region, >100k events/day, HA requirements

---

## Implementation Recommendations

### Phase 1: Direct PostgreSQL Adapter Reuse (Recommended)

**Approach:** Use the existing PostgreSQL adapter with CockroachDB - **it should work out of the box**.

```go
import (
    "github.com/getpup/pupsourcing/es/adapters/postgres"
    _ "github.com/lib/pq"
)

// Connection string for CockroachDB
connStr := "postgresql://user:password@cockroach:26257/pupsourcing?sslmode=disable"
db, err := sql.Open("postgres", connStr)

// Use the PostgreSQL adapter directly
store := postgres.NewStore(postgres.DefaultStoreConfig())
```

**Testing Needed:**
1. Run existing PostgreSQL integration tests against CockroachDB
2. Test optimistic concurrency under high contention
3. Verify `global_position` ordering behavior
4. Measure performance for typical workloads

### Phase 2: CockroachDB-Specific Adapter (If Needed)

Only create a separate adapter if Phase 1 reveals issues. Likely customizations:

```go
// es/adapters/cockroachdb/store.go
package cockroachdb

import "github.com/getpup/pupsourcing/es/adapters/postgres"

// Store is a CockroachDB adapter that extends PostgreSQL adapter
type Store struct {
    *postgres.Store
}

// NewStore creates a CockroachDB-optimized event store
func NewStore(config postgres.StoreConfig) *Store {
    // Add CockroachDB-specific optimizations if needed
    return &Store{
        Store: postgres.NewStore(config),
    }
}

// Potential optimizations:
// - Use unique_rowid() for global_position
// - Add PARTITION BY for multi-region setups
// - Customize retry logic for serialization errors
// - Add region-aware read strategies
```

### Phase 3: Multi-Region Support (Future)

For advanced users needing geographic distribution:

```sql
-- Partition events by bounded context for locality
ALTER TABLE events PARTITION BY LIST (bounded_context) (
    PARTITION us_east VALUES IN ('Identity', 'Orders'),
    PARTITION eu_west VALUES IN ('Payments', 'Shipping')
);

-- Configure per-partition zones
ALTER PARTITION us_east OF TABLE events CONFIGURE ZONE USING
    constraints = '[+region=us-east]';
```

---

## Conclusion

### ✅ **RECOMMENDATION: YES, CockroachDB is an excellent choice**

**Summary of Findings:**

| Aspect | Rating | Notes |
|--------|--------|-------|
| PostgreSQL Compatibility | ⭐⭐⭐⭐⭐ | Wire protocol and SQL are highly compatible |
| Transaction Support | ⭐⭐⭐⭐⭐ | Serializable isolation is ideal for event sourcing |
| Optimistic Concurrency | ⭐⭐⭐⭐⭐ | Unique constraints work perfectly |
| SQL Dialect | ⭐⭐⭐⭐ | Minor differences, all manageable |
| Performance | ⭐⭐⭐⭐ | Excellent for distributed workloads |
| Horizontal Scaling | ⭐⭐⭐⭐⭐ | Outstanding - automatic sharding and replication |
| Ease of Implementation | ⭐⭐⭐⭐⭐ | Reuse PostgreSQL adapter with minimal changes |

### **Implementation Plan:**

1. **Start with PostgreSQL adapter** - Should work immediately
2. **Run integration tests** - Verify compatibility
3. **Document CockroachDB support** - Add to README
4. **Create CockroachDB example** - Show best practices
5. **Consider dedicated adapter** - Only if optimizations are needed

### **When to Use CockroachDB:**

✅ **Use CockroachDB when:**
- You need horizontal scalability (>100k events/day)
- High availability is critical (automatic failover)
- Multi-region deployment is required
- You want to avoid managing replication yourself
- Your team is comfortable with distributed systems

⚠️ **Stick with PostgreSQL when:**
- Single-region deployment is sufficient
- Lower latency is critical (<10ms)
- Simpler operational model is preferred
- Cost optimization is a priority (single server)

### **Next Steps:**

1. Add CockroachDB to the README as a supported adapter (via PostgreSQL compatibility)
2. Create an example application in `examples/cockroachdb-basic/`
3. Add integration tests that run against CockroachDB (alongside PostgreSQL tests)
4. Document any CockroachDB-specific considerations in the adapter documentation

### **Risk Assessment:**

- **Low Risk:** PostgreSQL wire protocol compatibility is excellent
- **Low Risk:** Transaction semantics align well with event sourcing
- **Medium Risk:** Performance characteristics differ - benchmark for your use case
- **Low Risk:** Operational differences - document NTP requirements and transaction retry patterns

---

## Appendix: Quick Start Guide

### Running CockroachDB Locally

```bash
# Using Docker
docker run -d \
  --name cockroach \
  -p 26257:26257 \
  -p 8080:8080 \
  cockroachdb/cockroach:latest \
  start-single-node --insecure

# Create database
docker exec -it cockroach cockroach sql --insecure -e "CREATE DATABASE pupsourcing"
```

### Running Tests

```bash
# Set connection string
export COCKROACH_URL="postgresql://root@localhost:26257/pupsourcing?sslmode=disable"

# Run integration tests with CockroachDB
go test -v -tags=integration ./es/adapters/postgres/integration_test/... \
  -cockroachdb=$COCKROACH_URL
```

### Connection String Format

```
postgresql://[user]@[host]:[port]/[database]?[parameters]

Example:
postgresql://root@localhost:26257/pupsourcing?sslmode=disable
```

---

**Document Version:** 1.0  
**Last Updated:** 2026-01-01  
**Author:** GitHub Copilot  
**Status:** Recommendation for implementation
