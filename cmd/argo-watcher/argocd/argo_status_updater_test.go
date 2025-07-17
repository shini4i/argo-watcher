package argocd

import (
	"fmt"
	"testing"
	"time"

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
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(3)
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
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 3, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		apiMock.EXPECT().GetApplication(task.App).Return(&unhealthyApp, nil).Times(2)
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
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status \"not available\"\n\nList of current images (last app check):\n\ttest-registry/ghcr.io/shini4i/argo-watcher:dev\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:dev")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not found", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, "ARGO API ERROR: applications.argoproj.io \"test-app\" not found")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - ArgoCD unavailable", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, "ARGO API ERROR: connect: connection refused")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application API error", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ARGO API ERROR: unexpected failure")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not available", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status \"not available\"\n\nList of current images (last app check):\n\ttest-image:v0.0.1\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:dev")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application out of Sync", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status \"not synced\"\n\nApp status \"NotWorking\"\nApp message \"Not working test app\"\nResources:\n\t")

		// run the rollout
		updater.WaitForRollout(task)
	})

	t.Run("Status Updater - Application not healthy", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
		assert.NoError(t, err)

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
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
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application deployment failed. Rollout status \"not healthy\"\n\nApp sync status \"Synced\"\nApp health status \"NotHealthy\"\nResources:\n\t")

		// run the rollout
		updater.WaitForRollout(task)
	})
}

func TestArgoStatusUpdater_processDeploymentResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mock.NewMockArgoApiInterface(ctrl)
	metricsMock := mock.NewMockMetricsInterface(ctrl)
	stateMock := mock.NewMockState(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := &ArgoStatusUpdater{}
	err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
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

		updater.processDeploymentResult(&task, app)
		assert.Equal(t, models.StatusDeployedMessage, task.Status)
	})

	// fire and forget scenario
	t.Run("processDeploymentResult - fire and forget", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		app := &models.Application{}
		app.Metadata.Annotations = map[string]string{"fire-and-forget": "true"}
		app.Status.Sync.Status = "Synced"
		app.Status.Health.Status = "Healthy"

		metricsMock.EXPECT().ResetFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusDeployedMessage, "")

		updater.processDeploymentResult(&task, app)
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

		updater.processDeploymentResult(&task, app)
		assert.Equal(t, models.StatusFailedMessage, task.Status)
	})
}

func TestArgoStatusUpdater_handleArgoAPIFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	apiMock := mock.NewMockArgoApiInterface(ctrl)
	metricsMock := mock.NewMockMetricsInterface(ctrl)
	stateMock := mock.NewMockState(ctrl)
	mockLocker := lock.NewInMemoryLocker()

	argo := &Argo{}
	argo.Init(stateMock, apiMock, metricsMock)

	updater := &ArgoStatusUpdater{}
	err := updater.Init(*argo, 1, 0*time.Second, "test-registry", false, mockWebhookConfig, mockLocker)
	assert.NoError(t, err)

	t.Run("handleArgoAPIFailure - generic error", func(t *testing.T) {
		task := models.Task{Id: "test-id", App: "test-app"}
		err := fmt.Errorf("some generic error")

		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, gomock.Any())

		updater.handleArgoAPIFailure(task, err)
	})
}
