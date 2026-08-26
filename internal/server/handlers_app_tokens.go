package server

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/shini4i/argo-watcher/internal/apptoken"
	"github.com/shini4i/argo-watcher/internal/models"
)

// appTokensEndpoint is the collection of application deploy tokens.
const appTokensEndpoint = "/app-tokens" // #nosec G101 - a route path, not a credential

// listAppTokens godoc
// @Summary List application deploy tokens
// @Description List every issued token, revoked ones included. Requires membership of OIDC_PRIVILEGED_GROUPS. Never returns a token's secret.
// @Tags frontend
// @Produce json
// @Success 200 {array} models.AppTokenResponse
// @Failure 401 {object} models.TaskStatus
// @Failure 503 {object} models.TaskStatus
// @Router /api/v1/app-tokens [get]
func (env *Env) listAppTokens(w http.ResponseWriter, r *http.Request) {
	if !env.requireOIDCAuth(w, r) {
		return
	}

	tokens, err := env.appTokens.List()
	if err != nil {
		slog.Error("failed to list application deploy tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.TaskStatus{
			Status: "failed to list application deploy tokens",
			Error:  internalErrorMessage,
		})
		return
	}

	payload := make([]models.AppTokenResponse, 0, len(tokens))
	for index := range tokens {
		payload = append(payload, appTokenPayload(&tokens[index], ""))
	}

	writeJSON(w, http.StatusOK, payload)
}

// issueAppToken godoc
// @Summary Issue an application deploy token
// @Description Mint a token for a set of applications, or for all of them. Requires membership of OIDC_PRIVILEGED_GROUPS. The secret is returned once and never again.
// @Tags frontend
// @Accept json
// @Produce json
// @Param request body models.AppTokenRequest true "Token scope"
// @Success 201 {object} models.AppTokenResponse
// @Failure 406 {object} models.TaskStatus "the scope or the payload was refused"
// @Failure 401 {object} models.TaskStatus
// @Failure 503 {object} models.TaskStatus
// @Router /api/v1/app-tokens [post]
func (env *Env) issueAppToken(w http.ResponseWriter, r *http.Request) {
	if !env.requireOIDCAuth(w, r) {
		return
	}

	var request models.AppTokenRequest
	if err := bindJSON(r, &request); err != nil {
		slog.Warn("failed to parse an application deploy token request", "error", err)
		writeJSON(w, payloadRejectionStatus(err), models.TaskStatus{
			Status: "invalid payload",
			Error:  err.Error(),
		})
		return
	}

	scope := apptoken.Scope{Apps: request.Apps, AllApps: request.AllApps}
	if err := scope.Validate(); err != nil {
		// The same code the rest of the API answers for a body it will not accept.
		writeJSON(w, http.StatusNotAcceptable, models.TaskStatus{
			Status: "invalid scope",
			Error:  err.Error(),
		})
		return
	}

	var expiresAt time.Time
	if request.ExpiresInDays > 0 {
		expiresAt = time.Now().AddDate(0, 0, request.ExpiresInDays)
	}

	issued, err := env.appTokens.Issue(scope, request.Description, env.requestUsername(r), expiresAt)
	if err != nil {
		slog.Error("failed to issue an application deploy token", "error", err)
		writeJSON(w, http.StatusInternalServerError, models.TaskStatus{
			Status: "failed to issue an application deploy token",
			Error:  internalErrorMessage,
		})
		return
	}

	slog.Info("issued an application deploy token",
		"token_id", issued.Id, "scope", scope.String(), "created_by", issued.CreatedBy)

	// This is the only response that ever carries the secret.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, appTokenPayload(&issued.Token, issued.Secret))
}

// revokeAppToken godoc
// @Summary Revoke an application deploy token
// @Description Withdraw a token, effective on its next use. Requires membership of OIDC_PRIVILEGED_GROUPS.
// @Tags frontend
// @Produce json
// @Param id path string true "Token id"
// @Success 200 {string} string
// @Failure 400 {object} models.TaskStatus
// @Failure 401 {object} models.TaskStatus
// @Failure 404 {object} models.TaskStatus
// @Failure 503 {object} models.TaskStatus
// @Router /api/v1/app-tokens/{id} [delete]
func (env *Env) revokeAppToken(w http.ResponseWriter, r *http.Request) {
	if !env.requireOIDCAuth(w, r) {
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, models.TaskStatus{
			Status: "invalid token id",
			Error:  "the token id must be a UUID",
		})
		return
	}

	err = env.appTokens.Revoke(id)
	if errors.Is(err, apptoken.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, models.TaskStatus{
			Status: "not found",
			Error:  "no such application deploy token",
		})
		return
	}
	if err != nil {
		slog.Error("failed to revoke an application deploy token", "token_id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, models.TaskStatus{
			Status: "failed to revoke the application deploy token",
			Error:  internalErrorMessage,
		})
		return
	}

	slog.Info("revoked an application deploy token", "token_id", id, "revoked_by", env.requestUsername(r))

	writeJSON(w, http.StatusOK, "application deploy token revoked")
}

// requestUsername resolves who is making a privileged request, for attribution.
// It is best-effort: the request is already authorized by the time it is called,
// so an operator the provider will not name must not fail the action.
func (env *Env) requestUsername(r *http.Request) string {
	username, err := env.authenticator.IdentifyRequest(r, oidcHeader)
	if err != nil || username == "" {
		slog.Debug("could not attribute a privileged action to a user", "error", err)
		return models.UnknownUser
	}

	return username
}

// appTokenPayload renders a token for the API. secret is empty everywhere except
// the response to the request that created the token.
func appTokenPayload(token *apptoken.Token, secret string) models.AppTokenResponse {
	return models.AppTokenResponse{
		Id:          token.Id.String(),
		Apps:        token.Scope.Apps,
		AllApps:     token.Scope.AllApps,
		Hint:        token.Hint,
		Description: token.Description,
		CreatedBy:   token.CreatedBy,
		CreatedAt:   unixMillis(token.CreatedAt),
		ExpiresAt:   unixMillis(token.ExpiresAt),
		RevokedAt:   unixMillis(token.RevokedAt),
		LastUsedAt:  unixMillis(token.LastUsedAt),
		Secret:      secret,
	}
}

// unixMillis renders a timestamp for the API, mapping the zero time to 0 so an
// absent expiry or revocation is omitted from the JSON rather than sent as a date
// in year one.
func unixMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}

	return value.UnixMilli()
}
