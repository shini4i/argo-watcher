package migrate

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/golang-migrate/migrate/v4"
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

// TestNewMigratorWithDriver verifies that the constructor correctly assigns the provided driver.
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
// It checks that log.Fatalf is called, which terminates the process.
func TestMigrator_Run_Failure(t *testing.T) {
	if os.Getenv("BE_A_FATAL_TEST") == "1" {
		mock := &mockMigrator{upError: errors.New("a serious migration error")}
		m := NewMigratorWithDriver(mock)
		m.Run()
		return
	}

	// Arrange
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrator_Run_Failure$")
	cmd.Env = append(os.Environ(), "BE_A_FATAL_TEST=1")

	// Act
	output, err := cmd.CombinedOutput()

	// Assert
	require.Error(t, err, "Process should exit with an error")
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "Error should be of type *exec.ExitError")
	assert.False(t, exitErr.Success(), "Process should not have exited successfully")
	assert.Contains(t, string(output), "Fatal: An error occurred while applying migrations: a serious migration error")
}

// TestNewMigrator_Failure tests that the real NewMigrator fails gracefully with an invalid DSN.
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
