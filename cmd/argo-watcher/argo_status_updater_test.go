package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
)

func TestArgoStatusUpdaterCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil)
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
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
		updater.Init(*argo, 3, 0*time.Second, "test-registry")

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
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil)
		metricsMock.EXPECT().ResetFailedDeployment(task.App)
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
		updater.Init(*argo, 1, 0*time.Second, "")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(2)
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application not available\n\nList of current images (last app check):\n\ttest-registry/ghcr.io/shini4i/argo-watcher:dev\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:dev")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf("applications.argoproj.io \"test-app\" not found"))
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAppNotFoundMessage, "ArgoCD API Error: applications.argoproj.io \"test-app\" not found")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf(argoUnavailableErrorMessage))
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusAborted, "ArgoCD API Error: connect: connection refused")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

		// prepare test data
		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}

		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf("Unexpected failure"))
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: Unexpected failure")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(2) // we call 1st time to check the status and 2nd time to
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application not available\n\nList of current images (last app check):\n\ttest-image:v0.0.1\n\nList of expected images:\n\tghcr.io/shini4i/argo-watcher:dev")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(2) // we call 1st time to check the status and 2nd time to
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application out of sync\n\nApp status \"NotWorking\"\nApp message \"Not working test app\"\nResources:\n\t")

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
		updater.Init(*argo, 1, 0*time.Second, "test-registry")

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
		apiMock.EXPECT().GetApplication(task.App).Return(&application, nil).Times(2) // we call 1st time to check the status and 2nd time to
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage,
			"Application not healthy\n\nApp sync status \"Synced\"\nApp health status \"NotHealthy\"\nResources:\n\t")

		// run the rollout
		updater.WaitForRollout(task)
	})
}
