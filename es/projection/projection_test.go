package projection

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/getpup/pupsourcing/es"
)

// mockGlobalProjection is a projection that receives all events
type mockGlobalProjection struct {
	name           string
	receivedEvents []es.PersistedEvent
}

func (p *mockGlobalProjection) Name() string {
	return p.name
}

//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
func (p *mockGlobalProjection) Handle(_ context.Context, _ es.DBTX, event es.PersistedEvent) error {
	p.receivedEvents = append(p.receivedEvents, event)
	return nil
}

// mockScopedProjection is a projection that only receives specific aggregate types
type mockScopedProjection struct {
	name           string
	aggregateTypes []string
	receivedEvents []es.PersistedEvent
}

func (p *mockScopedProjection) Name() string {
	return p.name
}

func (p *mockScopedProjection) AggregateTypes() []string {
	return p.aggregateTypes
}

//nolint:gocritic // hugeParam: Intentionally pass by value to enforce immutability
func (p *mockScopedProjection) Handle(_ context.Context, _ es.DBTX, event es.PersistedEvent) error {
	p.receivedEvents = append(p.receivedEvents, event)
	return nil
}

func TestScopedProjection_Interface(_ *testing.T) {
	// Test that mockScopedProjection implements both interfaces
	var _ Projection = &mockScopedProjection{}
	var _ ScopedProjection = &mockScopedProjection{}

	// Test that mockGlobalProjection implements only Projection
	var _ Projection = &mockGlobalProjection{}
}

func TestScopedProjection_TypeAssertion(t *testing.T) {
	globalProj := &mockGlobalProjection{name: "global"}
	scopedProj := &mockScopedProjection{name: "scoped", aggregateTypes: []string{"User"}}

	// Global projection should not be a ScopedProjection
	if _, ok := Projection(globalProj).(ScopedProjection); ok {
		t.Error("Global projection should not implement ScopedProjection")
	}

	// Scoped projection should be a ScopedProjection
	if _, ok := Projection(scopedProj).(ScopedProjection); !ok {
		t.Error("Scoped projection should implement ScopedProjection")
	}
}

func TestScopedProjection_EmptyAggregateTypes(t *testing.T) {
	// Test that empty aggregate types list is valid
	scopedProj := &mockScopedProjection{
		name:           "scoped_empty",
		aggregateTypes: []string{},
	}

	types := scopedProj.AggregateTypes()
	if types == nil {
		t.Error("AggregateTypes should not return nil")
	}
	if len(types) != 0 {
		t.Errorf("Expected empty slice, got %v", types)
	}
}

func TestHashPartitionStrategy_SinglePartition(t *testing.T) {
	strategy := HashPartitionStrategy{}

	// With single partition, all events should be processed
	aggregateID := uuid.New().String()

	if !strategy.ShouldProcess(aggregateID, 0, 1) {
		t.Error("Single partition should process all events")
	}
}

func TestHashPartitionStrategy_MultiplePartitions(t *testing.T) {
	strategy := HashPartitionStrategy{}
	totalPartitions := 4

	// Test that each aggregate ID maps to exactly one partition
	for i := 0; i < 100; i++ {
		aggregateID := uuid.New().String()
		processedBy := 0

		for partition := 0; partition < totalPartitions; partition++ {
			if strategy.ShouldProcess(aggregateID, partition, totalPartitions) {
				processedBy++
			}
		}

		if processedBy != 1 {
			t.Errorf("Aggregate %s processed by %d partitions, expected 1", aggregateID, processedBy)
		}
	}
}

func TestHashPartitionStrategy_Deterministic(t *testing.T) {
	strategy := HashPartitionStrategy{}
	aggregateID := uuid.New().String()
	totalPartitions := 4

	// First call
	var assignedPartition int
	for partition := 0; partition < totalPartitions; partition++ {
		if strategy.ShouldProcess(aggregateID, partition, totalPartitions) {
			assignedPartition = partition
			break
		}
	}

	// Subsequent calls should return same result
	for i := 0; i < 10; i++ {
		if !strategy.ShouldProcess(aggregateID, assignedPartition, totalPartitions) {
			t.Error("Partition assignment is not deterministic")
		}

		// Other partitions should not process this aggregate
		for partition := 0; partition < totalPartitions; partition++ {
			if partition == assignedPartition {
				continue
			}
			if strategy.ShouldProcess(aggregateID, partition, totalPartitions) {
				t.Errorf("Aggregate assigned to multiple partitions")
			}
		}
	}
}

func TestHashPartitionStrategy_Distribution(t *testing.T) {
	strategy := HashPartitionStrategy{}
	totalPartitions := 4
	iterations := 1000

	// Count assignments per partition
	counts := make([]int, totalPartitions)

	for i := 0; i < iterations; i++ {
		aggregateID := uuid.New().String()
		for partition := 0; partition < totalPartitions; partition++ {
			if strategy.ShouldProcess(aggregateID, partition, totalPartitions) {
				counts[partition]++
			}
		}
	}

	// Check that distribution is reasonably even
	// Each partition should get roughly 25% (250 ± 75 for 1000 iterations)
	expectedCount := iterations / totalPartitions
	tolerance := expectedCount / 3 // 33% tolerance

	for partition, count := range counts {
		if count < expectedCount-tolerance || count > expectedCount+tolerance {
			t.Logf("Partition distribution: %v", counts)
			t.Errorf("Partition %d has %d assignments, expected %d ± %d",
				partition, count, expectedCount, tolerance)
		}
	}
}
