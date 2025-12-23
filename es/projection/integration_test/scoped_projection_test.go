// Package integration_test contains integration tests for projections.
// These tests require a running PostgreSQL instance.
//
// Run with: go test -tags=integration ./es/projection/integration_test/...
//
//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/adapters/postgres"
	"github.com/getpup/pupsourcing/es/projection"
	"github.com/google/uuid"
)

// globalProjection receives all events
type globalProjection struct {
	name           string
	mu             sync.Mutex
	receivedEvents []es.PersistedEvent
}

func (p *globalProjection) Name() string {
	return p.name
}

//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
func (p *globalProjection) Handle(_ context.Context, _ es.DBTX, event es.PersistedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.receivedEvents = append(p.receivedEvents, event)
	return nil
}

func (p *globalProjection) getReceivedEvents() []es.PersistedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]es.PersistedEvent, len(p.receivedEvents))
	copy(result, p.receivedEvents)
	return result
}

// scopedProjection only receives specified aggregate types
type scopedProjection struct {
	name           string
	aggregateTypes []string
	mu             sync.Mutex
	receivedEvents []es.PersistedEvent
}

func (p *scopedProjection) Name() string {
	return p.name
}

func (p *scopedProjection) AggregateTypes() []string {
	return p.aggregateTypes
}

//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
func (p *scopedProjection) Handle(_ context.Context, _ es.DBTX, event es.PersistedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.receivedEvents = append(p.receivedEvents, event)
	return nil
}

func (p *scopedProjection) getReceivedEvents() []es.PersistedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]es.PersistedEvent, len(p.receivedEvents))
	copy(result, p.receivedEvents)
	return result
}

