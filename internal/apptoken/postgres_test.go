package apptoken

import (
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestStore connects to the migrated test database, skipped unless
// POSTGRES_DSN is set (migrations applied by task ci-migrate). Each test starts
// and ends with an empty table, so a run leaves no working credential behind.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN environment variable not set. Skipping integration test.")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	truncate := func() { require.NoError(t, db.Exec("DELETE FROM app_tokens").Error) }
	truncate()
	t.Cleanup(truncate)

	return db
}

func newTestStore(t *testing.T) Store {
	t.Helper()
	return NewPostgresStore(newTestDB(t))
}

func TestPostgresStoreIssueAndLookup(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{Apps: []string{"app1", "app2"}}, "ci pipeline", "alice", time.Time{})
	require.NoError(t, err)

	assert.True(t, HasPrefix(issued.Secret))
	assert.Equal(t, issued.Secret[len(issued.Secret)-hintLength:], issued.Hint)
	assert.Equal(t, "alice", issued.CreatedBy)
	assert.Equal(t, "ci pipeline", issued.Description)
	assert.Equal(t, []string{"app1", "app2"}, issued.Scope.Apps)
	assert.False(t, issued.Scope.AllApps)
	assert.NotEqual(t, uuid.Nil, issued.Id)
	assert.False(t, issued.CreatedAt.IsZero())
	assert.True(t, issued.ExpiresAt.IsZero(), "no expiry was requested")

	found, err := store.Lookup(issued.Secret)
	require.NoError(t, err)

	assert.Equal(t, issued.Id, found.Id)
	assert.Equal(t, []string{"app1", "app2"}, found.Scope.Apps)
	assert.True(t, found.Usable(time.Now()))
	assert.True(t, found.Scope.Allows("app1"))
	assert.False(t, found.Scope.Allows("app3"))
}

func TestPostgresStoreIssueWildcard(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{AllApps: true}, "", "alice", time.Time{})
	require.NoError(t, err)

	found, err := store.Lookup(issued.Secret)
	require.NoError(t, err)

	assert.True(t, found.Scope.AllApps)
	assert.Empty(t, found.Scope.Apps)
	assert.True(t, found.Scope.Allows("any-application-at-all"))
}

func TestPostgresStoreIssueRejectsAnUnstorableScope(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Issue(Scope{}, "", "alice", time.Time{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one application or to all of them")
}

func TestPostgresStoreIssuePersistsAnExpiry(t *testing.T) {
	store := newTestStore(t)
	expiry := time.Now().Add(time.Hour).Truncate(time.Millisecond)

	issued, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", expiry)
	require.NoError(t, err)

	found, err := store.Lookup(issued.Secret)
	require.NoError(t, err)

	assert.WithinDuration(t, expiry, found.ExpiresAt, time.Second)
	assert.True(t, found.Usable(time.Now()))
	assert.False(t, found.Usable(expiry.Add(time.Second)))
}

func TestPostgresStoreLookupUnknownSecret(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Lookup("awt_thisWasNeverIssued")

	require.ErrorIs(t, err, ErrNotFound)
}

func TestPostgresStoreLookupReturnsARevokedToken(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)
	require.NoError(t, store.Revoke(issued.Id))

	// A revoked token is still found, so the holder can be told it was withdrawn
	// rather than that it never existed.
	found, err := store.Lookup(issued.Secret)
	require.NoError(t, err)

	assert.False(t, found.RevokedAt.IsZero())
	assert.False(t, found.Usable(time.Now()))
}

func TestPostgresStoreRevoke(t *testing.T) {
	store := newTestStore(t)

	t.Run("reports an unknown id", func(t *testing.T) {
		require.ErrorIs(t, store.Revoke(uuid.New()), ErrNotFound)
	})

	t.Run("revoking twice keeps the first revocation time", func(t *testing.T) {
		issued, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
		require.NoError(t, err)

		require.NoError(t, store.Revoke(issued.Id))
		first, err := store.Lookup(issued.Secret)
		require.NoError(t, err)

		require.NoError(t, store.Revoke(issued.Id))
		second, err := store.Lookup(issued.Secret)
		require.NoError(t, err)

		assert.Equal(t, first.RevokedAt, second.RevokedAt)
	})
}

func TestPostgresStoreList(t *testing.T) {
	store := newTestStore(t)

	older, err := store.Issue(Scope{Apps: []string{"app1"}}, "older", "alice", time.Time{})
	require.NoError(t, err)
	newer, err := store.Issue(Scope{AllApps: true}, "newer", "bob", time.Time{})
	require.NoError(t, err)
	require.NoError(t, store.Revoke(older.Id))

	tokens, err := store.List()
	require.NoError(t, err)

	require.Len(t, tokens, 2)
	assert.Equal(t, newer.Id, tokens[0].Id, "the newest token comes first")
	assert.Equal(t, older.Id, tokens[1].Id)
	assert.False(t, tokens[1].RevokedAt.IsZero(), "a revoked token stays on the list")

	// The store cannot return a secret -- Token has no such field -- so assert the
	// contract Hint does have. The secret-leak guard lives at the API boundary.
	for index, token := range tokens {
		assert.Len(t, token.Hint, hintLength, "row %d", index)
	}
	assert.Equal(t, newer.Hint, tokens[0].Hint)
	assert.Equal(t, older.Hint, tokens[1].Hint)
}

func TestPostgresStoreMarkUsed(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)
	require.True(t, issued.LastUsedAt.IsZero())

	require.NoError(t, store.MarkUsed(issued.Id))

	found, err := store.Lookup(issued.Secret)
	require.NoError(t, err)
	assert.False(t, found.LastUsedAt.IsZero())
}

