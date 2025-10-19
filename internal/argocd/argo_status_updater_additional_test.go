package argocd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type spyLocker struct {
	called bool
	err    error
}

func (s *spyLocker) WithLock(key string, f func() error) error {
	s.called = true
	if s.err != nil {
		return s.err
	}
	return f()
}

type failingStrategy struct {
	err error
}

func (s failingStrategy) Send(models.Task) error {
	return s.err
}

func TestDeploymentMonitorStoreInitialAppStatusRequiresApplication(t *testing.T) {
	monitor := NewDeploymentMonitor(Argo{}, "", nil, false)
	err := monitor.StoreInitialAppStatus(&models.Task{}, nil)
	require.Error(t, err)
	assert.Equal(t, "application is nil", err.Error())
}

func TestDeploymentMonitorHandleDeploymentSuccessHandlesStateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mock.NewMockMetricsInterface(ctrl)
	state := mock.NewMockTaskRepository(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
	}, "", nil, false)

	task := models.Task{Id: "task-id", App: "demo"}

	metrics.EXPECT().ResetFailedDeployment(task.App)
	state.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "").Return(errors.New("update failed"))

	monitor.handleDeploymentSuccess(&task)
	assert.Equal(t, models.StatusDeployedMessage, task.Status)
}

func TestDeploymentMonitorHandleDeploymentFailureHandlesStateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mock.NewMockMetricsInterface(ctrl)
	state := mock.NewMockTaskRepository(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
	}, "", nil, false)

	task := models.Task{
		Id:  "task-id",
		App: "demo",
		Images: []models.Image{
			{Image: "example.com/app", Tag: "v1"},
		},
	}
	application := &models.Application{}
	application.Status.Summary.Images = []string{"example.com/app:v1"}

	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		Return(errors.New("update failed"))

	monitor.handleDeploymentFailure(&task, models.ArgoRolloutAppNotHealthy, application)
	assert.Equal(t, models.StatusFailedMessage, task.Status)
}

func TestDeploymentMonitorHandleArgoAPIFailureHandlesStateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mock.NewMockMetricsInterface(ctrl)
	state := mock.NewMockTaskRepository(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
	}, "", nil, false)

	task := models.Task{Id: "task-id", App: "demo"}

	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		Return(errors.New("persist failed"))

	monitor.HandleArgoAPIFailure(task, fmt.Errorf("boom"))
}

func TestGitUpdaterUpdateIfNeeded(t *testing.T) {
	makeApp := func(managed bool) *models.Application {
		app := &models.Application{}
		if managed {
			app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}
		}
		app.Spec.Source.RepoURL = "git@example.com/repo.git"
		app.Spec.Source.TargetRevision = "main"
		app.Spec.Source.Path = "path"
		return app
	}

	validTask := models.Task{
		Id:        "task-id",
		App:       "demo",
		Validated: true,
		Images: []models.Image{
			{Image: "example.com/app", Tag: "v1"},
		},
	}

	t.Run("skipsWhenAppNotManaged", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache")
		err := updater.UpdateIfNeeded(makeApp(false), validTask)
		assert.NoError(t, err)
		assert.False(t, locker.called)
	})

	t.Run("skipsWhenTaskNotValidated", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache")
		task := validTask
		task.Validated = false
		err := updater.UpdateIfNeeded(makeApp(true), task)
		assert.NoError(t, err)
		assert.False(t, locker.called)
	})

	t.Run("failsWhenRepoInvalid", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache")
		app := makeApp(true)
		app.Spec.Source.RepoURL = ""

		err := updater.UpdateIfNeeded(app, validTask)
		assert.Error(t, err)
		assert.False(t, locker.called)
	})

	t.Run("returnsLockerError", func(t *testing.T) {
		locker := &spyLocker{err: errors.New("lock failed")}
		updater := NewGitUpdater(locker, "/tmp/cache")

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.EqualError(t, err, "lock failed")
		assert.True(t, locker.called)
	})

	t.Run("propagatesUpdateError", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache")
		app := makeApp(true)
		app.Metadata.Annotations["argo-watcher/managed-images"] = "broken"

		err := updater.UpdateIfNeeded(app, validTask)
		assert.Error(t, err)
		assert.True(t, locker.called)
	})

	t.Run("succeedsWhenNoUpdateNeeded", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache")

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.NoError(t, err)
		assert.True(t, locker.called)
	})
}

