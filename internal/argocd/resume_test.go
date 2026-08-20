package argocd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
)

func monitorWithDefaultWindow(window time.Duration) *DeploymentMonitor {
	return &DeploymentMonitor{
		retryDelay:      time.Second,
		defaultAttempts: uint(window / time.Second),
	}
}

func TestRemainingWindow(t *testing.T) {
	now := time.Unix(2000, 0)

	tests := []struct {
		name          string
		taskTimeout   int
		createdAgo    time.Duration
		defaultWindow time.Duration
		want          time.Duration
		wantResumable bool
	}{
		{
			// The window is a whole number of polls, so a 600s timeout at a 1s delay is
			// polled for 601s — and that is what the remainder is measured against.
			name:          "a task resumed early keeps the rest of its own window",
			taskTimeout:   600,
			createdAgo:    100 * time.Second,
			defaultWindow: 60 * time.Second,
			want:          501 * time.Second,
			wantResumable: true,
		},
		{
			name:          "a task without an override is bounded by the instance default",
			createdAgo:    20 * time.Second,
			defaultWindow: 60 * time.Second,
			want:          40 * time.Second,
			wantResumable: true,
		},
		{
			name:          "a task whose window already elapsed is not resumed",
			taskTimeout:   60,
			createdAgo:    90 * time.Second,
			defaultWindow: 60 * time.Second,
			wantResumable: false,
		},
		{
			// The replica that accepted this task would still be polling here: its last
			// attempt is the one that lands on the configured timeout.
			name:          "a task at its configured timeout still has its final poll",
			taskTimeout:   60,
			createdAgo:    60 * time.Second,
			defaultWindow: 60 * time.Second,
			want:          time.Second,
			wantResumable: true,
		},
		{
			// A sub-second remainder rounds down to a zero timeout, which the rollout
			// reads as "no override" and answers with a full default window — so this
			// must not be resumable, or the deadline silently restarts.
			name:          "a task with under a second left is not resumed",
			taskTimeout:   60,
			createdAgo:    60500 * time.Millisecond,
			defaultWindow: 60 * time.Second,
			wantResumable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := monitorWithDefaultWindow(tt.defaultWindow)
			task := models.Task{
				Timeout: tt.taskTimeout,
				Created: float64(now.Add(-tt.createdAgo).Unix()),
			}

			remaining, resumable := monitor.remainingWindow(task, now)

			assert.Equal(t, tt.wantResumable, resumable)
			if tt.wantResumable {
				assert.Equal(t, tt.want, remaining)
			}
		})
	}
}

// A handoff must not restart the clock: however many replicas a task passes
// through, the total time it may poll is the window it was accepted with.
func TestRemainingWindow_ShrinksAcrossSuccessiveHandoffs(t *testing.T) {
	monitor := monitorWithDefaultWindow(time.Minute)
	created := time.Unix(1000, 0)
	task := models.Task{Timeout: 300, Created: float64(created.Unix())}

	first, resumable := monitor.remainingWindow(task, created.Add(time.Minute))
	assert.True(t, resumable)

	second, resumable := monitor.remainingWindow(task, created.Add(3*time.Minute))
	assert.True(t, resumable)

	assert.Less(t, second, first)
	assert.Equal(t, 2*time.Minute+time.Second, second)
}

// TestResumeRollout_AbortsAnElapsedWindow covers the arm remainingWindow only
// feeds: a deployment whose window ran out while nobody was watching is recorded
// as aborted here, and — because the replica that accepted it announced it as
// started — announced as finished too. Argo CD is never called, since there is
// nothing left to wait for.
func TestResumeRollout_AbortsAnElapsedWindow(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := newTaskRepositoryMock(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := models.Task{
		Id:      "stale-id",
		App:     "test-app",
		Timeout: 30,
		// Accepted well over its own window ago.
		Created:   float64(time.Now().Add(-10 * time.Minute).Unix()),
		Validated: true,
		Images:    []models.Image{{Image: "app", Tag: "v1"}},
	}

	metricsMock.EXPECT().AddUnconfirmedFailure()
	stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, StaleResumedTaskReason)
	// No GetApplication expectation: polling a deployment past its deadline would
	// only end in the same abort.

	updater.ResumeRollout(task, neverDraining)

	require.Len(t, capture.sent, 1, "the deployment must still be reported as finished")
	assert.Equal(t, models.StatusAborted, capture.sent[0].Status)
}

