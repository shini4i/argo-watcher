package argocd

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/internal/mocks"
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
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		// mock calls
		state.EXPECT().Check().Return(true)
		user := &models.Userinfo{
			LoggedIn: true,
		}
		api.EXPECT().GetUserInfo().Return(user, nil)
		metrics.EXPECT().SetArgoUnavailable(false)

		// mock calls to add task
		stateError := fmt.Errorf("database error")
		state.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return([]models.Task{}, int64(0))
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any()).Return(int64(0), nil)
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
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

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

		// mock calls to add task. In-progress deployments for the app MUST be
		// cancelled before the new task is persisted; otherwise the new task would
		// match the cancel filter and cancel itself. gomock.InOrder locks that.
		state.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return([]models.Task{}, int64(0))
		gomock.InOrder(
			// The task's images MUST be forwarded to the cancel call so superseding
			// is scoped to matching images, not the whole app.
			state.EXPECT().CancelInProgressTasks("test-app", gomock.Eq(task.Images), supersededTaskReason).Return(int64(0), nil),
			state.EXPECT().AddTask(gomock.Any()).Return(&newTask, nil),
		)

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

	t.Run("Argo - Cancel failure does not block new deployment", func(t *testing.T) {
		// mocks
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil)
		metrics.EXPECT().SetArgoUnavailable(false)
		metrics.EXPECT().AddProcessedDeployment("test-app")

		task := models.Task{App: "test-app", Images: []models.Image{{Tag: taskImageTag}}}
		newTask := models.Task{Id: uuid.NewString(), App: "test-app", Images: []models.Image{{Tag: taskImageTag}}}

		// Cancelling prior in-progress tasks is best-effort: a failure here must not
		// block the new deployment from being persisted.
		state.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return([]models.Task{}, int64(0))
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), supersededTaskReason).Return(int64(0), fmt.Errorf("cancel failed"))
		state.EXPECT().AddTask(gomock.Any()).Return(&newTask, nil)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		newTaskReturned, err := argo.AddTask(task)

		assert.NoError(t, err, "a best-effort cancel failure must not fail the new deployment")
		assert.NotNil(t, newTaskReturned)
	})

	t.Run("Argo - Rollback fields are computed and persisted", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil)
		metrics.EXPECT().SetArgoUnavailable(false)
		metrics.EXPECT().AddProcessedDeployment("test-app")

		// History ordered created DESC: current is v2, an earlier task (target) ran v1.
		deployed := []models.Task{
			{Id: "current", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v2"}}, Status: models.StatusDeployedMessage},
			{Id: "earlier", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v1"}}, Status: models.StatusDeployedMessage},
		}
		state.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return(deployed, int64(len(deployed)))

		// Capture the task actually handed to the repository.
		var captured models.Task
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any()).Return(int64(0), nil)
		state.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
			captured = task
			task.Id = uuid.NewString()
			return &task, nil
		})

		argo := &Argo{}
		argo.Init(state, api, metrics)
		_, err := argo.AddTask(models.Task{App: "test-app", Images: []models.Image{{Image: "app", Tag: "v1"}}})

		assert.NoError(t, err)
		assert.True(t, captured.IsRollback)
		assert.Equal(t, "earlier", captured.RollbackTargetId)
	})

	t.Run("Argo - Client-supplied rollback fields are overwritten from history", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil)
		metrics.EXPECT().SetArgoUnavailable(false)
		metrics.EXPECT().AddProcessedDeployment("test-app")

		// Deploying a brand-new version (v3) with no earlier match: not a rollback.
		deployed := []models.Task{
			{Id: "current", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v2"}}, Status: models.StatusDeployedMessage},
		}
		state.EXPECT().GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, gomock.Any(), gomock.Any()).Return(deployed, int64(len(deployed)))

		var captured models.Task
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any()).Return(int64(0), nil)
		state.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
			captured = task
			task.Id = uuid.NewString()
			return &task, nil
		})

		argo := &Argo{}
		argo.Init(state, api, metrics)
		// Client tries to spoof the rollback flag; the server must overwrite it.
		_, err := argo.AddTask(models.Task{
			App:              "test-app",
			Images:           []models.Image{{Image: "app", Tag: "v3"}},
			IsRollback:       true,
			RollbackTargetId: "spoofed",
		})

		assert.NoError(t, err)
		assert.False(t, captured.IsRollback)
		assert.Empty(t, captured.RollbackTargetId)
	})
}

