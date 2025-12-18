// Package integration_test contains integration tests for the Postgres adapter.
// These tests require a running PostgreSQL instance.
//
// Run with: go test -tags=integration ./es/adapters/postgres/integration_test/...
//
//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/adapters/postgres"
	"github.com/getpup/pupsourcing/es/migrations"
	"github.com/getpup/pupsourcing/es/store"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Default to localhost, but allow override via env var for CI
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		user = "postgres"
	}

	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		password = "postgres"
	}

	dbname := os.Getenv("POSTGRES_DB")
	if dbname == "" {
		dbname = "pupsourcing_test"
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	return db
}

func setupTestTables(t *testing.T, db *sql.DB) {
	t.Helper()

	// Drop existing objects to ensure clean state
	_, err := db.Exec(`
		DROP TABLE IF EXISTS projection_checkpoints CASCADE;
		DROP TABLE IF EXISTS events CASCADE;
	`)
	if err != nil {
		t.Fatalf("Failed to drop tables: %v", err)
	}

	// Generate and execute migration
	tmpDir := t.TempDir()
	config := migrations.Config{
		OutputFolder:     tmpDir,
		OutputFilename:   "test.sql",
		EventsTable:      "events",
		CheckpointsTable: "projection_checkpoints",
	}

	if err := migrations.GeneratePostgres(config); err != nil {
		t.Fatalf("Failed to generate migration: %v", err)
	}

	migrationSQL, err := os.ReadFile(fmt.Sprintf("%s/%s", tmpDir, config.OutputFilename))
	if err != nil {
		t.Fatalf("Failed to read migration: %v", err)
	}

	_, err = db.Exec(string(migrationSQL))
	if err != nil {
		t.Fatalf("Failed to execute migration: %v", err)
	}
}

func TestAppendEvents(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Create test events
	aggregateID := uuid.New()
	events := []es.Event{
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEventCreated",
			EventVersion:  1,
			Payload:       []byte(`{"test":"data"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     "TestEventUpdated",
			EventVersion:  1,
			Payload:       []byte(`{"test":"updated"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	positions, err := store.Append(ctx, tx, events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}

	if len(positions) != len(events) {
		t.Errorf("Expected %d positions, got %d", len(events), len(positions))
	}

	// Verify positions are sequential
	for i := 1; i < len(positions); i++ {
		if positions[i] != positions[i-1]+1 {
			t.Errorf("Positions not sequential: %v", positions)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}
}

func TestAppendEvents_OptimisticConcurrency(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	str := postgres.NewStore(postgres.DefaultStoreConfig())

	aggregateID := uuid.New()

	// First append succeeds
	event1 := es.Event{
		AggregateType: "TestAggregate",
		AggregateID:   aggregateID,
		EventID:       uuid.New(),
		EventType:     "TestEventCreated",
		EventVersion:  1,
		Payload:       []byte(`{}`),
		Metadata:      []byte(`{}`),
		CreatedAt:     time.Now(),
	}

	tx1, _ := db.BeginTx(ctx, nil)
	defer tx1.Rollback()

	_, err := str.Append(ctx, tx1, []es.Event{event1})
	if err != nil {
		t.Fatalf("First append failed: %v", err)
	}
	tx1.Commit()

	// Second concurrent append should fail due to optimistic concurrency
	// Both transactions try to append to the same aggregate simultaneously
	event2 := es.Event{
		AggregateType: "TestAggregate",
		AggregateID:   aggregateID,
		EventID:       uuid.New(),
		EventType:     "TestEventUpdated",
		EventVersion:  1,
		Payload:       []byte(`{}`),
		Metadata:      []byte(`{}`),
		CreatedAt:     time.Now(),
	}

	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()

	_, err = str.Append(ctx, tx2, []es.Event{event2})
	if !errors.Is(err, store.ErrOptimisticConcurrency) {
		t.Errorf("Expected optimistic concurrency error, got: %v", err)
	}
}

func TestReadEvents(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	pgStore := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append some events
	aggregateID1 := uuid.New()
	aggregateID2 := uuid.New()

	events := []es.Event{
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID1,
			EventID:       uuid.New(),
			EventType:     "Event1",
			EventVersion:  1,
			Payload:       []byte(`{}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "TestAggregate",
			AggregateID:   aggregateID2,
			EventID:       uuid.New(),
			EventType:     "Event2",
			EventVersion:  1,
			Payload:       []byte(`{}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := pgStore.Append(ctx, tx, events[:1])
	if err != nil {
		t.Fatalf("Failed to append first event: %v", err)
	}
	_, err = pgStore.Append(ctx, tx, events[1:])
	if err != nil {
		t.Fatalf("Failed to append second event: %v", err)
	}
	tx.Commit()

	// Read events
	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()

	readEvents, err := pgStore.ReadEvents(ctx, tx2, 0, 10)
	if err != nil {
		t.Fatalf("Failed to read events: %v", err)
	}

	if len(readEvents) != 2 {
		t.Errorf("Expected 2 events, got %d", len(readEvents))
	}

	// Verify ordering
	if readEvents[0].GlobalPosition >= readEvents[1].GlobalPosition {
		t.Error("Events not ordered by global position")
	}
}

func TestReadEvents_Pagination(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	pgStore := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append multiple events
	for i := 0; i < 5; i++ {
		event := es.Event{
			AggregateType: "TestAggregate",
			AggregateID:   uuid.New(),
			EventID:       uuid.New(),
			EventType:     fmt.Sprintf("Event%d", i),
			EventVersion:  1,
			Payload:       []byte(`{}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		}

		tx, _ := db.BeginTx(ctx, nil)
		_, err := pgStore.Append(ctx, tx, []es.Event{event})
		if err != nil {
			t.Fatalf("Failed to append event: %v", err)
		}
		tx.Commit()
	}

	// Read first batch
	tx1, _ := db.BeginTx(ctx, nil)
	defer tx1.Rollback()

	batch1, err := pgStore.ReadEvents(ctx, tx1, 0, 2)
	if err != nil {
		t.Fatalf("Failed to read first batch: %v", err)
	}

	if len(batch1) != 2 {
		t.Errorf("Expected 2 events in first batch, got %d", len(batch1))
	}

	// Read second batch
	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()

	batch2, err := pgStore.ReadEvents(ctx, tx2, batch1[len(batch1)-1].GlobalPosition, 2)
	if err != nil {
		t.Fatalf("Failed to read second batch: %v", err)
	}

	if len(batch2) != 2 {
		t.Errorf("Expected 2 events in second batch, got %d", len(batch2))
	}

	// Verify no overlap
	for _, e1 := range batch1 {
		for _, e2 := range batch2 {
			if e1.GlobalPosition == e2.GlobalPosition {
				t.Error("Batches have overlapping events")
			}
		}
	}
}
