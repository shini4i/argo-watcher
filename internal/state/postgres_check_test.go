package state

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// blackholeConnector stands in for a database that accepts the connection and then
// answers nothing — a failover mid-flight, or a route that silently drops packets.
type blackholeConnector struct{}

func (blackholeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blackholeConnector) Driver() driver.Driver { return nil }

// TestPostgresState_CheckIsBoundedAgainstAnUnresponsiveDatabase pins the timeout on the
// health probe. An unbounded ping parks the readiness handler for as long as the network
// stays silent, so the replica never reports unready and is never restarted.
func TestPostgresState_CheckIsBoundedAgainstAnUnresponsiveDatabase(t *testing.T) {
	sqlDB := sql.OpenDB(blackholeConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })

	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DisableAutomaticPing: true})
	require.NoError(t, err)

	state := &PostgresState{orm: orm}

	done := make(chan bool, 1)
	go func() { done <- state.Check() }()

	select {
	case alive := <-done:
		assert.False(t, alive, "an unresponsive database is not a healthy one")
	case <-time.After(checkPingTimeout + 5*time.Second):
		t.Fatal("Check did not return within its own timeout")
	}
}
