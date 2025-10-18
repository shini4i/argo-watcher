package argocd

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/mock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

const loggedInUsername = "unit-test"
const taskImageTag = "test:v0.0.1"

func TestArgoCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Argo Watcher - Up", func(t *testing.T) {
		// mocks
		apiMock := mock.NewMockArgoApiInterface(ctrl)
		metricsMock := mock.NewMockMetricsInterface(ctrl)
		stateMock := mock.NewMockTaskRepository(ctrl)

		// mock calls
		stateMock.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: loggedInUsername,
		}
		apiMock.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metricsMock.EXPECT().SetArgoUnavailable(false)

		// argo manager
		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		status, err := argo.Check()

		// assertions
		assert.Nil(t, err)
		assert.Equal(t, "up", status)
	})

	t.Run("Argo Watcher - Down - Cannot connect to State manager", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(false)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: loggedInUsername,
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
	})

	t.Run("Argo Watcher - Down - Cannot login", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: false,
			Username: loggedInUsername,
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
	})

	t.Run("Argo Watcher - Down - Unexpected login failure", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("unexpected login error"))
		metrics.EXPECT().SetArgoUnavailable(true)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		// assertions
		assert.EqualError(t, err, models.StatusArgoCDUnavailableMessage)
		assert.Equal(t, "down", status)
	})
}

func TestArgoAddTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Argo Unavailable", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("unexpected login error"))
		metrics.EXPECT().SetArgoUnavailable(true)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{} // empty task
		newTask, err := argo.AddTask(task)

		// assertions
		assert.Nil(t, newTask)
		assert.EqualError(t, err, models.StatusArgoCDUnavailableMessage)
	})

	t.Run("Argo - Image not passed", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		user := &models.Userinfo{
			LoggedIn: true,
		}
		api.EXPECT().GetUserInfo().Return(user, nil)
		metrics.EXPECT().SetArgoUnavailable(false)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{} // empty task
		newTask, err := argo.AddTask(task)

		// assertions
		assert.Nil(t, newTask)
		assert.EqualError(t, err, "trying to create task without images")
	})

	t.Run("Argo - App not passed", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		user := &models.Userinfo{
			LoggedIn: true,
		}
		api.EXPECT().GetUserInfo().Return(user, nil)
		metrics.EXPECT().SetArgoUnavailable(false)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}
		newTask, err := argo.AddTask(task)

		// assertions
		assert.Nil(t, newTask)
		assert.EqualError(t, err, "trying to create task without app name")
	})

	t.Run("Argo - State add failed", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		user := &models.Userinfo{
			LoggedIn: true,
		}
		api.EXPECT().GetUserInfo().Return(user, nil)
		metrics.EXPECT().SetArgoUnavailable(false)

		// mock calls to add task
		stateError := fmt.Errorf("database error")
		state.EXPECT().AddTask(gomock.Any()).Return(nil, stateError)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{
			App: "test-app",
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}
		newTask, err := argo.AddTask(task)

		// assertions
		assert.Nil(t, newTask)
		assert.EqualError(t, err, stateError.Error())
	})

	t.Run("Argo - Task added", func(t *testing.T) {
		// mocks
		api := mock.NewMockArgoApiInterface(ctrl)
		metrics := mock.NewMockMetricsInterface(ctrl)
		state := mock.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		user := &models.Userinfo{
			LoggedIn: true,
		}
		api.EXPECT().GetUserInfo().Return(user, nil)
		metrics.EXPECT().SetArgoUnavailable(false)
		metrics.EXPECT().AddProcessedDeployment("test-app")

		// tasks
		task := models.Task{
			App: "test-app",
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}
		newTask := models.Task{
			Id:  uuid.NewString(),
			App: "test-app",
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}

		// mock calls to add task
		state.EXPECT().AddTask(gomock.Any()).Return(&newTask, nil)

		// argo manager
		argo := &Argo{}
		argo.Init(state, api, metrics)
		newTaskReturned, err := argo.AddTask(task)

		// assertions
		assert.Nil(t, err)
		assert.NotNil(t, newTaskReturned)
		uuidRegexp := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$")
		assert.Regexp(t, uuidRegexp, newTaskReturned.Id, "Must match Regexp for uuid v4")
	})
}
