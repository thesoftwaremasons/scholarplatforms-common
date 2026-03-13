package eventsourcing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// TimescaleDBEventStore implements EventStore using TimescaleDB
type TimescaleDBEventStore struct {
	db     *sql.DB
	logger logger.Logger
	table  string
}

// TimescaleDBConfig holds TimescaleDB configuration
type TimescaleDBConfig struct {
	ConnectionString string
	TableName        string // Default: "events"
}

// NewTimescaleDBEventStore creates a new TimescaleDB event store
func NewTimescaleDBEventStore(cfg TimescaleDBConfig, log logger.Logger) (*TimescaleDBEventStore, error) {
	// Open database connection
	db, err := sql.Open("postgres", cfg.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Ping to verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	tableName := cfg.TableName
	if tableName == "" {
		tableName = "events"
	}

	store := &TimescaleDBEventStore{
		db:     db,
		logger: log,
		table:  tableName,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Info("TimescaleDB Event Store initialized",
		logger.String("table", tableName),
	)

	return store, nil
}

// initSchema creates the events table and converts it to a hypertable
func (ts *TimescaleDBEventStore) initSchema() error {
	ctx := context.Background()

	// Create events table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id TEXT PRIMARY KEY,
			aggregate_id TEXT NOT NULL,
			aggregate_type TEXT NOT NULL,
			event_type TEXT NOT NULL,
			event_version TEXT NOT NULL,
			data JSONB NOT NULL,
			metadata JSONB,
			timestamp TIMESTAMPTZ NOT NULL,
			user_id TEXT,
			correlation_id TEXT,
			causation_id TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
	`, ts.table)

	if _, err := ts.db.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Convert to hypertable (TimescaleDB-specific)
	// This will fail if already a hypertable, so we ignore that error
	hypertableSQL := fmt.Sprintf(`
		SELECT create_hypertable('%s', 'timestamp',
			if_not_exists => TRUE,
			chunk_time_interval => INTERVAL '1 day'
		);
	`, ts.table)

	_, err := ts.db.ExecContext(ctx, hypertableSQL)
	if err != nil {
		// Check if error is because it's already a hypertable
		if !isHypertableExistsError(err) {
			return fmt.Errorf("failed to create hypertable: %w", err)
		}
	}

	// Create indexes
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_aggregate ON %s(aggregate_id, timestamp DESC)", ts.table, ts.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_event_type ON %s(event_type, timestamp DESC)", ts.table, ts.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_correlation ON %s(correlation_id)", ts.table, ts.table),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_user ON %s(user_id, timestamp DESC)", ts.table, ts.table),
	}

	for _, indexSQL := range indexes {
		if _, err := ts.db.ExecContext(ctx, indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	ts.logger.Info("Event store schema initialized")

	return nil
}

// Append appends events to the store
func (ts *TimescaleDBEventStore) Append(events ...*Event) error {
	if len(events) == 0 {
		return nil
	}

	ctx := context.Background()

	// Start transaction
	tx, err := ts.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare insert statement
	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (
			id, aggregate_id, aggregate_type, event_type, event_version,
			data, metadata, timestamp, user_id, correlation_id, causation_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, ts.table)

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert events
	for _, event := range events {
		dataJSON, err := json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("failed to marshal event data: %w", err)
		}

		metadataJSON, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal event metadata: %w", err)
		}

		_, err = stmt.ExecContext(ctx,
			event.ID,
			event.AggregateID,
			event.AggregateType,
			event.EventType,
			event.EventVersion,
			dataJSON,
			metadataJSON,
			event.Timestamp,
			nullString(event.UserID),
			nullString(event.CorrelationID),
			nullString(event.CausationID),
		)

		if err != nil {
			return fmt.Errorf("failed to insert event: %w", err)
		}

		ts.logger.Debug("Event appended",
			logger.String("event_id", event.ID),
			logger.String("event_type", event.EventType),
		)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	ts.logger.Info("Events appended",
		logger.Int("count", len(events)),
	)

	return nil
}

// Load loads events for an aggregate
func (ts *TimescaleDBEventStore) Load(aggregateID string) ([]*Event, error) {
	ctx := context.Background()

	query := fmt.Sprintf(`
		SELECT id, aggregate_id, aggregate_type, event_type, event_version,
			   data, metadata, timestamp, user_id, correlation_id, causation_id
		FROM %s
		WHERE aggregate_id = $1
		ORDER BY timestamp ASC
	`, ts.table)

	rows, err := ts.db.QueryContext(ctx, query, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	return ts.scanEvents(rows)
}

// LoadByType loads events by type since a given time
func (ts *TimescaleDBEventStore) LoadByType(eventType string, since time.Time) ([]*Event, error) {
	ctx := context.Background()

	query := fmt.Sprintf(`
		SELECT id, aggregate_id, aggregate_type, event_type, event_version,
			   data, metadata, timestamp, user_id, correlation_id, causation_id
		FROM %s
		WHERE event_type = $1 AND timestamp >= $2
		ORDER BY timestamp ASC
	`, ts.table)

	rows, err := ts.db.QueryContext(ctx, query, eventType, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query events by type: %w", err)
	}
	defer rows.Close()

	return ts.scanEvents(rows)
}

// LoadAll loads all events since a given time
func (ts *TimescaleDBEventStore) LoadAll(since time.Time, limit int) ([]*Event, error) {
	ctx := context.Background()

	query := fmt.Sprintf(`
		SELECT id, aggregate_id, aggregate_type, event_type, event_version,
			   data, metadata, timestamp, user_id, correlation_id, causation_id
		FROM %s
		WHERE timestamp >= $1
		ORDER BY timestamp ASC
		LIMIT $2
	`, ts.table)

	rows, err := ts.db.QueryContext(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query all events: %w", err)
	}
	defer rows.Close()

	return ts.scanEvents(rows)
}

// scanEvents scans rows into events
func (ts *TimescaleDBEventStore) scanEvents(rows *sql.Rows) ([]*Event, error) {
	var events []*Event

	for rows.Next() {
		var event Event
		var dataJSON, metadataJSON []byte
		var userID, correlationID, causationID sql.NullString

		err := rows.Scan(
			&event.ID,
			&event.AggregateID,
			&event.AggregateType,
			&event.EventType,
			&event.EventVersion,
			&dataJSON,
			&metadataJSON,
			&event.Timestamp,
			&userID,
			&correlationID,
			&causationID,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(dataJSON, &event.Data); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &event.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event metadata: %w", err)
		}

		// Handle nullable fields
		if userID.Valid {
			event.UserID = userID.String
		}
		if correlationID.Valid {
			event.CorrelationID = correlationID.String
		}
		if causationID.Valid {
			event.CausationID = causationID.String
		}

		events = append(events, &event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return events, nil
}

// GetEventStats returns statistics about events
func (ts *TimescaleDBEventStore) GetEventStats() (map[string]interface{}, error) {
	ctx := context.Background()

	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_events,
			COUNT(DISTINCT aggregate_id) as total_aggregates,
			COUNT(DISTINCT event_type) as total_event_types,
			MIN(timestamp) as oldest_event,
			MAX(timestamp) as newest_event
		FROM %s
	`, ts.table)

	var stats struct {
		TotalEvents     int64
		TotalAggregates int64
		TotalEventTypes int64
		OldestEvent     time.Time
		NewestEvent     time.Time
	}

	err := ts.db.QueryRowContext(ctx, query).Scan(
		&stats.TotalEvents,
		&stats.TotalAggregates,
		&stats.TotalEventTypes,
		&stats.OldestEvent,
		&stats.NewestEvent,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get event stats: %w", err)
	}

	return map[string]interface{}{
		"total_events":      stats.TotalEvents,
		"total_aggregates":  stats.TotalAggregates,
		"total_event_types": stats.TotalEventTypes,
		"oldest_event":      stats.OldestEvent,
		"newest_event":      stats.NewestEvent,
	}, nil
}

// Close closes the database connection
func (ts *TimescaleDBEventStore) Close() error {
	return ts.db.Close()
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func isHypertableExistsError(err error) bool {
	// Check if error message contains hypertable already exists
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return contains(errMsg, "already a hypertable") || contains(errMsg, "table is already a hypertable")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
