package projections

import (
	"errors"
	"testing"
)

func TestNewSnapshotProjection(t *testing.T) {
	config := DefaultSnapshotConfig()
	proj, err := NewSnapshotProjection(config)

	if err != nil {
		t.Fatalf("NewSnapshotProjection returned error: %v", err)
	}

	if proj == nil {
		t.Fatal("NewSnapshotProjection returned nil")
	}

	if proj.Name() != "snapshot_projection" {
		t.Errorf("Expected projection name 'snapshot_projection', got '%s'", proj.Name())
	}
}

func TestSnapshotProjection_Name(t *testing.T) {
	proj, err := NewSnapshotProjection(DefaultSnapshotConfig())
	if err != nil {
		t.Fatalf("NewSnapshotProjection returned error: %v", err)
	}

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
	proj, err := NewSnapshotProjection(config)

	if err != nil {
		t.Fatalf("NewSnapshotProjection returned error: %v", err)
	}

	if proj.config.SnapshotsTable != "custom_snapshots" {
		t.Errorf("Expected custom table name 'custom_snapshots', got '%s'", proj.config.SnapshotsTable)
	}
}

func TestSnapshotProjection_InvalidTableName(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
	}{
		{"with semicolon", "table;DROP TABLE users"},
		{"with space", "my table"},
		{"with dash", "my-table"},
		{"with quote", "table'"},
		{"empty", ""},
		{"starting with number", "1table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := SnapshotConfig{
				SnapshotsTable: tt.tableName,
			}
			proj, err := NewSnapshotProjection(config)

			if err == nil {
				t.Errorf("Expected error for invalid table name %q, got nil", tt.tableName)
			}

			if proj != nil {
				t.Errorf("Expected nil projection for invalid table name, got non-nil")
			}

			if err != nil && !errors.Is(err, ErrInvalidTableName) {
				t.Errorf("Expected ErrInvalidTableName, got %v", err)
			}
		})
	}
}

func TestSnapshotProjection_ValidTableNames(t *testing.T) {
	tests := []string{
		"snapshots",
		"custom_snapshots",
		"my_table_123",
		"_table",
		"TABLE",
		"SnapshotsTable",
	}

	for _, tableName := range tests {
		t.Run(tableName, func(t *testing.T) {
			config := SnapshotConfig{
				SnapshotsTable: tableName,
			}
			proj, err := NewSnapshotProjection(config)

			if err != nil {
				t.Errorf("Expected no error for valid table name %q, got %v", tableName, err)
			}

			if proj == nil {
				t.Errorf("Expected non-nil projection for valid table name %q", tableName)
			}
		})
	}
}
