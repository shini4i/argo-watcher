package argocd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/helpers"
	"github.com/shini4i/argo-watcher/internal/lock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/notifications"
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

// capturingStrategy records every task passed to a notifier so tests can assert
// on the status reported in the outgoing notification.
type capturingStrategy struct {
	sent []models.Task
}

func (s *capturingStrategy) Send(task models.Task) error {
	s.sent = append(s.sent, task)
	return nil
}

func zeroDelay(_ uint, _ error, _ *retry.Config) time.Duration {
	return 0
}

// newArgoApiMock builds an ArgoApi mock pre-loaded with the best-effort defaults every test
// tolerates. The failure-path resource-tree fetch is best-effort, so it defaults to "no tree"
// (GetRolloutMessage then falls back to the app's top-level resources). Only tests that assert
// on tree-derived diagnostics register their own GetResourceTree expectation (using a raw mock).
//
// Register any future best-effort (optional) ArgoApi call here so adding one never has to touch
// every test setup again — the whole point of routing mock creation through this constructor.
func newArgoApiMock(ctrl *gomock.Controller) *mocks.MockArgoApiInterface {
	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	return api
}

// notSupersededState returns a TaskRepository mock whose GetTask always reports an
// in-progress task, so the poll loop's supersession check never fires. Use it when
// a test exercises rollout polling but is not about cancellation.
func notSupersededState(ctrl *gomock.Controller) *mocks.MockTaskRepository {
	state := mocks.NewMockTaskRepository(ctrl)
	state.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()
	return state
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
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).MinTimes(2).MaxTimes(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().ObserveDeploymentDuration(task.App, gomock.Any())
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed with Retry", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&unhealthyApp, nil).MinTimes(1).MaxTimes(2)
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&healthyApp, nil).Times(1)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().ObserveDeploymentDuration(task.App, gomock.Any())
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed with Registry proxy", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).MinTimes(2).MaxTimes(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		metricsMock.EXPECT().ObserveDeploymentDuration(task.App, gomock.Any())
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application deployed without Registry proxy", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status is not available\n\n"+
				"List of current images (last app check):\n"+
				"\ttest-registry/ghcr.io/shini4i/argo-watcher:dev\n\n"+
				"List of expected images:\n"+
				"\tghcr.io/shini4i/argo-watcher:dev")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not found", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, fmt.Errorf("applications.argoproj.io \"test-app\" not found"))
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, "ArgoCD API Error: applications.argoproj.io \"test-app\" not found")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - ArgoCD unavailable", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		unavailableErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, unavailableErr)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, "ArgoCD API Error: dial tcp: connect: connection refused")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application API error", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

		// argo updater
		updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, fmt.Errorf("unexpected failure"))
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: unexpected failure")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not available", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status is not available\n\n"+
				"List of current images (last app check):\n"+
				"\ttest-image:v0.0.1\n\n"+
				"List of expected images:\n"+
				"\tghcr.io/shini4i/argo-watcher:dev")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application out of Sync", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status is not synced\n\n"+
				"App status \"NotWorking\"\n"+
				"App message \"Not working test app\"\n"+
				"Resources:\n"+
				"\t")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not healthy", func(t *testing.T) {
		// mocks
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).Times(3)
		metricsMock.EXPECT().AddInProgressTask()
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		metricsMock.EXPECT().RemoveInProgressTask()
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status is not healthy\n\n"+
				"App sync status \"Synced\"\n"+
				"App health status \"NotHealthy\"\n"+
				"Resources:\n"+
				"\t")

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

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)

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

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)
	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
		api:     api,
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

