package server

import (
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
)

// SetDeployLockCron is a function that sets a deploy lock and sends a notification to all active WebSocket clients.
// It first sets the deployLockSet field of the Env struct to true, indicating that a deploy lock is set.
// It then sends a "locked" message to all active WebSocket clients using the notifyWebSocketClients function.
// Finally, it starts a new goroutine that will release the deploy lock after a specified duration by calling the ReleaseDeployLockCron function.
// The duration parameter specifies the duration for which the deploy lock should be set.
func (env *Env) SetDeployLockCron(duration time.Duration) {
	log.Debug().Msgf("Setting automatic deploy lock for %s", duration.String())
	env.deployLockSet = true
	notifyWebSocketClients("locked")
	go env.ReleaseDeployLockCron(duration)
}

// ReleaseDeployLockCron is a function that releases a previously set deploy lock after a specified duration and sends a notification to all active WebSocket clients.
// It first sleeps for the duration specified by the releaseAfter parameter.
// After the sleep duration has passed, it sets the deployLockSet field of the Env struct to false, indicating that the deploy lock has been released.
// It then sends an "unlocked" message to all active WebSocket clients using the notifyWebSocketClients function.
func (env *Env) ReleaseDeployLockCron(releaseAfter time.Duration) {
	time.Sleep(releaseAfter)
	log.Debug().Msg("Releasing deploy lock")
	env.deployLockSet = false
	notifyWebSocketClients("unlocked")
}

// SetCron is a function that sets up cron jobs based on the provided lockdown schedules.
// It first creates a new cron.Cron instance.
// Then, for each schedule in the provided schedules, it parses the duration of the schedule and adds a new cron job to the cron.Cron instance.
// The cron job, when triggered, will call the SetDeployLockCron function with the parsed duration.
// If there's an error while parsing the duration or adding the cron job, it logs the error and returns.
// Finally, it starts the cron.Cron instance, which will start triggering the cron jobs at their specified times.
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