func TestGitUpdaterUpdateGitRepo(t *testing.T) {
	task := models.Task{
		Id: "task-id",
		Images: []models.Image{
			{Image: "example.com/app", Tag: "v1"},
		},
	}
	repo := &models.GitopsRepo{Path: "some/path"}

	t.Run("returnsNilWhenNoOverrides", func(t *testing.T) {
		updater := NewGitUpdater(lock.NewInMemoryLocker(), "/tmp/cache")
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}

		err := updater.updateGitRepo(app, &task, repo)
		assert.NoError(t, err)
	})

	t.Run("propagatesOverrideError", func(t *testing.T) {
		updater := NewGitUpdater(lock.NewInMemoryLocker(), "/tmp/cache")
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{
			"argo-watcher/managed":        "true",
			"argo-watcher/managed-images": "invalid",
		}

		err := updater.updateGitRepo(app, &task, repo)
		assert.Error(t, err)
	})
}

func TestArgoStatusUpdaterInitWebhook(t *testing.T) {
	updater := &ArgoStatusUpdater{}
	locker := lock.NewInMemoryLocker()

	t.Run("handlesNilConfig", func(t *testing.T) {
		require.NoError(t, updater.Init(Argo{}, 1, time.Second, "", "", false, nil, locker))
	})

	t.Run("returnsErrorOnWebhookSetupFailure", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Enabled: true,
			Url:     "http://example.com",
		}

		err := updater.Init(Argo{}, 1, time.Second, "", "", false, cfg, locker)
		assert.Error(t, err)
	})

	t.Run("configuresNotifierWhenWebhookEnabled", func(t *testing.T) {
		cfg := &config.WebhookConfig{
			Enabled:              true,
			Url:                  "http://example.com",
			ContentType:          "application/json",
			AuthorizationHeader:  "Authorization",
			Token:                "token",
			AllowedResponseCodes: []int{http.StatusOK},
			Format:               `{"app":"{{.App}}"}`,
		}

		err := updater.Init(Argo{}, 1, time.Second, "", "", false, cfg, locker)
		require.NoError(t, err)
		require.NotNil(t, updater.notifier)
	})
}

func TestArgoStatusUpdaterWaitForApplicationDeploymentErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mock.NewMockArgoApiInterface(ctrl)
	metrics := mock.NewMockMetricsInterface(ctrl)
	state := mock.NewMockTaskRepository(ctrl)
	locker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(state, api, metrics)

	updater := &ArgoStatusUpdater{}
	require.NoError(t, updater.Init(*argo, 1, 0, "", "/tmp/cache", false, nil, locker))

	task := models.Task{App: "demo", Validated: true}

	t.Run("failsWhenFetchFails", func(t *testing.T) {
		api.EXPECT().GetApplication(task.App).Return(nil, errors.New("network")).Times(1)
		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})

	t.Run("failsWhenApplicationNil", func(t *testing.T) {
		api.EXPECT().GetApplication(task.App).Return(nil, nil).Times(1)
		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})

	t.Run("failsWhenGitUpdateFails", func(t *testing.T) {
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}
		app.Spec.Source.RepoURL = ""
		api.EXPECT().GetApplication(task.App).Return(app, nil).Times(1)

		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})
}

