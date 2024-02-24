package server

import (
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
)

func (env *Env) SetDeployLockCron(duration time.Duration) {
	log.Debug().Msgf("Setting automatic deploy lock for %s", duration.String())
	env.deployLockSet = true
	go env.ReleaseDeployLockCron(duration)
}

func (env *Env) ReleaseDeployLockCron(releaseAfter time.Duration) {
	time.Sleep(releaseAfter)
	log.Debug().Msg("Releasing deploy lock")
	env.deployLockSet = false
}

func (env *Env) SetCron(schedules config.LockdownSchedules) {
	log.Debug().Msg("Setting up cron jobs")

	c := cron.New()

	for _, schedule := range schedules {
		duration, err := time.ParseDuration(schedule.Duration)
		if err != nil {
			log.Error().Msgf("Couldn't parse duration for cron job. Got the following error: %s", err)
			return
		}

		_, err = c.AddFunc(schedule.Cron, func() { env.SetDeployLockCron(duration) })
		if err != nil {
			log.Error().Msgf("Couldn't add cron job to set deploy lock. Got the following error: %s", err)
		}
	}

	c.Start()
}
