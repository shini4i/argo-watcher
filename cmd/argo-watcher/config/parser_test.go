package config

import (
	"testing"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestLockdownSchedules_UnmarshalText(t *testing.T) {
	jsonString := `[
		{"cron": "*/2 * * * *", "duration": "2h"},
		{"cron": "*/20 * * * *", "duration": "3h"}
	]`
	expectedLockdownSchedules := LockdownSchedules{
		models.LockdownSchedule{
			Cron:     "*/2 * * * *",
			Duration: "2h",
		},
		models.LockdownSchedule{
			Cron:     "*/20 * * * *",
			Duration: "3h",
		},
	}

	var lockdownSchedules LockdownSchedules
	err := lockdownSchedules.UnmarshalText([]byte(jsonString))

	assert.NoError(t, err)
	assert.Equal(t, expectedLockdownSchedules, lockdownSchedules)
}
