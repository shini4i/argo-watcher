package state_models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/internal/models"
	"gorm.io/datatypes"
)

type TaskModel struct {
	Id               uuid.UUID                         `gorm:"column:id;type:uuid;not null;default:gen_random_uuid();"`
	Created          time.Time                         `gorm:"column:created;autoCreateTime;not null;index;"`
	Updated          time.Time                         `gorm:"column:updated;autoUpdateTime;not null;"`
	Images           datatypes.JSONSlice[models.Image] `gorm:"column:images;type:jsonb;not null;"`
	Status           string                            `gorm:"column:status;type:VARCHAR(20);not null;index;"`
	ApplicationName  sql.NullString                    `gorm:"column:app;type:VARCHAR(255);not null;"`
	Author           sql.NullString                    `gorm:"column:author;type:VARCHAR(255);not null;"`
	Project          sql.NullString                    `gorm:"column:project;type:VARCHAR(255);not null;"`
	StatusReason     sql.NullString                    `gorm:"column:status_reason;"`
	IsRollback       bool                              `gorm:"column:is_rollback;not null;default:false;"`
	RollbackTargetId string                            `gorm:"column:rollback_target_id;not null;default:'';"`
	// Validated is persisted because CancelInProgressTasks weighs it against the
	// superseding task, which may be handled by another replica.
	Validated bool `gorm:"column:validated;not null;default:false;"`
	// Timeout is the per-task rollout deadline in seconds, 0 when the client did
	// not override the instance default. Persisted so a task resumed by another
	// replica keeps the deadline it was accepted with.
	Timeout int `gorm:"column:timeout;not null;default:0;"`
	// Refresh optionally overrides the instance-wide ARGO_REFRESH_APP setting.
	// NULL means the client omitted it, which is distinct from an explicit false.
	Refresh sql.NullBool `gorm:"column:refresh;"`
	// OwnerId names the replica currently monitoring this task, and
	// LeaseExpiresAt is the instant its claim lapses. A row whose lease has
	// expired is available for another replica to claim and resume.
	OwnerId        sql.NullString `gorm:"column:owner_id;"`
	LeaseExpiresAt sql.NullTime   `gorm:"column:lease_expires_at;"`
}

func (TaskModel) TableName() string {
	return "tasks"
}

// ConvertToExternalTask maps the row onto the API-facing task. The fields that
// grant authority or steer the rollout — Validated, Timeout, Refresh — are
// deliberately left out: this task is served to clients and re-read for status
// checks, neither of which may act on them. Resuming a rollout needs them, and
// uses ConvertToResumedTask instead.
func (ormTask *TaskModel) ConvertToExternalTask() *models.Task {
	return &models.Task{
		Id:               ormTask.Id.String(),
		Created:          float64(ormTask.Created.Unix()),
		Updated:          float64(ormTask.Updated.Unix()),
		App:              ormTask.ApplicationName.String,
		Author:           ormTask.Author.String,
		Project:          ormTask.Project.String,
		Images:           ormTask.Images,
		Status:           ormTask.Status,
		StatusReason:     ormTask.StatusReason.String,
		IsRollback:       ormTask.IsRollback,
		RollbackTargetId: ormTask.RollbackTargetId,
	}
}

// ConvertToResumedTask maps the row onto a task ready to be monitored again by a
// replica that claimed it after its previous owner stopped. Unlike
// ConvertToExternalTask it carries the fields the rollout acts on: Validated,
// without which UpdateIfNeeded would silently skip the git write-back; and the
// Timeout and Refresh overrides the deployment was accepted with, which would
// otherwise fall back to this replica's defaults.
func (ormTask *TaskModel) ConvertToResumedTask() *models.Task {
	task := ormTask.ConvertToExternalTask()
	task.Validated = ormTask.Validated
	task.Timeout = ormTask.Timeout
	if ormTask.Refresh.Valid {
		refresh := ormTask.Refresh.Bool
		task.Refresh = &refresh
	}

	return task
}
