//go:build integration
// +build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/adapters/postgres"
	"github.com/getpup/pupsourcing/es/adapters/postgres/projections"
	"github.com/getpup/pupsourcing/es/projection"
)

func getTestDB(t *testing.T) *sql.DB {
	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	user := getEnv("POSTGRES_USER", "postgres")
	password := getEnv("POSTGRES_PASSWORD", "postgres")
	dbname := getEnv("POSTGRES_DB", "pupsourcing_test")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	return db
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func setupTestSchema(t *testing.T, db *sql.DB) {
	ctx := context.Background()

	// Drop tables if they exist
	_, err := db.ExecContext(ctx, `
		DROP TABLE IF EXISTS test_events CASCADE;
		DROP TABLE IF EXISTS test_checkpoints CASCADE;
		DROP TABLE IF EXISTS test_snapshots CASCADE;
	`)
	if err != nil {
		t.Fatalf("Failed to drop tables: %v", err)
	}

	// Create events table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE test_events (
			global_position BIGSERIAL PRIMARY KEY,
			aggregate_type TEXT NOT NULL,
			aggregate_id UUID NOT NULL,
			aggregate_version BIGINT NOT NULL,
			event_id UUID NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			event_version INT NOT NULL DEFAULT 1,
			payload BYTEA NOT NULL,
			trace_id UUID,
			correlation_id UUID,
			causation_id UUID,
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (aggregate_type, aggregate_id, aggregate_version)
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create events table: %v", err)
	}

	// Create checkpoints table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE test_checkpoints (
			projection_name TEXT PRIMARY KEY,
			last_global_position BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create checkpoints table: %v", err)
	}

	// Create snapshots table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE test_snapshots (
			aggregate_type TEXT NOT NULL,
			aggregate_id UUID NOT NULL,
			aggregate_version BIGINT NOT NULL,
			payload BYTEA NOT NULL,
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (aggregate_type, aggregate_id)
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create snapshots table: %v", err)
	}
}

func TestSnapshotProjection_Integration(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestSchema(t, db)

	ctx := context.Background()

	// Create store and projection
	storeConfig := postgres.StoreConfig{
		EventsTable:      "test_events",
		CheckpointsTable: "test_checkpoints",
		SnapshotsTable:   "test_snapshots",
	}
	store := postgres.NewStore(storeConfig)

	snapshotConfig := projections.SnapshotConfig{
		SnapshotsTable: "test_snapshots",
	}
	snapshotProj := projections.NewSnapshotProjection(snapshotConfig)

	// Append some events
	aggregateID := uuid.New()
	events := []es.Event{
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEvent1",
			EventVersion:  1,
			Payload:       []byte(`{"state": "version1"}`),
			Metadata:      []byte(`{"meta": "data1"}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEvent2",
			EventVersion:  1,
			Payload:       []byte(`{"state": "version2"}`),
			Metadata:      []byte(`{"meta": "data2"}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	_, err = store.Append(ctx, tx, events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// Process events with snapshot projection
	procConfig := projection.ProcessorConfig{
		EventsTable:      "test_events",
		CheckpointsTable: "test_checkpoints",
		BatchSize:        10,
		PartitionKey:     0,
		TotalPartitions:  1,
	}
	processor := projection.NewProcessor(db, store, procConfig)

	// Process one batch
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- processor.Run(ctx, snapshotProj)
	}()

	// Wait for processing or timeout
	select {
	case err := <-errChan:
		if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
			t.Fatalf("Projection processing failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Projection processing timed out")
	}

	// Verify snapshot was created
	var snapshotVersion int64
	var snapshotPayload []byte
	err = db.QueryRowContext(context.Background(), `
		SELECT aggregate_version, payload
		FROM test_snapshots
		WHERE aggregate_type = $1 AND aggregate_id = $2
	`, "TestAggregate", aggregateID).Scan(&snapshotVersion, &snapshotPayload)

	if err != nil {
		t.Fatalf("Failed to query snapshot: %v", err)
	}

	// Snapshot should reflect the last event (version 2)
	if snapshotVersion != 2 {
		t.Errorf("Expected snapshot version 2, got %d", snapshotVersion)
	}

	expectedPayload := `{"state": "version2"}`
	if string(snapshotPayload) != expectedPayload {
		t.Errorf("Expected payload %q, got %q", expectedPayload, string(snapshotPayload))
	}
}

func TestSnapshotProjection_UpdatesExisting(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestSchema(t, db)

	ctx := context.Background()

	// Create store and projection
	storeConfig := postgres.StoreConfig{
		EventsTable:      "test_events",
		CheckpointsTable: "test_checkpoints",
		SnapshotsTable:   "test_snapshots",
	}
	store := postgres.NewStore(storeConfig)

	snapshotConfig := projections.SnapshotConfig{
		SnapshotsTable: "test_snapshots",
	}
	snapshotProj := projections.NewSnapshotProjection(snapshotConfig)

	aggregateID := uuid.New()

	// Append first event and process
	event1 := []es.Event{
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEvent1",
			EventVersion:  1,
			Payload:       []byte(`{"state": "v1"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	store.Append(ctx, tx, event1)
	tx.Commit()

	// Process first event
	procConfig := projection.ProcessorConfig{
		EventsTable:      "test_events",
		CheckpointsTable: "test_checkpoints",
		BatchSize:        10,
	}
	processor := projection.NewProcessor(db, store, procConfig)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel1()

	go processor.Run(ctx1, snapshotProj)
	time.Sleep(500 * time.Millisecond)

	// Verify first snapshot
	var count int
	db.QueryRow("SELECT COUNT(*) FROM test_snapshots WHERE aggregate_id = $1", aggregateID).Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 snapshot after first event, got %d", count)
	}

	// Append second event
	event2 := []es.Event{
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEvent2",
			EventVersion:  1,
			Payload:       []byte(`{"state": "v2"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx2, _ := db.BeginTx(ctx, nil)
	store.Append(ctx, tx2, event2)
	tx2.Commit()

	// Process second event
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	go processor.Run(ctx2, snapshotProj)
	time.Sleep(500 * time.Millisecond)

	// Verify still only one snapshot (updated, not inserted)
	db.QueryRow("SELECT COUNT(*) FROM test_snapshots WHERE aggregate_id = $1", aggregateID).Scan(&count)
	if count != 1 {
		t.Errorf("Expected still 1 snapshot after second event, got %d", count)
	}

	// Verify snapshot has latest data
	var snapshotPayload []byte
	db.QueryRow("SELECT payload FROM test_snapshots WHERE aggregate_id = $1", aggregateID).Scan(&snapshotPayload)
	expectedPayload := `{"state": "v2"}`
	if string(snapshotPayload) != expectedPayload {
		t.Errorf("Expected payload %q, got %q", expectedPayload, string(snapshotPayload))
	}
}
