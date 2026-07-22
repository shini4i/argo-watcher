package argocd

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
	"github.com/shini4i/argo-watcher/internal/updater"
)

// errBatcherClosed is returned by Submit after the batcher has been shut down.
// The task's write-back is rejected rather than silently dropped so the caller
// surfaces a real error instead of hanging forever on its result channel.
var errBatcherClosed = errors.New("git write-back batcher is shutting down")

// Batcher coalesces concurrent git write-backs to the same repository branch into
// a single clone + push. It is the optional, contention-driven alternative to the
// per-app serialized path: an idle repo flushes immediately (no added latency),
// while requests that arrive while a flush for the same key is in flight — cloning,
// pushing, or waiting on the per-repo lock — queue into the next batch and flush
// together the moment the current flush finishes. Batching therefore happens
// exactly during the window a push occupies, i.e. precisely when contention exists.
type Batcher struct {
	locker        lock.Locker
	repoCachePath string
	maxBatchSize  int
	// metrics records the coalesced batch size. May be nil, in which case
	// observation is skipped.
	metrics prometheus.MetricsInterface

	// flushFn runs one batch and delivers a result on each request's channel.
	// It defaults to (*Batcher).flush and is overridable in tests so the
	// coalescing logic can be exercised without real git operations.
	flushFn func(batch []*batchWriteRequest)

	mu sync.Mutex
	// pending holds the not-yet-flushed requests per key. A key is present in
	// active exactly while a flush goroutine is draining its pending queue.
	pending map[string][]*batchWriteRequest
	active  map[string]bool
	// wg tracks in-flight flush goroutines so Close can wait for them to drain.
	wg     sync.WaitGroup
	closed bool
}

// NewBatcher creates a Batcher. maxBatchSize bounds how many apps are committed in
// a single flush; the pending queue keeps accumulating across flushes until
// drained, so this caps one flush's commit count, not total in-flight work.
// metrics may be nil.
func NewBatcher(locker lock.Locker, repoCachePath string, maxBatchSize uint, metrics prometheus.MetricsInterface) *Batcher {
	b := &Batcher{
		locker:        locker,
		repoCachePath: repoCachePath,
		maxBatchSize:  int(maxBatchSize),
		metrics:       metrics,
		pending:       make(map[string][]*batchWriteRequest),
		active:        make(map[string]bool),
	}
	b.flushFn = b.flush
	return b
}

// batchKey groups requests that can share a clone and a commit: the same
// repository URL and branch. Different branches of the same repo get separate
// keys (they cannot share a commit) but still serialize on the per-URL lock.
func batchKey(repo *models.GitopsRepo) string {
	return repo.RepoUrl + "\x00" + repo.BranchName
}

// Submit enqueues a write-back request and blocks until the batch it is folded
// into has been flushed, returning that request's individual outcome. It returns
// errBatcherClosed if the batcher is shutting down.
func (b *Batcher) Submit(req *batchWriteRequest) error {
	key := batchKey(req.gitopsRepo)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errBatcherClosed
	}
	b.pending[key] = append(b.pending[key], req)
	// Start a flush goroutine only if one is not already draining this key. An
	// idle key therefore flushes immediately; a busy key coalesces into the next
	// batch handled by the existing goroutine.
	if !b.active[key] {
		b.active[key] = true
		b.wg.Add(1)
		go b.flushLoop(key)
	}
	b.mu.Unlock()

	return <-req.resultCh
}

// flushLoop drains a key's pending queue, flushing in batches of at most
// maxBatchSize, until the queue is empty. It then clears the key's active flag
// under the lock so a subsequent Submit starts a fresh loop. Holding the lock
// across the empty-check and the clear is what makes the hand-off race-free.
func (b *Batcher) flushLoop(key string) {
	defer b.wg.Done()
	for {
		b.mu.Lock()
		queue := b.pending[key]
		if len(queue) == 0 {
			delete(b.pending, key)
			delete(b.active, key)
			b.mu.Unlock()
			return
		}
		n := len(queue)
		if n > b.maxBatchSize {
			n = b.maxBatchSize
		}
		batch := queue[:n]
		// Reassign the remainder to a fresh slice so future appends by Submit do
		// not touch the backing array the current batch still references.
		b.pending[key] = append([]*batchWriteRequest(nil), queue[n:]...)
		b.mu.Unlock()

		b.flushFn(batch)
	}
}

// flush runs one batch under the per-repository lock and delivers each request's
// outcome. All requests in a batch share the same repo URL and branch, so a single
// GitRepo (one clone) and a single push serve the whole batch.
func (b *Batcher) flush(batch []*batchWriteRequest) {
	if len(batch) == 0 {
		return
	}

	if b.metrics != nil {
		b.metrics.ObserveGitBatchSize(len(batch))
	}

	repoURL := batch[0].gitopsRepo.RepoUrl
	branch := batch[0].gitopsRepo.BranchName

	// Path/FileName are empty: in batch mode each app supplies its own via
	// CommitAppLocal. The clone is keyed by URL+branch only, so one GitRepo serves
	// every app in the batch.
	repo, err := updater.NewGitRepo(repoURL, branch, "", "", b.repoCachePath, updater.GitClient{})
	if err != nil {
		b.deliverAll(batch, err)
		return
	}

	var outcomes map[*batchWriteRequest]error
	// context.Background() mirrors the single-app path (updateGitRepo); git
	// operations are bounded by GIT_OP_TIMEOUT per attempt rather than by a
	// caller context.
	lockErr := b.locker.WithLock(repoURL, func() error {
		outcomes = runBatchWriteBack(context.Background(), repo, batch)
		return nil
	})
	if lockErr != nil {
		// The lock itself failed (e.g. the Postgres advisory-lock transaction);
		// no write-back ran, so fail the whole batch with that error.
		b.deliverAll(batch, lockErr)
		return
	}

	for _, req := range batch {
		req.resultCh <- outcomes[req]
	}
}

// deliverAll sends the same error to every request in the batch. Used when the
// whole batch fails before per-request outcomes exist (repo construction or lock
// acquisition failure).
func (b *Batcher) deliverAll(batch []*batchWriteRequest, err error) {
	for _, req := range batch {
		req.resultCh <- err
	}
}

// Close stops accepting new requests and waits — bounded by ctx — for in-flight
// flush goroutines to drain their pending queues and deliver all results. New
// Submit calls return errBatcherClosed immediately.
//
// The wait is bounded so shutdown cannot exceed the caller's grace period: a flush
// stuck on the per-repo lock or retrying an unreachable remote could otherwise run
// for GIT_OP_TIMEOUT × GIT_MAX_ATTEMPTS, well past a typical shutdown deadline and
// risking a SIGKILL. When ctx expires first, Close returns and the still-running
// flushes are abandoned (they end when the process does) — the same outcome a hung
// unbounded drain would force, but without blocking termination.
func (b *Batcher) Close(ctx context.Context) {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("git write-back batch drain did not finish before the shutdown deadline; abandoning in-flight flushes", "error", ctx.Err())
	}
}
