package argocd

import (
	"errors"
	"time"

	"github.com/shini4i/argo-watcher/internal/models"
)

type DefaultRolloutMonitor struct {
	registryProxyUrl string
	acceptSuspended  bool
}

func NewDefaultRolloutMonitor(registryProxyUrl string, acceptSuspended bool) RolloutMonitor {
	return &DefaultRolloutMonitor{
		registryProxyUrl: registryProxyUrl,
		acceptSuspended:  acceptSuspended,
	}
}

func (m *DefaultRolloutMonitor) WaitForRollout(app *models.Application, images []string, timeout time.Duration) (*models.Application, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := m.GetRolloutStatus(app, images)
		if err != nil {
			return nil, err
		}

		if status == models.ArgoRolloutAppSuccess {
			return app, nil
		}

		if status == models.ArgoRolloutAppDegraded {
			return app, errors.New("application deployment degraded")
		}

		time.Sleep(time.Second * 5)
	}

	return app, errors.New("timeout waiting for rollout")
}

func (m *DefaultRolloutMonitor) GetRolloutStatus(app *models.Application, images []string) (string, error) {
	if app.IsFireAndForgetModeActive() {
		return models.ArgoRolloutAppSuccess, nil
	}

	return app.GetRolloutStatus(images, m.registryProxyUrl, m.acceptSuspended), nil
}