func TestArgoDetectRollback(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	img := func(image, tag string) []models.Image {
		return []models.Image{{Image: image, Tag: tag}}
	}

	// deployedTask is one entry in the app's deployment history.
	type deployedTask struct {
		id     string
		images []models.Image
	}

	tests := []struct {
		name    string
		history []deployedTask // successfully deployed tasks, oldest first
		target  []models.Image // image set being deployed now
		// wantTargetID is the expected rollback target task ID ("" = not a rollback).
		wantTargetID string
	}{
		{
			name:         "first deployment is not a rollback",
			history:      nil,
			target:       img("app", "v1"),
			wantTargetID: "",
		},
		{
			name:         "forward deployment of a new version is not a rollback",
			history:      []deployedTask{{"t1", img("app", "v1")}, {"t2", img("app", "v2")}},
			target:       img("app", "v3"),
			wantTargetID: "",
		},
		{
			name:         "redeploying the current version is not a rollback",
			history:      []deployedTask{{"t1", img("app", "v1")}, {"t2", img("app", "v2")}},
			target:       img("app", "v2"),
			wantTargetID: "",
		},
		{
			name: "redeploying the current version short-circuits even when an older duplicate exists",
			history: []deployedTask{
				{"t1", img("app", "v2")}, // older deployment of the same version
				{"t2", img("app", "v1")},
				{"t3", img("app", "v2")}, // current version
			},
			target:       img("app", "v2"),
			wantTargetID: "",
		},
		{
			name:         "returning to an earlier version rolls back to that task",
			history:      []deployedTask{{"t1", img("app", "v1")}, {"t2", img("app", "v2")}},
			target:       img("app", "v1"),
			wantTargetID: "t1",
		},
		{
			name: "rolls back to the most recent earlier deployment of the version",
			history: []deployedTask{
				{"t1", img("app", "v1")},
				{"t2", img("app", "v1")},
				{"t3", img("app", "v2")},
			},
			target:       img("app", "v1"),
			wantTargetID: "t2",
		},
		{
			name: "image order does not affect the signature",
			history: []deployedTask{
				{"t1", []models.Image{{Image: "a", Tag: "1"}, {Image: "b", Tag: "2"}}},
				{"t2", img("app", "v2")},
			},
			target:       []models.Image{{Image: "b", Tag: "2"}, {Image: "a", Tag: "1"}},
			wantTargetID: "t1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GetTasks returns deployed tasks ordered created DESC (most recent
			// first), so reverse the oldest-first history fixture.
			deployed := make([]models.Task, 0, len(tt.history))
			for i := len(tt.history) - 1; i >= 0; i-- {
				deployed = append(deployed, models.Task{
					Id:     tt.history[i].id,
					App:    "test-app",
					Images: tt.history[i].images,
					Status: models.StatusDeployedMessage,
				})
			}

			state := mocks.NewMockTaskRepository(ctrl)
			state.EXPECT().
				GetTasks(gomock.Any(), gomock.Any(), "test-app", models.StatusDeployedMessage, rollbackHistoryWindow, 0).
				Return(deployed, int64(len(deployed)))

			argo := &Argo{State: state}
			result := argo.detectRollback(models.Task{App: "test-app", Images: tt.target})

			assert.Equal(t, tt.wantTargetID, result)
		})
	}
}

func TestArgoGetTasks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Listing tasks is a pure read from the state store and must NOT be gated on
	// ArgoCD reachability. Verify GetTasks never calls Check()/GetUserInfo(): the
	// mocks below would fail the run if it did, since no such calls are expected.
	t.Run("readsFromStateWithoutCheckingArgoCD", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		start := 10.0
		end := 20.0

		expectedTasks := []models.Task{
			{Id: "task-1", App: "demo", Images: []models.Image{{Image: "example.com/app", Tag: "v1.0.0"}}},
		}
		state.EXPECT().GetTasks(start, end, "demo", "", 0, 0).Return(expectedTasks, int64(len(expectedTasks)))

		argo := &Argo{}
		argo.Init(state, api, metrics)

		response := argo.GetTasks(start, end, "demo", "", 0, 0)

		assert.Equal(t, expectedTasks, response.Tasks)
		assert.Equal(t, int64(len(expectedTasks)), response.Total)
		assert.Empty(t, response.Error)
	})

	// The invariant "GetTasks issues no ArgoCD/metrics calls" must hold for every
	// input, not just the one above — this case pins it on a distinct filter and
	// time window. The api/metrics mocks expect zero interactions, so any ArgoCD
	// call regresses. (There is no reachability to simulate: the read never
	// touches ArgoCD, which is precisely why stored history stays viewable during
	// an outage.)
	t.Run("makesNoArgoCDCallsRegardlessOfInput", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := mocks.NewMockTaskRepository(ctrl)

		expectedTasks := []models.Task{
			{Id: "task-1", App: "demo"},
		}
		state.EXPECT().GetTasks(0.0, 100.0, "demo", "", 0, 0).Return(expectedTasks, int64(len(expectedTasks)))

		argo := &Argo{}
		argo.Init(state, api, metrics)

		response := argo.GetTasks(0, 100, "demo", "", 0, 0)

		assert.Equal(t, expectedTasks, response.Tasks)
		assert.Equal(t, int64(len(expectedTasks)), response.Total)
		assert.Empty(t, response.Error)
	})
}

func TestArgoStartLivenessProbe(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	api := newArgoApiMock(ctrl)
	metrics := mocks.NewMockMetricsInterface(ctrl)
	state := mocks.NewMockTaskRepository(ctrl)

	// Cancel before starting: the immediate probe still runs Check() exactly
	// once (refreshing the metric), then the loop returns on ctx.Done() without
	// waiting for a tick. This proves the ambient refresh + clean-exit contract
	// deterministically, with no reliance on timer scheduling.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state.EXPECT().Check().Return(true)
	api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil)
	metrics.EXPECT().SetArgoUnavailable(false)

	argo := &Argo{}
	argo.Init(state, api, metrics)

	done := make(chan struct{})
	go func() {
		argo.StartLivenessProbe(ctx, time.Hour)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartLivenessProbe did not return after context cancellation")
	}
}

func TestArgoSimpleHealthCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mocks.NewMockTaskRepository(ctrl)
	state.EXPECT().Check().Return(true)

	argo := &Argo{}
	argo.Init(state, nil, nil)

	assert.True(t, argo.SimpleHealthCheck())
}
