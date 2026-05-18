package updater

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPushRaceError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Negative cases
		{"nil", nil, false},
		{"unrelated error", errors.New("connection refused"), false},
		{"empty message", errors.New(""), false},
		// Ensure we don't false-positive on prose that incidentally contains
		// a marker word in a non-race context.
		{"false positive guard - fetch first in prose", errors.New("could not fetch first reference from remote"), false},

		// go-git's own wording when the remote rejects a non-FF push.
		{"go-git non-fast-forward", errors.New("non-fast-forward update: refs/heads/main"), true},

		// Verbatim string captured from argo-watcher's failure UI in the user's
		// GitLab-backed prod (pre-recovery-fix). Pin it so a future cleanup that
		// narrows pushRaceMarkers fails loudly.
		{"gitlab prod-observed (verbatim)", errors.New("command error on refs/heads/master: incorrect old value provided"), true},

		// Common receive-pack wordings from GitHub / GitLab / vanilla git.
		{"stale info", errors.New("failed to push some refs: ! [rejected] main -> main (stale info)"), true},
		{"fetch first - rejection line", errors.New("! [rejected] main -> main (fetch first)"), true},

		// git receive-pack concurrent ref-lock collision.
		{"cannot lock ref", errors.New("remote: error: cannot lock ref 'refs/heads/main': is at abc123 but expected def456"), true},

		// Gitea wording for a non-fast-forward push, captured from the integration
		// test suite running against gitea/gitea:1.22 under concurrent writers.
		{"gitea failed to update ref", errors.New("command error on refs/heads/master: failed to update ref"), true},

		// Capitalised variants (some transports uppercase the first word).
		{"capitalised stale info", errors.New("Stale info: refs/heads/main"), true},
		{"capitalised fetch first - rejection line", errors.New("! [rejected] main -> main (Fetch first)"), true},

		// Wrapped errors must still be detected.
		{"wrapped non-fast-forward", fmt.Errorf("push failed: %w", errors.New("non-fast-forward update")), true},
		{"wrapped incorrect old value", fmt.Errorf("push failed: %w", errors.New("incorrect old value provided")), true},

		// errors.Join concatenates messages with newlines; marker must still be found.
		{"joined errors", errors.Join(errors.New("unrelated"), errors.New("non-fast-forward update")), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPushRaceError(tc.err))
		})
	}
}

// TestMatchPushRaceMarkers_Extras covers the operator-supplied marker path used
// by (*GitRepo).IsPushRaceError. The built-in cases are already covered above
// (matchPushRaceMarkers with nil extras is the IsPushRaceError code path).
func TestMatchPushRaceMarkers_Extras(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		extra []string
		want  bool
	}{
		{"extra marker matches", errors.New("gerrit: change conflicts with master"), []string{"change conflicts"}, true},
		{"extra is additive — built-ins still match", errors.New("non-fast-forward update"), []string{"some unrelated marker"}, true},
		{"empty extras behave like nil", errors.New("non-fast-forward update"), []string{}, true},
		{"no match across built-ins or extras", errors.New("connection refused"), []string{"server panic"}, false},
		{"extras are case-insensitive (caller normalizes)", errors.New("SERVER PANIC: refusing push"), []string{"server panic"}, true},
		{"empty entries in extras are ignored", errors.New("connection refused"), []string{"", ""}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchPushRaceMarkers(tc.err, tc.extra))
		})
	}
}

func TestPushRaceMarkersNotEmpty(t *testing.T) {
	assert.NotEmpty(t, pushRaceMarkers, "pushRaceMarkers must not be empty or IsPushRaceError will never trigger")
}

func TestPushRaceMarkersAreLowercase(t *testing.T) {
	for _, marker := range pushRaceMarkers {
		assert.Equal(t, strings.ToLower(marker), marker,
			"marker %q must be lowercase — IsPushRaceError lowercases the haystack, not the needles", marker)
	}
}
