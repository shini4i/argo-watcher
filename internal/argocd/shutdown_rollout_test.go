package argocd

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
	"github.com/shini4i/argo-watcher/internal/state"
)

// managedApp is a settled application the watcher writes back for, so a rollout
// reaches the git write-back and then ends on its first poll.
func managedApp() *models.Application {
	app := &models.Application{}
	app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}
	app.Spec.Source.RepoURL = "git@example.com/repo.git"
	app.Spec.Source.TargetRevision = "main"
	app.Spec.Source.Path = "path"
	app.Status.Summary.Images = []string{"app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Healthy"
	return app
}

func shutdownTestTask(id string) models.Task {
	return models.Task{
		Id:        id,
		App:       "test-app",
		Status:    models.StatusInProgressMessage,
		Timeout:   15,
		Validated: true,
		Images:    []models.Image{{Image: "app", Tag: "v1"}},
	}
}

// A write-back that failed and one that never ran because the batcher was torn
// down surface on the same return path. Which of the two a task is depends on
// whether anything can resume it: a "failed" task is never re-claimed by a sweep,
// so misreading a handover as a failure buries a healthy rollout permanently.
func TestWaitForRollout_ShutdownWriteBackIsNotADeploymentFailure(t *testing.T) {
	gitFailure := errors.New("remote refused the update")
	batchDrained := fmt.Errorf("batch git update stopped early: %w", errors.Join(errWritebackDraining, gitFailure))

	tests := []struct {
		name string
		// closeBatcher reproduces errBatcherClosed the way production raises it:
		// from Submit itself, once shutdown has torn the batcher down.
		closeBatcher bool
		writeBack    error
		draining     bool
		wantStatus   string
	}{
		{
			name:         "the batcher was closed before the write-back was queued",
			closeBatcher: true,
			draining:     true,
		},
		{
			// A different error, deliberately reaching the same outcome: once the replica
			// is draining the cause no longer decides anything, which is the arm's point.
			name:      "the batch retry loop stopped for the drain",
			writeBack: batchDrained,
			draining:  true,
		},
		{
			// In-memory state, where nothing resumes the task: silence would lose the
			// deployment outright, so the lost commit is reported as the failure it is.
			name:         "a write-back lost to shutdown with nobody to hand over to",
			closeBatcher: true,
			wantStatus:   models.StatusFailedMessage,
		},
		{
			// The control: an ordinary write-back failure is still this deployment's
			// outcome and must keep failing the task.
			name:       "an ordinary write-back failure still fails the deployment",
			writeBack:  gitFailure,
			wantStatus: models.StatusFailedMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			apiMock := newArgoApiMock(ctrl)
			metricsMock := mocks.NewMockMetricsInterface(ctrl)
			stateMock := notSupersededState(ctrl)

			argo := &Argo{}
			argo.Init(stateMock, apiMock, metricsMock)
			apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(managedApp(), nil).AnyTimes()

			metricsMock.EXPECT().AddInProgressTask()
			metricsMock.EXPECT().RemoveInProgressTask()
			metricsMock.EXPECT().InitDeploymentOutcomes("test-app")

			locker := lock.NewInMemoryLocker()
			updater := initTestUpdater(t, newUpdaterTestConfig(locker), argo)
			capture := &capturingStrategy{}
			updater.notifier = notifications.NewNotifier(capture)

			batcher := NewBatcher(locker, "/tmp/cache", 20, nil)
			batcher.flushFn = func(batch []*batchWriteRequest) {
				for _, req := range batch {
					req.resultCh <- tt.writeBack
				}
			}
			updater.gitUpdater = NewGitUpdater(locker, "/tmp/cache", nil, batcher)
			if tt.closeBatcher {
				batcher.Close(context.Background())
			}

			task := shutdownTestTask("shutdown-id")

			if tt.wantStatus != "" {
				metricsMock.EXPECT().AddFailedDeployment(task.App)
				metricsMock.EXPECT().AddDeploymentOutcome(task.App, tt.wantStatus)
				stateMock.EXPECT().SetTaskStatus(task.Id, tt.wantStatus, gomock.Any())
			}

			updater.WaitForRollout(task, false, func() bool { return tt.draining })

			if tt.wantStatus == "" {
				require.Len(t, capture.sent, 1, "the replica that resumes the task announces the result")
				assert.Equal(t, models.StatusInProgressMessage, capture.sent[0].Status)
				return
			}
			require.Len(t, capture.sent, 2, "a deployment this replica failed must be announced")
			assert.Equal(t, tt.wantStatus, capture.sent[1].Status)
		})
	}
}

