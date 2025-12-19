# Deployment & Operations Guide

This guide covers production deployment patterns, monitoring, and operational best practices for pupsourcing.

## Deployment Patterns

### Pattern 1: Single Binary, Multiple Instances

Run the same binary multiple times with different configuration.

#### Docker Compose

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: myapp
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  projection-worker-0:
    image: myapp:latest
    depends_on:
      - postgres
    environment:
      DATABASE_URL: "postgres://postgres:postgres@postgres:5432/myapp?sslmode=disable"
      PARTITION_KEY: "0"
      TOTAL_PARTITIONS: "4"
    command: ["./myapp", "run-projections"]
    restart: unless-stopped

  projection-worker-1:
    image: myapp:latest
    depends_on:
      - postgres
    environment:
      DATABASE_URL: "postgres://postgres:postgres@postgres:5432/myapp?sslmode=disable"
      PARTITION_KEY: "1"
      TOTAL_PARTITIONS: "4"
    command: ["./myapp", "run-projections"]
    restart: unless-stopped

  projection-worker-2:
    image: myapp:latest
    depends_on:
      - postgres
    environment:
      DATABASE_URL: "postgres://postgres:postgres@postgres:5432/myapp?sslmode=disable"
      PARTITION_KEY: "2"
      TOTAL_PARTITIONS: "4"
    command: ["./myapp", "run-projections"]
    restart: unless-stopped

  projection-worker-3:
    image: myapp:latest
    depends_on:
      - postgres
    environment:
      DATABASE_URL: "postgres://postgres:postgres@postgres:5432/myapp?sslmode=disable"
      PARTITION_KEY: "3"
      TOTAL_PARTITIONS: "4"
    command: ["./myapp", "run-projections"]
    restart: unless-stopped

volumes:
  postgres_data:
```

#### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: projection-workers
  labels:
    app: projection-workers
spec:
  replicas: 4
  selector:
    matchLabels:
      app: projection-workers
  template:
    metadata:
      labels:
        app: projection-workers
    spec:
      containers:
      - name: worker
        image: myapp:latest
        command: ["./myapp", "run-projections"]
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: url
        - name: PARTITION_KEY
          valueFrom:
            fieldRef:
              fieldPath: metadata.labels['statefulset.kubernetes.io/pod-name']
        - name: TOTAL_PARTITIONS
          value: "4"
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: projection-workers
spec:
  selector:
    app: projection-workers
  ports:
  - port: 8080
    targetPort: 8080
```

**Note:** Extract partition key from pod ordinal in init container if using StatefulSet.

#### Systemd

```ini
# /etc/systemd/system/projection-worker@.service
[Unit]
Description=Projection Worker %i
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=myapp
Group=myapp
WorkingDirectory=/opt/myapp
Environment="DATABASE_URL=postgres://myapp:password@localhost/myapp"
Environment="PARTITION_KEY=%i"
Environment="TOTAL_PARTITIONS=4"
ExecStart=/opt/myapp/bin/myapp run-projections
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=projection-worker-%i

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/myapp

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable projection-worker@{0..3}
sudo systemctl start projection-worker@{0..3}

# Check status
sudo systemctl status projection-worker@*

# View logs
sudo journalctl -u projection-worker@0 -f
```

### Pattern 2: Separate Services per Projection

Run different projections in separate deployments for better isolation.

```yaml
# docker-compose.yml
version: '3.8'

services:
  user-projection:
    image: myapp:latest
    environment:
      PROJECTION_NAME: "user_read_model"
      DATABASE_URL: "postgres://..."
    command: ["./myapp", "run-projection", "--name=user_read_model"]
    restart: unless-stopped

  analytics-projection:
    image: myapp:latest
    environment:
      PROJECTION_NAME: "analytics"
      DATABASE_URL: "postgres://..."
      WORKERS: "4"  # Scale this projection
    command: ["./myapp", "run-projection", "--name=analytics", "--workers=4"]
    restart: unless-stopped

  notification-projection:
    image: myapp:latest
    environment:
      PROJECTION_NAME: "notifications"
      DATABASE_URL: "postgres://..."
    command: ["./myapp", "run-projection", "--name=notifications"]
    restart: unless-stopped
```

### Pattern 3: Combined Deployment

Run multiple projections in the same process, separate processes for partitioned ones.

```go
func main() {
    if len(os.Args) > 1 && os.Args[1] == "projections" {
        runProjections()
    } else {
        runWebServer()
    }
}

func runProjections() {
    // Parse flags
    partitionKey := flag.Int("partition-key", -1, "Partition key for scaled projections")
    flag.Parse()
    
    configs := []runner.ProjectionConfig{
        // Fast projections - run unpartitioned
        {
            Projection: &FastProjection1{},
            ProcessorConfig: projection.DefaultProcessorConfig(),
        },
        {
            Projection: &FastProjection2{},
            ProcessorConfig: projection.DefaultProcessorConfig(),
        },
    }
    
    // Slow projection - only if partition key provided
    if *partitionKey >= 0 {
        config := projection.DefaultProcessorConfig()
        config.PartitionKey = *partitionKey
        config.TotalPartitions = 4
        
        configs = append(configs, runner.ProjectionConfig{
            Projection: &SlowProjection{},
            ProcessorConfig: config,
        })
    }
    
    runner.RunMultipleProjections(ctx, db, store, configs)
}
```

