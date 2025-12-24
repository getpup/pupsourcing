package runner

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/getpup/pupsourcing/es"
	"github.com/getpup/pupsourcing/es/projection"
)

// mockEventReader implements store.EventReader for testing
type mockEventReader struct {
	events []es.PersistedEvent
}

func (m *mockEventReader) ReadEvents(_ context.Context, _ es.DBTX, fromPosition int64, limit int) ([]es.PersistedEvent, error) {
	var result []es.PersistedEvent
	for i := range m.events {
		if m.events[i].GlobalPosition > fromPosition {
			result = append(result, m.events[i])
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// mockCheckpointStore implements store.CheckpointStore for testing
type mockCheckpointStore struct {
	mu          sync.Mutex
	checkpoints map[string]int64
}

func newMockCheckpointStore() *mockCheckpointStore {
	return &mockCheckpointStore{
		checkpoints: make(map[string]int64),
	}
}

func (m *mockCheckpointStore) GetCheckpoint(_ context.Context, _ es.DBTX, projectionName string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkpoints[projectionName], nil
}

func (m *mockCheckpointStore) UpdateCheckpoint(_ context.Context, _ es.DBTX, projectionName string, position int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[projectionName] = position
	return nil
}

// mockTxProvider implements projection.TxProvider for testing
type mockTxProvider struct{}

func (m *mockTxProvider) BeginTx(_ context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
	// For unit tests, we don't actually need a real transaction
	// The tests mock out the checkpoint store and event reader
	return nil, errors.New("transaction not supported in mock")
}

// mockProjection implements projection.Projection for testing
type mockProjection struct {
	name          string
	handleCount   int32
	shouldFail    bool
	handleDelay   time.Duration
	lastProcessed int64
}

func (m *mockProjection) Name() string {
	return m.name
}

//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
func (m *mockProjection) Handle(_ context.Context, _ es.DBTX, event es.PersistedEvent) error {
	if m.handleDelay > 0 {
		time.Sleep(m.handleDelay)
	}

	atomic.AddInt32(&m.handleCount, 1)
	atomic.StoreInt64(&m.lastProcessed, event.GlobalPosition)

	if m.shouldFail {
		return errors.New("mock projection error")
	}
	return nil
}

func TestRunner_Run_NoProjections(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	err := runner.Run(context.Background(), []ProjectionConfig{})
	if !errors.Is(err, ErrNoProjections) {
		t.Errorf("Expected ErrNoProjections, got %v", err)
	}
}

func TestRunner_Run_NilProjection(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	configs := []ProjectionConfig{
		{
			Projection:      nil,
			ProcessorConfig: projection.DefaultProcessorConfig(),
		},
	}

	err := runner.Run(context.Background(), configs)
	if err == nil || err.Error() != "projection at index 0 is nil" {
		t.Errorf("Expected nil projection error, got %v", err)
	}
}

func TestRunner_Run_InvalidPartitionKey(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	config := projection.DefaultProcessorConfig()
	config.PartitionKey = -1

	configs := []ProjectionConfig{
		{
			Projection:      &mockProjection{name: "test"},
			ProcessorConfig: config,
		},
	}

	err := runner.Run(context.Background(), configs)
	if !errors.Is(err, ErrInvalidPartitionConfig) {
		t.Errorf("Expected ErrInvalidPartitionConfig, got %v", err)
	}
}

func TestRunner_Run_InvalidTotalPartitions(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	config := projection.DefaultProcessorConfig()
	config.TotalPartitions = 0

	configs := []ProjectionConfig{
		{
			Projection:      &mockProjection{name: "test"},
			ProcessorConfig: config,
		},
	}

	err := runner.Run(context.Background(), configs)
	if !errors.Is(err, ErrInvalidPartitionConfig) {
		t.Errorf("Expected ErrInvalidPartitionConfig, got %v", err)
	}
}

func TestRunner_Run_PartitionKeyOutOfRange(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	config := projection.DefaultProcessorConfig()
	config.PartitionKey = 4
	config.TotalPartitions = 4

	configs := []ProjectionConfig{
		{
			Projection:      &mockProjection{name: "test"},
			ProcessorConfig: config,
		},
	}

	err := runner.Run(context.Background(), configs)
	if !errors.Is(err, ErrInvalidPartitionConfig) {
		t.Errorf("Expected ErrInvalidPartitionConfig, got %v", err)
	}
}

func TestRunner_Run_ContextCancellation(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	configs := []ProjectionConfig{
		{
			Projection:      &mockProjection{name: "test"},
			ProcessorConfig: projection.DefaultProcessorConfig(),
		},
	}

	err := runner.Run(ctx, configs)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestRunProjectionPartitions_InvalidTotalPartitions(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	proj := &mockProjection{name: "test"}

	err := RunProjectionPartitions(context.Background(), txProvider, eventReader, checkpointStore, proj, 0)
	if !errors.Is(err, ErrInvalidPartitionConfig) {
		t.Errorf("Expected ErrInvalidPartitionConfig, got %v", err)
	}

	err = RunProjectionPartitions(context.Background(), txProvider, eventReader, checkpointStore, proj, -1)
	if !errors.Is(err, ErrInvalidPartitionConfig) {
		t.Errorf("Expected ErrInvalidPartitionConfig, got %v", err)
	}
}

func TestRunProjectionPartitions_CreatesCorrectConfigs(t *testing.T) {
	// This test validates that the helper creates the correct number of configs
	// We can't easily test the actual running without a real database,
	// but we can test the configuration logic by checking for validation errors

	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()
	proj := &mockProjection{name: "test"}

	// Create a context that we'll cancel immediately to avoid actual processing
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should create 4 partition configs without validation errors
	err := RunProjectionPartitions(ctx, txProvider, eventReader, checkpointStore, proj, 4)
	// We expect context.Canceled, not a validation error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestRunMultipleProjections_CallsRunnerRun(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}
	checkpointStore := newMockCheckpointStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	configs := []ProjectionConfig{
		{
			Projection:      &mockProjection{name: "test1"},
			ProcessorConfig: projection.DefaultProcessorConfig(),
		},
	}

	err := RunMultipleProjections(ctx, txProvider, eventReader, checkpointStore, configs)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestNew(t *testing.T) {
	txProvider := &mockTxProvider{}
	eventReader := &mockEventReader{}

	checkpointStore := newMockCheckpointStore()
	runner := New(txProvider, eventReader, checkpointStore)
	if runner == nil {
		t.Fatal("New returned nil")
	}
	if runner.txProvider != txProvider {
		t.Error("Runner txProvider not set correctly")
	}
	if runner.eventReader != eventReader {
		t.Error("Runner eventReader not set correctly")
	}
	if runner.checkpointStore != checkpointStore {
		t.Error("Runner checkpointStore not set correctly")
	}
}

func TestProjectionConfig_Structure(t *testing.T) {
	// Test that ProjectionConfig can be created with all fields
	proj := &mockProjection{name: "test"}
	config := projection.DefaultProcessorConfig()

	pc := ProjectionConfig{
		Projection:      proj,
		ProcessorConfig: config,
	}

	if pc.Projection != proj {
		t.Error("Projection not set correctly")
	}
	if pc.ProcessorConfig.BatchSize != config.BatchSize {
		t.Error("ProcessorConfig not set correctly")
	}
}

// TestHashPartitionStrategy_Integration verifies hash partitioning works correctly
// when used with the runner (even though the runner itself is partition-agnostic)
func TestHashPartitionStrategy_Integration(t *testing.T) {
	strategy := projection.HashPartitionStrategy{}

	// Generate some aggregate IDs
	ids := make([]uuid.UUID, 100)
	for i := range ids {
		ids[i] = uuid.New()
	}

	// Verify each ID is handled by exactly one partition
	for _, id := range ids {
		handledBy := 0
		for partitionKey := 0; partitionKey < 4; partitionKey++ {
			if strategy.ShouldProcess(id.String(), partitionKey, 4) {
				handledBy++
			}
		}
		if handledBy != 1 {
			t.Errorf("Aggregate %s handled by %d partitions, expected 1", id, handledBy)
		}
	}
}
