package main

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestArgo_Check(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	
	t.Run("application status deployed", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockState(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: "unit-test",
		}
		api.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metrics.EXPECT().SetArgoUnavailable(false)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()
		
		// assertions
		assert.Nil(t, err)
		assert.NotNil(t, status)
	});
}