// TestResumeRollout_KeepsTheOriginalDeadline is the guard against a handover
// silently restarting the clock: the resumed rollout must be given what is left
// of the window the deployment was accepted with, never a fresh one.
func TestResumeRollout_KeepsTheOriginalDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := newTaskRepositoryMock(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	stateMock.EXPECT().GetTask(gomock.Any()).
		Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	// 600s window, 590s of it already spent.
	task := models.Task{
		Id:        "resumed-id",
		App:       "test-app",
		Timeout:   600,
		Created:   float64(time.Now().Add(-590 * time.Second).Unix()),
		Validated: true,
		Images:    []models.Image{{Image: "app", Tag: "v1"}},
	}

	// The deployment is already done, so the resumed monitor settles on its first poll.
	application := models.Application{}
	application.Status.Summary.Images = []string{"app:v1"}
	application.Status.Sync.Status = "Synced"
	application.Status.Health.Status = "Healthy"
	// The initial fetch runs unbounded by design; the polling loop is what carries
	// the rollout deadline, so that is where the resumed window is observable.
	var polledWindows []time.Duration
	apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, _ bool) (*models.Application, error) {
			if deadline, ok := ctx.Deadline(); ok {
				polledWindows = append(polledWindows, time.Until(deadline))
			}
			return &application, nil
		}).AnyTimes()

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().ResetFailedDeployment(task.App)
	metricsMock.EXPECT().ObserveDeploymentDuration(task.App, gomock.Any())
	stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

	updater.ResumeRollout(task, neverDraining)

	require.NotEmpty(t, polledWindows, "the resumed rollout must be polled under a deadline")
	for _, window := range polledWindows {
		assert.Less(t, window, time.Duration(task.Timeout)*time.Second,
			"a resumed rollout must get what is left of its window, never a fresh one")
	}

	require.Len(t, capture.sent, 1,
		"a resumed deployment was already announced as started by the replica that accepted it")
	assert.Equal(t, models.StatusDeployedMessage, capture.sent[0].Status)
}

// Shutdown can begin after the reaper handed the task over but before the rollout
// finishes. From that point on this replica finishes nothing: its claim is released
// in the last shutdown phase and another replica resumes the deployment. Writing a
// status here would record an outcome for a rollout this replica no longer decides —
// and once the write-back batcher is closed, that outcome is a failure for a
// deployment that is otherwise healthy.
func TestResumeRollout_StopsWhenTheReplicaStartsDraining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := newTaskRepositoryMock(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	stateMock.EXPECT().GetTask(gomock.Any()).
		Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := models.Task{
		Id:        "draining-id",
		App:       "test-app",
		Timeout:   600,
		Created:   float64(time.Now().Unix()),
		Validated: true,
		Images:    []models.Image{{Image: "app", Tag: "v1"}},
	}

	// The rollout has already succeeded, so a monitor that keeps going records a
	// terminal status — which is what must not happen here.
	application := models.Application{}
	application.Status.Summary.Images = []string{"app:v1"}
	application.Status.Sync.Status = "Synced"
	application.Status.Health.Status = "Healthy"

	// The call count is load-bearing: shutdown begins during the fetch that precedes
	// the polling loop, so the loop must give up on its first check and never reach a
	// fetch of its own. A second call here means the rollout kept polling ArgoCD for
	// a deployment this replica had already abandoned.
	var draining atomic.Bool
	apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).
		DoAndReturn(func(context.Context, string, bool) (*models.Application, error) {
			draining.Store(true)
			return &application, nil
		}).Times(1)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	// No SetTaskStatus and no deployment metrics are expected: gomock fails the test
	// if the abandoned monitor records anything.

	updater.ResumeRollout(task, draining.Load)

	assert.Empty(t, capture.sent,
		"a deployment left to another replica must not be announced as finished")
}