// TestDeploymentMonitorHandleDeploymentFailureEnrichesReasonFromResourceTree pins the wiring:
// the failure path fetches the live resource tree and the pod-level cause it carries lands in the
// stored status reason. Uses a raw mock (not newArgoApiMock) so its GetResourceTree return is not
// shadowed by the best-effort nil default.
func TestDeploymentMonitorHandleDeploymentFailureEnrichesReasonFromResourceTree(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)
	api := mocks.NewMockArgoApiInterface(ctrl)

	podNode := models.ApplicationTreeNode{Kind: "Pod", Name: "app-xyz", Namespace: "demo"}
	podNode.Health.Status = "Degraded"
	podNode.Health.Message = `Back-off pulling image "example.com/app:v2": ErrImagePull`
	api.EXPECT().GetResourceTree(gomock.Any(), "demo").Return(&models.ApplicationTree{Nodes: []models.ApplicationTreeNode{podNode}}, nil)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
		api:     api,
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

	task := models.Task{
		Id:     "task-id",
		App:    "demo",
		Images: []models.Image{{Image: "example.com/app", Tag: "v2"}},
	}
	application := &models.Application{}
	application.Status.Summary.Images = []string{"example.com/app:v1"}

	var capturedReason string
	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		DoAndReturn(func(_ string, _ string, reason string) error {
			capturedReason = reason
			return nil
		})

	monitor.handleDeploymentFailure(&task, models.ArgoRolloutAppNotAvailable, application)

	assert.Contains(t, capturedReason, "Unhealthy resources:")
	assert.Contains(t, capturedReason, `Pod(app-xyz) Degraded with message Back-off pulling image "example.com/app:v2": ErrImagePull`)
}

// TestDeploymentMonitorHandleDeploymentFailureResourceTreeErrorIsNonFatal pins the best-effort
// contract: if the resource-tree fetch errors, the deployment is still marked failed and the
// reason falls back to the baseline message (no panic on the nil tree). A regression that made
// the fetch fatal — propagating the error and skipping SetTaskStatus — would fail this test.
func TestDeploymentMonitorHandleDeploymentFailureResourceTreeErrorIsNonFatal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)
	api := mocks.NewMockArgoApiInterface(ctrl)
	api.EXPECT().GetResourceTree(gomock.Any(), "demo").Return(nil, errors.New("resource-tree unavailable"))

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
		api:     api,
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

	task := models.Task{
		Id:     "task-id",
		App:    "demo",
		Images: []models.Image{{Image: "example.com/app", Tag: "v2"}},
	}
	application := &models.Application{}
	application.Status.Summary.Images = []string{"example.com/app:v1"}

	var capturedReason string
	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		DoAndReturn(func(_ string, _ string, reason string) error {
			capturedReason = reason
			return nil
		})

	monitor.handleDeploymentFailure(&task, models.ArgoRolloutAppNotAvailable, application)

	assert.Equal(t, models.StatusFailedMessage, task.Status)
	assert.Contains(t, capturedReason, "Rollout status is not available")
	assert.Contains(t, capturedReason, "List of expected images:")
}

func TestDeploymentMonitorHandleArgoAPIFailureHandlesStateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

	task := models.Task{Id: "task-id", App: "demo"}

	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().
		SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any()).
		Return(errors.New("persist failed"))

	monitor.HandleArgoAPIFailure(&task, fmt.Errorf("boom"))
}

