package main

import (
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestArgoApi_GetUserInfo(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Argo API - Get Userinfo", func(t *testing.T) {
		api := mock.NewMockArgoApiInterface(ctrl)

		userinfo := models.Userinfo{
			Username: "test",
			LoggedIn: true,
		}

		api.EXPECT().GetUserInfo().Return(&userinfo, nil)

		receivedUserinfo, err := api.GetUserInfo()

		assert.Nil(t, err)
		assert.Equal(t, &userinfo, receivedUserinfo)
	})
}

func TestArgoApi_GetApplication(t *testing.T) {
	t.Skip("skipping test")
}
