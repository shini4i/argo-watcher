package lock

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test requires a running PostgreSQL database.
// Run with: go test -v -tags integration ./...
func TestPostgresLocker(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	assert.NoError(t, err)

	locker := NewPostgresLocker(db)
	key := "integration-test-key"
	var wg sync.WaitGroup
	// The buffer must be large enough to hold all sent values before they are read.
	executionOrder := make(chan int, 4)

	// Goroutine 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := locker.WithLock(key, func() error {
			executionOrder <- 1
			// Hold the lock to ensure the second goroutine has to wait.
			time.Sleep(100 * time.Millisecond)
			executionOrder <- 1
			return nil
		})
		assert.NoError(t, err)
	}()

	// Give the first goroutine a moment to acquire the lock
	time.Sleep(10 * time.Millisecond)

	// Goroutine 2
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := locker.WithLock(key, func() error {
			executionOrder <- 2
			executionOrder <- 2
			return nil
		})
		assert.NoError(t, err)
	}()

	wg.Wait()
	close(executionOrder)

	// Verify execution order
	var order []int
	for i := range executionOrder {
		order = append(order, i)
	}

	expectedOrder := []int{1, 1, 2, 2}
	assert.Equal(t, expectedOrder, order, "The second goroutine should not have started until the first one committed its transaction")
}
