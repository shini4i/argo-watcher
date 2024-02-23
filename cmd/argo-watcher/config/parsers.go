package config

import (
	"encoding/json"

	"github.com/shini4i/argo-watcher/internal/models"
)

type LockdownSchedules []models.LockdownSchedule

func (ls *LockdownSchedules) UnmarshalText(text []byte) error {
	temp := &[]models.LockdownSchedule{}
	if err := json.Unmarshal(text, temp); err != nil {
		return err
	}
	*ls = *temp
	return nil
}
