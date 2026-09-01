package argocd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

func settledApp() *models.Application {
	app := &models.Application{}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Healthy"
	return app
}

func managedResources(manifests ...string) *models.ManagedResources {
	resources := &models.ManagedResources{}
	for _, manifest := range manifests {
		resources.Items = append(resources.Items, models.ManagedResource{TargetState: manifest})
	}
	return resources
}

func refreshMetrics(ctrl *gomock.Controller) *mocks.MockMetricsInterface {
	metrics := mocks.NewMockMetricsInterface(ctrl)
	metrics.EXPECT().ObserveRefreshDuration(gomock.Any(), gomock.Any()).AnyTimes()
	return metrics
}

func deploymentWith(image string) string {
	return `{"kind":"Deployment","spec":{"template":{"spec":{"containers":[{"image":"` + image + `"}]}}}}`
}

func TestShouldValidateDesiredImages(t *testing.T) {
	tests := []struct {
		name     string
		sync     string
		health   string
		status   string
		expected bool
	}{
		{"settledWithoutImage", "Synced", "Healthy", models.ArgoRolloutAppNotAvailable, true},
		{"stillSyncing", "OutOfSync", "Healthy", models.ArgoRolloutAppNotAvailable, false},
		{"stillProgressing", "Synced", "Progressing", models.ArgoRolloutAppNotAvailable, false},
		{"imageAlreadyThere", "Synced", "Healthy", models.ArgoRolloutAppSuccess, false},
		{"degraded", "Synced", "Degraded", models.ArgoRolloutAppDegraded, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := &models.Application{}
			app.Status.Sync.Status = test.sync
			app.Status.Health.Status = test.health
			assert.Equal(t, test.expected, shouldValidateDesiredImages(app, test.status))
		})
	}
}

func TestValidateDesiredImages(t *testing.T) {
	task := models.Task{
		Id:     "task-id",
		App:    "demo",
		Images: []models.Image{{Image: "ghcr.io/shini4i/typo", Tag: "v1"}},
	}

	newMonitor := func(api ArgoApiInterface, registryProxyUrl string) *DeploymentMonitor {
		return NewDeploymentMonitor(Argo{api: api}, registryProxyUrl, nil, false, time.Millisecond)
	}

	t.Run("failsWhenImageAbsentFromDesiredState", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(deploymentWith("ghcr.io/shini4i/app:v1")), nil)

		err := newMonitor(api, "").validateDesiredImages(context.Background(), task, settledApp())

		var imageErr *ImageNotPartOfAppError
		require.ErrorAs(t, err, &imageErr)
		assert.Equal(t, "ghcr.io/shini4i/typo", imageErr.Image)
		assert.Equal(t, "demo", imageErr.App)
		assert.Equal(t, []string{"ghcr.io/shini4i/app"}, imageErr.DesiredImages)
	})

	// The regression this whole check has to avoid: a CronJob that never fired contributes
	// no image to the live pod set, but its manifest is part of the desired state.
	t.Run("keepsWaitingWhenImageDeclaredButNotRunning", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		cronTask := task
		cronTask.Images = []models.Image{{Image: "ghcr.io/shini4i/cleanup", Tag: "v2"}}

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(`{"kind":"CronJob","spec":{"jobTemplate":{"spec":{"template":{"spec":{"containers":[{"image":"ghcr.io/shini4i/cleanup:v1"}]}}}}}}`), nil)

		assert.NoError(t, newMonitor(api, "").validateDesiredImages(context.Background(), cronTask, settledApp()))
	})

	t.Run("ignoresTagOnTheRequestedImage", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		tagged := task
		tagged.Images = []models.Image{{Image: "ghcr.io/shini4i/app:v1", Tag: "v2"}}

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(deploymentWith("ghcr.io/shini4i/app:v1")), nil)

		assert.NoError(t, newMonitor(api, "").validateDesiredImages(context.Background(), tagged, settledApp()))
	})

	t.Run("reportsTheFirstImageMissingFromAMultiImageTask", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		multi := task
		multi.Images = []models.Image{
			{Image: "ghcr.io/shini4i/app", Tag: "v2"},
			{Image: "ghcr.io/shini4i/typo", Tag: "v2"},
		}

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(deploymentWith("ghcr.io/shini4i/app:v1")), nil)

		err := newMonitor(api, "").validateDesiredImages(context.Background(), multi, settledApp())

		var imageErr *ImageNotPartOfAppError
		require.ErrorAs(t, err, &imageErr)
		assert.Equal(t, "ghcr.io/shini4i/typo", imageErr.Image)
	})

	t.Run("matchesThroughRegistryProxy", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(deploymentWith("proxy.local/ghcr.io/shini4i/typo:v1")), nil)

		assert.NoError(t, newMonitor(api, "proxy.local").validateDesiredImages(context.Background(), task, settledApp()))
	})

	t.Run("skipsWhenAppOptedOut", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		app := settledApp()
		app.Metadata.Annotations = map[string]string{"argo-watcher/skip-image-validation": "true"}

		api := mocks.NewMockArgoApiInterface(ctrl)

		assert.NoError(t, newMonitor(api, "").validateDesiredImages(context.Background(), task, app))
	})

	t.Run("keepsWaitingWhenLookupFails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(nil, errors.New("unavailable"))

		assert.NoError(t, newMonitor(api, "").validateDesiredImages(context.Background(), task, settledApp()))
	})

	t.Run("keepsWaitingWhenDesiredStateDeclaresNoImages", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		api := mocks.NewMockArgoApiInterface(ctrl)
		api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
			managedResources(`{"kind":"Service","spec":{"ports":[{"port":80}]}}`), nil)

		assert.NoError(t, newMonitor(api, "").validateDesiredImages(context.Background(), task, settledApp()))
	})
}

