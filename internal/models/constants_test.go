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
