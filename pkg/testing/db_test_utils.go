// Package testing provides database testing utilities
package testing

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thesoftwaremasons/scholarplatforms-common/pkg/logger"
)

// DatabaseTestUtils provides utilities for testing database operations
type DatabaseTestUtils struct {
	DB             *sql.DB
	Logger         logger.Logger
	SchemaPath     string
	FixturesPath   string
	MigrationPath  string
	TestDataPath   string
}

// Migration represents a database migration
type Migration struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	UpScript    string `json:"up_script"`
	DownScript  string `json:"down_script"`
	Applied     bool   `json:"applied"`
	AppliedAt   *time.Time `json:"applied_at,omitempty"`
}

// TestFixture represents test data fixture
type TestFixture struct {
	Name     string                 `json:"name"`
	Table    string                 `json:"table"`
	Data     []map[string]interface{} `json:"data"`
	DependsOn []string               `json:"depends_on"`
}

// DatabaseSnapshot represents a point-in-time database state
type DatabaseSnapshot struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Tables    map[string][]map[string]interface{} `json:"tables"`
}

// NewDatabaseTestUtils creates a new database test utilities instance
func NewDatabaseTestUtils(db *sql.DB, logger logger.Logger, testDataPath string) *DatabaseTestUtils {
	return &DatabaseTestUtils{
		DB:            db,
		Logger:        logger,
		TestDataPath:  testDataPath,
		SchemaPath:    filepath.Join(testDataPath, "schemas"),
		FixturesPath:  filepath.Join(testDataPath, "fixtures"),
		MigrationPath: filepath.Join(testDataPath, "migrations"),
	}
}

// SetupTestSchema creates the test database schema
func (utils *DatabaseTestUtils) SetupTestSchema(ctx context.Context) error {
	utils.Logger.Info("Setting up test database schema")
	
	// Create migration tracking table
	if err := utils.createMigrationsTable(ctx); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}
	
	// Run migrations
	if err := utils.RunMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	
	utils.Logger.Info("Test schema setup completed")
	return nil
}

