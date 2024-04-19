package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLockdown_Parse(t *testing.T) {
	var testCases = []struct {
		input             string
		expectError       bool
		expectedSchedules []LockdownSchedule
	}{
		{"Fri 13:20 - Mon 06:30", false, []LockdownSchedule{
			{time.Friday, 13, 20, time.Monday, 6, 30},
		}},
		{"Fri 13:20 - Mon 06:30, Tue 03:00 - Thu 08:00", false, []LockdownSchedule{
			{time.Friday, 13, 20, time.Monday, 6, 30},
			{time.Tuesday, 3, 0, time.Thursday, 8, 0},
		}},
		{"13:20 - mon 06:30", true, nil},
		{"Fri - mon 06:30", true, nil},
		{"Fri 13:20 -", true, nil},
		{"", true, nil},
	}

	for _, tt := range testCases {
		l := Lockdown{}
		err := l.Parse(tt.input)

		if tt.expectError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedSchedules, l.Schedules)
		}
	}
}

func TestLockdown_SetLock_ReleaseLock(t *testing.T) {
	testCases := []struct {
		name         string
		action       func(l *Lockdown)
		expectedLock bool
	}{
		{
			name: "test setting the lock",
			action: func(l *Lockdown) {
				l.SetLock()
			},
			expectedLock: true,
		},
		{
			name: "test releasing the lock",
			action: func(l *Lockdown) {
				l.SetLock()
				l.ReleaseLock()
			},
			expectedLock: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			l := &Lockdown{}
			tt.action(l)
			isLocked := l.IsLocked()
			assert.Equal(t, tt.expectedLock, isLocked)
		})
	}
}
