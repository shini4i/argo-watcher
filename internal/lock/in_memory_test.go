package lock

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryLocker(t *testing.T) {
	locker := NewInMemoryLocker()
	key := "test-key"
	var wg sync.WaitGroup
	// The buffer must be large enough to hold all sent values before they are read.
	executionOrder := make(chan int, 4)

	// Goroutine 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := locker.WithLock(key, func() error {
			executionOrder <- 1
			// Hold the lock for a moment to ensure the second goroutine has to wait.
			time.Sleep(10 * time.Millisecond)
			executionOrder <- 1
			return nil
		})
		assert.NoError(t, err)
	}()

	// Give the first goroutine a moment to acquire the lock
	time.Sleep(1 * time.Millisecond)

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
	// Expected: 1 (start), 1 (end), 2 (start), 2 (end)
	var order []int
	for i := range executionOrder {
		order = append(order, i)
	}

	expectedOrder := []int{1, 1, 2, 2}
	assert.Equal(t, expectedOrder, order, "The second goroutine should not start until the first one has finished")
}
