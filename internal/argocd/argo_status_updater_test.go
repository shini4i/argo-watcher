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
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/lock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
	"go.uber.org/mock/gomock"
)

var (
	mockWebhookConfig = &config.WebhookConfig{
		Enabled: false,
	}
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

func zeroDelay(_ uint, _ error, _ *retry.Config) time.Duration {
	return 0
}

func newUpdaterTestConfig(locker lock.Locker) ArgoStatusUpdaterConfig {
	return ArgoStatusUpdaterConfig{
		RetryAttempts:    1,
		RetryDelay:       ArgoSyncRetryDelay,
		RegistryProxyURL: "test-registry",
		RepoCachePath:    "/tmp",
		AcceptSuspended:  false,
		WebhookConfig:    mockWebhookConfig,
		Locker:           locker,
	}
}

func initTestUpdater(t *testing.T, cfg ArgoStatusUpdaterConfig, argo *Argo) *ArgoStatusUpdater {
	t.Helper()
	updater := &ArgoStatusUpdater{}
	require.NoError(t, updater.Init(*argo, cfg))
	require.Equal(t, cfg.RetryAttempts, updater.monitor.defaultAttempts)
	updater.monitor.retryOptions = []retry.Option{
		retry.DelayType(zeroDelay),
		retry.LastErrorOnly(true),
	}
	updater.monitor.retryDelay = cfg.RetryDelay
	return updater
}

func TestArgoStatusUpdaterCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Use a real in-memory locker for testing the updater's logic,
	// as its behavior is simple and predictable.
	mockLocker := lock.NewInMemoryLocker()

	t.Run("Status Updater - Application deployed", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		cfg := newUpdaterTestConfig(mockLocker)
		cfg.RepoCachePath = "/tmp/"
		updater := initTestUpdater(t, cfg, argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Healthy"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).MinTimes(2).MaxTimes(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed with Retry", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		cfg := newUpdaterTestConfig(mockLocker)
		cfg.RetryAttempts = 3
		updater := initTestUpdater(t, cfg, argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// unhealthyApp
		unhealthyApp := models.Application{}
		unhealthyApp.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
		unhealthyApp.Status.Sync.Status = "Synced"
		unhealthyApp.Status.Health.Status = "NotHealthy"

		// healthy app
		healthyApp := models.Application{}
		healthyApp.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
		healthyApp.Status.Sync.Status = "Synced"
		healthyApp.Status.Health.Status = "Healthy"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&unhealthyApp, nil).MinTimes(1).MaxTimes(2)
		apiMock.EXPECT().GetApplication(task.App).Return(&healthyApp, nil).Times(1)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed with Registry proxy", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"test-registry/ghcr.io/shini4i/argo-watcher:dev"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Healthy"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).MinTimes(2).MaxTimes(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed without Registry proxy", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		cfg := newUpdaterTestConfig(mockLocker)
		cfg.RegistryProxyURL = ""
		updater := initTestUpdater(t, cfg, argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"test-registry/ghcr.io/shini4i/argo-watcher:dev"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "Healthy"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: force retry")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not found", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf("applications.argoproj.io \"test-app\" not found"))
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, "ArgoCD API Error: applications.argoproj.io \"test-app\" not found")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - ArgoCD unavailable", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf(argoUnavailableErrorMessage))
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, "ArgoCD API Error: connect: connection refused")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application API error", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf("unexpected failure"))
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: unexpected failure")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not available", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"test-image:v0.0.1"}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: force retry")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application out of Sync", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
		application.Status.Sync.Status = "Syncing"
		application.Status.Health.Status = "Healthy"
		application.Status.OperationState.Phase = "NotWorking"
		application.Status.OperationState.Message = "Not working test app"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: force retry")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not healthy", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:      "test-id",
			App:     "test-app",
			Timeout: 15,
			Images: []models.Image{
				{
					Image: "ghcr.io/shini4i/argo-watcher",
					Tag:   "dev",
				},
			},
		}

		// application
		application := models.Application{}
		application.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
		application.Status.Sync.Status = "Synced"
		application.Status.Health.Status = "NotHealthy"

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: force retry")

		// run the rollout
		updater.WaitForRollout(task)
	})
}

