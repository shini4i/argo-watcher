package state

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sql.Open is lazy, so the pool is configurable without a reachable database.
func TestConfigurePool_BoundsTheConnectionCount(t *testing.T) {
	sqlDB, err := sql.Open("pgx", "postgres://user@127.0.0.1:1/argo_watcher")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })

	assert.Equal(t, 0, sqlDB.Stats().MaxOpenConnections, "database/sql defaults to an unlimited pool")

	configurePool(sqlDB)

	assert.Equal(t, maxOpenConns, sqlDB.Stats().MaxOpenConnections)
}
