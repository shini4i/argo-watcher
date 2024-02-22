package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCronLock(t *testing.T) {
	env := &Env{}

	t.Run("SetDeployLockCron", func(t *testing.T) {
		env := &Env{}
		env.SetDeployLockCron()
		assert.Equal(t, true, env.deployLockSet)

	})

	t.Run("ReleaseDeployLockCron", func(t *testing.T) {
		env.ReleaseDeployLockCron()
		assert.Equal(t, false, env.deployLockSet)
	})
}
