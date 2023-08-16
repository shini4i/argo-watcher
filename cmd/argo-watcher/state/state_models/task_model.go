package state_models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/internal/models"
	"gorm.io/datatypes"
)

type TaskModel struct {
	Id              uuid.UUID                         `gorm:"column:id;type:uuid;not null;default:gen_random_uuid();"`
	Created         time.Time                         `gorm:"column:created;autoCreateTime;type:timestamp;not null;index;"`
	Updated         time.Time                         `gorm:"column:updated;autoUpdateTime;type:timestamp;not null;"`
	Images          datatypes.JSONSlice[models.Image] `gorm:"column:images;type:jsonb;not null;"`
	Status          string                            `gorm:"column:status;type:VARCHAR(20);not null;index;"`
	ApplicationName sql.NullString                    `gorm:"column:app;type:VARCHAR(255);not null;"`
	Author          sql.NullString                    `gorm:"column:author;type:VARCHAR(255);not null;"`
	Project         sql.NullString                    `gorm:"column:project;type:VARCHAR(255);not null;"`
	StatusReason    sql.NullString                    `gorm:"column:status_reason;"`
}

func (TaskModel) TableName() string {
	return "tasks"
}

func (ormTask *TaskModel) ConvertToExternalTask() *models.Task {
	return &models.Task{
		Id:           ormTask.Id.String(),
		Created:      float64(ormTask.Created.Unix()),
		Updated:      float64(ormTask.Updated.Unix()),
		App:          ormTask.ApplicationName.String,
		Author:       ormTask.Author.String,
		Project:      ormTask.Project.String,
		Images:       ormTask.Images,
		Status:       ormTask.Status,
		StatusReason: ormTask.StatusReason.String,
	}
}
