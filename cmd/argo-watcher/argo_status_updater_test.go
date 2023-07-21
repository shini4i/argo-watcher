package main

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
)

func TestArgoStatusUpdaterCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Status Updater - ArgoCD cannot fetch app information", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockState(ctrl)

		task := models.Task{
			Id:  "test-id",
			App: "test-app",
		}
		// mock calls
		apiMock.EXPECT().GetApplication(task.App).Return(nil, fmt.Errorf("Unexpected failure"))
		metricsMock.EXPECT().AddFailedDeployment(task.App)
		stateMock.EXPECT().SetTaskStatus(task.Id, models.StatusFailedMessage, "ArgoCD API Error: Unexpected failure")

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)

		// argo updater
		updater := &ArgoStatusUpdater{}
		updater.Init(*argo, 1, "test-registry")

		// run the rollout
		updater.WaitForRollout(task)
	})
}