// The drain now stops an in-flight write-back for a rollout this replica accepted,
// not only for a resumed one. The commit is deliberately not pushed, so the task
// must be handed on — unless a newer deployment cancelled it, which no sweep will
// re-claim and therefore nobody else could announce.
func TestWaitForRollout_WriteBackStoppedByTheDrain(t *testing.T) {
	tests := []struct {
		name      string
		cancelled bool
		wantFinal string
	}{
		{name: "handed on, so this replica announces nothing"},
		{
			name:      "cancelled mid-write-back, so the drain must not swallow it",
			cancelled: true,
			wantFinal: models.StatusCancelledMessage,
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
			apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(managedApp(), nil).AnyTimes()

			// In progress when the rollout starts, so the write-back is reached. The
			// flush below is what flips this, which is what makes the cancellation
			// discovered inside the write-back however many reads precede it.
			var cancelledMidFlush atomic.Bool
			stateMock.EXPECT().GetTask(gomock.Any()).
				DoAndReturn(func(string) (*models.Task, error) {
					if cancelledMidFlush.Load() {
						return &models.Task{Status: models.StatusCancelledMessage}, nil
					}
					return &models.Task{Status: models.StatusInProgressMessage}, nil
				}).AnyTimes()

			metricsMock.EXPECT().AddInProgressTask()
			metricsMock.EXPECT().RemoveInProgressTask()
			metricsMock.EXPECT().InitDeploymentOutcomes("test-app")
			if tt.wantFinal != "" {
				// The cancellation is an outcome this replica reports; it still writes no
				// status, because whichever replica cancelled the task already did.
				metricsMock.EXPECT().AddDeploymentOutcome("test-app", tt.wantFinal)
			}
			// Nothing else is declared: gomock fails the test if a status, a failure or a
			// deployment outcome is recorded for a write-back this replica gave up.

			locker := lock.NewInMemoryLocker()
			updater := initTestUpdater(t, newUpdaterTestConfig(locker), argo)
			capture := &capturingStrategy{}
			updater.notifier = notifications.NewNotifier(capture)

			// The real batch loop consults each request's stop predicate and resolves it
			// as superseded; this stands in for that without reaching a repository.
			var pushed atomic.Bool
			batcher := NewBatcher(locker, "/tmp/cache", 20, nil)
			batcher.flushFn = func(batch []*batchWriteRequest) {
				for _, req := range batch {
					// A newer deployment cancels the task while this batch is in flight.
					cancelledMidFlush.Store(tt.cancelled)
					if req.isSuperseded != nil && req.isSuperseded() {
						req.resultCh <- ErrDeploymentSuperseded
						continue
					}
					pushed.Store(true)
					req.resultCh <- nil
				}
			}
			updater.gitUpdater = NewGitUpdater(locker, "/tmp/cache", nil, batcher)

			updater.WaitForRollout(shutdownTestTask("write-back-id"), false, func() bool { return true })

			assert.False(t, pushed.Load(), "a write-back given up to the shutdown must not commit")
			if tt.wantFinal == "" {
				require.Len(t, capture.sent, 1, "the replica that resumes the task announces the result")
				assert.Equal(t, models.StatusInProgressMessage, capture.sent[0].Status)
				return
			}
			require.Len(t, capture.sent, 2, "a cancellation is never re-claimed, so it must be announced here")
			assert.Equal(t, tt.wantFinal, capture.sent[1].Status)
		})
	}
}

// Shutdown can begin after the rollout is already being polled. The accepting side
// must give it up there too, exactly as a resumed rollout does: its claim is
// released in the last shutdown phase and another replica records the outcome.
func TestWaitForRollout_GivesUpWhenTheDrainBeginsMidPoll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := notSupersededState(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	// Unmanaged, so no write-back stands between the confirmation and the poll loop,
	// and still Progressing, so the loop reaches a second iteration to be stopped at.
	app := &models.Application{}
	app.Status.Summary.Images = []string{"app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Progressing"

	// Shutdown begins during the poll's own fetch: after the abandon check that opens
	// the iteration, so only the next one can act on it.
	var fetches atomic.Int32
	var draining atomic.Bool
	apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, bool) (*models.Application, error) {
			if fetches.Add(1) > 1 {
				draining.Store(true)
			}
			return app, nil
		}).Times(2)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().InitDeploymentOutcomes("test-app")
	// No SetTaskStatus and no outcome counter: this replica decides nothing now.

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := shutdownTestTask("mid-poll-id")
	task.Validated = false

	updater.WaitForRollout(task, false, draining.Load)

	assert.Equal(t, int32(2), fetches.Load(), "the rollout must be polled before it is given up")
	require.Len(t, capture.sent, 1, "the replica that resumes the task announces the result")
	assert.Equal(t, models.StatusInProgressMessage, capture.sent[0].Status)
}

