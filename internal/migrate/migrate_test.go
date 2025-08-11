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
	// upError is the error that the Up() method will return.
	// Set to nil for success, migrate.ErrNoChange for no-op, or a standard error for failure.
	upError error
}

// Up simulates running the database migration and returns the predefined error.
func (m *mockMigrator) Up() error {
	return m.upError
}

// TestMigrator_Run_Success tests the successful execution of migrations.
func TestMigrator_Run_Success(t *testing.T) {
	// Arrange
	m := &Migrator{
		migrator: &mockMigrator{upError: nil},
	}

	// Act & Assert: This should run without calling log.Fatalf
	assert.NotPanics(t, func() {
		m.Run()
	}, "Run should not panic on success")
}

// TestMigrator_Run_NoChange tests the case where there are no new migrations to apply.
func TestMigrator_Run_NoChange(t *testing.T) {
	// Arrange
	m := &Migrator{
		migrator: &mockMigrator{upError: migrate.ErrNoChange},
	}

	// Act & Assert: This should run without calling log.Fatalf
	assert.NotPanics(t, func() {
		m.Run()
	}, "Run should not panic when there are no changes")
}

// TestMigrator_Run_Failure tests the case where the migration fails.
// It checks that log.Fatalf is called, which terminates the process.
func TestMigrator_Run_Failure(t *testing.T) {
	// This test runs the failure case in a separate process to assert that
	// log.Fatalf is called, as it terminates the current process.
	if os.Getenv("BE_A_FATAL_TEST") == "1" {
		m := &Migrator{
			migrator: &mockMigrator{upError: errors.New("a serious migration error")},
		}
		m.Run()
		return
	}

	// Arrange: Create a new command to run the test function in a separate process.
	// We pass an environment variable to trigger the test's fatal logic.
	cmd := exec.Command(os.Args[0], "-test.run=^TestMigrator_Run_Failure$")
	cmd.Env = append(os.Environ(), "BE_A_FATAL_TEST=1")

	// Act: Run the command and capture the output.
	output, err := cmd.CombinedOutput()

	// Assert: Check that the process exited with a non-zero status code,
	// which is the behavior of log.Fatalf.
	require.Error(t, err, "Process should exit with an error")
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "Error should be of type *exec.ExitError")
	assert.False(t, exitErr.Success(), "Process should not have exited successfully")

	// Assert that the captured output contains our expected fatal error message.
	assert.Contains(t, string(output), "Fatal: An error occurred while applying migrations: a serious migration error")
}

// TestNewMigrator_Failure tests that NewMigrator fails gracefully with an invalid DSN.
func TestNewMigrator_Failure(t *testing.T) {
	// Arrange: Create a config with a deliberately invalid DSN format.
	cfg := &MigrationConfig{
		DSN: "this-is-not-a-valid-uri",
	}

	// Act
	migrator, err := NewMigrator(cfg)

	// Assert
	require.Error(t, err, "NewMigrator should return an error with an invalid DSN")
	assert.Nil(t, migrator, "Migrator should be nil on initialization failure")
	assert.Contains(t, err.Error(), "migration initialization failed", "Error message should indicate initialization failure")
}
