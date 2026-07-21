package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// argoStatus godoc
// @Summary Report ArgoCD reachability
// @Description Report whether argo-watcher can currently reach ArgoCD. Reflects
// @Description the cached liveness-probe state and never performs a live probe,
// @Description so it is cheap to poll. The frontend uses it to bootstrap the
// @Description "ArgoCD unreachable" banner on connect; live changes then arrive
// @Description over the WebSocket. Returns true when ArgoCD is reachable.
// @Tags frontend
// @Produce json
// @Success 200 {boolean} boolean
// @Router /api/v1/argocd-status [get]
func (env *Env) argoStatus(c *gin.Context) {
	c.JSON(http.StatusOK, env.argo.IsAvailable())
}