func TestDeploymentMonitorWaitRollout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := mock.NewMockArgoApiInterface(ctrl)
	monitor := NewDeploymentMonitor(Argo{api: api}, "", []retry.Option{retry.Attempts(1)}, false)
	task := models.Task{App: "demo"}

	t.Run("handlesFireAndForget", func(t *testing.T) {
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/fire-and-forget": "true"}
		api.EXPECT().GetApplication(task.App).Return(app, nil).Times(1)

		received, err := monitor.WaitRollout(task)
		require.NoError(t, err)
		assert.Equal(t, app, received)
	})

	t.Run("wrapsApplicationFetchErrors", func(t *testing.T) {
		errNotFound := fmt.Errorf("applications.argoproj.io %q not found", task.App)
		api.EXPECT().GetApplication(task.App).Return(nil, errNotFound).Times(1)

		_, err := monitor.WaitRollout(task)
		require.Error(t, err)
		assert.Contains(t, err.Error(), errNotFound.Error())
	})
}

func TestHandleApplicationFetchError(t *testing.T) {
	task := models.Task{App: "demo"}
	t.Run("returnsUnrecoverableForNotFound", func(t *testing.T) {
		err := handleApplicationFetchError(task, fmt.Errorf("applications.argoproj.io %q not found", task.App))
		assert.False(t, retry.IsRecoverable(err))
	})

	t.Run("returnsOriginalErrorForOthers", func(t *testing.T) {
		errOrig := errors.New("boom")
		err := handleApplicationFetchError(task, errOrig)
		assert.True(t, retry.IsRecoverable(err))
	})
}

func TestCheckRolloutStatus(t *testing.T) {
	task := models.Task{
		Id: "task-id",
		Images: []models.Image{
			{Image: "example.com/app", Tag: "v1"},
		},
		SavedAppStatus: models.SavedAppStatus{
			ImagesHash: []byte("hash"),
		},
	}

	app := &models.Application{}
	app.Status.Summary.Images = []string{"example.com/app:v1"}
	app.Status.Sync.Status = "Synced"
	app.Status.Health.Status = "Healthy"

	t.Run("returnsNilOnSuccess", func(t *testing.T) {
		err := checkRolloutStatus(task, app, "", false)
		assert.NoError(t, err)
	})

	t.Run("returnsForceRetryOnPending", func(t *testing.T) {
		pending := *app
		pending.Status.Health.Status = "Progressing"
		err := checkRolloutStatus(task, &pending, "", false)
		require.Error(t, err)
		assert.True(t, retry.IsRecoverable(err))
	})

	t.Run("returnsUnrecoverableWhenDegradedWithDifferentHash", func(t *testing.T) {
		degraded := *app
		degraded.Status.Health.Status = "Degraded"
		degraded.Status.Sync.Status = "Synced"
		degraded.Status.Summary.Images = []string{"example.com/app:v1", "example.com/app:v2"}
		taskCopy := task
		taskCopy.SavedAppStatus.ImagesHash = helpers.GenerateHash("example.com/app:v1")

		err := checkRolloutStatus(taskCopy, &degraded, "", false)
		require.Error(t, err)
		assert.False(t, retry.IsRecoverable(err))
	})

	t.Run("returnsForceRetryWhenDegradedWithSameHash", func(t *testing.T) {
		degraded := *app
		degraded.Status.Health.Status = "Degraded"
		degraded.Status.Sync.Status = "Synced"
		degraded.Status.Summary.Images = []string{"example.com/app:v1", "example.com/app:v2"}
		hash := helpers.GenerateHash(strings.Join(degraded.Status.Summary.Images, ","))
		taskCopy := task
		taskCopy.SavedAppStatus.ImagesHash = hash

		err := checkRolloutStatus(taskCopy, &degraded, "", false)
		require.Error(t, err)
		assert.True(t, retry.IsRecoverable(err))
	})
}

func TestSendNotification(t *testing.T) {
	task := models.Task{Id: "task-id"}

	t.Run("returnsImmediatelyWhenNotifierNil", func(t *testing.T) {
		sendNotification(task, nil)
	})

	t.Run("logsErrorsWhenStrategyFails", func(t *testing.T) {
		notifier := notifications.NewNotifier(failingStrategy{err: errors.New("boom")})
		sendNotification(task, notifier)
	})
}
