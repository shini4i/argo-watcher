package argocd

import (
	"fmt"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/lock"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
	"go.uber.org/mock/gomock"
)

var (
	mockWebhookConfig = &config.WebhookConfig{
		Enabled: false,
	}
)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp/", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 3, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

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

func TestArgoStatusUpdater_processDeploymentResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mock.NewMockArgoApiInterface(ctrl)
	metricsMock := mock.NewMockMetricsInterface(ctrl)
	stateMock := mock.NewMockTaskRepository(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := &ArgoStatusUpdater{}
	err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
	assert.NoError(t, err)

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

	updater := &ArgoStatusUpdater{}
	err := updater.Init(*argo, 1, 0*time.Second, "test-registry", "/tmp", false, mockWebhookConfig, mockLocker)
	assert.NoError(t, err)

	t.Run("handleArgoAPIFailure - generic error", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		err := fmt.Errorf("some generic error")

		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any())

		updater.monitor.HandleArgoAPIFailure(task, err)
	})
}

func TestDeploymentMonitor_configureRetryOptions(t *testing.T) {
	monitor := NewDeploymentMonitor(Argo{}, "", []retry.Option{retry.Attempts(1), retry.Delay(0)}, false)

	countAttempts := func(options []retry.Option) int {
		attempts := 0
		_ = retry.Do(func() error {
			attempts++
			return fmt.Errorf("retry")
		}, options...)
		return attempts
	}

	testCases := []struct {
		name             string
		timeout          int
		expectedAttempts int
	}{
		{
			name:             "nonPositiveTimeoutUsesMinimumAttempts",
			timeout:          0,
			expectedAttempts: 15,
		},
		{
			name:             "negativeTimeoutUsesMinimumAttempts",
			timeout:          -5,
			expectedAttempts: 15,
		},
		{
			name:             "positiveTimeoutLessThanWindow",
			timeout:          10,
			expectedAttempts: 1,
		},
		{
			name:             "positiveTimeoutExactMultiple",
			timeout:          30,
			expectedAttempts: 3,
		},
		{
			name:             "positiveTimeoutWithRemainderRoundsUp",
			timeout:          46,
			expectedAttempts: 4,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			options := monitor.configureRetryOptions(models.Task{Id: "test-id", Timeout: tc.timeout})
			attempts := countAttempts(options)
			assert.Equal(t, tc.expectedAttempts, attempts)
		})
	}
}
