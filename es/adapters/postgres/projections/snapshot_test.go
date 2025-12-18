package projections

import (
	"testing"
)

func TestNewSnapshotProjection(t *testing.T) {
	config := DefaultSnapshotConfig()
	proj := NewSnapshotProjection(config)

	if proj == nil {
		t.Fatal("NewSnapshotProjection returned nil")
	}

	if proj.Name() != "snapshot_projection" {
		t.Errorf("Expected projection name 'snapshot_projection', got '%s'", proj.Name())
	}
}

func TestSnapshotProjection_Name(t *testing.T) {
	proj := NewSnapshotProjection(DefaultSnapshotConfig())
	name := proj.Name()

	if name != "snapshot_projection" {
		t.Errorf("Expected name 'snapshot_projection', got '%s'", name)
	}
}

func TestDefaultSnapshotConfig(t *testing.T) {
	config := DefaultSnapshotConfig()

	if config.SnapshotsTable != "snapshots" {
		t.Errorf("Expected default table name 'snapshots', got '%s'", config.SnapshotsTable)
	}
}

func TestSnapshotProjection_CustomTableName(t *testing.T) {
	config := SnapshotConfig{
		SnapshotsTable: "custom_snapshots",
	}
	proj := NewSnapshotProjection(config)

	if proj.config.SnapshotsTable != "custom_snapshots" {
		t.Errorf("Expected custom table name 'custom_snapshots', got '%s'", proj.config.SnapshotsTable)
	}
}