// TestDeploymentMonitorHandleArgoAPIFailureAbortCountsAsFailure verifies that an
// unreachable-ArgoCD error is stored as aborted and still counted in the failure
// metric, so the failed deployment stays visible regardless of cause.
func TestDeploymentMonitorHandleArgoAPIFailureAbortCountsAsFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)

	monitor := NewDeploymentMonitor(Argo{
		metrics: metrics,
		State:   state,
	}, "", []retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)}, false, time.Millisecond)

	task := models.Task{Id: "task-id", App: "demo"}

	metrics.EXPECT().AddFailedDeployment(task.App)
	state.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, gomock.Any())

	monitor.HandleArgoAPIFailure(&task, context.DeadlineExceeded)
	assert.Equal(t, models.StatusAborted, task.Status)
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
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)
		err := updater.UpdateIfNeeded(makeApp(false), validTask)
		assert.NoError(t, err)
		assert.False(t, locker.called)
	})

	t.Run("skipsWhenTaskNotValidated", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)
		task := validTask
		task.Validated = false
		err := updater.UpdateIfNeeded(makeApp(true), task)
		assert.NoError(t, err)
		assert.False(t, locker.called)
	})

	t.Run("failsWhenRepoInvalid", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)
		app := makeApp(true)
		app.Spec.Source.RepoURL = ""

		err := updater.UpdateIfNeeded(app, validTask)
		assert.Error(t, err)
		assert.False(t, locker.called)
	})

	t.Run("returnsLockerError", func(t *testing.T) {
		locker := &spyLocker{err: errors.New("lock failed")}
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.EqualError(t, err, "lock failed")
		assert.True(t, locker.called)
	})

	t.Run("propagatesUpdateError", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)
		app := makeApp(true)
		app.Metadata.Annotations["argo-watcher/managed-images"] = "broken"

		err := updater.UpdateIfNeeded(app, validTask)
		assert.Error(t, err)
		assert.True(t, locker.called)
	})

	t.Run("succeedsWhenNoUpdateNeeded", func(t *testing.T) {
		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", nil, nil)

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.NoError(t, err)
		assert.True(t, locker.called)
	})

	t.Run("observesLockWaitAndWritebackDurations", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		metrics := mocks.NewMockMetricsInterface(ctrl)
		metrics.EXPECT().ObserveGitLockWaitDuration(validTask.App, gomock.Any()).Times(1)
		metrics.EXPECT().ObserveGitWritebackDuration(validTask.App, gomock.Any()).Times(1)

		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", metrics, nil)

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.NoError(t, err)
		assert.True(t, locker.called)
	})

	t.Run("skipsMetricsWhenLockNeverAcquired", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		// Lock acquisition fails before the closure runs, so neither duration is recorded.
		metrics := mocks.NewMockMetricsInterface(ctrl)

		locker := &spyLocker{err: errors.New("lock failed")}
		updater := NewGitUpdater(locker, "/tmp/cache", metrics, nil)

		err := updater.UpdateIfNeeded(makeApp(true), validTask)
		assert.EqualError(t, err, "lock failed")
	})

	t.Run("observesDurationsEvenWhenWritebackFails", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		// The write-back timer is deferred so a failed (and typically slow, retried)
		// write-back — the tail-latency case the histogram exists to surface — is still
		// measured once the lock is acquired.
		metrics := mocks.NewMockMetricsInterface(ctrl)
		metrics.EXPECT().ObserveGitLockWaitDuration(validTask.App, gomock.Any()).Times(1)
		metrics.EXPECT().ObserveGitWritebackDuration(validTask.App, gomock.Any()).Times(1)

		locker := &spyLocker{}
		updater := NewGitUpdater(locker, "/tmp/cache", metrics, nil)
		app := makeApp(true)
		app.Metadata.Annotations["argo-watcher/managed-images"] = "broken"

		err := updater.UpdateIfNeeded(app, validTask)
		assert.Error(t, err)
		assert.True(t, locker.called)
	})

	t.Run("routesThroughBatcherWhenEnabled", func(t *testing.T) {
		// With a batcher configured, UpdateIfNeeded must enqueue the request and
		// return that app's individual outcome — not take the per-repo lock itself.
		locker := &spyLocker{}
		batcher := NewBatcher(locker, "/tmp/cache", 20, nil)

		var captured *batchWriteRequest
		wantErr := errors.New("batched write-back failed")
		batcher.flushFn = func(batch []*batchWriteRequest) {
			for _, req := range batch {
				captured = req
				req.resultCh <- wantErr
			}
		}

		updater := NewGitUpdater(locker, "/tmp/cache", nil, batcher)
		err := updater.UpdateIfNeeded(makeApp(true), validTask)

		assert.ErrorIs(t, err, wantErr, "batch outcome must propagate to the caller")
		require.NotNil(t, captured, "request must reach the batcher")
		assert.Equal(t, validTask.Id, captured.task.Id)
		assert.False(t, locker.called, "batch mode must not take the per-repo lock directly")
	})

	t.Run("batcherSuccessReturnsNil", func(t *testing.T) {
		locker := &spyLocker{}
		batcher := NewBatcher(locker, "/tmp/cache", 20, nil)
		batcher.flushFn = func(batch []*batchWriteRequest) {
			for _, req := range batch {
				req.resultCh <- nil
			}
		}

		updater := NewGitUpdater(locker, "/tmp/cache", nil, batcher)
		assert.NoError(t, updater.UpdateIfNeeded(makeApp(true), validTask))
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
		updater := NewGitUpdater(lock.NewInMemoryLocker(), "/tmp/cache", nil, nil)
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}

		err := updater.updateGitRepo(app, &task, repo)
		assert.NoError(t, err)
	})

	t.Run("propagatesOverrideError", func(t *testing.T) {
		updater := NewGitUpdater(lock.NewInMemoryLocker(), "/tmp/cache", nil, nil)
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

	t.Run("configuresNotifierWhenMattermostEnabled", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		cfg := &config.MattermostConfig{
			Enabled:   true,
			Url:       "http://mattermost.example.com",
			Token:     "token",
			ChannelId: "channel123",
			Format:    `{{.App}}: {{.Status}}`,
		}

		err := updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts:    1,
			RetryDelay:       time.Second,
			MattermostConfig: cfg,
			Locker:           locker,
		})
		require.NoError(t, err)
		require.NotNil(t, updater.notifier)
	})

	t.Run("returnsErrorOnMattermostSetupFailure", func(t *testing.T) {
		updater := &ArgoStatusUpdater{}
		cfg := &config.MattermostConfig{
			Enabled: true,
			Url:     "http://mattermost.example.com",
		}

		err := updater.Init(Argo{}, ArgoStatusUpdaterConfig{
			RetryAttempts:    1,
			RetryDelay:       time.Second,
			MattermostConfig: cfg,
			Locker:           locker,
		})
		assert.Error(t, err)
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

	api := newArgoApiMock(ctrl)

	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)
	locker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(state, api, metrics)
	state.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

	cfg := newUpdaterTestConfig(locker)
	cfg.RegistryProxyURL = ""
	cfg.RepoCachePath = "/tmp/cache"
	cfg.WebhookConfig = nil
	updater := initTestUpdater(t, cfg, argo)

	task := models.Task{App: "demo", Validated: true}

	t.Run("failsWhenFetchFails", func(t *testing.T) {
		api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, errors.New("network")).Times(1)
		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})

	t.Run("failsWhenApplicationNil", func(t *testing.T) {
		api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, nil).Times(1)
		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})

	t.Run("failsWhenGitUpdateFails", func(t *testing.T) {
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/managed": "true"}
		app.Spec.Source.RepoURL = ""
		api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(app, nil).Times(1)

		_, err := updater.waitForApplicationDeployment(task)
		assert.Error(t, err)
	})
}

