// internal/migrate/migrate_test.go
package migrate

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	// Add blank import for the sqlite3 driver for testing purposes.
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

// TestNewMigratorWithDriver verifies that the constructor for tests correctly
// assigns the provided driver.
func TestNewMigratorWithDriver(t *testing.T) {
	// Arrange
	mock := &mockMigrator{}

	// Act
	m := NewMigratorWithDriver(mock)

	// Assert
	require.NotNil(t, m, "Migrator should not be nil")
	assert.Same(t, mock, m.migrator, "The provided driver should be assigned to the migrator field")
}

// TestMigrator_Run_Success tests the successful execution of migrations.
func TestMigrator_Run_Success(t *testing.T) {
	// Arrange
	mock := &mockMigrator{upError: nil}
	m := NewMigratorWithDriver(mock)

	// Act & Assert
	assert.NotPanics(t, func() {
		m.Run()
	}, "Run should not panic on success")
}

// TestMigrator_Run_NoChange tests the case where there are no new migrations to apply.
func TestMigrator_Run_NoChange(t *testing.T) {
	// Arrange
	mock := &mockMigrator{upError: migrate.ErrNoChange}
	m := NewMigratorWithDriver(mock)

	// Act & Assert
	assert.NotPanics(t, func() {
		m.Run()
	}, "Run should not panic when there are no changes")
}

// TestMigrator_Run_Failure tests the case where the migration fails.
// This is the crucial test that covers the fatal error path.
func TestMigrator_Run_Failure(t *testing.T) {
	// This "if" block is the key. It checks for an environment variable.
	// When the test runs itself as a sub-process, this block is executed.
	if os.Getenv("BE_A_FATAL_TEST") == "1" {
		mock := &mockMigrator{upError: errors.New("a serious migration error")}
		m := NewMigratorWithDriver(mock)
		// This call to Run() will trigger log.Fatalf and terminate the sub-process.
		m.Run()
		return
	}

	// Arrange: The main test process creates a command to run itself.
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrator_Run_Failure$")
	// It sets the special environment variable to trigger the logic above.
	cmd.Env = append(os.Environ(), "BE_A_FATAL_TEST=1")

	// Act: Run the sub-process and capture its output.
	output, err := cmd.CombinedOutput()

	// Assert: Check that the sub-process failed as expected.
	require.Error(t, err, "Process should exit with an error because log.Fatalf was called")
	// Assert that the output contains the specific fatal error message.
	assert.Contains(t, string(output), "Fatal: An error occurred while applying migrations: a serious migration error")
}

// TestNewMigrator_Success tests that the real NewMigrator constructor succeeds.
func TestNewMigrator_Success(t *testing.T) {
	// Arrange
	migrationsDir := t.TempDir()
	require.NoError(t, os.WriteFile(migrationsDir+"/1_init.up.sql", []byte("CREATE TABLE users (id int);"), 0600))

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

// TestNewMigrator_Failure tests that the real NewMigrator fails gracefully.
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
	assert.Contains(t, err.Error(), "migration initialization failed")
}