## Configuration Management

### Environment Variables

```bash
# Database
export DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require"
export DATABASE_MAX_OPEN_CONNS="25"
export DATABASE_MAX_IDLE_CONNS="5"
export DATABASE_CONN_MAX_LIFETIME="5m"

# Projection Configuration
export PARTITION_KEY="0"
export TOTAL_PARTITIONS="4"
export BATCH_SIZE="100"
export EVENTS_TABLE="events"
export CHECKPOINTS_TABLE="projection_checkpoints"

# Observability
export LOG_LEVEL="info"
export METRICS_PORT="9090"
export TRACE_ENDPOINT="http://jaeger:14268/api/traces"
```

### Configuration File

```yaml
# config.yaml
database:
  url: postgres://user:pass@localhost:5432/myapp
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 5m

projections:
  batch_size: 100
  events_table: events
  checkpoints_table: projection_checkpoints
  
  workers:
    - name: user_read_model
      partition_key: 0
      total_partitions: 1
    
    - name: analytics
      partition_key: 0
      total_partitions: 4
      batch_size: 500

logging:
  level: info
  format: json

metrics:
  enabled: true
  port: 9090
```

## Monitoring

### Key Metrics to Track

1. **Projection Lag**
```sql
-- How far behind is each projection?
SELECT 
    projection_name,
    last_global_position,
    (SELECT MAX(global_position) FROM events) - last_global_position as lag,
    updated_at
FROM projection_checkpoints
ORDER BY lag DESC;
```

2. **Event Throughput**
```sql
-- Events per minute over the last hour
SELECT 
    date_trunc('minute', created_at) as minute,
    COUNT(*) as event_count
FROM events
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY minute
ORDER BY minute DESC;
```

3. **Projection Processing Rate**
```sql
-- Checkpoint updates per minute (processing activity)
SELECT 
    projection_name,
    COUNT(*) as updates_count,
    MAX(last_global_position) - MIN(last_global_position) as events_processed
FROM projection_checkpoints
WHERE updated_at > NOW() - INTERVAL '1 minute'
GROUP BY projection_name;
```

### Prometheus Metrics

Instrument your application:

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    projectionLag = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "pupsourcing_projection_lag",
            Help: "Number of events projection is behind",
        },
        []string{"projection_name"},
    )
    
    eventsProcessed = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "pupsourcing_events_processed_total",
            Help: "Total number of events processed",
        },
        []string{"projection_name"},
    )
    
    projectionErrors = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "pupsourcing_projection_errors_total",
            Help: "Total number of projection errors",
        },
        []string{"projection_name"},
    )
)

func init() {
    prometheus.MustRegister(projectionLag)
    prometheus.MustRegister(eventsProcessed)
    prometheus.MustRegister(projectionErrors)
}

// Update metrics in your projection wrapper
type InstrumentedProjection struct {
    inner projection.Projection
}

func (p *InstrumentedProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
    err := p.inner.Handle(ctx, tx, event)
    if err != nil {
        projectionErrors.WithLabelValues(p.inner.Name()).Inc()
        return err
    }
    eventsProcessed.WithLabelValues(p.inner.Name()).Inc()
    return nil
}
```

### Grafana Dashboard

Example Prometheus queries:

```promql
# Projection lag
pupsourcing_projection_lag

# Event processing rate (events/sec)
rate(pupsourcing_events_processed_total[1m])

# Error rate
rate(pupsourcing_projection_errors_total[5m])

# Lag as percentage of total events
(pupsourcing_projection_lag / on() group_left() 
  max(pg_stat_user_tables_n_tup_ins{table="events"})) * 100
```

### Health Checks

```go
func healthCheck(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Check database connectivity
        if err := db.PingContext(r.Context()); err != nil {
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]string{
                "status": "unhealthy",
                "error": err.Error(),
            })
            return
        }
        
        // Check projection lag
        var maxLag int64
        err := db.QueryRowContext(r.Context(),
            `SELECT COALESCE(MAX((SELECT MAX(global_position) FROM events) - last_global_position), 0)
             FROM projection_checkpoints`).Scan(&maxLag)
        if err != nil || maxLag > 10000 {  // Threshold
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]interface{}{
                "status": "unhealthy",
                "lag": maxLag,
                "threshold": 10000,
            })
            return
        }
        
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{
            "status": "healthy",
        })
    }
}
```

## Operational Best Practices

### Database Connection Pooling

```go
db, _ := sql.Open("postgres", connStr)