func TestDeploymentMonitorWaitRollout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(zeroDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	task := models.Task{App: "demo"}

	t.Run("handlesFireAndForget", func(t *testing.T) {
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"argo-watcher/fire-and-forget": "true"}
		api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(app, nil).Times(1)

		received, err := monitor.WaitRollout(task)
		require.NoError(t, err)
		assert.Equal(t, app, received)
	})

	t.Run("wrapsApplicationFetchErrors", func(t *testing.T) {
		errNotFound := fmt.Errorf("applications.argoproj.io %q not found", task.App)
		api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, errNotFound).Times(1)

		_, err := monitor.WaitRollout(task)
		require.Error(t, err)
		assert.Contains(t, err.Error(), errNotFound.Error())
	})
}

// TestDeploymentMonitorWaitRolloutRespectsDeadline verifies that WaitRollout stops at its
// wall-clock deadline instead of exhausting the full attempt budget when the ArgoCD API
// responds slowly. This is the regression guard for issue #304: with the attempt budget alone,
// slow polls would let a rollout run far past its configured timeout.
func TestDeploymentMonitorWaitRolloutRespectsDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)

	// Timeout 20 with a ~1s-rounded step yields 21 attempts and a 21ms deadline. Each poll sleeps
	// far longer than the deadline, so the loop must abort after the first poll, not after 21.
	task := models.Task{Id: "test-id", App: "demo", Timeout: 20}

	// A perpetually non-final application keeps checkRolloutStatus returning the force-retry sentinel.
	app := &models.Application{}
	app.Status.Sync.Status = "OutOfSync"
	app.Status.Health.Status = "Progressing"

	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, _ bool) (*models.Application, error) {
			time.Sleep(40 * time.Millisecond)
			return app, nil
		}).MinTimes(1).MaxTimes(3)

	start := time.Now()
	received, err := monitor.WaitRollout(task)
	elapsed := time.Since(start)

	require.NoError(t, err, "deadline expiry while polling must be swallowed so the caller reports the real status")
	assert.Equal(t, app, received)
	assert.Less(t, elapsed, 300*time.Millisecond, "loop must honor the wall-clock deadline, not run all 21 attempts")
}

