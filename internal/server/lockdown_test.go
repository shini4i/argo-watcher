package server

import (
	"sync"
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

func TestTimeWithinSchedule(t *testing.T) {
	tt := []struct {
		name      string
		now       time.Time
		startDay  time.Weekday
		endDay    time.Weekday
		startHour int
		startMin  int
		endHour   int
		endMin    int
		expected  bool
	}{
		{
			name:      "Thursday 14:00, within lockdown hours",
			now:       time.Date(2022, time.October, 20, 14, 0, 0, 0, time.UTC), // Thursday
			startDay:  time.Monday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  true,
		},
		{
			name:      "Thursday 20:00, within lockdown hours",
			now:       time.Date(2022, time.October, 20, 20, 0, 0, 0, time.UTC), // Thursday
			startDay:  time.Monday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  true,
		},
		{
			name:      "Sunday 20:00, within lockdown hours",
			now:       time.Date(2022, time.October, 23, 20, 0, 0, 0, time.UTC), // Sunday
			startDay:  time.Friday,
			endDay:    time.Monday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  true,
		},
		{
			name:      "Saturday 8:00, within lockdown hours",
			now:       time.Date(2022, time.October, 22, 8, 0, 0, 0, time.UTC), // Saturday
			startDay:  time.Friday,
			endDay:    time.Monday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  true,
		},
		{
			name:      "Saturday 18:00, outside lockdown hours",
			now:       time.Date(2022, time.October, 22, 18, 0, 0, 0, time.UTC), // Saturday at 18:00
			startDay:  time.Monday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  false,
		},
		{
			name:      "Thursday 8:00, inside lockdown hours",
			now:       time.Date(2022, time.October, 20, 8, 0, 0, 0, time.UTC), // Thursday at 08:00
			startDay:  time.Monday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  true,
		},
		{
			name:      "Friday 8:00, outside lockdown hours",
			now:       time.Date(2022, time.November, 4, 8, 0, 0, 0, time.UTC), // Friday
			startDay:  time.Friday,
			endDay:    time.Monday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  false,
		},
		{
			name:      "Sunday 8:00, outside lockdown hours",
			now:       time.Date(2022, time.October, 23, 8, 0, 0, 0, time.UTC), // Sunday
			startDay:  time.Monday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  false,
		},
		{
			name:      "Tuesday 10:00, outside lockdown hours",
			now:       time.Date(2022, time.October, 25, 10, 0, 0, 0, time.UTC), // Tuesday
			startDay:  time.Wednesday,
			endDay:    time.Friday,
			startHour: 9,
			startMin:  0,
			endHour:   17,
			endMin:    0,
			expected:  false,
		},
		{
			name:      "same day start and end, before end time",
			now:       time.Date(2024, 11, 24, 10, 4, 0, 0, time.UTC), // Sun 10:04
			startDay:  time.Sunday,
			endDay:    time.Sunday,
			startHour: 10,
			startMin:  0,
			endHour:   10,
			endMin:    5,
			expected:  true,
		},
		{
			name:      "same day start and end, outside lockdown hours",
			now:       time.Date(2024, 11, 24, 10, 6, 0, 0, time.UTC),
			startDay:  time.Sunday,
			endDay:    time.Sunday,
			startHour: 10,
			startMin:  0,
			endHour:   10,
			endMin:    5,
			expected:  false,
		},
		{
			name:      "across two days lockdown, current time is in next day and after end time",
			now:       time.Date(2024, 11, 25, 11, 0, 0, 0, time.UTC), // next day Mon 11:00
			startDay:  time.Sunday,
			endDay:    time.Monday,
			startHour: 10,
			startMin:  0,
			endHour:   10, // end time is earlier than current time in hours
			endMin:    30,
			expected:  false, // because 11:00 on Monday is after the end time on Monday
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			res := timeWithinSchedule(tc.now, tc.startDay, tc.endDay, tc.startHour, tc.startMin, tc.endHour, tc.endMin)
			assert.Equal(t, res, tc.expected)
		})
	}
}