// A superseded task is the one outcome a draining replica must still announce. The
// newer deployment already wrote "cancelled", and a cancelled task is never
// re-claimed by a sweep — so the replica that resumes nothing here is the last one
// able to report it, and staying silent loses the notification for good.
func TestResumeRollout_AnnouncesASupersededTaskEvenWhileDraining(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := newTaskRepositoryMock(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	// A newer deployment for the same app already cancelled this task.
	stateMock.EXPECT().GetTask(gomock.Any()).
		Return(&models.Task{Status: models.StatusCancelledMessage}, nil).AnyTimes()

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := models.Task{
		Id:        "superseded-id",
		App:       "test-app",
		Timeout:   600,
		Created:   float64(time.Now().Unix()),
		Validated: true,
		Images:    []models.Image{{Image: "app", Tag: "v1"}},
	}

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	// No SetTaskStatus: the superseding deployment already recorded "cancelled".

	updater.ResumeRollout(task, func() bool { return true })

	require.Len(t, capture.sent, 1, "the cancellation must still be announced")
	assert.Equal(t, models.StatusCancelledMessage, capture.sent[0].Status)
}

// Shutdown is not observed at a single point: the poll loop checks it at the top of
// an iteration, while the rollout's outcome is decided later in that same iteration.
// A rollout that finishes — or fails — in that gap reaches the end of monitoring with
// an ordinary result, and only the re-check that follows keeps this replica from
// recording it. Both arms are covered: an outcome that would be written as a success,
// and a fetch failure that would be written as a terminal failure.
func TestResumeRollout_WritesNothingWhenShutdownBeginsMidPoll(t *testing.T) {
	tests := []struct {
		name           string
		inLoopFetchErr error
	}{
		{name: "the rollout succeeded in the same iteration"},
		{
			name:           "the fetch failed terminally in the same iteration",
			inLoopFetchErr: errors.New(`rpc error: code = NotFound desc = applications.argoproj.io "test-app" not found`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			apiMock := newArgoApiMock(ctrl)
			metricsMock := mocks.NewMockMetricsInterface(ctrl)
			stateMock := newTaskRepositoryMock(ctrl)

			argo := &Argo{}
			argo.Init(stateMock, apiMock, metricsMock)
			stateMock.EXPECT().GetTask(gomock.Any()).
				Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

			updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
			capture := &capturingStrategy{}
			updater.notifier = notifications.NewNotifier(capture)

			task := models.Task{
				Id:        "mid-poll-id",
				App:       "test-app",
				Timeout:   600,
				Created:   float64(time.Now().Unix()),
				Validated: true,
				Images:    []models.Image{{Image: "app", Tag: "v1"}},
			}

			application := models.Application{}
			application.Status.Summary.Images = []string{"app:v1"}
			application.Status.Sync.Status = "Synced"
			application.Status.Health.Status = "Healthy"

			// Shutdown begins during the poll loop's own fetch — after the abandon check
			// that opens the iteration, and before its outcome is classified.
			var draining atomic.Bool
			var fetches int
			apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).
				DoAndReturn(func(context.Context, string, bool) (*models.Application, error) {
					fetches++
					if fetches == 1 {
						return &application, nil
					}
					draining.Store(true)
					if tt.inLoopFetchErr != nil {
						return nil, tt.inLoopFetchErr
					}
					return &application, nil
				}).Times(2)

			metricsMock.EXPECT().AddInProgressTask()
			metricsMock.EXPECT().RemoveInProgressTask()
			// Nothing else is declared: gomock fails the test if this replica records a
			// status, a deployment metric or a failure for a rollout it is abandoning.

			updater.ResumeRollout(task, draining.Load)

			assert.Empty(t, capture.sent,
				"a rollout left to another replica must not be announced")
		})
	}
}

// The window a handover is judged against must be the window the rollout is actually
// given: the poll loop is configured in whole attempts, so a task carrying a timeout
// polls for that timeout rounded up to the next attempt. Measuring the remainder
// against the raw timeout instead would abort a handover that lands in the last
// interval — while the replica that accepted it would still have been polling.
func TestRemainingWindow_MatchesThePollBudget(t *testing.T) {
	// 900s at a 15s delay is 61 attempts, so the rollout polls for 915s.
	monitor := &DeploymentMonitor{retryDelay: 15 * time.Second, defaultAttempts: 61}
	created := time.Unix(1000, 0)
	task := models.Task{Timeout: 900, Created: float64(created.Unix())}

	_, deadline := monitor.configureRetryOptions(task)
	require.Equal(t, 915*time.Second, deadline, "the poll budget is the attempts times the delay")

	remaining, resumable := monitor.remainingWindow(task, created.Add(900*time.Second))

	assert.True(t, resumable, "the accepting replica would still be polling at this age")
	assert.Equal(t, deadline-900*time.Second, remaining)
}

// The bound the documentation states: a handover costs at most one extra poll
// interval, because the remainder is re-expressed in whole attempts, and it does not
// widen however often the task changes hands. Each handover is measured against the
// persisted task — ResumeRollout rewrites Timeout on its own copy only — so this
// walks the real sequence rather than a chain of rewritten tasks.
func TestRemainingWindow_HandoverOvershootStaysBounded(t *testing.T) {
	const delay = 15 * time.Second
	monitor := &DeploymentMonitor{retryDelay: delay, defaultAttempts: 61}
	created := time.Unix(1000, 0)
	task := models.Task{Timeout: 900, Created: float64(created.Unix())}

	_, unattended := monitor.configureRetryOptions(task)
	bound := unattended + delay

	var worst time.Duration
	for _, handoverAt := range []time.Duration{time.Second, 100 * time.Second, 450 * time.Second, 899 * time.Second, 914 * time.Second} {
		remaining, resumable := monitor.remainingWindow(task, created.Add(handoverAt))
		require.True(t, resumable, "a handover at %s is still inside the window", handoverAt)

		resumed := task
		resumed.Timeout = int(remaining.Seconds())
		_, deadline := monitor.configureRetryOptions(resumed)

		if end := handoverAt + deadline; end > worst {
			worst = end
		}
	}

	assert.LessOrEqual(t, worst, bound,
		"a resumed rollout must not poll more than one interval past the un-handed-over budget")
}
