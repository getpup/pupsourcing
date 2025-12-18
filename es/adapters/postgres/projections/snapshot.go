package projections

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/getpup/pupsourcing/es"
)

var (
	// ErrInvalidTableName indicates the table name contains invalid characters
	ErrInvalidTableName = errors.New("invalid table name: must contain only alphanumeric characters and underscores")

	// validTableNameRegex validates PostgreSQL table names
	// Allows: letters, numbers, underscores (standard SQL identifiers)
	validTableNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// SnapshotConfig configures the snapshot projection.
type SnapshotConfig struct {
	// SnapshotsTable is the name of the snapshots table
	SnapshotsTable string
}

// DefaultSnapshotConfig returns the default snapshot configuration.
func DefaultSnapshotConfig() SnapshotConfig {
	return SnapshotConfig{
		SnapshotsTable: "snapshots",
	}
}

// SnapshotProjection is a built-in projection that maintains snapshots of aggregate state.
// It listens to all events and updates the snapshot for each aggregate, storing the
// latest aggregate state based on the most recent event.
//
// The snapshot stores:
// - The latest event's payload as the aggregate state
// - The aggregate version
// - Metadata from the event
//
// Snapshots are keyed by (aggregate_type, aggregate_id), so only the most recent
// state is stored per aggregate.
type SnapshotProjection struct {
	config SnapshotConfig
}

// NewSnapshotProjection creates a new snapshot projection with the given configuration.
// Returns an error if the table name is invalid.
func NewSnapshotProjection(config SnapshotConfig) (*SnapshotProjection, error) {
	if !validTableNameRegex.MatchString(config.SnapshotsTable) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTableName, config.SnapshotsTable)
	}

	return &SnapshotProjection{
		config: config,
	}, nil
}

// Name returns the projection name used for checkpoint tracking.
func (p *SnapshotProjection) Name() string {
	return "snapshot_projection"
}

// Handle processes an event and updates the snapshot for its aggregate.
// This upserts the snapshot record, ensuring only the latest state is kept.
func (p *SnapshotProjection) Handle(ctx context.Context, tx es.DBTX, event *es.PersistedEvent) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (
			aggregate_type, aggregate_id, aggregate_version,
			payload, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (aggregate_type, aggregate_id)
		DO UPDATE SET
			aggregate_version = EXCLUDED.aggregate_version,
			payload = EXCLUDED.payload,
			metadata = EXCLUDED.metadata,
			created_at = EXCLUDED.created_at
		WHERE %s.aggregate_version < EXCLUDED.aggregate_version
	`, p.config.SnapshotsTable, p.config.SnapshotsTable)

	_, err := tx.ExecContext(ctx, query,
		event.AggregateType,
		event.AggregateID,
		event.AggregateVersion,
		event.Payload,
		event.Metadata,
		event.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert snapshot: %w", err)
	}

	return nil
}
