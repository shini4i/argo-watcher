package argocd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const loggedInUsername = "unit-test"
const taskImageTag = "test:v0.0.1"

// anyLimit tells deployedHistoryFilter to accept whatever page size the caller used.
const anyLimit = -1

// deployedHistoryFilter matches the filter detectRollback issues for app: its
// deployed history from the first page, capped at limit. Search must stay empty
// — a caller's search term would narrow the history and pick a wrong target.
func deployedHistoryFilter(app string, limit int) gomock.Matcher {
	return gomock.Cond(func(filter models.TaskFilter) bool {
		if filter.App != app || filter.Status != models.StatusDeployedMessage {
			return false
		}
		if filter.Offset != 0 || filter.Search != "" {
			return false
		}
		return limit == anyLimit || filter.Limit == limit
	})
}

func TestArgoCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Argo Watcher - Up", func(t *testing.T) {
		apiMock := newArgoApiMock(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)
		stateMock := newTaskRepositoryMock(ctrl)

		stateMock.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: loggedInUsername,
		}
		apiMock.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metricsMock.EXPECT().SetArgoUnavailable(false)
		metricsMock.EXPECT().SetStateUnavailable(false)

		argo := &Argo{}
		argo.Init(stateMock, apiMock, metricsMock)
		status, err := argo.Check()

		assert.Nil(t, err)
		assert.Equal(t, "up", status)
	})

	t.Run("Argo Watcher - Down - Cannot connect to State manager", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		state.EXPECT().Check().Return(false)
		testUserInfo := &models.Userinfo{
			LoggedIn: true,
			Username: loggedInUsername,
		}
		api.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metrics.EXPECT().SetArgoUnavailable(false)
		metrics.EXPECT().SetStateUnavailable(true)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		assert.EqualError(t, err, models.StatusConnectionUnavailable)
		assert.Equal(t, "down", status)
		assert.Equal(t, ReasonDatabase, argo.UnavailableReason())
	})

	t.Run("Argo Watcher - Down - Cannot login", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		state.EXPECT().Check().Return(true)
		testUserInfo := &models.Userinfo{
			LoggedIn: false,
			Username: loggedInUsername,
		}
		api.EXPECT().GetUserInfo().Return(testUserInfo, nil)
		metrics.EXPECT().SetArgoUnavailable(true)
		metrics.EXPECT().SetStateUnavailable(false)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		assert.EqualError(t, err, models.StatusArgoCDFailedLogin)
		assert.Equal(t, "down", status)
		assert.Equal(t, ReasonArgoCD, argo.UnavailableReason())
	})

	t.Run("Argo Watcher - Down - Unexpected login failure", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		state.EXPECT().Check().Return(true)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("unexpected login error"))
		metrics.EXPECT().SetArgoUnavailable(true)
		metrics.EXPECT().SetStateUnavailable(false)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		assert.EqualError(t, err, models.StatusArgoCDUnavailableMessage)
		assert.Equal(t, "down", status)
		assert.Equal(t, ReasonArgoCD, argo.UnavailableReason())
	})

	t.Run("Argo Watcher - Down - Both ArgoCD and state backend unreachable", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		state.EXPECT().Check().Return(false)
		api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("unexpected login error"))
		metrics.EXPECT().SetArgoUnavailable(true)
		metrics.EXPECT().SetStateUnavailable(true)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		assert.Equal(t, "down", status)
		assert.Equal(t, ReasonBoth, argo.UnavailableReason())
		assert.ErrorContains(t, err, models.StatusConnectionUnavailable)
		assert.ErrorContains(t, err, models.StatusArgoCDUnavailableMessage)
	})

	t.Run("Argo Watcher - Down - Both, ArgoCD login rejected", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		// State backend down AND ArgoCD reachable-but-not-logged-in (the other
		// ArgoCD-down branch): the combined error must carry the login-rejected
		// cause, not only the transport-error variant.
		state.EXPECT().Check().Return(false)
		api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: false}, nil)
		metrics.EXPECT().SetArgoUnavailable(true)
		metrics.EXPECT().SetStateUnavailable(true)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		status, err := argo.Check()

		assert.Equal(t, "down", status)
		assert.Equal(t, ReasonBoth, argo.UnavailableReason())
		assert.ErrorContains(t, err, models.StatusConnectionUnavailable)
		assert.ErrorContains(t, err, models.StatusArgoCDFailedLogin)
	})
}