// TestDeploymentMonitorWaitRolloutReportsLastGoodStatusOnDeadline verifies that when the deadline
// fires while a poll is in flight (the realistic slow-ArgoCD case for issue #304), WaitRollout still
// returns the last successfully-fetched application — so ProcessDeploymentResult reports the real
// rollout status rather than a raw "context deadline exceeded" error.
func TestDeploymentMonitorWaitRolloutReportsLastGoodStatusOnDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	task := models.Task{Id: "test-id", App: "demo", Timeout: 1}

	goodApp := &models.Application{}
	goodApp.Status.Sync.Status = "OutOfSync"
	goodApp.Status.Health.Status = "Progressing"

	firstDone := false
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ string, _ bool) (*models.Application, error) {
			if !firstDone {
				firstDone = true
				return goodApp, nil
			}
			// Subsequent poll is still in flight when the deadline fires: it is cancelled and
			// returns a nil application, exactly as the real HTTP layer would.
			<-ctx.Done()
			return nil, ctx.Err()
		}).MinTimes(2).MaxTimes(3)

	received, err := monitor.WaitRollout(task)
	require.NoError(t, err)
	assert.Equal(t, goodApp, received, "should report the last successfully-fetched application, not nil")
}

// TestDeploymentMonitorWaitRolloutSurfacesErrorWhenNoFetchSucceeds verifies that when ArgoCD is
// unreachable for the entire window, WaitRollout surfaces the underlying fetch error (not a swallowed
// nil) so the caller can classify it — e.g. "connect: connection refused" -> aborted.
func TestDeploymentMonitorWaitRolloutSurfacesErrorWhenNoFetchSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	task := models.Task{Id: "test-id", App: "demo", Timeout: 1}

	unavailableErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).
		Return(nil, unavailableErr).MinTimes(1)

	received, err := monitor.WaitRollout(task)
	require.Error(t, err)
	assert.Nil(t, received)
	assert.ErrorIs(t, err, unavailableErr,
		"the underlying cause must survive so determineFailureStatus can classify it")
}

// TestDeploymentMonitorWaitRolloutSurfacesArgoAPIError verifies that a 5xx
// *ArgoAPIError survives WaitRollout's error wrapping intact, so errors.As can
// still recover the status code downstream and classify the outage as aborted.
func TestDeploymentMonitorWaitRolloutSurfacesArgoAPIError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)

	monitor := NewDeploymentMonitor(
		Argo{api: api, State: notSupersededState(ctrl)},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	task := models.Task{Id: "test-id", App: "demo", Timeout: 1}

	apiErr := &ArgoAPIError{StatusCode: http.StatusServiceUnavailable, Message: "upstream unavailable"}
	api.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).
		Return(nil, apiErr).MinTimes(1)

	received, err := monitor.WaitRollout(task)
	require.Error(t, err)
	assert.Nil(t, received)

	var got *ArgoAPIError
	require.ErrorAs(t, err, &got, "the *ArgoAPIError must survive wrapping for downstream classification")
	assert.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
	assert.Equal(t, models.StatusAborted, determineFailureStatus(task, err))
}

// TestArgoStatusUpdaterStopsWhenSuperseded verifies that once a newer deployment
// has marked the task "cancelled" in the shared state, the poll loop stops before
// making any ArgoCD call and does not overwrite the cancelled status. Because the
// signal travels through the shared state, this is exactly how cross-replica
// supersession works in an HA setup.
func TestArgoStatusUpdaterStopsWhenSuperseded(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)
	capture := &capturingStrategy{}
	updater.notifier = notifications.NewNotifier(capture)

	task := models.Task{
		Id:      "old-id",
		App:     "test-app",
		Timeout: 15,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"}},
	}

	// The task is already cancelled in the shared state (a newer deployment, maybe
	// on another replica, set it). The poller must observe this and stop.
	stateMock.EXPECT().GetTask(task.Id).Return(&models.Task{Id: task.Id, Status: models.StatusCancelledMessage}, nil).AnyTimes()

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	// No ArgoCD call and no status write: the poller bails immediately, leaving the
	// "cancelled" status the newer deployment already wrote untouched. Any
	// GetApplication or SetTaskStatus call would be unexpected and fail the test.

	updater.WaitForRollout(task)

	// The final outgoing notification must report the cancelled status.
	require.NotEmpty(t, capture.sent)
	assert.Equal(t, models.StatusCancelledMessage, capture.sent[len(capture.sent)-1].Status)
}

