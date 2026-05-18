package updater

import "strings"

// pushRaceMarkers are lowercase substrings that different Git servers and
// protocol layers use to report the same underlying condition: a push was
// rejected because the expected old value of the target ref no longer matches
// the remote tip (i.e., another writer advanced the branch between our fetch
// and our push). Matching is done case-insensitively to guard against servers
// that capitalise differently.
//
// NOTE: go-git's push path uses fmt.Errorf (not %w) for these messages, so
// errors.Is against go-git sentinels does not work here — substring matching
// is intentional.
var pushRaceMarkers = []string{
	// go-git, when talking to a go-git bare remote.
	"non-fast-forward",
	// GitLab (prod-confirmed) / Gitea / Forgejo receive-pack wording.
	// Verbatim string observed in prod, surfaced via ArgoCD-prefixed failure UI:
	//   "ArgoCD API Error: command error on refs/heads/master: incorrect old value provided"
	"incorrect old value",
	// GitHub / GitLab / vanilla git receive-pack wordings.
	// "(fetch first)" matches the rejection line "! [rejected] main -> main (fetch first)"
	// and avoids false-positives on unrelated messages like "fetch first reference from remote".
	"stale info",
	"(fetch first)",
	// git receive-pack when two concurrent pushes race on the ref lock file.
	"cannot lock ref",
	// Gitea (observed in integration tests against gitea/gitea:1.22) when the
	// internal hook rejects a non-fast-forward update:
	//   "command error on refs/heads/master: failed to update ref"
	"failed to update ref",
}

// IsPushRaceError reports whether err describes a push rejected by the remote
// because the target ref advanced between our fetch and our push. When true,
// the safe recovery is to refresh the cache via fetch + reset (Clone on an
// existing cache), re-apply the change on top of the new tip, and retry the push.
//
// This checks only the built-in marker list. To also consult operator-supplied
// extras from EXTRA_PUSH_RACE_MARKERS, call (*GitRepo).IsPushRaceError instead.
func IsPushRaceError(err error) bool {
	return matchPushRaceMarkers(err, nil)
}

// matchPushRaceMarkers returns true when err's message contains any built-in
// marker or any entry in extra. Callers must pass extras already lowercased
// (see normalizeMarkers); the message is lowercased here.
func matchPushRaceMarkers(err error, extra []string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range pushRaceMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	for _, marker := range extra {
		if marker == "" {
			continue
		}
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