// createMigrationsTable creates the migrations tracking table
func (utils *DatabaseTestUtils) createMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`
	
	_, err := utils.DB.ExecContext(ctx, query)
	return err
}

// RunMigrations applies all pending database migrations
func (utils *DatabaseTestUtils) RunMigrations(ctx context.Context) error {
	migrations, err := utils.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}
	
	appliedMigrations, err := utils.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}
	
	for _, migration := range migrations {
		if !appliedMigrations[migration.Version] {
			utils.Logger.Debug("Applying migration",
				logger.String("version", migration.Version),
				logger.String("name", migration.Name))
			
			if err := utils.applyMigration(ctx, migration); err != nil {
				return fmt.Errorf("failed to apply migration %s: %w", migration.Version, err)
			}
		}
	}
	
	return nil
}

// loadMigrations loads migration files from the migrations directory
func (utils *DatabaseTestUtils) loadMigrations() ([]Migration, error) {
	var migrations []Migration
	
	if _, err := os.Stat(utils.MigrationPath); os.IsNotExist(err) {
		utils.Logger.Debug("Migrations directory not found", logger.String("path", utils.MigrationPath))
		return migrations, nil
	}
	
	err := filepath.WalkDir(utils.MigrationPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		if d.IsDir() || !strings.HasSuffix(path, ".up.sql") {
			return nil
		}
		
		// Parse migration version and name from filename
		filename := d.Name()
		parts := strings.Split(strings.TrimSuffix(filename, ".up.sql"), "_")
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename format: %s", filename)
		}
		
		version := parts[0]
		name := strings.Join(parts[1:], "_")
		
		// Read up script
		upScript, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read up migration %s: %w", path, err)
		}
		
		// Try to read down script
		downPath := strings.Replace(path, ".up.sql", ".down.sql", 1)
		var downScript []byte
		if _, err := os.Stat(downPath); err == nil {
			downScript, _ = os.ReadFile(downPath)
		}
		
		migration := Migration{
			Version:    version,
			Name:       name,
			UpScript:   string(upScript),
			DownScript: string(downScript),
		}
		
		migrations = append(migrations, migration)
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	
	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	
	return migrations, nil
}

// getAppliedMigrations returns a map of applied migration versions
func (utils *DatabaseTestUtils) getAppliedMigrations(ctx context.Context) (map[string]bool, error) {
	applied := make(map[string]bool)
	
	rows, err := utils.DB.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return applied, err
	}
	defer rows.Close()
	
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return applied, err
		}
		applied[version] = true
	}
	
	return applied, rows.Err()
}

// applyMigration applies a single migration
func (utils *DatabaseTestUtils) applyMigration(ctx context.Context, migration Migration) error {
	tx, err := utils.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	
	// Execute migration script
	if _, err := tx.ExecContext(ctx, migration.UpScript); err != nil {
		return fmt.Errorf("failed to execute migration script: %w", err)
	}
	
	// Record migration as applied
	_, err = tx.ExecContext(ctx, 
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		migration.Version, migration.Name)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}
	
	return tx.Commit()
}

// LoadFixtures loads test fixtures into the database
func (utils *DatabaseTestUtils) LoadFixtures(ctx context.Context, fixtureNames ...string) error {
	utils.Logger.Info("Loading test fixtures", logger.Any("fixtures", fixtureNames))
	
	fixtures, err := utils.loadFixtureDefinitions(fixtureNames...)
	if err != nil {
		return fmt.Errorf("failed to load fixture definitions: %w", err)
	}
	
	// Sort fixtures by dependencies
	sortedFixtures, err := utils.sortFixturesByDependencies(fixtures)
	if err != nil {
		return fmt.Errorf("failed to sort fixtures: %w", err)
	}
	
	// Load fixtures in order
	for _, fixture := range sortedFixtures {
		if err := utils.loadFixture(ctx, fixture); err != nil {
			return fmt.Errorf("failed to load fixture %s: %w", fixture.Name, err)
		}
	}
	
	utils.Logger.Info("Fixtures loaded successfully")
	return nil
}

// loadFixtureDefinitions loads fixture definitions from files
func (utils *DatabaseTestUtils) loadFixtureDefinitions(names ...string) ([]TestFixture, error) {
	var fixtures []TestFixture
	
	// If no names specified, load all fixtures
	if len(names) == 0 {
		err := filepath.WalkDir(utils.FixturesPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			
			name := strings.TrimSuffix(d.Name(), ".json")
			names = append(names, name)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	
	for _, name := range names {
		_ = filepath.Join(utils.FixturesPath, name+".json") // fixturePath for future use
		
		// For now, create a simple fixture structure
		// In a real implementation, you would load from JSON files
		fixture := TestFixture{
			Name:  name,
			Table: name, // Assume table name matches fixture name
			Data:  []map[string]interface{}{}, // Empty for now
		}
		
		fixtures = append(fixtures, fixture)
	}
	
	return fixtures, nil
}

// sortFixturesByDependencies sorts fixtures based on their dependencies
func (utils *DatabaseTestUtils) sortFixturesByDependencies(fixtures []TestFixture) ([]TestFixture, error) {
	// Simple topological sort based on dependencies
	var sorted []TestFixture
	loaded := make(map[string]bool)
	
	var loadFixture func(fixture TestFixture) error
	loadFixture = func(fixture TestFixture) error {
		if loaded[fixture.Name] {
			return nil
		}
		
		// Load dependencies first
		for _, dep := range fixture.DependsOn {
			for _, f := range fixtures {
				if f.Name == dep {
					if err := loadFixture(f); err != nil {
						return err
					}
					break
				}
			}
		}
		
		sorted = append(sorted, fixture)
		loaded[fixture.Name] = true
		return nil
	}
	
	for _, fixture := range fixtures {
		if err := loadFixture(fixture); err != nil {
			return nil, err
		}
	}
	
	return sorted, nil
}

// loadFixture loads a single fixture into the database
func (utils *DatabaseTestUtils) loadFixture(ctx context.Context, fixture TestFixture) error {
	if len(fixture.Data) == 0 {
		return nil // No data to load
	}
	
	utils.Logger.Debug("Loading fixture",
		logger.String("name", fixture.Name),
		logger.String("table", fixture.Table),
		logger.Int("records", len(fixture.Data)))
	
	// Clear existing data
	_, err := utils.DB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", fixture.Table))
	if err != nil {
		return fmt.Errorf("failed to clear table %s: %w", fixture.Table, err)
	}
	
	// Insert fixture data
	for _, record := range fixture.Data {
		columns := make([]string, 0, len(record))
		placeholders := make([]string, 0, len(record))
		values := make([]interface{}, 0, len(record))
		
		i := 1
		for column, value := range record {
			columns = append(columns, column)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			values = append(values, value)
			i++
		}
		
		query := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			fixture.Table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)
		
		_, err := utils.DB.ExecContext(ctx, query, values...)
		if err != nil {
			return fmt.Errorf("failed to insert record: %w", err)
		}
	}
	
	return nil
}

// CleanDatabase removes all test data from the database
func (utils *DatabaseTestUtils) CleanDatabase(ctx context.Context) error {
	utils.Logger.Info("Cleaning test database")
	
	// Get all table names
	tables, err := utils.getTables(ctx)
	if err != nil {
		return fmt.Errorf("failed to get table list: %w", err)
	}
	
	// Disable foreign key constraints temporarily
	_, err = utils.DB.ExecContext(ctx, "SET foreign_key_checks = 0")
	if err != nil {
		utils.Logger.Warn("Failed to disable foreign key checks", logger.ErrorField(err))
	}
	
	// Truncate all tables except migrations
	for _, table := range tables {
		if table == "schema_migrations" {
			continue
		}
		
		_, err := utils.DB.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s", table))
		if err != nil {
			utils.Logger.Warn("Failed to truncate table",
				logger.String("table", table),
				logger.ErrorField(err))
		}
	}
	
	// Re-enable foreign key constraints
	_, err = utils.DB.ExecContext(ctx, "SET foreign_key_checks = 1")
	if err != nil {
		utils.Logger.Warn("Failed to re-enable foreign key checks", logger.ErrorField(err))
	}
	
	utils.Logger.Info("Database cleaned successfully")
	return nil
}

// getTables returns a list of all tables in the database
func (utils *DatabaseTestUtils) getTables(ctx context.Context) ([]string, error) {
	var tables []string
	
	// CockroachDB query for tables
	rows, err := utils.DB.QueryContext(ctx, `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = current_database()
		AND table_type = 'BASE TABLE'
	`)
	if err != nil {
		return tables, err
	}
	defer rows.Close()
	
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return tables, err
		}
		tables = append(tables, table)
	}
	
	return tables, rows.Err()
}

// CreateSnapshot creates a snapshot of the current database state
func (utils *DatabaseTestUtils) CreateSnapshot(ctx context.Context, name string) (*DatabaseSnapshot, error) {
	utils.Logger.Info("Creating database snapshot", logger.String("name", name))
	
	tables, err := utils.getTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}
	
	snapshot := &DatabaseSnapshot{
		Name:      name,
		Timestamp: time.Now(),
		Tables:    make(map[string][]map[string]interface{}),
	}
	
	for _, table := range tables {
		if table == "schema_migrations" {
			continue
		}
		
		tableData, err := utils.dumpTable(ctx, table)
		if err != nil {
			return nil, fmt.Errorf("failed to dump table %s: %w", table, err)
		}
		
		snapshot.Tables[table] = tableData
	}
	
	utils.Logger.Info("Database snapshot created",
		logger.String("name", name),
		logger.Int("tables", len(snapshot.Tables)))
	
	return snapshot, nil
}

// dumpTable dumps all data from a table
func (utils *DatabaseTestUtils) dumpTable(ctx context.Context, tableName string) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	
	rows, err := utils.DB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", tableName))
	if err != nil {
		return data, err
	}
	defer rows.Close()
	
	columns, err := rows.Columns()
	if err != nil {
		return data, err
	}
	
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		
		if err := rows.Scan(valuePtrs...); err != nil {
			return data, err
		}
		
		record := make(map[string]interface{})
		for i, column := range columns {
			record[column] = values[i]
		}
		
		data = append(data, record)
	}
	
	return data, rows.Err()
}

// AssertTableCount asserts the number of records in a table
func (utils *DatabaseTestUtils) AssertTableCount(ctx context.Context, tableName string, expectedCount int) error {
	var count int
	err := utils.DB.QueryRowContext(ctx, 
		fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count records in %s: %w", tableName, err)
	}
	
	if count != expectedCount {
		return fmt.Errorf("expected %d records in %s, got %d", expectedCount, tableName, count)
	}
	
	return nil
}

// AssertRecordExists asserts that a record exists with given conditions
func (utils *DatabaseTestUtils) AssertRecordExists(ctx context.Context, tableName string, conditions map[string]interface{}) error {
	whereParts := make([]string, 0, len(conditions))
	values := make([]interface{}, 0, len(conditions))
	
	i := 1
	for column, value := range conditions {
		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", column, i))
		values = append(values, value)
		i++
	}
	
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE %s LIMIT 1",
		tableName, strings.Join(whereParts, " AND "))
	
	var exists int
	err := utils.DB.QueryRowContext(ctx, query, values...).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no record found in %s with conditions %v", tableName, conditions)
	}
	if err != nil {
		return fmt.Errorf("failed to check record existence: %w", err)
	}
	
	return nil
}