// TestArgoStatusUpdaterStopsMidPollWhenSuperseded verifies that a task cancelled
// while a rollout is already polling is detected on the next poll iteration: the
// loop stops and the terminal status is not overwritten.
func TestArgoStatusUpdaterStopsMidPollWhenSuperseded(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)

	task := models.Task{
		Id:      "old-id",
		App:     "test-app",
		Timeout: 15,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"}},
	}

	// Not-final app so the loop would keep polling if not cancelled.
	application := models.Application{}
	application.Status.Summary.Images = []string{"test-registry/ghcr.io/shini4i/argo-watcher:dev"}
	application.Status.Sync.Status = "OutOfSync"
	application.Status.Health.Status = "Progressing"
	apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).AnyTimes()

	// First status read (initial supersession check) reports in-progress; the next
	// read — at the top of the first poll iteration — reports cancelled.
	gomock.InOrder(
		stateMock.EXPECT().GetTask(task.Id).Return(&models.Task{Id: task.Id, Status: models.StatusInProgressMessage}, nil),
		stateMock.EXPECT().GetTask(task.Id).Return(&models.Task{Id: task.Id, Status: models.StatusCancelledMessage}, nil).AnyTimes(),
	)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	// No SetTaskStatus and no failed-deployment metric: a superseded rollout is not
	// a failure and its status must not be overwritten.

	updater.WaitForRollout(task)
}

// TestArgoStatusUpdaterAppDisappearsMidRollout is the regression guard for issue #387:
// the application exists at the initial check, then disappears (is deleted from ArgoCD)
// while the rollout is still being polled. The rollout must not crash the process — it
// runs in a goroutine, so a panic here would take down the whole server — and must abort
// the task with a terminal "app not found" status instead of retrying indefinitely.
func TestArgoStatusUpdaterAppDisappearsMidRollout(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)

	task := models.Task{
		Id:      "test-id",
		App:     "test-app",
		Timeout: 15,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"}},
	}

	// Not-final app so the loop keeps polling past the initial check.
	inProgress := models.Application{}
	inProgress.Status.Summary.Images = []string{"ghcr.io/shini4i/argo-watcher:dev"}
	inProgress.Status.Sync.Status = "Synced"
	inProgress.Status.Health.Status = "Progressing"

	notFound := fmt.Errorf("applications.argoproj.io %q not found", task.App)

	gomock.InOrder(
		// Initial check and first poll succeed: the app is present but still progressing.
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&inProgress, nil).Times(2),
		// The app is then deleted mid-rollout; every subsequent fetch reports not found.
		apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(nil, notFound).MinTimes(1),
	)

	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().RemoveInProgressTask()
	metricsMock.EXPECT().AddFailedDeployment(task.App)
	stateMock.EXPECT().SetTaskStatus(
		task.Id,
		models.StatusAppNotFoundMessage,
		fmt.Sprintf(ArgoAPIErrorTemplate, notFound.Error()),
	)

	updater.WaitForRollout(task)
}

