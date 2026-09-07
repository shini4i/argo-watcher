package models

import "testing"

func TestIsAllowedTaskStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   bool
	}{
		{"cancelled is allowed", StatusCancelledMessage, true},
		{"in progress is allowed", StatusInProgressMessage, true},
		{"deployed is allowed", StatusDeployedMessage, true},
		{"unknown is rejected", "totally-bogus", false},
		{"empty is rejected", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAllowedTaskStatus(tc.status); got != tc.want {
				t.Errorf("IsAllowedTaskStatus(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// FailedTaskStatuses feeds the `status IN (?)` predicate behind the per-app
// failure counter, and web/src/features/tasks/utils/statusPresentation.tsx
// mirrors the same set. Narrowing either silently understates the count.
func TestFailedTaskStatuses(t *testing.T) {
	failed := []string{
		StatusFailedMessage,
		StatusAborted,
		StatusArgoCDUnavailableMessage,
		StatusConnectionUnavailable,
		StatusArgoCDFailedLogin,
	}

	for _, status := range failed {
		if !IsFailedTaskStatus(status) {
			t.Errorf("IsFailedTaskStatus(%q) = false, want true", status)
		}
	}

	// "app not found" is a misconfiguration and "cancelled" a supersession;
	// neither is a failed rollout.
	for _, status := range []string{
		StatusDeployedMessage,
		StatusInProgressMessage,
		StatusCancelledMessage,
		StatusAppNotFoundMessage,
		StatusAccepted,
		"",
	} {
		if IsFailedTaskStatus(status) {
			t.Errorf("IsFailedTaskStatus(%q) = true, want false", status)
		}
	}

	if got := FailedTaskStatuses(); len(got) != len(failed) {
		t.Errorf("FailedTaskStatuses() has %d entries, want %d", len(got), len(failed))
	}

	seen := make(map[string]bool, len(failed))
	for _, status := range FailedTaskStatuses() {
		seen[status] = true
	}
	for _, status := range failed {
		if !seen[status] {
			t.Errorf("FailedTaskStatuses() is missing %q", status)
		}
	}
}
