package apptoken

import (
	"time"

	"github.com/google/uuid"
)

// IssuedToken is the result of minting a token: the stored metadata, plus the
// Secret, which exists in clear only here and in the response built from it.
type IssuedToken struct {
	Token
	Secret string
}

// Store persists application deploy tokens. There is deliberately no in-memory
// implementation: a token outlives the process holding it, so an in-memory store
// would revoke every token on restart and never agree between replicas. The
// feature is therefore available only with STATE_TYPE=postgres.
type Store interface {
	// Issue mints a token for the given scope. The scope is validated and
	// normalized first, so a caller cannot store one that matches nothing. A zero
	// expiresAt means the token never expires.
	Issue(scope Scope, description, createdBy string, expiresAt time.Time) (*IssuedToken, error)

	// Lookup finds the token a secret belongs to, returning ErrNotFound when none
	// does. It returns revoked and expired tokens too, so the caller can tell an
	// operator that their token was withdrawn rather than that it never existed;
	// callers must gate on Token.Usable before granting anything.
	Lookup(secret string) (*Token, error)

	// List returns every token, newest first, including revoked ones — the record
	// of what was withdrawn is part of what the list is for.
	List() ([]Token, error)

	// Revoke withdraws a token, keeping its row. Revoking an already-revoked token
	// succeeds without moving the original revocation time. It reports ErrNotFound
	// when no token has that id.
	Revoke(id uuid.UUID) error

	// MarkUsed records that the token authorized a deployment just now. An id that no
	// longer exists is not an error: this is bookkeeping, and it must never be able to
	// fail a deployment.
	MarkUsed(id uuid.UUID) error
}