// TestArgoStatusUpdaterProceedsWhenStatusReadFails verifies that a transient
// failure to read the task status (the supersession check) does not abort an
// otherwise healthy rollout: taskSuperseded returns false on a read error, so the
// deployment proceeds to its normal terminal result.
func TestArgoStatusUpdaterProceedsWhenStatusReadFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := initTestUpdater(t, newUpdaterTestConfig(lock.NewInMemoryLocker()), argo)

	task := models.Task{
		Id:      "test-id",
		App:     "test-app",
		Timeout: 15,
		Images:  []models.Image{{Image: "ghcr.io/shini4i/argo-watcher", Tag: "dev"}},
	}

	application := models.Application{}
	application.Status.Summary.Images = []string{"test-registry/ghcr.io/shini4i/argo-watcher:dev"}
	application.Status.Sync.Status = "Synced"
	application.Status.Health.Status = "Healthy"

	// Every supersession check fails to read the status; the rollout must carry on.
	stateMock.EXPECT().GetTask(task.Id).Return(nil, errors.New("db unavailable")).AnyTimes()
	apiMock.EXPECT().GetApplication(gomock.Any(), task.App, gomock.Any()).Return(&application, nil).MinTimes(1)
	metricsMock.EXPECT().AddInProgressTask()
	metricsMock.EXPECT().ResetFailedDeployment(task.App)
	metricsMock.EXPECT().ObserveDeploymentDuration(task.App, gomock.Any())
	metricsMock.EXPECT().RemoveInProgressTask()
	// Proceeds to a normal deployed result rather than stopping as superseded.
	stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

	updater.WaitForRollout(task)
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

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

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

	apiMock := newArgoApiMock(ctrl)

	metricsMock := mocks.NewMockMetricsInterface(ctrl)
	stateMock := mocks.NewMockTaskRepository(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)
	stateMock.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()

	updater := initTestUpdater(t, newUpdaterTestConfig(mockLocker), argo)

	t.Run("handleArgoAPIFailure - generic error", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		err := fmt.Errorf("some generic error")

		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any())

		updater.monitor.HandleArgoAPIFailure(&task, err)

		// The resolved terminal status must be reflected back onto the task so the
		// result notification does not report a stale "in progress" for a failure.
		assert.Equal(t, models.StatusFailedMessage, task.Status)
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
			options, deadline := monitor.configureRetryOptions(models.Task{Id: "test-id", Timeout: tc.timeout})
			attempts := countAttempts(options)
			assert.Equal(t, tc.expectedAttempts, attempts)

			// The deadline must match attempts*delay so the wall-clock cap and the attempt cap
			// describe the same budget (delay falls back to ArgoSyncRetryDelay when non-positive).
			delay := tc.retryDelay
			if delay <= 0 {
				delay = ArgoSyncRetryDelay
			}
			assert.Equal(t, time.Duration(tc.expectedAttempts)*delay, deadline)
		})
	}

	t.Run("usesDefaultAttemptsWhenSet", func(t *testing.T) {
		monitor := NewDeploymentMonitor(
			Argo{},
			"",
			[]retry.Option{
				retry.DelayType(retry.FixedDelay),
				retry.Delay(0),
			},
			false,
			ArgoSyncRetryDelay,
		)
		monitor.defaultAttempts = 5

		options, deadline := monitor.configureRetryOptions(models.Task{Id: "test-id", Timeout: 0})
		attempts := countAttempts(options)
		assert.Equal(t, 5, attempts, "Should use the configured defaultAttempts when timeout is zero")
		assert.Equal(t, 5*ArgoSyncRetryDelay, deadline, "Default-attempts deadline should be attempts*delay")
	})
}

func boolPtr(b bool) *bool { return &b }

// TestDeploymentMonitorResolveRefresh verifies the per-task refresh override precedence (issue #334):
// an explicit Refresh wins over the instance default, and a nil override (old clients) keeps the default.
func TestDeploymentMonitorResolveRefresh(t *testing.T) {
	tests := []struct {
		name            string
		instanceDefault bool
		taskRefresh     *bool
		want            bool
	}{
		{name: "nil override keeps instance default (true)", instanceDefault: true, taskRefresh: nil, want: true},
		{name: "nil override keeps instance default (false)", instanceDefault: false, taskRefresh: nil, want: false},
		{name: "explicit false overrides default true", instanceDefault: true, taskRefresh: boolPtr(false), want: false},
		{name: "explicit true overrides default false", instanceDefault: false, taskRefresh: boolPtr(true), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			monitor := &DeploymentMonitor{refreshApp: tc.instanceDefault}
			got := monitor.resolveRefresh(models.Task{Refresh: tc.taskRefresh})
			assert.Equal(t, tc.want, got)
		})
	}
}

// newFetchTestMonitor builds a monitor wired to mock API + metrics for FetchApplication tests.
func newFetchTestMonitor(t *testing.T) (*DeploymentMonitor, *mocks.MockArgoApiInterface, *mocks.MockMetricsInterface) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	api := newArgoApiMock(ctrl)

	metrics := mocks.NewMockMetricsInterface(ctrl)
	monitor := NewDeploymentMonitor(
		Argo{api: api, metrics: metrics},
		"",
		[]retry.Option{retry.DelayType(retry.FixedDelay), retry.LastErrorOnly(true)},
		false,
		time.Millisecond,
	)
	return monitor, api, metrics
}