func TestScopedProjection_GlobalReceivesAllEvents(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append events from different aggregate types
	events := []es.Event{
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserCreated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Alice"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Order",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			EventVersion:  1,
			Payload:       []byte(`{"amount":99.99}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Product",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "ProductAdded",
			EventVersion:  1,
			Payload:       []byte(`{"sku":"ABC123"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := store.Append(ctx, tx, es.Any(), events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}
	tx.Commit()

	// Create global projection
	globalProj := &globalProjection{name: "global_test"}
	config := projection.DefaultProcessorConfig()
	processor := projection.NewProcessor(db, store, &config)

	// Run projection for a short time
	ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err = processor.Run(ctx2, globalProj)
	// Accept context deadline or cancellation
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify all events were received
	receivedEvents := globalProj.getReceivedEvents()
	if len(receivedEvents) != 3 {
		t.Errorf("Expected 3 events, got %d", len(receivedEvents))
	}

	// Verify we got events from all aggregate types
	aggregateTypes := make(map[string]bool)
	for _, event := range receivedEvents {
		aggregateTypes[event.AggregateType] = true
	}

	expectedTypes := []string{"User", "Order", "Product"}
	for _, expectedType := range expectedTypes {
		if !aggregateTypes[expectedType] {
			t.Errorf("Missing events from aggregate type: %s", expectedType)
		}
	}
}

func TestScopedProjection_OnlyReceivesMatchingAggregates(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append events from different aggregate types
	events := []es.Event{
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserCreated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Alice"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserUpdated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Bob"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Order",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			EventVersion:  1,
			Payload:       []byte(`{"amount":99.99}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Product",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "ProductAdded",
			EventVersion:  1,
			Payload:       []byte(`{"sku":"ABC123"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := store.Append(ctx, tx, es.Any(), events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}
	tx.Commit()

	// Create scoped projection that only cares about User events
	scopedProj := &scopedProjection{
		name:           "user_scoped_test",
		aggregateTypes: []string{"User"},
	}
	config := projection.DefaultProcessorConfig()
	processor := projection.NewProcessor(db, store, &config)

	// Run projection for a short time
	ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err = processor.Run(ctx2, scopedProj)
	// Accept context deadline or cancellation
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify only User events were received
	receivedEvents := scopedProj.getReceivedEvents()
	if len(receivedEvents) != 2 {
		t.Errorf("Expected 2 User events, got %d", len(receivedEvents))
	}

	// Verify all received events are User events
	for _, event := range receivedEvents {
		if event.AggregateType != "User" {
			t.Errorf("Expected only User events, got %s", event.AggregateType)
		}
	}
}

func TestScopedProjection_MultipleAggregateTypes(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append events from different aggregate types
	events := []es.Event{
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserCreated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Alice"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Order",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			EventVersion:  1,
			Payload:       []byte(`{"amount":99.99}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Product",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "ProductAdded",
			EventVersion:  1,
			Payload:       []byte(`{"sku":"ABC123"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Inventory",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "InventoryAdjusted",
			EventVersion:  1,
			Payload:       []byte(`{"quantity":100}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := store.Append(ctx, tx, es.Any(), events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}
	tx.Commit()

	// Create scoped projection that cares about User and Order events
	scopedProj := &scopedProjection{
		name:           "user_order_scoped_test",
		aggregateTypes: []string{"User", "Order"},
	}
	config := projection.DefaultProcessorConfig()
	processor := projection.NewProcessor(db, store, &config)

	// Run projection for a short time
	ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err = processor.Run(ctx2, scopedProj)
	// Accept context deadline or cancellation
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify only User and Order events were received
	receivedEvents := scopedProj.getReceivedEvents()
	if len(receivedEvents) != 2 {
		t.Errorf("Expected 2 events (User and Order), got %d", len(receivedEvents))
	}

	// Verify all received events are User or Order events
	for _, event := range receivedEvents {
		if event.AggregateType != "User" && event.AggregateType != "Order" {
			t.Errorf("Expected only User or Order events, got %s", event.AggregateType)
		}
	}
}

func TestScopedProjection_EmptyAggregateTypesReceivesAll(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append events from different aggregate types
	events := []es.Event{
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserCreated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Alice"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Order",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			EventVersion:  1,
			Payload:       []byte(`{"amount":99.99}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := store.Append(ctx, tx, es.Any(), events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}
	tx.Commit()

	// Create scoped projection with empty aggregate types list
	scopedProj := &scopedProjection{
		name:           "empty_scoped_test",
		aggregateTypes: []string{},
	}
	config := projection.DefaultProcessorConfig()
	processor := projection.NewProcessor(db, store, &config)

	// Run projection for a short time
	ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err = processor.Run(ctx2, scopedProj)
	// Accept context deadline or cancellation
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify all events were received (empty list means no filtering)
	receivedEvents := scopedProj.getReceivedEvents()
	if len(receivedEvents) != 2 {
		t.Errorf("Expected 2 events (no filtering), got %d", len(receivedEvents))
	}
}

func TestScopedProjection_MixedProjectionsWorkCorrectly(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	setupTestTables(t, db)

	ctx := context.Background()
	store := postgres.NewStore(postgres.DefaultStoreConfig())

	// Append events from different aggregate types
	events := []es.Event{
		{
			AggregateType: "User",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "UserCreated",
			EventVersion:  1,
			Payload:       []byte(`{"name":"Alice"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Order",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "OrderPlaced",
			EventVersion:  1,
			Payload:       []byte(`{"amount":99.99}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
		{
			AggregateType: "Product",
			AggregateID:   uuid.New().String(),
			EventID:       uuid.New(),
			EventType:     "ProductAdded",
			EventVersion:  1,
			Payload:       []byte(`{"sku":"ABC123"}`),
			Metadata:      []byte(`{}`),
			CreatedAt:     time.Now(),
		},
	}

	tx, _ := db.BeginTx(ctx, nil)
	_, err := store.Append(ctx, tx, es.Any(), events)
	if err != nil {
		t.Fatalf("Failed to append events: %v", err)
	}
	tx.Commit()

	// Create both global and scoped projections
	globalProj := &globalProjection{name: "mixed_global_test"}
	scopedProj := &scopedProjection{
		name:           "mixed_scoped_test",
		aggregateTypes: []string{"User"},
	}

	// Run global projection
	config1 := projection.DefaultProcessorConfig()
	processor1 := projection.NewProcessor(db, store, &config1)

	ctx1, cancel1 := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel1()

	err = processor1.Run(ctx1, globalProj)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error from global projection: %v", err)
	}

	// Run scoped projection
	config2 := projection.DefaultProcessorConfig()
	processor2 := projection.NewProcessor(db, store, &config2)

	ctx2, cancel2 := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel2()

	err = processor2.Run(ctx2, scopedProj)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Unexpected error from scoped projection: %v", err)
	}

	// Verify global projection received all events
	globalEvents := globalProj.getReceivedEvents()
	if len(globalEvents) != 3 {
		t.Errorf("Global projection: expected 3 events, got %d", len(globalEvents))
	}

	// Verify scoped projection only received User events
	scopedEvents := scopedProj.getReceivedEvents()
	if len(scopedEvents) != 1 {
		t.Errorf("Scoped projection: expected 1 User event, got %d", len(scopedEvents))
	}

	if len(scopedEvents) > 0 && scopedEvents[0].AggregateType != "User" {
		t.Errorf("Scoped projection: expected User event, got %s", scopedEvents[0].AggregateType)
	}
}