// The drain can flip during the poll that settles the rollout, so the iteration
// finishes and reports a terminal outcome. Writing it is what a monitor about to
// disappear must not do: the claim is released moments later and the replica that
// resumes the task decides the result.
func TestWaitForRollout_GivesUpARolloutThatSettledAsTheDrainBegan(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := notSupersededState(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	// Unmanaged, so nothing but the poll loop stands between confirmation and the
	// outcome, and already carrying the task's image, so the first poll settles it.
	app := &models.Application{}
	app.Status.Summary.Images = []string{"app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Healthy"

	var fetches atomic.Int32
	var draining atomic.Bool
	apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, bool) (*models.Application, error) {
			if fetches.Add(1) > 1 {
				draining.Store(true)
			}
			return app, nil
		}).Times(2)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().InitDeploymentOutcomes("test-app")
	// Nothing else: gomock fails the test if this replica records the outcome it
	// observed — the status, the outcome counter or the duration.

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := shutdownTestTask("settled-id")
	task.Validated = false

	updater.WaitForRollout(task, false, draining.Load)

	assert.Equal(t, int32(2), fetches.Load(), "the rollout must reach its outcome before it is given up")
	require.Len(t, capture.sent, 1, "the replica that resumes the task announces the result")
}

// The claim can move on between the last lease check and the status write, which
// the backend refuses. The outcome stored is then the new owner's, so counting or
// announcing one here would report the deployment twice — and the two reports can
// disagree, this replica saying failed where the owner recorded deployed.
func TestWaitForRollout_ARefusedWriteIsNotCountedOrAnnounced(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := notSupersededState(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	app := &models.Application{}
	app.Status.Summary.Images = []string{"app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Healthy"
	apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).Return(app, nil).AnyTimes()

	// The rollout succeeded, so this replica tries to record "deployed" — and the
	// backend refuses it because the claim is no longer here.
	stateMock.EXPECT().SetTaskStatus("refused-id", models.StatusDeployedMessage, "").
		Return(state.ErrTaskNotOwned)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().InitDeploymentOutcomes("test-app")
	// Nothing else: gomock fails the test if this replica touches the app's gauge,
	// counts an outcome or times a deployment the owner already recorded.

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	updater.WaitForRollout(shutdownTestTask("refused-id"), false, neverDraining)

	require.Len(t, capture.sent, 1, "only the start notification may be sent")
	assert.Equal(t, models.StatusInProgressMessage, capture.sent[0].Status)
}

// WaitForRollout re-checks draining before dispatching the outcome, and the failure
// path then fetches the resource tree for up to ten seconds before writing. A drain
// starting inside that fetch reaches SetTaskStatus unchecked, which is why the store
// refuses the write rather than another in-process guard.
func TestWaitForRollout_ADrainStartingInsideTheDiagnosticsFetchStillReachesTheWrite(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mocks.NewMockArgoApiInterface(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := notSupersededState(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	app := &models.Application{}
	app.Status.Summary.Images = []string{"app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Degraded"
	apiMock.EXPECT().GetApplication(gomock.Any(), gomock.Any(), gomock.Any()).Return(app, nil).AnyTimes()
	apiMock.EXPECT().GetManagedResources(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	var draining atomic.Bool
	apiMock.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string) (*models.ApplicationTree, error) {
			draining.Store(true)
			return nil, nil
		})

	// Reached despite the drain: the check that would have stopped it is already past.
	stateMock.EXPECT().SetTaskStatus("drained-id", models.StatusFailedMessage, gomock.Any()).
		Return(state.ErrTaskNotOwned).Times(1)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().InitDeploymentOutcomes("test-app")

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	updater.WaitForRollout(shutdownTestTask("drained-id"), false, draining.Load)

	require.Len(t, capture.sent, 1, "only the start notification may be sent")
	assert.Equal(t, models.StatusInProgressMessage, capture.sent[0].Status)
}
