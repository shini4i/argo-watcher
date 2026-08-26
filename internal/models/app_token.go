package models

// AppTokenRequest is the body of POST /api/v1/app-tokens. Apps and AllApps are
// mutually exclusive; the server rejects a request setting both.
type AppTokenRequest struct {
	// Apps names the applications the token may deploy.
	Apps []string `json:"apps,omitempty" binding:"max=200,dive,max=255"`
	// AllApps is the wildcard: every application, present and future.
	AllApps bool `json:"all_apps,omitempty"`
	// Description is free text identifying the holder, e.g. the pipeline name.
	Description string `json:"description,omitempty" binding:"max=255"`
	// ExpiresInDays bounds the token's life; zero means it never expires. The cap of
	// ten years rejects a mistyped value, and lives in the tag because a struct tag
	// cannot reference a constant.
	ExpiresInDays int `json:"expires_in_days,omitempty" binding:"min=0,max=3650"`
}

// AppTokenResponse describes an issued token. Secret is populated only in the
// response to the request that created it, which is the one time the token is
// ever shown; every other response carries Hint instead.
type AppTokenResponse struct {
	Id          string   `json:"id"`
	Apps        []string `json:"apps,omitempty"`
	AllApps     bool     `json:"all_apps"`
	Hint        string   `json:"hint"`
	Description string   `json:"description,omitempty"`
	CreatedBy   string   `json:"created_by"`
	// Timestamps are Unix milliseconds, matching how a task reports its own.
	CreatedAt  int64  `json:"created_at"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	RevokedAt  int64  `json:"revoked_at,omitempty"`
	LastUsedAt int64  `json:"last_used_at,omitempty"`
	Secret     string `json:"secret,omitempty"`
}

// UnknownUser attributes an action whose operator could not be named, so the
// audit trail records that it happened rather than dropping the row.
const UnknownUser = "unknown"
