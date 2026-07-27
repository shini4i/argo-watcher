package lock

import (
	"sync"
	"time"
)

// DeployLockState is the part of the deploy lock a replica cannot derive from
// its own configuration and therefore has to share with its peers. Lockdown
// schedules are deliberately absent: every replica reads the same
// LOCKDOWN_SCHEDULE value and evaluates it locally.
type DeployLockState struct {
	// ManualLock is true while an operator-set lockdown is in effect. It
	// outranks everything else.
	ManualLock bool
	// OverrideUntil is when a temporary override of a *scheduled* lockdown
	// expires. A zero value means no override is active. Storing a deadline
	// instead of a boolean plus a timer keeps the expiry meaningful across
	// replicas and restarts.
	OverrideUntil time.Time
}

// DeployLockStore persists the deploy lock state. The in-memory implementation
// serves single-replica deployments; the Postgres one makes the lock effective
// across every replica of an HA setup.
type DeployLockStore interface {
	// State returns the current deploy lock state.
	State() (DeployLockState, error)
	// Lock engages the manual lockdown and drops any pending override.
	Lock() error
	// Release clears the manual lockdown. A non-zero overrideUntil suppresses
	// an active scheduled lockdown until that instant.
	Release(overrideUntil time.Time) error
}

// InMemoryDeployLockStore keeps the deploy lock state in process memory. It is
// correct only for a single replica: peers never observe its changes.
type InMemoryDeployLockStore struct {
	mu    sync.RWMutex
	state DeployLockState
}

// NewInMemoryDeployLockStore creates a DeployLockStore backed by process memory.
func NewInMemoryDeployLockStore() DeployLockStore {
	return &InMemoryDeployLockStore{}
}

// State returns the current deploy lock state. The error is always nil; it
// exists to satisfy the interface shared with the Postgres implementation.
func (s *InMemoryDeployLockStore) State() (DeployLockState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, nil
}

// Lock engages the manual lockdown and drops any pending override.
func (s *InMemoryDeployLockStore) Lock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = DeployLockState{ManualLock: true}
	return nil
}

// Release clears the manual lockdown, recording overrideUntil as the deadline
// of a temporary override of an active scheduled lockdown.
func (s *InMemoryDeployLockStore) Release(overrideUntil time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = DeployLockState{OverrideUntil: overrideUntil}
	return nil
}
