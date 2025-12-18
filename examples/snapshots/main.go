// Package main demonstrates using the snapshot projection.
//
// This example shows how to:
// 1. Set up a database with events and snapshots tables
// 2. Append events to the event store
// 3. Run the snapshot projection to maintain aggregate snapshots
//
// Note: This is a demonstration. In production, you'd typically:
// - Run the projection in a separate process/goroutine
// - Use proper database configuration
// - Add proper error handling and logging
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/adapters/postgres"
	"github.com/getpup/pupsourcing/es/adapters/postgres/projections"
)

func main() {
	fmt.Println("Snapshot Projection Example")
	fmt.Println("============================")
	fmt.Println()
	fmt.Println("This example demonstrates the snapshot projection feature.")
	fmt.Println("In a real application:")
	fmt.Println("  1. Apply database migrations using migrate-gen and snapshots-migrate-gen")
	fmt.Println("  2. Run the snapshot projection in a background process")
	fmt.Println("  3. Query snapshots for fast aggregate reconstitution")
	fmt.Println()

	// Simulate appending events
	aggregateID := uuid.New()
	events := createSampleEvents(aggregateID, 5)

	fmt.Printf("Created %d sample events for aggregate %s\n", len(events), aggregateID)
	fmt.Println()

	// Create configurations
	storeConfig := postgres.DefaultStoreConfig()
	snapshotConfig := projections.DefaultSnapshotConfig()

	fmt.Println("Configuration:")
	fmt.Printf("  Events table: %s\n", storeConfig.EventsTable)
	fmt.Printf("  Checkpoints table: %s\n", storeConfig.CheckpointsTable)
	fmt.Printf("  Snapshots table: %s\n", storeConfig.SnapshotsTable)
	fmt.Println()

	// Create snapshot projection
	snapshotProj, err := projections.NewSnapshotProjection(snapshotConfig)
	if err != nil {
		log.Fatalf("Failed to create snapshot projection: %v", err)
	}

	fmt.Printf("Snapshot projection created: %s\n", snapshotProj.Name())
	fmt.Println()

	fmt.Println("To use this in production:")
	fmt.Println("  1. Generate migrations:")
	fmt.Println("     go run github.com/getpup/pupsourcing/cmd/migrate-gen -output migrations")
	fmt.Println("     go run github.com/getpup/pupsourcing/cmd/snapshots-migrate-gen -output migrations")
	fmt.Println()
	fmt.Println("  2. Apply migrations to your database")
	fmt.Println()
	fmt.Println("  3. Run the projection:")
	fmt.Println("     processor := projection.NewProcessor(db, store, processorConfig)")
	fmt.Println("     err := processor.Run(ctx, snapshotProj)")
	fmt.Println()
	fmt.Println("The snapshot projection will automatically:")
	fmt.Println("  - Listen to all events from the event store")
	fmt.Println("  - Update snapshots for each aggregate")
	fmt.Println("  - Track progress via checkpoints")
	fmt.Println("  - Resume from last checkpoint on restart")
}

func createSampleEvents(aggregateID uuid.UUID, count int) []es.Event {
	events := make([]es.Event, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		events[i] = es.Event{
			AggregateType: "SampleAggregate",
			AggregateID:   aggregateID,
			EventID:       uuid.New(),
			EventType:     fmt.Sprintf("SampleEvent%d", i+1),
			EventVersion:  1,
			Payload:       []byte(fmt.Sprintf(`{"state": "version%d", "data": "example"}`, i+1)),
			Metadata:      []byte(`{"source": "example"}`),
			CreatedAt:     now.Add(time.Duration(i) * time.Second),
		}
	}

	return events
}