// Configure connection pool
db.SetMaxOpenConns(25)  // Max concurrent connections
db.SetMaxIdleConns(5)   // Idle connections to keep alive
db.SetConnMaxLifetime(5 * time.Minute)
db.SetConnMaxIdleTime(1 * time.Minute)
```

**Guidelines:**
- `MaxOpenConns` = number of workers × 2 (for safety)
- Monitor `pg_stat_activity` to verify connection usage
- Adjust based on PostgreSQL `max_connections`

### Graceful Shutdown

```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Signal handling
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
    
    // Run projections in goroutine
    errChan := make(chan error, 1)
    go func() {
        errChan <- runner.RunProjections(ctx, db, store, configs)
    }()
    
    // Wait for signal or error
    select {
    case <-sigChan:
        log.Println("Shutdown signal received")
        cancel()
        
        // Wait for graceful shutdown with timeout
        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer shutdownCancel()
        
        select {
        case <-errChan:
            log.Println("Projections stopped gracefully")
        case <-shutdownCtx.Done():
            log.Println("Shutdown timeout exceeded")
        }
        
    case err := <-errChan:
        log.Printf("Projection error: %v", err)
    }
}
```

### Backup and Recovery

**Backup Strategy:**
1. Regular PostgreSQL backups (pg_dump or continuous archiving)
2. Keep backups for retention period (e.g., 30 days)
3. Test recovery procedures regularly

**Recovery Scenarios:**

**Scenario 1: Lost Checkpoint**
```sql
-- Projection will restart from position 0
-- Ensure projections are idempotent!
DELETE FROM projection_checkpoints WHERE projection_name = 'lost_projection';
```

**Scenario 2: Corrupted Read Model**
```sql
-- 1. Stop projection
-- 2. Clear read model
TRUNCATE TABLE my_read_model;

-- 3. Delete checkpoint
DELETE FROM projection_checkpoints WHERE projection_name = 'my_projection';

-- 4. Restart projection (rebuilds from scratch)
```

### Security Considerations

1. **Database Credentials**
   - Use connection pooling
   - Rotate credentials regularly
   - Store in secrets management (Vault, AWS Secrets Manager)

2. **Network Security**
   - Use SSL/TLS for database connections (`sslmode=require`)
   - Firewall rules to restrict access
   - VPC isolation in cloud environments

3. **Least Privilege**
```sql
-- Create read-only user for read models
CREATE USER projection_reader WITH PASSWORD '...';
GRANT SELECT ON ALL TABLES IN SCHEMA public TO projection_reader;

-- Create write user for projections
CREATE USER projection_writer WITH PASSWORD '...';
GRANT SELECT, INSERT, UPDATE ON events TO projection_writer;
GRANT ALL ON projection_checkpoints TO projection_writer;
GRANT ALL ON my_read_model TO projection_writer;
```

## Troubleshooting

### Projection Falling Behind

**Symptoms:** Increasing lag, slow checkpoint updates

**Diagnosis:**
```sql
-- Check current lag
SELECT projection_name, 
       (SELECT MAX(global_position) FROM events) - last_global_position as lag
FROM projection_checkpoints;

-- Check processing rate
SELECT projection_name, updated_at
FROM projection_checkpoints
ORDER BY updated_at DESC;
```

**Solutions:**
1. **Increase batch size** - Process more events per transaction
2. **Add partitions** - Scale horizontally
3. **Optimize projection logic** - Profile and optimize slow code
4. **Check database performance** - Indexes, query plans

### High Database Load

**Symptoms:** Slow queries, connection pool exhaustion

**Diagnosis:**
```sql
-- Active connections
SELECT COUNT(*) FROM pg_stat_activity WHERE state = 'active';

-- Long-running queries
SELECT pid, now() - query_start as duration, query
FROM pg_stat_activity
WHERE state = 'active'
ORDER BY duration DESC;

-- Lock contention
SELECT * FROM pg_locks WHERE NOT granted;
```

**Solutions:**
1. Reduce `MaxOpenConns`
2. Decrease batch size
3. Add database indexes
4. Scale database (read replicas, sharding)

### Stuck Projections

**Symptoms:** Checkpoint not updating, no errors

**Diagnosis:**
```bash
# Check if process is running
ps aux | grep myapp

# Check logs
journalctl -u projection-worker@0 -n 100

# Check for deadlocks
docker logs projection-worker-0 | grep -i deadlock
```

**Solutions:**
1. Restart projection process
2. Check for infinite loops in projection code
3. Verify database connectivity
4. Check for blocking locks in database

## See Also

- [Scaling Guide](./scaling.md) - Projection scaling patterns
- [Core Concepts](./core-concepts.md) - Understanding the architecture
- [Examples](../examples/) - Deployment examples
