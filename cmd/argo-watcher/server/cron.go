package server

import (
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

func (env *Env) SetDeployLockCron() {
	log.Debug().Msg("Setting deploy lock from cron job")
	env.deployLockSet = true
}

func (env *Env) ReleaseDeployLockCron() {
	log.Debug().Msg("Releasing deploy lock from cron job")
	env.deployLockSet = false
}

func (env *Env) SetCron() {
	log.Debug().Msg("Setting up cron jobs")

	c := cron.New()

	_, err := c.AddFunc("*/2 * * * *", func() { env.SetDeployLockCron() })
	if err != nil {
		log.Error().Msgf("Couldn't add cron job to set deploy lock. Got the following error: %s", err)
	}

	_, err = c.AddFunc("*/3 * * * *", func() { env.ReleaseDeployLockCron() })
	if err != nil {
		log.Error().Msgf("Couldn't add cron job to release deploy lock. Got the following error: %s", err)
	}

	c.Start()
}