func TestArgoIsAvailable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("defaults to available after Init", func(t *testing.T) {
		argo := &Argo{}
		argo.Init(newTaskRepositoryMock(ctrl), newArgoApiMock(ctrl), mocks.NewMockMetricsInterface(ctrl))
		// Optimistic default avoids a spurious "unreachable" banner before the
		// first liveness probe runs.
		assert.True(t, argo.IsAvailable())
	})

	t.Run("Check flips the cached state and mirrors the gauge", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		// A failing Check must mark ArgoCD unavailable (gauge 1 / cache false),
		// a passing one must restore it (gauge 0 / cache true). setReason sets the
		// ArgoCD gauge before the state gauge, so the order reflects that. The
		// state backend is reachable throughout, so its gauge stays 0.
		gomock.InOrder(
			state.EXPECT().Check().Return(true),
			api.EXPECT().GetUserInfo().Return(nil, fmt.Errorf("boom")),
			metrics.EXPECT().SetArgoUnavailable(true),
			metrics.EXPECT().SetStateUnavailable(false),
			state.EXPECT().Check().Return(true),
			api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil),
			metrics.EXPECT().SetArgoUnavailable(false),
			metrics.EXPECT().SetStateUnavailable(false),
		)

		argo := &Argo{}
		argo.Init(state, api, metrics)

		_, err := argo.Check()
		assert.Error(t, err)
		assert.False(t, argo.IsAvailable())

		_, err = argo.Check()
		assert.NoError(t, err)
		assert.True(t, argo.IsAvailable())
	})
}

func TestArgoAddTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("Argo Unavailable", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		// Simulate a cached outage: AddTask must reject fast, without a live
		// probe (no Check/GetUserInfo calls) so the client is not held on the
		// retry budget (issue #498).
		argo.reason.Store(ReasonArgoCD)
		task := models.Task{}
		newTask, err := argo.AddTask(task)

		assert.Nil(t, newTask)
		assert.EqualError(t, err, models.StatusArgoCDUnavailableMessage)
	})

	t.Run("Argo - Image not passed", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{}
		newTask, err := argo.AddTask(task)

		assert.Nil(t, newTask)
		assert.EqualError(t, err, "trying to create task without images")
	})

	t.Run("Argo - App not passed", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}
		newTask, err := argo.AddTask(task)

		assert.Nil(t, newTask)
		assert.EqualError(t, err, "trying to create task without app name")
	})

	t.Run("Argo - State add failed", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		stateError := fmt.Errorf("database error")
		state.EXPECT().GetTasks(deployedHistoryFilter("test-app", anyLimit)).Return([]models.Task{}, int64(0))
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(0), nil)
		state.EXPECT().AddTask(gomock.Any()).Return(nil, stateError)

		argo := &Argo{}
		argo.Init(state, api, metrics)
		task := models.Task{
			App: "test-app",
			Images: []models.Image{
				{Tag: taskImageTag},
			},
		}
		newTask, err := argo.AddTask(task)

		assert.Nil(t, newTask)
		assert.EqualError(t, err, stateError.Error())
	})

	// Run for both authorities: pinning only the validated case would let a
	// regression that hardcodes `true` at the cancel call site pass, restoring the
	// vulnerability the authority rule closes.
	for _, validated := range []bool{true, false} {
		t.Run(fmt.Sprintf("Argo - Task added (validated=%t)", validated), func(t *testing.T) {
			api := newArgoApiMock(ctrl)
			metrics := mocks.NewMockMetricsInterface(ctrl)
			state := newTaskRepositoryMock(ctrl)

			// Acceptance is counted without naming the app: submission is open and the
			// name is free text, so nothing here may become a label value (issue #552).
			// The app is named only once ArgoCD confirms it, which happens in the
			// monitoring path.
			metrics.EXPECT().AddAcceptedDeployment()

			task := models.Task{
				App: "test-app",
				Images: []models.Image{
					{Tag: taskImageTag},
				},
				Validated: validated,
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
			state.EXPECT().GetTasks(deployedHistoryFilter("test-app", anyLimit)).Return([]models.Task{}, int64(0))
			gomock.InOrder(
				// The task's images MUST be forwarded to the cancel call so superseding
				// is scoped to matching images, not the whole app. Its own Validated flag
				// MUST be forwarded verbatim: that is what stops an uncredentialed
				// deployment from cancelling a credentialed one.
				state.EXPECT().CancelInProgressTasks("test-app", gomock.Eq(task.Images), supersededTaskReason, gomock.Eq(validated)).Return(int64(0), nil),
				state.EXPECT().AddTask(gomock.Any()).Return(&newTask, nil),
			)

			argo := &Argo{}
			argo.Init(state, api, metrics)
			newTaskReturned, err := argo.AddTask(task)

			assert.Nil(t, err)
			assert.NotNil(t, newTaskReturned)
			uuidRegexp := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$")
			assert.Regexp(t, uuidRegexp, newTaskReturned.Id, "Must match Regexp for uuid v4")
		})
	}

	t.Run("Argo - Cancel failure does not block new deployment", func(t *testing.T) {
		api := newArgoApiMock(ctrl)
		metrics := mocks.NewMockMetricsInterface(ctrl)
		state := newTaskRepositoryMock(ctrl)

		metrics.EXPECT().AddAcceptedDeployment()

		task := models.Task{App: "test-app", Images: []models.Image{{Tag: taskImageTag}}, Validated: true}
		newTask := models.Task{Id: uuid.NewString(), App: "test-app", Images: []models.Image{{Tag: taskImageTag}}}

		state.EXPECT().GetTasks(deployedHistoryFilter("test-app", anyLimit)).Return([]models.Task{}, int64(0))
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), supersededTaskReason, gomock.Any()).Return(int64(0), fmt.Errorf("cancel failed"))
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
		state := newTaskRepositoryMock(ctrl)

		metrics.EXPECT().AddAcceptedDeployment()

		// History ordered created DESC: current is v2, an earlier task (target) ran v1.
		deployed := []models.Task{
			{Id: "current", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v2"}}, Status: models.StatusDeployedMessage},
			{Id: "earlier", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v1"}}, Status: models.StatusDeployedMessage},
		}
		state.EXPECT().GetTasks(deployedHistoryFilter("test-app", anyLimit)).Return(deployed, int64(len(deployed)))

		var captured models.Task
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(0), nil)
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
		state := newTaskRepositoryMock(ctrl)

		metrics.EXPECT().AddAcceptedDeployment()

		deployed := []models.Task{
			{Id: "current", App: "test-app", Images: []models.Image{{Image: "app", Tag: "v2"}}, Status: models.StatusDeployedMessage},
		}
		state.EXPECT().GetTasks(deployedHistoryFilter("test-app", anyLimit)).Return(deployed, int64(len(deployed)))

		var captured models.Task
		state.EXPECT().CancelInProgressTasks("test-app", gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(0), nil)
		state.EXPECT().AddTask(gomock.Any()).DoAndReturn(func(task models.Task) (*models.Task, error) {
			captured = task
			task.Id = uuid.NewString()
			return &task, nil
		})

		argo := &Argo{}
		argo.Init(state, api, metrics)
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

	type deployedTask struct {
		id     string
		images []models.Image
	}

	tests := []struct {
		name    string
		history []deployedTask // successfully deployed tasks, oldest first
		target  []models.Image
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

			state := newTaskRepositoryMock(ctrl)
			state.EXPECT().
				GetTasks(deployedHistoryFilter("test-app", rollbackHistoryWindow)).
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
		state := newTaskRepositoryMock(ctrl)

		start := 10.0
		end := 20.0

		expectedTasks := []models.Task{
			{Id: "task-1", App: "demo", Images: []models.Image{{Image: "example.com/app", Tag: "v1.0.0"}}},
		}
		state.EXPECT().GetTasks(models.TaskFilter{StartTime: start, EndTime: end, App: "demo"}).Return(expectedTasks, int64(len(expectedTasks)))

		argo := &Argo{}
		argo.Init(state, api, metrics)

		response := argo.GetTasks(models.TaskFilter{StartTime: start, EndTime: end, App: "demo"})

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
		state := newTaskRepositoryMock(ctrl)

		expectedTasks := []models.Task{
			{Id: "task-1", App: "demo"},
		}
		state.EXPECT().GetTasks(models.TaskFilter{StartTime: 0, EndTime: 100, App: "demo"}).Return(expectedTasks, int64(len(expectedTasks)))

		argo := &Argo{}
		argo.Init(state, api, metrics)

		response := argo.GetTasks(models.TaskFilter{StartTime: 0, EndTime: 100, App: "demo"})

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
	state := newTaskRepositoryMock(ctrl)

	// Cancel before starting: the immediate probe still runs Check() exactly
	// once (refreshing the metric), then the loop returns on ctx.Done() without
	// waiting for a tick. This proves the ambient refresh + clean-exit contract
	// deterministically, with no reliance on timer scheduling.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state.EXPECT().Check().Return(true)
	api.EXPECT().GetUserInfo().Return(&models.Userinfo{LoggedIn: true}, nil)
	metrics.EXPECT().SetArgoUnavailable(false)
	metrics.EXPECT().SetStateUnavailable(false)

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

	state := newTaskRepositoryMock(ctrl)
	state.EXPECT().Check().Return(true)

	argo := &Argo{}
	argo.Init(state, nil, nil)

	assert.True(t, argo.SimpleHealthCheck())
}

// TestArgoAddTaskClaimsTheTask pins claim-on-accept. The shared repository mock
// permits ClaimTask any number of times, so without this the claim could be
// removed from AddTask entirely and every other test would still pass — leaving
// each new task unowned until a sweep noticed it a reap interval later.
func TestArgoAddTaskClaimsTheTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	t.Run("the accepting replica claims what it accepted", func(t *testing.T) {
		stateMock := mocks.NewMockTaskRepository(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)

		argo := &Argo{}
		argo.Init(stateMock, newArgoApiMock(ctrl), metricsMock)

		task := models.Task{App: "test-app", Author: "author", Project: "project", Validated: true,
			Images: []models.Image{{Image: "app", Tag: "v1"}}}

		stateMock.EXPECT().GetTasks(gomock.Any()).
			Return([]models.Task{}, int64(0)).AnyTimes()
		stateMock.EXPECT().CancelInProgressTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(int64(0), nil).AnyTimes()
		stateMock.EXPECT().AddTask(gomock.Any()).Return(&models.Task{Id: "new-id", App: task.App}, nil)
		stateMock.EXPECT().ClaimTask("new-id").Return(nil).Times(1)
		metricsMock.EXPECT().AddAcceptedDeployment()

		created, err := argo.AddTask(task)
		require.NoError(t, err)
		assert.Equal(t, "new-id", created.Id)
	})

	t.Run("a claim that fails does not reject the deployment", func(t *testing.T) {
		stateMock := mocks.NewMockTaskRepository(ctrl)
		metricsMock := mocks.NewMockMetricsInterface(ctrl)

		argo := &Argo{}
		argo.Init(stateMock, newArgoApiMock(ctrl), metricsMock)

		task := models.Task{App: "test-app", Author: "author", Project: "project", Validated: true,
			Images: []models.Image{{Image: "app", Tag: "v1"}}}

		stateMock.EXPECT().GetTasks(gomock.Any()).
			Return([]models.Task{}, int64(0)).AnyTimes()
		stateMock.EXPECT().CancelInProgressTasks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(int64(0), nil).AnyTimes()
		stateMock.EXPECT().AddTask(gomock.Any()).Return(&models.Task{Id: "new-id", App: task.App}, nil)
		stateMock.EXPECT().ClaimTask("new-id").Return(errors.New("database unreachable"))
		metricsMock.EXPECT().AddAcceptedDeployment()

		created, err := argo.AddTask(task)
		require.NoError(t, err, "the claim is best-effort; a sweep picks the task up instead")
		assert.Equal(t, "new-id", created.Id)
	})
}
