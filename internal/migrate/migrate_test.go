// internal/migrate/migrate_test.go
package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMigrator implements the migrator interface for testing purposes.
type mockMigrator struct {
	upError error
}

// Up simulates running the database migration and returns the predefined error.
func (m *mockMigrator) Up() error {
	return m.upError
}

// TestMigrator_Run_Success tests the successful execution of migrations.
func TestMigrator_Run_Success(t *testing.T) {
	// Arrange
	mock := &mockMigrator{upError: nil}
	m := NewMigratorWithDriver(mock)

	// Act
	err := m.Run()

	// Assert
	assert.NoError(t, err)
}

// TestMigrator_Run_NoChange tests the case where there are no new migrations.
func TestMigrator_Run_NoChange(t *testing.T) {
	// Arrange
	mock := &mockMigrator{upError: migrate.ErrNoChange}
	m := NewMigratorWithDriver(mock)

	// Act
	err := m.Run()

	// Assert
	assert.NoError(t, err, "migrate.ErrNoChange should be treated as a success")
}

// TestMigrator_Run_Failure tests the case where the migration returns an error.
func TestMigrator_Run_Failure(t *testing.T) {
	// Arrange
	expectedErr := errors.New("a serious migration error")
	mock := &mockMigrator{upError: expectedErr}
	m := NewMigratorWithDriver(mock)

	// Act
	err := m.Run()

	// Assert
	require.Error(t, err)
	// Check that our specific error was wrapped and returned.
	assert.ErrorIs(t, err, expectedErr)
}

// TestNewMigrator_Success tests that the real constructor succeeds with a valid config.
func TestNewMigrator_Success(t *testing.T) {
	// Arrange
	migrationsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "1_init.up.sql"), []byte("CREATE TABLE users (id int);"), 0600))

	cfg := &MigrationConfig{
		DSN:            "sqlite3://file::memory:?cache=shared",
		MigrationsPath: migrationsDir,
	}

	// Act
	migrator, err := NewMigrator(cfg)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, migrator)
}

// TestNewMigrator_Failure tests that the real constructor fails with an invalid DSN.
func TestNewMigrator_Failure(t *testing.T) {
	// Arrange
	cfg := &MigrationConfig{
		DSN: "this-is-not-a-valid-uri",
	}

	// Act
	migrator, err := NewMigrator(cfg)

	// Assert
	require.Error(t, err)
	assert.Nil(t, migrator)
}
