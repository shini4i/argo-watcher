package argocd

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// newBatchReq builds a minimal request for a given repo URL and branch. The app,
// task, and supersede predicate are unused by the fake flush functions in these
// tests, which exercise only the Batcher's coalescing/fan-out logic.
func newBatchReq(repoURL, branch string) *batchWriteRequest {
	return &batchWriteRequest{
		gitopsRepo: &models.GitopsRepo{RepoUrl: repoURL, BranchName: branch},
		resultCh:   make(chan error, 1),
	}
}

// waitForPendingLen blocks until the batcher has exactly want requests queued for
// key, so tests can deterministically enqueue into an in-flight flush without
// relying on sleeps.
func waitForPendingLen(t *testing.T, b *Batcher, key string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.pending[key]) == want
	}, 2*time.Second, time.Millisecond, "expected %d pending requests for key", want)
}

// gatedFlush returns a fake flushFn that signals its batch size on started when a
// flush begins, blocks until release is closed, then delivers the given outcome to
// every request. The started signal lets tests guarantee a flush is in flight
// before enqueuing more requests, making coalescing deterministic.
func gatedFlush(started chan<- int, release <-chan struct{}, outcome error) func([]*batchWriteRequest) {
	return func(batch []*batchWriteRequest) {
		started <- len(batch)
		<-release
		for _, req := range batch {
			req.resultCh <- outcome
		}
	}
}

func TestBatcher_IdleKeyFlushesImmediately(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)

	started := make(chan int, 1)
	release := make(chan struct{})
	close(release) // never block
	b.flushFn = gatedFlush(started, release, nil)

	require.NoError(t, b.Submit(newBatchReq("repo", "main")))
	assert.Equal(t, 1, <-started, "an idle key must flush a single request immediately")
}

func TestBatcher_CoalescesRequestsDuringInFlightFlush(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)
	key := batchKey(&models.GitopsRepo{RepoUrl: "repo", BranchName: "main"})

	started := make(chan int, 4)
	release := make(chan struct{})
	b.flushFn = gatedFlush(started, release, nil)

	done := make(chan error, 3)
	// First request: idle key, its flush starts immediately and blocks on release.
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	assert.Equal(t, 1, <-started, "req1 flushes alone")

	// Two more arrive while flush #1 is in flight: they coalesce into pending.
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	waitForPendingLen(t, b, key, 2)

	close(release)
	assert.Equal(t, 2, <-started, "req2+req3 coalesce into a single second flush")
	for i := 0; i < 3; i++ {
		require.NoError(t, <-done)
	}
}

func TestBatcher_SizeCapBoundsFlush(t *testing.T) {
	b := NewBatcher(nil, "", 2, nil) // cap each flush at 2 apps
	key := batchKey(&models.GitopsRepo{RepoUrl: "repo", BranchName: "main"})

	started := make(chan int, 8)
	release := make(chan struct{})
	var mu sync.Mutex
	seen := map[*batchWriteRequest]int{}
	b.flushFn = func(batch []*batchWriteRequest) {
		started <- len(batch)
		<-release
		mu.Lock()
		for _, req := range batch {
			seen[req]++
		}
		mu.Unlock()
		for _, req := range batch {
			req.resultCh <- nil
		}
	}

	// First request starts an immediate flush that blocks on release.
	r0 := newBatchReq("repo", "main")
	done := make(chan error, 5)
	go func() { done <- b.Submit(r0) }()
	assert.Equal(t, 1, <-started)

	// Four more coalesce while flush #1 is in flight.
	rest := make([]*batchWriteRequest, 4)
	for i := range rest {
		rest[i] = newBatchReq("repo", "main")
		r := rest[i]
		go func() { done <- b.Submit(r) }()
	}
	waitForPendingLen(t, b, key, 4)

	close(release)
	// The 4 queued are drained in caps of 2: two flushes of size 2.
	assert.Equal(t, 2, <-started)
	assert.Equal(t, 2, <-started)
	for i := 0; i < 5; i++ {
		require.NoError(t, <-done)
	}

	// Identity invariant: every submitted request is flushed exactly once — the
	// slice hand-off across the size-cap boundary neither drops nor duplicates.
	mu.Lock()
	defer mu.Unlock()
	all := append([]*batchWriteRequest{r0}, rest...)
	require.Len(t, seen, len(all))
	for _, req := range all {
		assert.Equal(t, 1, seen[req], "each request must be flushed exactly once")
	}
}

