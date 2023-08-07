package state_models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/shini4i/argo-watcher/internal/models"
	"gorm.io/datatypes"
)

type TaskModel struct {
	Id              uuid.UUID                         `gorm:"column:id;type:uuid;default:gen_random_uuid()"`
	Created         time.Time                         `gorm:"column:created;autoCreateTime;not null;index;"`
	Updated         time.Time                         `gorm:"column:updated;autoUpdateTime;not null;"`
	Images          datatypes.JSONSlice[models.Image] `gorm:"column:images;not null;"`
	Status          string                            `gorm:"column:status;type:VARCHAR(20);not null;index;"`
	ApplicationName sql.NullString                    `gorm:"column:app;type:VARCHAR(255);"`
	Author          sql.NullString                    `gorm:"column:author;type:VARCHAR(255);"`
	Project         sql.NullString                    `gorm:"column:project;type:VARCHAR(255);"`
	StatusReason    sql.NullString                    `gorm:"column:status_reason;default:''"`
}

func (TaskModel) TableName() string {
	return "tasks"
}
