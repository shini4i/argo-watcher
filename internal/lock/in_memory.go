package lock

import "sync"

// MutexMap is a map of mutexes for each application. It is not exported.
type mutexMap struct {
	mutexes map[string]*sync.Mutex
	mu      sync.Mutex
}

// newMutexMap creates a new MutexMap.
func newMutexMap() *mutexMap {
	return &mutexMap{
		mutexes: make(map[string]*sync.Mutex),
	}
}

// get returns a mutex for the given key, creating it if it doesn't exist.
func (m *mutexMap) get(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mutexes[key]; !ok {
		m.mutexes[key] = &sync.Mutex{}
	}
	return m.mutexes[key]
}

// InMemoryLocker is an implementation of the Locker interface that uses
// an in-memory mutex map.
type InMemoryLocker struct {
	mutexMap *mutexMap
}

// NewInMemoryLocker creates a new instance of InMemoryLocker.
func NewInMemoryLocker() Locker {
	return &InMemoryLocker{
		mutexMap: newMutexMap(),
	}
}

// Lock acquires a lock for the given key. It is a blocking call.
func (l *InMemoryLocker) Lock(key string) error {
	mutex := l.mutexMap.get(key)
	mutex.Lock()
	return nil
}

// Unlock releases the lock for the given key.
func (l *InMemoryLocker) Unlock(key string) error {
	mutex := l.mutexMap.get(key)
	mutex.Unlock()
	return nil
}
