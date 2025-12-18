package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePostgres(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		OutputFolder:     tmpDir,
		OutputFilename:   "test_migration.sql",
		EventsTable:      "events",
		CheckpointsTable: "projection_checkpoints",
	}

	err := GeneratePostgres(config)
	if err != nil {
		t.Fatalf("GeneratePostgres failed: %v", err)
	}

	// Verify file was created
	outputPath := filepath.Join(tmpDir, config.OutputFilename)
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	sql := string(content)

	// Verify essential components are present
	requiredStrings := []string{
		"CREATE TABLE IF NOT EXISTS events",
		"global_position BIGSERIAL PRIMARY KEY",
		"aggregate_type TEXT NOT NULL",
		"aggregate_id UUID NOT NULL",
		"aggregate_version BIGINT NOT NULL",
		"event_id UUID NOT NULL UNIQUE",
		"event_type TEXT NOT NULL",
		"event_version INT NOT NULL DEFAULT 1",
		"payload BYTEA NOT NULL",
		"trace_id UUID",
		"correlation_id UUID",
		"causation_id UUID",
		"metadata JSONB",
		"created_at TIMESTAMPTZ NOT NULL",
		"CREATE TABLE IF NOT EXISTS projection_checkpoints",
		"projection_name TEXT PRIMARY KEY",
		"last_global_position BIGINT NOT NULL",
		"updated_at TIMESTAMPTZ NOT NULL",
	}

	for _, required := range requiredStrings {
		if !strings.Contains(sql, required) {
			t.Errorf("Generated SQL missing required string: %s", required)
		}
	}

	// Verify indexes are created
	requiredIndexes := []string{
		"idx_events_aggregate",
		"idx_events_event_type",
		"idx_events_correlation",
		"idx_projection_checkpoints_updated",
	}

	for _, idx := range requiredIndexes {
		if !strings.Contains(sql, idx) {
			t.Errorf("Generated SQL missing index: %s", idx)
		}
	}

	// Verify comments about design decisions
	if !strings.Contains(sql, "BYTEA for payload") {
		t.Error("Missing comment explaining BYTEA choice for payload")
	}
	if !strings.Contains(sql, "projection_checkpoints") {
		t.Error("Missing projection_checkpoints table")
	}
}

func TestGeneratePostgres_CustomTableNames(t *testing.T) {
	tmpDir := t.TempDir()

	config := Config{
		OutputFolder:     tmpDir,
		OutputFilename:   "custom_migration.sql",
		EventsTable:      "custom_events",
		CheckpointsTable: "custom_checkpoints",
	}

	err := GeneratePostgres(config)
	if err != nil {
		t.Fatalf("GeneratePostgres failed: %v", err)
	}

	outputPath := filepath.Join(tmpDir, config.OutputFilename)
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	sql := string(content)

	// Verify custom table names are used
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS custom_events") {
		t.Error("Custom events table name not used")
	}
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS custom_checkpoints") {
		t.Error("Custom checkpoints table name not used")
	}
}

func TestGenerateSnapshotsPostgres(t *testing.T) {
	tmpDir := t.TempDir()

	config := SnapshotsConfig{
		OutputFolder:   tmpDir,
		OutputFilename: "test_snapshots_migration.sql",
		SnapshotsTable: "snapshots",
	}

	err := GenerateSnapshotsPostgres(config)
	if err != nil {
		t.Fatalf("GenerateSnapshotsPostgres failed: %v", err)
	}

	// Verify file was created
	outputPath := filepath.Join(tmpDir, config.OutputFilename)
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	sql := string(content)

	// Verify essential components are present
	requiredStrings := []string{
		"CREATE TABLE IF NOT EXISTS snapshots",
		"aggregate_type TEXT NOT NULL",
		"aggregate_id UUID NOT NULL",
		"aggregate_version BIGINT NOT NULL",
		"payload BYTEA NOT NULL",
		"metadata JSONB",
		"created_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (aggregate_type, aggregate_id)",
	}

	for _, required := range requiredStrings {
		if !strings.Contains(sql, required) {
			t.Errorf("Generated SQL missing required string: %s", required)
		}
	}

	// Verify indexes are created
	requiredIndexes := []string{
		"idx_snapshots_type",
		"idx_snapshots_version",
	}

	for _, idx := range requiredIndexes {
		if !strings.Contains(sql, idx) {
			t.Errorf("Generated SQL missing index: %s", idx)
		}
	}
}

func TestGenerateSnapshotsPostgres_CustomTableName(t *testing.T) {
	tmpDir := t.TempDir()

	config := SnapshotsConfig{
		OutputFolder:   tmpDir,
		OutputFilename: "custom_snapshots_migration.sql",
		SnapshotsTable: "custom_snapshots",
	}

	err := GenerateSnapshotsPostgres(config)
	if err != nil {
		t.Fatalf("GenerateSnapshotsPostgres failed: %v", err)
	}

	outputPath := filepath.Join(tmpDir, config.OutputFilename)
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	sql := string(content)

	// Verify custom table name is used
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS custom_snapshots") {
		t.Error("Custom snapshots table name not used")
	}

	// Verify indexes use custom table name
	if !strings.Contains(sql, "idx_custom_snapshots_type") {
		t.Error("Custom table name not used in index names")
	}
}