func TestPostgresStoreIssueProducesDistinctTokens(t *testing.T) {
	store := newTestStore(t)

	first, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)
	second, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)

	assert.NotEqual(t, first.Secret, second.Secret)
	assert.NotEqual(t, first.Id, second.Id)

	found, err := store.Lookup(first.Secret)
	require.NoError(t, err)
	assert.Equal(t, first.Id, found.Id)
}

func TestAppTokensScopeConstraint(t *testing.T) {
	db := newTestDB(t)

	// The hash travels as hex because gorm expands a []byte argument into a list of
	// values, which makes the statement itself malformed.
	insert := func(label, apps string, allApps bool) error {
		return db.Exec(`
			INSERT INTO app_tokens (token_hash, apps, all_apps, hint, created_by)
			VALUES (decode(?, 'hex'), CAST(? AS jsonb), ?, 'aaaa', 'test')`,
			hex.EncodeToString(Hash(label)), apps, allApps).Error
	}

	t.Run("accepts a wildcard with an empty array", func(t *testing.T) {
		require.NoError(t, insert("wildcard", `[]`, true))
	})

	t.Run("accepts a list without the wildcard", func(t *testing.T) {
		require.NoError(t, insert("scoped", `["app1"]`, false))
	})

	rejected := []struct {
		name    string
		apps    string
		allApps bool
	}{
		{name: "a wildcard that also lists applications", apps: `["app1"]`, allApps: true},
		{name: "neither a wildcard nor a list", apps: `[]`, allApps: false},
		// JSON null would make jsonb_array_length return SQL NULL, and a CHECK
		// evaluating to NULL passes — jsonb_typeof is what closes that hole.
		{name: "JSON null instead of an array", apps: `null`, allApps: true},
		{name: "an object instead of an array", apps: `{"app":"app1"}`, allApps: true},
	}

	for _, test := range rejected {
		t.Run("refuses "+test.name, func(t *testing.T) {
			err := insert(test.name, test.apps, test.allApps)

			// Naming the constraint keeps a malformed statement from passing as a
			// refusal, which is what hid a broken cast in this test once already.
			require.ErrorContains(t, err, "app_tokens_scope_exclusive")
		})
	}
}

// TestAppTokensHashIsUnique pins the UNIQUE index on token_hash, which is what makes
// Lookup's single indexed Take() deterministic: a duplicate row would leave which
// token a secret resolves to up to the planner.
func TestAppTokensHashIsUnique(t *testing.T) {
	db := newTestDB(t)

	insert := func() error {
		return db.Exec(`
			INSERT INTO app_tokens (token_hash, apps, all_apps, hint, created_by)
			VALUES (decode(?, 'hex'), CAST('["app1"]' AS jsonb), false, 'aaaa', 'test')`,
			hex.EncodeToString(Hash("awt_collision"))).Error
	}

	require.NoError(t, insert())

	// Naming the index keeps a malformed statement from passing as a refusal.
	require.ErrorContains(t, insert(), "app_tokens_token_hash_key")
}

// TestPostgresStoreRevokeIsSafeUnderContention pins Revoke's non-atomic
// UPDATE-then-existence-check. Two operators reacting to the same leaked credential
// must both be told it worked, and the first revocation time must stand.
func TestPostgresStoreRevokeIsSafeUnderContention(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{Apps: []string{"app1"}}, "", "alice", time.Time{})
	require.NoError(t, err)

	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := range racers {
		go func(index int) {
			defer wg.Done()
			errs[index] = store.Revoke(issued.Id)
		}(i)
	}
	wg.Wait()

	for index, err := range errs {
		// The loser of the race must not be told the token does not exist.
		assert.NoError(t, err, "racer %d", index)
	}

	revoked, err := store.Lookup(issued.Secret)
	require.NoError(t, err)
	require.False(t, revoked.RevokedAt.IsZero())
	assert.False(t, revoked.Usable(time.Now()))

	// A later revocation is a no-op that keeps the original instant.
	require.NoError(t, store.Revoke(issued.Id))
	again, err := store.Lookup(issued.Secret)
	require.NoError(t, err)
	assert.Equal(t, revoked.RevokedAt, again.RevokedAt)
}

// TestPostgresStoreConcurrentRevokeAndUse covers the pair that genuinely races in
// production: a revocation landing while the token is authorizing a deployment.
func TestPostgresStoreConcurrentRevokeAndUse(t *testing.T) {
	store := newTestStore(t)

	issued, err := store.Issue(Scope{AllApps: true}, "", "alice", time.Time{})
	require.NoError(t, err)

	var wg sync.WaitGroup
	var revokeErr, markErr error
	wg.Add(2)
	go func() { defer wg.Done(); revokeErr = store.Revoke(issued.Id) }()
	go func() { defer wg.Done(); markErr = store.MarkUsed(issued.Id) }()
	wg.Wait()

	assert.NoError(t, revokeErr)
	assert.NoError(t, markErr)

	stored, err := store.Lookup(issued.Secret)
	require.NoError(t, err)
	assert.False(t, stored.RevokedAt.IsZero(), "revocation must survive a concurrent use")
	assert.False(t, stored.Usable(time.Now()))
}

// TestPostgresStoreMarkUsedOnAnUnknownId pins the documented contract: bookkeeping
// for a row that is gone is not an error, because a failure here must never fail a
// deployment.
func TestPostgresStoreMarkUsedOnAnUnknownId(t *testing.T) {
	store := newTestStore(t)

	assert.NoError(t, store.MarkUsed(uuid.New()))
}
