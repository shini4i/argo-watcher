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
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
)

func TestMigrator_Run_Success(t *testing.T) {
	mock := mocks.NewMockmigrator(gomock.NewController(t))
	mock.EXPECT().Up().Return(nil)
	m := NewMigratorWithDriver(mock)

	err := m.Run()

	assert.NoError(t, err)
}

func TestMigrator_Run_NoChange(t *testing.T) {
	mock := mocks.NewMockmigrator(gomock.NewController(t))
	mock.EXPECT().Up().Return(migrate.ErrNoChange)
	m := NewMigratorWithDriver(mock)

	err := m.Run()

	assert.NoError(t, err, "migrate.ErrNoChange should be treated as a success")
}

func TestMigrator_Run_Failure(t *testing.T) {
	expectedErr := errors.New("a serious migration error")
	mock := mocks.NewMockmigrator(gomock.NewController(t))
	mock.EXPECT().Up().Return(expectedErr)
	m := NewMigratorWithDriver(mock)

	err := m.Run()

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
}

func TestNewMigrator_Success(t *testing.T) {
	migrationsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "1_init.up.sql"), []byte("CREATE TABLE users (id int);"), 0600))

	cfg := &MigrationConfig{
		DSN:            "sqlite3://file::memory:?cache=shared",
		MigrationsPath: migrationsDir,
	}

	migrator, err := NewMigrator(cfg)

	require.NoError(t, err)
	assert.NotNil(t, migrator)
}

func TestNewMigrator_Failure(t *testing.T) {
	cfg := &MigrationConfig{
		DSN: "this-is-not-a-valid-uri",
	}

	migrator, err := NewMigrator(cfg)

	require.Error(t, err)
	assert.Nil(t, migrator)
}
