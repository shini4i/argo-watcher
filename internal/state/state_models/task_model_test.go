package state_models

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/shini4i/argo-watcher/internal/models"
)

func storedTask() TaskModel {
	return TaskModel{
		Id:              uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Created:         time.Unix(1700000000, 0),
		Updated:         time.Unix(1700000060, 0),
		Images:          datatypes.NewJSONSlice([]models.Image{{Image: "app", Tag: "v1"}}),
		Status:          models.StatusInProgressMessage,
		ApplicationName: sql.NullString{String: "app", Valid: true},
		Author:          sql.NullString{String: "author", Valid: true},
		Project:         sql.NullString{String: "project", Valid: true},
		Validated:       true,
		Timeout:         900,
		Refresh:         sql.NullBool{Bool: true, Valid: true},
	}
}

func TestConvertToExternalTask_WithholdsAuthority(t *testing.T) {
	stored := storedTask()
	external := stored.ConvertToExternalTask()

	assert.False(t, external.Validated,
		"the API-facing task must not claim authority it cannot prove")
	// Timeout and Refresh steer the rollout, so folding them into the base
	// converter would hand them to every re-read and to API clients.
	assert.Zero(t, external.Timeout)
	assert.Nil(t, external.Refresh)
	assert.Equal(t, "app", external.App)
	assert.Equal(t, models.StatusInProgressMessage, external.Status)
}

func TestConvertToResumedTask_CarriesEverythingTheRolloutNeeds(t *testing.T) {
	stored := storedTask()
	resumed := stored.ConvertToResumedTask()

	require.NotNil(t, resumed)
	assert.True(t, resumed.Validated,
		"a resumed task that loses Validated has its git write-back skipped silently")
	assert.Equal(t, 900, resumed.Timeout,
		"a resumed task must keep its per-task deadline, not fall back to the instance default")
	require.NotNil(t, resumed.Refresh)
	assert.True(t, *resumed.Refresh)
	assert.Equal(t, "app", resumed.App)
	assert.Equal(t, []models.Image{{Image: "app", Tag: "v1"}}, []models.Image(resumed.Images))
}

func TestConvertToResumedTask_OmittedRefreshStaysNil(t *testing.T) {
	stored := storedTask()
	stored.Refresh = sql.NullBool{}

	resumed := stored.ConvertToResumedTask()

	assert.Nil(t, resumed.Refresh,
		"a NULL refresh means the client never overrode it, so the instance default applies")
}