// TestDeploymentMonitorFetchApplicationRefreshRecordsDuration verifies that a refresh request is
// forwarded with refresh=true and its duration recorded (argocd_refresh_duration_seconds), so slow or
// stuck refreshes are diagnosable (issue #334). The API error is surfaced unchanged.
func TestDeploymentMonitorFetchApplicationRefreshRecordsDuration(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		monitor, api, metrics := newFetchTestMonitor(t)
		wantApp := &models.Application{}
		api.EXPECT().GetApplication(gomock.Any(), "demo", true).Return(wantApp, nil)
		metrics.EXPECT().ObserveRefreshDuration("demo", gomock.Any())

		app, err := monitor.FetchApplication(context.Background(), "demo", true)
		require.NoError(t, err)
		assert.Same(t, wantApp, app)
	})

	t.Run("error is surfaced, duration still recorded", func(t *testing.T) {
		monitor, api, metrics := newFetchTestMonitor(t)
		api.EXPECT().GetApplication(gomock.Any(), "demo", true).Return(nil, errors.New("connection refused"))
		metrics.EXPECT().ObserveRefreshDuration("demo", gomock.Any())

		app, err := monitor.FetchApplication(context.Background(), "demo", true)
		require.Error(t, err)
		assert.Nil(t, app)
		assert.Contains(t, err.Error(), "connection refused")
	})
}

// TestDeploymentMonitorFetchApplicationNoRefresh verifies that when refresh is not requested, the call
// is forwarded with refresh=false and no refresh duration is recorded.
func TestDeploymentMonitorFetchApplicationNoRefresh(t *testing.T) {
	monitor, api, _ := newFetchTestMonitor(t)
	wantApp := &models.Application{}
	// No ObserveRefreshDuration expectation: the mock controller fails the test if it is called.
	api.EXPECT().GetApplication(gomock.Any(), "demo", false).Return(wantApp, nil)

	app, err := monitor.FetchApplication(context.Background(), "demo", false)
	require.NoError(t, err)
	assert.Same(t, wantApp, app)
}

// TestDetermineFailureStatus verifies that unreachable-ArgoCD errors (transport
// failures and 5xx responses) are classified as aborted, while genuine
// application/API errors are classified as failed.
func TestDetermineFailureStatus(t *testing.T) {
	task := models.Task{App: "demo"}

	// *net.OpError is the type net returns for a refused dial; constructing it keeps
	// the table hermetic and fast.
	connRefused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"app not found", &ArgoAPIError{StatusCode: http.StatusNotFound, Message: `applications.argoproj.io "demo" not found`}, models.StatusAppNotFoundMessage},
		{"permission denied", &ArgoAPIError{StatusCode: http.StatusForbidden, Message: "permission denied"}, models.StatusAppNotFoundMessage},
		{"client timeout", context.DeadlineExceeded, models.StatusAborted},
		{"context canceled", context.Canceled, models.StatusAborted},
		{"connection refused", connRefused, models.StatusAborted},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "argocd.invalid", IsNotFound: true}, models.StatusAborted},
		{"url.Error wrapping deadline", &url.Error{Op: "Get", URL: "https://argocd", Err: context.DeadlineExceeded}, models.StatusAborted},
		{"argocd 503", &ArgoAPIError{StatusCode: http.StatusServiceUnavailable, Message: "upstream unavailable"}, models.StatusAborted},
		{"argocd 500", &ArgoAPIError{StatusCode: http.StatusInternalServerError, Message: "internal error"}, models.StatusAborted},
		{"wrapped argocd 503", fmt.Errorf("waiting for rollout: %w", &ArgoAPIError{StatusCode: http.StatusServiceUnavailable, Message: "upstream unavailable"}), models.StatusAborted},
		{"argocd 400", &ArgoAPIError{StatusCode: http.StatusBadRequest, Message: "bad request"}, models.StatusFailedMessage},
		{"generic error", errors.New("something else"), models.StatusFailedMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, determineFailureStatus(task, tt.err))
		})
	}
}
