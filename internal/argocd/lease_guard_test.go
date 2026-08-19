package argocd

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
)

// waitFor polls until condition holds, failing the test if it never does. It
// keeps the lease tests free of fixed sleeps sized to the renewal interval.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	require.Eventually(t, condition, time.Second, time.Millisecond)
}

func TestLeaseGuard_HoldsTheClaimWhileMonitoring(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	renewed := make(chan struct{}, 1)
	stateMock.EXPECT().RenewLease("task-id").DoAndReturn(func(string) (bool, error) {
		select {
		case renewed <- struct{}{}:
		default:
		}
		return true, nil
	}).MinTimes(1)

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, time.Hour)
	defer guard.Stop()

	<-renewed
	assert.False(t, guard.Lost(), "a renewed claim is still held")
}

func TestLeaseGuard_ReportsATakenOverTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().RenewLease("task-id").Return(false, nil).MinTimes(1)

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, time.Hour)
	defer guard.Stop()

	waitFor(t, guard.Lost)
}

// A failed renewal is not a lost claim: the lease outlives several renewal
// intervals, so a blip must not abandon a rollout this replica still owns.
func TestLeaseGuard_KeepsGoingWhenARenewalFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	attempts := make(chan struct{}, 8)
	stateMock.EXPECT().RenewLease("task-id").DoAndReturn(func(string) (bool, error) {
		select {
		case attempts <- struct{}{}:
		default:
		}
		return false, errors.New("database unreachable")
	}).MinTimes(2)

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, time.Hour)
	defer guard.Stop()

	<-attempts
	<-attempts
	assert.False(t, guard.Lost(), "an unreadable lease state is not proof of a takeover")
}

func TestLeaseGuard_StopsRenewing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var renewals atomic.Int32
	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().RenewLease("task-id").DoAndReturn(func(string) (bool, error) {
		renewals.Add(1)
		return true, nil
	}).AnyTimes()

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, time.Hour)
	require.Eventually(t, func() bool { return renewals.Load() > 0 }, time.Second, time.Millisecond)

	guard.Stop()
	settled := renewals.Load()

	// Stop waits for the goroutine, so nothing may renew after it returns — a leaked
	// guard would keep a finished rollout's claim alive for as long as the process runs.
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, settled, renewals.Load())

	guard.Stop() // idempotent: the deferred stop must survive an early one
}

// neverLost is the lease predicate for tests that are not about takeover: this
// replica keeps the task for the whole rollout.
func neverLost() bool { return false }

// newTaskRepositoryMock returns a task repository mock that already allows the
// lease calls every monitored rollout makes, so each test declares only the
// expectations it is actually about. Tests asserting on the lease itself build
// the mock directly instead.
func newTaskRepositoryMock(ctrl *gomock.Controller) *mocks.MockTaskRepository {
	repository := mocks.NewMockTaskRepository(ctrl)
	repository.EXPECT().ClaimTask(gomock.Any()).Return(nil).AnyTimes()
	repository.EXPECT().RenewLease(gomock.Any()).Return(true, nil).AnyTimes()
	return repository
}

// A renewal outage that outlasts the lease means the claim is gone whether or not
// this replica noticed. Holding on regardless is what would let this very process
// re-claim the task in a sweep — under the same owner id, so every later renewal
// still reports "held" — and monitor the same rollout twice.
func TestLeaseGuard_GivesUpAClaimThatOutlivedItsLease(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().RenewLease("task-id").Return(false, errors.New("database unreachable")).MinTimes(1)

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, 10*time.Millisecond)
	defer guard.Stop()

	waitFor(t, guard.Lost)
}

// The bound is measured from the last SUCCESSFUL renewal, so intermittent errors
// between good renewals must not accumulate toward it.
func TestLeaseGuard_FailuresBetweenSuccessesDoNotExpireTheClaim(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var calls atomic.Int32
	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().RenewLease("task-id").DoAndReturn(func(string) (bool, error) {
		// Alternate failure, success, failure, success...
		if calls.Add(1)%2 == 0 {
			return true, nil
		}
		return false, errors.New("transient blip")
	}).MinTimes(6)

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, 20*time.Millisecond)
	defer guard.Stop()

	require.Eventually(t, func() bool { return calls.Load() >= 6 }, time.Second, time.Millisecond)
	assert.False(t, guard.Lost(), "a claim renewed between blips is still held")
}

// A renewal that never returns is the case the error-branch bound cannot see: no
// tick completes, so nothing sets the flag, while the claim ages out in the
// database and a sweep hands the rollout to someone else. Lost must become true
// on the clock alone.
func TestLeaseGuard_ReportsALapsedClaimWhileRenewalIsStuck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	release := make(chan struct{})

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().RenewLease("task-id").DoAndReturn(func(string) (bool, error) {
		<-release
		return true, nil
	}).AnyTimes()

	guard := newLeaseGuard(stateMock, "task-id", time.Millisecond, 20*time.Millisecond)

	waitFor(t, guard.Lost)

	// Released before stopping, because Stop waits for the renewing goroutine. In
	// production the same wait is bounded by RenewLease's own statement timeout.
	close(release)
	guard.Stop()
}
