package lock

// Locker defines the interface for a distributed, blocking locking mechanism.
type Locker interface {
	// WithLock acquires a lock for the given key, executes the provided function,
	// and guarantees the lock is released afterward.
	WithLock(key string, f func() error) error
}
