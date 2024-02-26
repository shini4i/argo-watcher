package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCronLock(t *testing.T) {
	env := &Env{}

	t.Run("SetDeployLockCron", func(t *testing.T) {
		env.SetDeployLockCron(2 * time.Second)
		assert.Equal(t, true, env.deployLockSet)
	})

	t.Run("Sleep and check if the lock is released", func(t *testing.T) {
		time.Sleep(3 * time.Second)
		assert.Equal(t, false, env.deployLockSet)
	})
}
