package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/argocd"
	"github.com/shini4i/argo-watcher/internal/auth"
	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/lock"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/prometheus"
)

// submitWithSlots posts one task through the real router with the given anonymous
// rollout slots. The insert is failed on purpose so no rollout goroutine starts; the
// returned bool reports whether the handler got as far as storing the task, and the int
// how many slots were taken when it did.
func submitWithSlots(t *testing.T, slots chan struct{}, deployToken string) (*httptest.ResponseRecorder, bool, int) {
	t.Helper()

	lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	repo.EXPECT().Check().Return(true).AnyTimes()

	stored := false
	heldAtInsert := 0
	repo.EXPECT().SupersedeAndAdd(gomock.Any(), gomock.Any()).
		DoAndReturn(func(task models.Task, _ string) (*models.Task, int64, error) {
			stored = true
			heldAtInsert = len(slots)
			return nil, 0, fmt.Errorf("stop before the rollout goroutine")
		}).AnyTimes()

	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), newMetrics(ctrl))

	strategies := map[string]auth.AuthStrategy{
		"ARGO_WATCHER_DEPLOY_TOKEN": auth.NewDeployTokenAuthService("deploy-secret"),
	}
	env := &Env{
		lockdown:          lockdown,
		strategies:        strategies,
		authenticator:     auth.NewAuthenticator(strategies),
		argo:              argo,
		config:            &config.ServerConfig{DeploymentTimeout: 900, StaticFilePath: t.TempDir()},
		metrics:           prometheus.NewMetrics(promclient.NewRegistry()),
		anonymousRollouts: slots,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskPayload("")))
	req.Header.Set("Content-Type", "application/json")
	if deployToken != "" {
		req.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", deployToken)
	}
	w := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(w, req)

	return w, stored, heldAtInsert
}

func fullSlots(n int) chan struct{} {
	slots := newRolloutSlots(uint(n))
	for range n {
		slots <- struct{}{}
	}
	return slots
}

func TestNewRolloutSlots(t *testing.T) {
	assert.Nil(t, newRolloutSlots(0), "zero means no cap")
	assert.Equal(t, 3, cap(newRolloutSlots(3)))
}

func TestAddTaskAnonymousRolloutCap(t *testing.T) {
	t.Run("an anonymous task over the cap is refused before it is stored", func(t *testing.T) {
		w, stored, _ := submitWithSlots(t, fullSlots(1), "")

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.False(t, stored)
		var status models.TaskStatus
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
		assert.Equal(t, "rejected", status.Status)
		assert.Contains(t, status.Error, "MAX_ANONYMOUS_ROLLOUTS")
	})

	t.Run("a credentialed task is never refused by the cap", func(t *testing.T) {
		_, stored, _ := submitWithSlots(t, fullSlots(1), "deploy-secret")
		assert.True(t, stored)
	})

	t.Run("no cap admits anonymous tasks", func(t *testing.T) {
		_, stored, _ := submitWithSlots(t, nil, "")
		assert.True(t, stored)
	})

	t.Run("a failed insert gives the slot back", func(t *testing.T) {
		slots := newRolloutSlots(1)
		_, stored, heldAtInsert := submitWithSlots(t, slots, "")

		assert.True(t, stored)
		assert.Equal(t, 1, heldAtInsert, "the slot is taken before the insert")
		assert.Equal(t, 0, len(slots))
	})
}

// TestAddTaskReleasesSlotWhenRolloutEnds pins that an admitted task gives its slot back
// once its rollout goroutine returns; a leaked slot would turn every later anonymous
// submission into a 429. The replica is draining, so the rollout ends at once.
func TestAddTaskReleasesSlotWhenRolloutEnds(t *testing.T) {
	lockdown, err := NewLockdown("", lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	repo, _ := newRepo(ctrl)
	repo.EXPECT().Check().Return(true).AnyTimes()
	repo.EXPECT().SupersedeAndAdd(gomock.Any(), gomock.Any()).
		DoAndReturn(func(task models.Task, _ string) (*models.Task, int64, error) {
			task.Id = "task-id"
			return &task, 0, nil
		})
	repo.EXPECT().GetTask(gomock.Any()).Return(&models.Task{Status: models.StatusInProgressMessage}, nil).AnyTimes()
	repo.EXPECT().ClaimTask(gomock.Any()).Return(nil).AnyTimes()
	repo.EXPECT().RenewLease(gomock.Any()).Return(true, nil).AnyTimes()
	repo.EXPECT().SetTaskStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	argo := &argocd.Argo{}
	argo.Init(repo, newArgoAPI(ctrl), prometheus.NewMetrics(promclient.NewRegistry()))

	updater := &argocd.ArgoStatusUpdater{}
	require.NoError(t, updater.Init(*argo, argocd.ArgoStatusUpdaterConfig{Locker: lock.NewInMemoryLocker(), RetryAttempts: 1}))

	strategies := map[string]auth.AuthStrategy{}
	slots := newRolloutSlots(1)
	env := &Env{
		lockdown:          lockdown,
		strategies:        strategies,
		authenticator:     auth.NewAuthenticator(strategies),
		argo:              argo,
		updater:           updater,
		config:            &config.ServerConfig{DeploymentTimeout: 900, StaticFilePath: t.TempDir(), StateType: "postgres"},
		metrics:           prometheus.NewMetrics(promclient.NewRegistry()),
		anonymousRollouts: slots,
	}
	env.draining.Store(true)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(taskPayload("")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.CreateRouter().ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	assert.Eventually(t, func() bool { return len(slots) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestNewEnvBuildsAnonymousRolloutSlots(t *testing.T) {
	serverConfig := &config.ServerConfig{MaxAnonymousRollouts: 2}

	env, err := NewEnv(serverConfig, &argocd.Argo{}, &prometheus.Metrics{}, &argocd.ArgoStatusUpdater{},
		lock.NewInMemoryDeployLockStore(), nil)

	require.NoError(t, err)
	assert.Equal(t, 2, cap(env.anonymousRollouts))
}
