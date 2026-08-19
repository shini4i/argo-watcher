package argocd

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shini4i/argo-watcher/internal/state"
)

// leaseGuard keeps this replica's claim on a task alive for as long as it is
// monitoring the rollout, and reports when the claim has been lost to another
// replica. Losing it is not an error to report to the user: the new owner is
// monitoring the same rollout and will record its outcome.
type leaseGuard struct {
	lost atomic.Bool
	// lastRenewed is when the most recent renewal was issued, as Unix nanoseconds.
	// Lost is derived from it as well as from the flag, so a claim that has aged
	// out is reported even if the renewing goroutine is wedged and never set the
	// flag itself.
	lastRenewed atomic.Int64
	interval    time.Duration
	ttl         time.Duration
	stop        chan struct{}
	stopOnce    sync.Once
	done        chan struct{}
}

// newLeaseGuard starts renewing the task's claim every interval until Stop is
// called. A renewal that fails to reach the store leaves the claim held: the
// lease outlives several intervals, so a blip must not abandon a rollout this
// replica still owns.
//
// That tolerance is bounded by the lease itself, and the bound is measured
// rather than inferred from a renewal returning. Once the claim is within one
// interval of ageing out it is given up, because past that point any sweep may
// take the task over — including a sweep in this very process, which would
// re-claim the row under the same owner id and so stay invisible to a renewal
// that only compares owners. Giving up early is safe: it costs a handover, while
// holding on too long costs two monitors writing two outcomes for one rollout.
func newLeaseGuard(repository state.TaskRepository, id string, interval, leaseTTL time.Duration) *leaseGuard {
	guard := &leaseGuard{
		interval: interval,
		ttl:      leaseTTL,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	guard.lastRenewed.Store(time.Now().UnixNano())

	go func() {
		defer close(guard.done)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-guard.stop:
				return
			case <-ticker.C:
				if guard.aged() {
					slog.Info("Giving up a claim that could not be kept alive within its lease", "id", id)
					guard.lost.Store(true)
					return
				}

				// Stamped before the statement, never after, so this replica's idea of the
				// deadline can only be earlier than the one Postgres recorded.
				issued := time.Now()
				held, err := repository.RenewLease(id)
				if err != nil {
					slog.Warn("Could not renew the claim on this deployment", "error", err, "id", id)
					continue
				}
				if !held {
					slog.Info("Another replica took this deployment over", "id", id)
					guard.lost.Store(true)
					return
				}

				guard.lastRenewed.Store(issued.UnixNano())
			}
		}
	}()

	return guard
}

// Lost reports whether this replica has stopped holding the task — because
// another replica took it over, or because the claim has aged far enough that it
// is about to become claimable by anyone.
func (guard *leaseGuard) Lost() bool {
	return guard.lost.Load() || guard.aged()
}

// aged reports whether the claim is within one renewal interval of lapsing,
// which is the last moment it can be given up before a sweep could take the task.
func (guard *leaseGuard) aged() bool {
	since := time.Since(time.Unix(0, guard.lastRenewed.Load()))

	return since+guard.interval >= guard.ttl
}

// Stop ends the renewals and waits for the goroutine to finish. It is safe to
// call more than once.
func (guard *leaseGuard) Stop() {
	guard.stopOnce.Do(func() { close(guard.stop) })
	<-guard.done
}
