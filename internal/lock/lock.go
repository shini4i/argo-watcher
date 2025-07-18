package lock

// Locker defines the interface for a distributed, blocking locking mechanism.
type Locker interface {
	// Lock acquires a lock for the given key. It blocks until the lock is available.
	Lock(key string) error
	// Unlock releases the lock for the given key.
	Unlock(key string) error
}
