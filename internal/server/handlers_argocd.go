package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/shini4i/argo-watcher/internal/argocd"
)

// ReachabilityResponse reports ArgoCD/state-backend reachability to the frontend.
// Reason names which subsystem is unreachable (see argocd.Reason* constants) so
// the banner can be specific; it is empty (omitted) when Available is true.
type ReachabilityResponse struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// reachability godoc
// @Summary Report dependency reachability
// @Description Report whether argo-watcher can currently reach ArgoCD and its
// @Description state backend. Reflects the cached liveness-probe state and never
// @Description performs a live probe, so it is cheap to poll. The frontend uses
// @Description it to bootstrap the "unreachable" banner on connect; live changes
// @Description then arrive over the WebSocket. `available` is true when both are
// @Description reachable; `reason` names the unreachable subsystem otherwise.
// @Tags frontend
// @Produce json
// @Success 200 {object} ReachabilityResponse
// @Router /api/v1/reachability [get]
func (env *Env) reachability(c *gin.Context) {
	// Read the cached reason once and derive availability from it, so the
	// response is a snapshot of a single atomic load. Reading availability and
	// the reason separately could tear across a concurrent liveness-probe update
	// and yield an internally contradictory body to an external poller.
	reason := env.argo.UnavailableReason()
	c.JSON(http.StatusOK, ReachabilityResponse{
		Available: reason == argocd.ReasonNone,
		Reason:    reason,
	})
}