func TestDayToWeekday(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected time.Weekday
		hasError bool
	}{
		{"Sunday", "Sun", time.Sunday, false},
		{"Monday", "Mon", time.Monday, false},
		{"Tuesday", "Tue", time.Tuesday, false},
		{"Wednesday", "Wed", time.Wednesday, false},
		{"Thursday", "Thu", time.Thursday, false},
		{"Friday", "Fri", time.Friday, false},
		{"Saturday", "Sat", time.Saturday, false},
		{"Invalid", "Invalid", 0, true},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := dayToWeekday(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNewLockdown(t *testing.T) {
	testCases := []struct {
		name      string
		schedules string
		hasError  bool
	}{
		{"Configured Schedule", "Mon 08:00 - Tue 08:00, Wed 08:00 - Thu 08:00", false},
		{"Blank Schedule", "", false},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLockdown(tt.schedules)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLockdown_ConcurrentAccess verifies that the lockdown struct is thread-safe.
func TestLockdown_ConcurrentAccess(t *testing.T) {
	l := &Lockdown{}
	var wg sync.WaitGroup

	// Multiple goroutines setting and releasing locks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.SetLock()
				_ = l.IsLocked()
				l.ReleaseLock()
				_ = l.IsLocked()
			}
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// No race conditions should have occurred (test passes if no data race detected with -race flag)
	assert.False(t, l.IsLocked())
}

// TestLockdown_WatchTransitions verifies that the watcher notifies clients only
// when the computed lock state actually changes, and that it stops on request.
func TestLockdown_WatchTransitions(t *testing.T) {
	// recv waits for a single message or fails the test on timeout.
	recv := func(t *testing.T, msgs <-chan string) string {
		t.Helper()
		select {
		case m := <-msgs:
			return m
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for notification")
			return ""
		}
	}

	t.Run("notifies on lock state transitions", func(t *testing.T) {
		l := &Lockdown{}
		stop := make(chan struct{})
		defer close(stop)

		// Establish a locked baseline, then let the watcher record it before
		// mutating state, so the transitions below are detected deterministically.
		l.SetLock()

		msgs := make(chan string, 4)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its baseline

		l.ReleaseLock()
		assert.Equal(t, "unlocked", recv(t, msgs))

		l.SetLock()
		assert.Equal(t, "locked", recv(t, msgs))
	})

	t.Run("notifies on a schedule-derived transition", func(t *testing.T) {
		// Build a schedule window spanning four minutes around now so IsLocked
		// is true via the schedule (not ManualLock) at baseline. The wide margin
		// keeps the window covering "now" across hour/day/week boundaries.
		now := time.Now()
		start := now.Add(-2 * time.Minute)
		end := now.Add(2 * time.Minute)
		l := &Lockdown{Schedules: []LockdownSchedule{{
			StartDay:  start.Weekday(),
			StartHour: start.Hour(),
			StartMin:  start.Minute(),
			EndDay:    end.Weekday(),
			EndHour:   end.Hour(),
			EndMin:    end.Minute(),
		}}}
		assert.True(t, l.IsLocked(), "schedule should lock the system at baseline")

		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 4)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its locked baseline

		// Enabling override mode makes isLockedInternal return false without
		// touching ManualLock, simulating a scheduled window boundary.
		l.mu.Lock()
		l.OverrideMode = true
		l.mu.Unlock()

		assert.Equal(t, "unlocked", recv(t, msgs))
	})

	t.Run("does not notify without a transition", func(t *testing.T) {
		l := &Lockdown{}
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 1)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })

		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification: %q", m)
		case <-time.After(50 * time.Millisecond):
			// no transition occurred, so no notification is expected
		}
	})

	t.Run("stops when stop channel is closed", func(t *testing.T) {
		l := &Lockdown{}
		stop := make(chan struct{})
		done := make(chan struct{})

		go func() {
			l.WatchTransitions(stop, 5*time.Millisecond, func(string) {})
			close(done)
		}()

		close(stop)

		select {
		case <-done:
			// returned as expected
		case <-time.After(time.Second):
			t.Fatal("WatchTransitions did not return after stop was closed")
		}
	})
}

// TestLockdown_ExpireOverride verifies that once the override window ends, the
// system re-notifies clients with "locked" only when it is genuinely still
// locked, and stays silent when the lock has meanwhile been lifted.
func TestLockdown_ExpireOverride(t *testing.T) {
	// scheduleAround builds a one-off schedule window offset from now, so tests
	// can reproduce a scheduled lockdown that is still open or already closed.
	// The wide margins keep the window on the correct side of "now" across
	// hour/day/week boundaries.
	scheduleAround := func(startOffset, endOffset time.Duration) LockdownSchedule {
		start := time.Now().Add(startOffset)
		end := time.Now().Add(endOffset)
		return LockdownSchedule{
			StartDay:  start.Weekday(),
			StartHour: start.Hour(),
			StartMin:  start.Minute(),
			EndDay:    end.Weekday(),
			EndHour:   end.Hour(),
			EndMin:    end.Minute(),
		}
	}

	t.Run("notifies locked when the scheduled window is still open", func(t *testing.T) {
		// Mirrors the production path: ReleaseLock clears ManualLock, so the only
		// way the system is still locked after the override expires is an active
		// schedule window. Once OverrideMode clears, that window must re-notify.
		l := &Lockdown{OverrideMode: true, Schedules: []LockdownSchedule{
			scheduleAround(-2*time.Minute, 2*time.Minute),
		}}

		msgs := make(chan string, 1)
		l.expireOverride(time.Millisecond, func(m string) { msgs <- m })

		assert.False(t, l.OverrideMode, "override should be cleared")
		select {
		case m := <-msgs:
			assert.Equal(t, "locked", m)
		case <-time.After(time.Second):
			t.Fatal("expected a 'locked' notification")
		}
	})

	t.Run("stays silent when the scheduled window closed during the override", func(t *testing.T) {
		// The exact bug scenario: the schedule window elapsed while the override
		// was active, so once OverrideMode clears the system is unlocked and no
		// stale "locked" message must be sent.
		l := &Lockdown{OverrideMode: true, Schedules: []LockdownSchedule{
			scheduleAround(-4*time.Minute, -2*time.Minute),
		}}

		msgs := make(chan string, 1)
		l.expireOverride(time.Millisecond, func(m string) { msgs <- m })

		assert.False(t, l.OverrideMode, "override should be cleared")
		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification: %q", m)
		case <-time.After(50 * time.Millisecond):
			// no notification is the expected outcome
		}
	})
}

// TestLockdown_IsLockedInternal tests the internal lock checking logic.
func TestLockdown_IsLockedInternal(t *testing.T) {
	t.Run("returns true when ManualLock is set", func(t *testing.T) {
		l := &Lockdown{ManualLock: true}
		assert.True(t, l.IsLocked())
	})

	t.Run("returns false when OverrideMode is set without ManualLock", func(t *testing.T) {
		l := &Lockdown{OverrideMode: true}
		assert.False(t, l.IsLocked())
	})

	t.Run("ManualLock takes precedence over OverrideMode", func(t *testing.T) {
		l := &Lockdown{ManualLock: true, OverrideMode: true}
		assert.True(t, l.IsLocked())
	})
}