func TestBatcher_FanOutDeliversPerRequestOutcome(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)
	key := batchKey(&models.GitopsRepo{RepoUrl: "repo", BranchName: "main"})

	errForSecond := &stringError{"boom"}
	started := make(chan int, 4)
	release := make(chan struct{})
	b.flushFn = func(batch []*batchWriteRequest) {
		started <- len(batch)
		<-release
		// Deliver a distinct outcome per request by position.
		for i, req := range batch {
			if i == 1 {
				req.resultCh <- errForSecond
			} else {
				req.resultCh <- nil
			}
		}
	}

	go func() { _ = b.Submit(newBatchReq("repo", "main")) }()
	assert.Equal(t, 1, <-started)

	res := make(chan error, 2)
	go func() { res <- b.Submit(newBatchReq("repo", "main")) }()
	go func() { res <- b.Submit(newBatchReq("repo", "main")) }()
	waitForPendingLen(t, b, key, 2)

	close(release)
	got := []error{<-res, <-res}
	assert.Contains(t, got, error(errForSecond), "one request must receive its own error")
	assert.Contains(t, got, nil, "the other must receive success")
}

func TestBatcher_DistinctKeysFlushIndependently(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)

	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	proceed := make(chan struct{})
	b.flushFn = func(batch []*batchWriteRequest) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		<-proceed
		for _, req := range batch {
			req.resultCh <- nil
		}
	}

	done := make(chan error, 2)
	// Two different branches => two keys => two concurrent flush goroutines.
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	go func() { done <- b.Submit(newBatchReq("repo", "dev")) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return maxInFlight == 2
	}, 2*time.Second, time.Millisecond, "distinct keys must flush concurrently")

	close(proceed)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}

func TestBatcher_CloseDrainsPendingAndUnblocksWaiters(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)
	key := batchKey(&models.GitopsRepo{RepoUrl: "repo", BranchName: "main"})

	started := make(chan int, 4)
	release := make(chan struct{})
	b.flushFn = gatedFlush(started, release, nil)

	done := make(chan error, 3)
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	assert.Equal(t, 1, <-started)
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	go func() { done <- b.Submit(newBatchReq("repo", "main")) }()
	waitForPendingLen(t, b, key, 2)

	// Release flushes, then Close must wait for the in-flight loop to drain every
	// queued request before returning.
	close(release)
	b.Close()

	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("Close returned before all enqueued requests were flushed")
		}
	}
}

func TestBatcher_SubmitAfterCloseIsRejected(t *testing.T) {
	b := NewBatcher(nil, "", 20, nil)
	b.flushFn = func(batch []*batchWriteRequest) {
		for _, req := range batch {
			req.resultCh <- nil
		}
	}

	b.Close()

	err := b.Submit(newBatchReq("repo", "main"))
	require.ErrorIs(t, err, errBatcherClosed)
}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

// TestBatcher_FlushDeliversLockError exercises the real flush path (not an
// injected fake): the batch-size metric is observed, and when the locker fails to
// acquire, that error is fanned out to every submitter rather than leaving them
// blocked on their result channels.
func TestBatcher_FlushDeliversLockError(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/dev/null") // lets updater.NewGitRepo load its config

	ctrl := gomock.NewController(t)
	metrics := mocks.NewMockMetricsInterface(ctrl)
	metrics.EXPECT().ObserveGitBatchSize(1).Times(1)

	locker := &spyLocker{err: errors.New("lock boom")}
	b := NewBatcher(locker, t.TempDir(), 20, metrics)
	// Note: flushFn is intentionally NOT overridden, so b.flush runs for real.

	req := newBatchReq("git@example.com:test/repo.git", "main")
	req.gitopsRepo.Path = "apps"

	err := b.Submit(req)

	require.EqualError(t, err, "lock boom", "lock acquisition failure must reach the submitter")
	assert.True(t, locker.called)
}