// TestWaitRolloutFailsFastOnImageNotPartOfApp verifies the poll loop aborts on the first
// settled poll instead of burning the task's whole timeout, and that the desired-state
// lookup happens only once.
func TestWaitRolloutFailsFastOnImageNotPartOfApp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := models.Task{
		Id:      "task-id",
		App:     "demo",
		Timeout: 600,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/typo", Tag: "v1"}},
	}

	// Raw mock: newArgoApiMock's catch-all GetManagedResources would shadow the
	// expectation this test is about.
	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(settledApp(), nil).Times(1)
	api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
		managedResources(deploymentWith("ghcr.io/shini4i/app:v1")), nil).Times(1)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl), metrics: refreshMetrics(ctrl)},
		"",
		[]retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	monitor.refreshApp = true

	_, _, err := monitor.WaitRollout(task, neverLost)

	var imageErr *ImageNotPartOfAppError
	require.ErrorAs(t, err, &imageErr)
}

func TestWaitRolloutValidatesDesiredImagesOnce(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := models.Task{
		Id:      "task-id",
		App:     "demo",
		Timeout: 3,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/app", Tag: "v2"}},
	}

	// Raw mock: newArgoApiMock's catch-all GetManagedResources would shadow the
	// expectation this test is about.
	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(settledApp(), nil).MinTimes(2)
	api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(nil, errors.New("unavailable")).Times(1)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl), metrics: refreshMetrics(ctrl)},
		"",
		[]retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	monitor.refreshApp = true

	_, _, err := monitor.WaitRollout(task, neverLost)
	require.NoError(t, err)
}

// TestWaitRolloutSkipsValidationWithoutRefresh pins the safety condition: without a
// refresh the app status and the desired state both come from ArgoCD's last
// reconciliation, which may predate the commit that introduces the image, so the check
// must not run and the rollout must keep polling.
func TestWaitRolloutSkipsValidationWithoutRefresh(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := models.Task{
		Id:      "task-id",
		App:     "demo",
		Timeout: 3,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/typo", Tag: "v1"}},
	}

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	api.EXPECT().GetApplication(gomock.Any(), task.App, false).Return(settledApp(), nil).MinTimes(1)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)

	// No GetManagedResources expectation: any call is a failure.
	_, _, err := monitor.WaitRollout(task, neverLost)
	require.NoError(t, err)
}

// TestWaitForRolloutCountsImageNotPartOfAppAsFailed drives issue #519's fail-fast through
// the whole rollout: it is the one terminal branch whose outcome nothing else exercises
// end to end, and the counter reads the status the branch leaves behind.
func TestWaitForRolloutCountsImageNotPartOfAppAsFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	task := models.Task{
		Id:      "task-id",
		App:     "demo",
		Timeout: 600,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/typo", Tag: "v1"}},
	}

	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(settledApp(), nil).MinTimes(1)
	api.EXPECT().GetManagedResources(gomock.Any(), task.App).Return(
		managedResources(deploymentWith("ghcr.io/shini4i/app:v1")), nil).Times(1)

	metrics := refreshMetrics(ctrl)
	state := notSupersededState(ctrl)
	argo := &Argo{}
	argo.Init(state, api, metrics)

	cfg := newUpdaterTestConfig(lock.NewInMemoryLocker())
	cfg.WebhookConfig = nil
	cfg.RefreshApp = true
	updater := initTestUpdater(t, cfg, argo)

	var capturedReason string
	metrics.EXPECT().AddInProgressTask()
	metrics.EXPECT().RemoveInProgressTask()
	metrics.EXPECT().AddFailedDeployment(task.App)
	metrics.EXPECT().InitDeploymentOutcomes(task.App)
	metrics.EXPECT().AddDeploymentOutcome(task.App, models.StatusFailedMessage).Times(1)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		DoAndReturn(func(_ string, _ string, reason string) error {
			capturedReason = reason
			return nil
		})

	updater.WaitForRollout(task, false)

	assert.Contains(t, capturedReason, "is not part of application")
}

func TestHandleImageNotPartOfApp(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := newTaskRepositoryMock(ctrl)

	monitor := NewDeploymentMonitor(Argo{metrics: metrics, State: state}, "", nil, false, time.Millisecond)
	task := models.Task{Id: "task-id", App: "demo"}

	var capturedReason string
	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		DoAndReturn(func(_ string, _ string, reason string) error {
			capturedReason = reason
			return nil
		})

	monitor.HandleImageNotPartOfApp(&task, &ImageNotPartOfAppError{
		App:           "demo",
		Image:         "ghcr.io/shini4i/typo",
		DesiredImages: []string{"ghcr.io/shini4i/app"},
	})

	assert.Equal(t, models.StatusFailedMessage, task.Status)
	assert.Contains(t, capturedReason, `Image "ghcr.io/shini4i/typo" is not part of application "demo"`)
	assert.Contains(t, capturedReason, "List of images defined in the application:")
}

func TestImageNotPartOfAppErrorReason(t *testing.T) {
	err := &ImageNotPartOfAppError{
		App:           "demo",
		Image:         "ghcr.io/shini4i/typo",
		DesiredImages: []string{"ghcr.io/shini4i/app", "ghcr.io/shini4i/cleanup"},
	}

	assert.Equal(t, `image "ghcr.io/shini4i/typo" is not part of application "demo"`, err.Error())

	reason := err.Reason()
	assert.Contains(t, reason, `Image "ghcr.io/shini4i/typo" is not part of application "demo"`)
	assert.Contains(t, reason, "\tghcr.io/shini4i/app\n\tghcr.io/shini4i/cleanup")
}
