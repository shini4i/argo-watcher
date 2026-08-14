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
// together the moment the current flush finishes.
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
	wg      sync.WaitGroup
	closed  bool
	// drainCh is closed by Close to tell in-flight retry loops to stop after their
	// current attempt. Without it a single flush can retry for
	// GIT_OP_TIMEOUT * GIT_MAX_ATTEMPTS and outlive the shutdown deadline, leaving
	// its requests abandoned without a result.
	drainCh chan struct{}
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
		drainCh:       make(chan struct{}),
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

// Submit blocks until the batch its request is folded into has been flushed, and
// returns that request's individual outcome, or errBatcherClosed when shutting down.
func (b *Batcher) Submit(req *batchWriteRequest) error {
	key := batchKey(req.gitopsRepo)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errBatcherClosed
	}
	b.pending[key] = append(b.pending[key], req)
	if !b.active[key] {
		b.active[key] = true
		b.wg.Add(1)
		go b.flushLoop(key)
	}
	b.mu.Unlock()

	return <-req.resultCh
}

// flushLoop drains a key's pending queue in batches of at most maxBatchSize. Holding
// the lock across the empty-check and the clear of the active flag is what makes the
// hand-off to a later Submit race-free.
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

// flush runs one batch under the per-repository lock. All requests in a batch share the
// same repo URL and branch, so a single GitRepo (one clone) and a single push serve the
// whole batch.
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
		outcomes = runBatchWriteBack(context.Background(), repo, batch, b.drainCh)
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

func (b *Batcher) deliverAll(batch []*batchWriteRequest, err error) {
	for _, req := range batch {
		req.resultCh <- err
	}
}

// Close stops accepting new requests and waits — bounded by ctx — for in-flight flush
// goroutines to drain. New Submit calls return errBatcherClosed.
//
// Closing drainCh tells in-flight retry loops to stop at their next retry boundary,
// so a flush that would otherwise spend GIT_OP_TIMEOUT × GIT_MAX_ATTEMPTS on an
// unreachable remote instead resolves its requests with errWritebackDraining after
// the attempt in flight.
//
// It is not a hard bound, in two ways. One attempt is itself capped only by
// GIT_OP_TIMEOUT (90s by default), which exceeds a typical shutdown budget, so a
// flush already blocked on a slow remote can outlive ctx. And drainCh is observed
// only at a retry boundary, never by flushLoop, so batches still queued behind a busy
// key each get one more full attempt — the drain bounds the retry budget per batch,
// not the number of queued batches.
//
// Requests abandoned when ctx expires are safe only because Run returns immediately
// afterwards and the process exits. Do not reuse Close in a context where the process
// keeps running — an abandoned request never receives a result, and whatever waits on
// its channel would block forever.
//
// Deliberately NOT done: having flushLoop resolve every queued batch on sight of the
// drain. That would discard write-backs that would have succeeded on their first
// attempt, trading a real commit for a faster shutdown.
//
// Close is safe to call more than once.
func (b *Batcher) Close(ctx context.Context) {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.drainCh)
	}
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
