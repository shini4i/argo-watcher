package argocd

import (
	"context"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// degradedGraceTask is a task whose saved images hash predates the rollout, so a Degraded
// app running the new image counts as a degradation of this deployment.
func degradedGraceTask(timeout int) models.Task {
	return models.Task{
		Id:      "task-id",
		App:     "demo",
		Timeout: timeout,
		Images:  []models.Image{{Image: "example.com/app", Tag: "v2"}},
		SavedAppStatus: models.SavedAppStatus{
			ImagesHash: helpers.GenerateHash("example.com/app:v1"),
		},
	}
}

func appWithHealth(health string) *models.Application {
	app := &models.Application{}
	app.Status.Summary.Images = []string{"example.com/app:v2"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = health
	return app
}

func newDegradedGraceMonitor(ctrl *gomock.Controller, api *mocks.MockArgoApiInterface, grace time.Duration) *DeploymentMonitor {
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	api.EXPECT().GetManifests(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl), metrics: refreshMetrics(ctrl)},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	monitor.degradedGrace = grace
	return monitor
}

func TestArgoStatusUpdaterInitAppliesTheDegradedGracePeriod(t *testing.T) {
	cfg := newUpdaterTestConfig(lock.NewInMemoryLocker())
	cfg.DegradedGracePeriod = 90 * time.Second

	updater := &ArgoStatusUpdater{}
	require.NoError(t, updater.Init(Argo{}, cfg))

	assert.Equal(t, 90*time.Second, updater.monitor.degradedGrace)
}

// A short outage unrelated to the rollout, such as an HPA briefly missing metrics, turns the
// app Degraded and back. The rollout must keep polling through it and succeed (issue #631).
func TestWaitRolloutSurvivesATransientDegradation(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mocks.NewMockArgoApiInterface(ctrl)
	gomock.InOrder(
		api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).Return(appWithHealth("Degraded"), nil).Times(2),
		api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).Return(appWithHealth("Healthy"), nil),
	)
	monitor := newDegradedGraceMonitor(ctrl, api, time.Hour)

	app, _, err := monitor.WaitRollout(degradedGraceTask(600), neverLost)

	require.NoError(t, err)
	assert.Equal(t, "Healthy", app.Status.Health.Status)
}

// A degradation that outlasts the grace period is still terminal: the rollout fails long
// before the task's own timeout instead of waiting it out.
func TestWaitRolloutFailsOnceTheDegradationOutlastsTheGracePeriod(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).Return(appWithHealth("Degraded"), nil).MinTimes(2)
	grace := 20 * time.Millisecond
	monitor := newDegradedGraceMonitor(ctrl, api, grace)

	// With 1ms polls a 3600s timeout gives a ~3.6s deadline, well past the bound below.
	app, waited, err := monitor.WaitRollout(degradedGraceTask(3600), neverLost)

	require.NoError(t, err, "the degraded sentinel must be swallowed so the caller reports the app state")
	assert.Equal(t, "Degraded", app.Status.Health.Status)
	assert.GreaterOrEqual(t, waited, grace)
	assert.Less(t, waited, time.Second, "a lasting degradation must not wait out the task timeout")
}

// The grace period measures one continuous degradation. A poll that is no longer Degraded
// restarts it, so two short outages never add up to a failure.
func TestWaitRolloutRestartsTheGracePeriodAfterARecovery(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	grace := 50 * time.Millisecond
	firstOutage := 30 * time.Millisecond
	var outageStart time.Time
	recovered := false

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).
		DoAndReturn(func(context.Context, string, bool) (*models.Application, error) {
			if outageStart.IsZero() {
				outageStart = time.Now()
			}
			if !recovered && time.Since(outageStart) >= firstOutage {
				recovered = true
				return appWithHealth("Progressing"), nil
			}
			return appWithHealth("Degraded"), nil
		}).MinTimes(3)
	monitor := newDegradedGraceMonitor(ctrl, api, grace)

	app, waited, err := monitor.WaitRollout(degradedGraceTask(3600), neverLost)

	require.NoError(t, err)
	assert.Equal(t, "Degraded", app.Status.Health.Status)
	assert.GreaterOrEqual(t, waited, firstOutage+grace)
	assert.Less(t, waited, time.Second, "the restarted grace period must still expire")
}

// A task whose timeout ends inside the grace period still reports the degraded state it
// observed, with no error for the caller to blame on ArgoCD.
func TestWaitRolloutReportsADegradationStillInsideTheGracePeriodAtTheDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).Return(appWithHealth("Degraded"), nil).MinTimes(1)
	monitor := newDegradedGraceMonitor(ctrl, api, time.Hour)

	app, _, err := monitor.WaitRollout(degradedGraceTask(1), neverLost)

	require.NoError(t, err)
	assert.Equal(t, "Degraded", app.Status.Health.Status)
}

// A zero grace period fails on the first Degraded poll.
func TestWaitRolloutWithoutAGracePeriodFailsOnTheFirstDegradedPoll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetApplication(gomock.Any(), "demo", gomock.Any()).Return(appWithHealth("Degraded"), nil).Times(1)
	monitor := newDegradedGraceMonitor(ctrl, api, 0)

	app, _, err := monitor.WaitRollout(degradedGraceTask(600), neverLost)

	require.NoError(t, err)
	assert.Equal(t, "Degraded", app.Status.Health.Status)
}
