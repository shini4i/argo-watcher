package lock

import "sync"

type mutexMap struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func newMutexMap() *mutexMap {
	return &mutexMap{
		m: make(map[string]*sync.Mutex),
	}
}

func (m *mutexMap) get(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mu, ok := m.m[key]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	m.m[key] = mu
	return mu
}

// InMemoryLocker is a Locker backed by an in-memory mutex map.
type InMemoryLocker struct {
	mutexMap *mutexMap
}

// NewInMemoryLocker creates a new instance of InMemoryLocker.
func NewInMemoryLocker() Locker {
	return &InMemoryLocker{
		mutexMap: newMutexMap(),
	}
}

// WithLock acquires a lock for the given key, executes the function, and releases the lock.
func (l *InMemoryLocker) WithLock(key string, f func() error) error {
	mutex := l.mutexMap.get(key)
	mutex.Lock()
	defer mutex.Unlock()
	return f()
}
