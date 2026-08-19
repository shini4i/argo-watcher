package server

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// resumeRecorder collects the tasks the reaper handed back for monitoring.
type resumeRecorder struct {
	mu     sync.Mutex
	tasks  []models.Task
	resume chan struct{}
}

func newResumeRecorder() *resumeRecorder {
	return &resumeRecorder{resume: make(chan struct{}, 8)}
}

func (r *resumeRecorder) record(task models.Task) {
	r.mu.Lock()
	r.tasks = append(r.tasks, task)
	r.mu.Unlock()

	select {
	case r.resume <- struct{}{}:
	default:
	}
}

func (r *resumeRecorder) recorded() []models.Task {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]models.Task(nil), r.tasks...)
}

func TestReapAbandonedTasks_ResumesWhatItClaims(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	abandoned := []models.Task{{Id: "first"}, {Id: "second"}}
	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().ClaimExpiredTasks(gomock.Any()).Return(abandoned, nil).MinTimes(1)

	recorder := newResumeRecorder()
	stop := make(chan struct{})
	defer close(stop)

	go reapAbandonedTasks(stop, notDraining, time.Millisecond, stateMock, recorder.record)

	// Sweeps keep firing, so assert that both tasks were handed over rather than
	// on the order or the exact count, which two overlapping sweeps make arbitrary.
	require.Eventually(t, func() bool {
		seen := map[string]bool{}
		for _, task := range recorder.recorded() {
			seen[task.Id] = true
		}
		return seen["first"] && seen["second"]
	}, time.Second, time.Millisecond)
}

// A sweep that cannot reach the database is a failed sweep, not a reason to stop
// sweeping: the next tick tries again once the outage clears.
func TestReapAbandonedTasks_SurvivesAFailedSweep(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sweeps := make(chan struct{}, 8)
	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().ClaimExpiredTasks(gomock.Any()).DoAndReturn(func(int) ([]models.Task, error) {
		select {
		case sweeps <- struct{}{}:
		default:
		}
		return nil, errors.New("database unreachable")
	}).MinTimes(2)

	recorder := newResumeRecorder()
	stop := make(chan struct{})
	defer close(stop)

	go reapAbandonedTasks(stop, notDraining, time.Millisecond, stateMock, recorder.record)

	<-sweeps
	<-sweeps
	assert.Empty(t, recorder.recorded())
}

func TestReapAbandonedTasks_StopsOnShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().ClaimExpiredTasks(gomock.Any()).Return(nil, nil).AnyTimes()

	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		reapAbandonedTasks(stop, notDraining, time.Millisecond, stateMock, newResumeRecorder().record)
		close(finished)
	}()

	close(stop)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("the reaper kept running after shutdown was signalled")
	}
}

// notDraining is the drain predicate for tests about sweeping itself: this
// replica is serving normally and may take on abandoned work.
func notDraining() bool { return false }

// A replica on its way out must not take on deployments it cannot finish.
func TestReapAbandonedTasks_ClaimsNothingWhileDraining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().ClaimExpiredTasks(gomock.Any()).Times(0)

	recorder := newResumeRecorder()
	stop := make(chan struct{})
	defer close(stop)

	// Counting the predicate proves the loop actually ticked and chose not to claim.
	// A fixed sleep would report success on a loaded runner where no tick ever fired.
	var checks atomic.Int32
	go reapAbandonedTasks(stop, func() bool {
		checks.Add(1)
		return true
	}, time.Millisecond, stateMock, recorder.record)

	require.Eventually(t, func() bool { return checks.Load() >= 2 }, time.Second, time.Millisecond)
	assert.Empty(t, recorder.recorded())
}

// A panic in one resumed deployment must not take the replica down, or the task
// would be re-claimed elsewhere and walk the crash through the fleet.
func TestReapAbandonedTasks_ContainsAPanickingResume(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := mocks.NewMockTaskRepository(ctrl)
	stateMock.EXPECT().ClaimExpiredTasks(gomock.Any()).Return([]models.Task{{Id: "poison"}}, nil).MinTimes(1)

	var attempts atomic.Int32
	stop := make(chan struct{})
	defer close(stop)

	go reapAbandonedTasks(stop, notDraining, time.Millisecond, stateMock, func(models.Task) {
		attempts.Add(1)
		panic("resume blew up")
	})

	// Sweeping past the first panic is the property: the panic was contained rather
	// than unwinding the process, and the reaper is still taking on work.
	require.Eventually(t, func() bool { return attempts.Load() >= 2 }, time.Second, time.Millisecond)
}