func TestDeploymentMonitorStoreInitialAppStatusRequiresApplication(t *testing.T) {
	monitor := NewDeploymentMonitor(Argo{}, "", nil, false, time.Millisecond)
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
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

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
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

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
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

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
	locker := lock.NewInMemoryLocker()

	t.Run("handlesNilConfig", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		require.NoError(t, updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts: 1,
			RetryDelay:    time.Second,
			Locker:        locker,
		}))
	})

	t.Run("returnsErrorOnWebhookSetupFailure", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		cfg := &config.WebhookConfig{
			Enabled: true,
			Url:     "http://example.com",
		}

		err := updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts: 1,
			RetryDelay:    time.Second,
			WebhookConfig: cfg,
			Locker:        locker,
		})
		assert.Error(t, err)
	})

	t.Run("configuresNotifierWhenWebhookEnabled", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		cfg := &config.WebhookConfig{
			Enabled:              true,
			Url:                  "http://example.com",
			ContentType:          "application/json",
			AuthorizationHeader:  "Authorization",
			Token:                "token",
			AllowedResponseCodes: []int{http.StatusOK},
			Format:               `{"app":"{{.App}}"}`,
		}

		err := updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts: 1,
			RetryDelay:    time.Second,
			WebhookConfig: cfg,
			Locker:        locker,
		})
		require.NoError(t, err)
		require.NotNil(t, updater.notifier)
	})

	t.Run("returnsErrorWhenLockerMissing", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		err := updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts: 1,
			RetryDelay:    time.Second,
		})
		assert.EqualError(t, err, "locker cannot be nil")
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

	cfg := newUpdaterTestConfig(locker)
	cfg.RegistryProxyURL = ""
	cfg.RepoCachePath = "/tmp/cache"
	cfg.WebhookConfig = nil
	updater := initTestUpdater(t, cfg, argo)

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
	monitor := NewDeploymentMonitor(
		Argo{api: api},
		"",
		[]retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
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

func TestArgoStatusUpdater_processDeploymentResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mock.NewMockArgoApiInterface(ctrl)
	metricsMock := mock.NewMockMetricsInterface(ctrl)
	stateMock := mock.NewMockTaskRepository(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

	// success scenario
	t.Run("processDeploymentResult - success", func(t *testing.T) {
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
			Images: []models.Image{
				{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"},
			},
		}
		app := &models.Application{}
		app.Status.Summary.Images = []string{"test-registry/ghcr.io/shini4i/argo-watcher:dev"}
		app.Status.Sync.Status = "Synced"
		app.Status.Health.Status = "Healthy"

		// setup status mocks
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		updater.monitor.ProcessDeploymentResult(&task, app)
		assert.Equal(t, models.StatusDeployedMessage, task.Status)
	})

	// fire and forget scenario
	t.Run("processDeploymentResult - fire and forget", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/fire-and-forget": "true"}
		app.Status.Sync.Status = "Synced"
		app.Status.Health.Status = "Healthy"

		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		updater.monitor.ProcessDeploymentResult(&task, app)
		assert.Equal(t, models.StatusDeployedMessage, task.Status)
	})

	t.Run("processDeploymentResult - fire and forget overrides failure", func(t *testing.T) {
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
			Images: []models.Image{
				{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"},
			},
		}
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/fire-and-forget": "true"}
		app.Status.Summary.Images = []string{"another-registry/image:v1"}

		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		updater.monitor.ProcessDeploymentResult(&task, app)
		assert.Equal(t, models.StatusDeployedMessage, task.Status)
	})

	// failure scenario
	t.Run("processDeploymentResult - failure", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		app := &models.Application{}
		// forcing failure by ignoring 'test-registry' mismatch or any condition
		app.Status.Summary.Images = []string{"another-registry/ghcr.io/shini4i/argo-watcher:dev"}

		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(gomock.Any(), models.StatusFailedMessage, gomock.Any())

		updater.monitor.ProcessDeploymentResult(&task, app)
		assert.Equal(t, models.StatusFailedMessage, task.Status)
	})
}

func TestArgoStatusUpdater_handleArgoAPIFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mock.NewMockArgoApiInterface(ctrl)
	metricsMock := mock.NewMockMetricsInterface(ctrl)
	stateMock := mock.NewMockTaskRepository(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

	t.Run("handleArgoAPIFailure - generic error", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		err := fmt.Errorf("some generic error")

		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any())

		updater.monitor.HandleArgoAPIFailure(task, err)
	})
}

func TestDeploymentMonitor_configureRetryOptions(t *testing.T) {
	countAttempts := func(options []retry.Option) int {
		attempts := 0
		options = append(options, retry.Delay(0))
		_ = retry.Do(func() error {
			attempts++
			return fmt.Errorf("retry")
		}, options...)
		return attempts
	}

	testCases := []struct {
		name             string
		timeout          int
		retryDelay       time.Duration
		expectedAttempts int
	}{
		{
			name:             "nonPositiveTimeoutUsesMinimumAttempts",
			timeout:          0,
			retryDelay:       ArgoSyncRetryDelay,
			expectedAttempts: 15,
		},
		{
			name:             "negativeTimeoutUsesMinimumAttempts",
			timeout:          -5,
			retryDelay:       ArgoSyncRetryDelay,
			expectedAttempts: 15,
		},
		{
			name:             "positiveTimeoutLessThanWindow",
			timeout:          10,
			retryDelay:       ArgoSyncRetryDelay,
			expectedAttempts: 1,
		},
		{
			name:             "positiveTimeoutExactMultiple",
			timeout:          30,
			retryDelay:       ArgoSyncRetryDelay,
			expectedAttempts: 3,
		},
		{
			name:             "positiveTimeoutWithRemainderRoundsUp",
			timeout:          46,
			retryDelay:       ArgoSyncRetryDelay,
			expectedAttempts: 4,
		},
		{
			name:             "positiveTimeoutCustomRetryDelay",
			timeout:          25,
			retryDelay:       5 * time.Second,
			expectedAttempts: 6,
		},
		{
			name:             "zeroRetryDelayFallsBackToDefaultWindow",
			timeout:          30,
			retryDelay:       0,
			expectedAttempts: 3,
		},
		{
			name:             "negativeRetryDelayFallsBackToDefaultWindow",
			timeout:          30,
			retryDelay:       -1 * time.Second,
			expectedAttempts: 3,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			monitor := NewDeploymentMonitor(
				Argo{},
				"",
				[]retry.Option{
					retry.DelayType(retry.FixedDelay),
					retry.Delay(0),
				},
				false,
				tc.retryDelay,
			)
			options := monitor.configureRetryOptions(models.Task{Id: "test-id", Timeout: tc.timeout})
			attempts := countAttempts(options)
			assert.Equal(t, tc.expectedAttempts, attempts)
		})
	}
}
