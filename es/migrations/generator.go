// Package migrations provides SQL migration generation for event sourcing infrastructure.
package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config configures migration generation.
type Config struct {
	// OutputFolder is the directory where the migration file will be written
	OutputFolder string

	// OutputFilename is the name of the migration file
	OutputFilename string

	// EventsTable is the name of the events table
	EventsTable string

	// CheckpointsTable is the name of the projection checkpoints table
	CheckpointsTable string
}

// SnapshotsConfig configures snapshots migration generation.
type SnapshotsConfig struct {
	// OutputFolder is the directory where the migration file will be written
	OutputFolder string

	// OutputFilename is the name of the migration file
	OutputFilename string

	// SnapshotsTable is the name of the snapshots table
	SnapshotsTable string
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	timestamp := time.Now().Format("20060102150405")
	return Config{
		OutputFolder:     "migrations",
		OutputFilename:   fmt.Sprintf("%s_init_event_sourcing.sql", timestamp),
		EventsTable:      "events",
		CheckpointsTable: "projection_checkpoints",
	}
}

// DefaultSnapshotsConfig returns the default snapshots configuration.
func DefaultSnapshotsConfig() SnapshotsConfig {
	timestamp := time.Now().Format("20060102150405")
	return SnapshotsConfig{
		OutputFolder:   "migrations",
		OutputFilename: fmt.Sprintf("%s_add_snapshots.sql", timestamp),
		SnapshotsTable: "snapshots",
	}
}

// GeneratePostgres generates a PostgreSQL migration file.
func GeneratePostgres(config Config) error {
	// Ensure output folder exists
	if err := os.MkdirAll(config.OutputFolder, 0o755); err != nil {
		return fmt.Errorf("failed to create output folder: %w", err)
	}

	sql := generatePostgresSQL(config)

	outputPath := filepath.Join(config.OutputFolder, config.OutputFilename)
	if err := os.WriteFile(outputPath, []byte(sql), 0o600); err != nil {
		return fmt.Errorf("failed to write migration file: %w", err)
	}

	return nil
}

func generatePostgresSQL(config Config) string {
	return fmt.Sprintf(`-- Event Sourcing Infrastructure Migration
-- Generated: %s

-- Events table stores all domain events in append-only fashion
-- Design decisions:
-- - BYTEA for payload: Supports any serialization format (JSON, Protobuf, etc.)
--   giving users flexibility without forcing a specific encoding
-- - BIGSERIAL for global_position: Ensures globally ordered event log
-- - UUID for event_id: Guarantees uniqueness even in distributed scenarios
-- - Optimistic concurrency via aggregate_version prevents race conditions
CREATE TABLE IF NOT EXISTS %s (
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
    
    -- Ensure version uniqueness per aggregate
    UNIQUE (aggregate_type, aggregate_id, aggregate_version)
);

-- Index for aggregate stream reads (get all events for an aggregate)
CREATE INDEX IF NOT EXISTS idx_%s_aggregate 
    ON %s (aggregate_type, aggregate_id, aggregate_version);

-- Index for event type queries (useful for specialized projections)
CREATE INDEX IF NOT EXISTS idx_%s_event_type 
    ON %s (event_type, global_position);

-- Index for correlation tracking
CREATE INDEX IF NOT EXISTS idx_%s_correlation 
    ON %s (correlation_id) WHERE correlation_id IS NOT NULL;

-- Projection checkpoints table tracks progress of each projection
-- Naming: "projection_checkpoints" clearly indicates purpose and scope
-- Alternative names considered:
-- - "offsets": Too generic, unclear what's being tracked
-- - "positions": Doesn't indicate it's projection-specific
-- - "projection_state": Too broad, checkpoints are specifically about position tracking
CREATE TABLE IF NOT EXISTS %s (
    projection_name TEXT PRIMARY KEY,
    last_global_position BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for checkpoint queries (though typically few rows, helps with concurrent access)
CREATE INDEX IF NOT EXISTS idx_%s_updated 
    ON %s (updated_at);
`,
		time.Now().Format(time.RFC3339),
		config.EventsTable,
		config.EventsTable, config.EventsTable,
		config.EventsTable, config.EventsTable,
		config.EventsTable, config.EventsTable,
		config.CheckpointsTable,
		config.CheckpointsTable, config.CheckpointsTable,
	)
}

// GenerateSnapshotsPostgres generates a PostgreSQL migration file for snapshots table.
func GenerateSnapshotsPostgres(config SnapshotsConfig) error {
	// Ensure output folder exists
	if err := os.MkdirAll(config.OutputFolder, 0o755); err != nil {
		return fmt.Errorf("failed to create output folder: %w", err)
	}

	sql := generateSnapshotsPostgresSQL(config)

	outputPath := filepath.Join(config.OutputFolder, config.OutputFilename)
	if err := os.WriteFile(outputPath, []byte(sql), 0o600); err != nil {
		return fmt.Errorf("failed to write migration file: %w", err)
	}

	return nil
}

func generateSnapshotsPostgresSQL(config SnapshotsConfig) string {
	return fmt.Sprintf(`-- Snapshots Table Migration
-- Generated: %s

-- Snapshots table stores the latest state of aggregates for quick reconstitution
-- Design decisions:
-- - PRIMARY KEY (aggregate_type, aggregate_id): One snapshot per aggregate
-- - aggregate_version tracks which version the snapshot represents
-- - BYTEA for payload: Consistent with events table, supports any serialization
-- - metadata as JSONB: Allows storing additional snapshot metadata
-- - created_at tracks when the snapshot was created/updated
CREATE TABLE IF NOT EXISTS %s (
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    aggregate_version BIGINT NOT NULL,
    payload BYTEA NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_type, aggregate_id)
);

-- Index for finding snapshots by aggregate type
CREATE INDEX IF NOT EXISTS idx_%s_type 
    ON %s (aggregate_type, created_at DESC);

-- Index for version lookups (useful for debugging and monitoring)
CREATE INDEX IF NOT EXISTS idx_%s_version 
    ON %s (aggregate_version DESC);
`,
		time.Now().Format(time.RFC3339),
		config.SnapshotsTable,
		config.SnapshotsTable, config.SnapshotsTable,
		config.SnapshotsTable, config.SnapshotsTable,
	)
}
