package main

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestArgo_Check(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	
	t.Run("Argo Watcher - Up", func(t *testing.T) {
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
		assert.Equal(t, "up", status)
	});

	t.Run("Argo Watcher - Down - Cannot connect to state manager", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockState(ctrl)

		// mock calls
		state.EXPECT().Check().Return(false)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: "unit-test",
		}
		api.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metrics.EXPECT().SetArgoUnavailable(true)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()
		
		// assertions
		assert.EqualError(t, err, models.StatusConnectionUnavailable)
		assert.Equal(t, "down", status)
	});

	
	t.Run("Argo Watcher - Down - Cannot login", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockState(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: false,
			Username: "unit-test",
		}
		api.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metrics.EXPECT().SetArgoUnavailable(true)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()
		
		// assertions
		assert.EqualError(t, err, models.StatusArgoCDFailedLogin)
		assert.Equal(t, "down", status)
	});

	t.Run("Argo Watcher - Down - Unexpected login failure", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockState(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("Unexpected login error"))
		metrics.EXPECT().SetArgoUnavailable(true)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()
		
		// assertions
		assert.EqualError(t, err, models.StatusArgoCDUnavailableMessage)
		assert.Equal(t, "down", status)
	});
}
