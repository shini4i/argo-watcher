package server

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/argo-watcher/internal/lock"
)

// newTestLockdown builds a Lockdown backed by an in-memory store, mirroring how
// a single-replica deployment is wired.
func newTestLockdown(t *testing.T, schedules string) *Lockdown {
	t.Helper()

	l, err := NewLockdown(schedules, lock.NewInMemoryDeployLockStore())
	require.NoError(t, err)
	return l
}

// scheduleAround builds a one-off schedule window offset from now, so tests can
// reproduce a scheduled lockdown that is still open or already closed. The wide
// margins keep the window on the correct side of "now" across hour/day/week
// boundaries.
func scheduleAround(startOffset, endOffset time.Duration) LockdownSchedule {
	now := time.Now()
	start := now.Add(startOffset)
	end := now.Add(endOffset)
	return LockdownSchedule{
		StartDay:  start.Weekday(),
		StartHour: start.Hour(),
		StartMin:  start.Minute(),
		EndDay:    end.Weekday(),
		EndHour:   end.Hour(),
		EndMin:    end.Minute(),
	}
}

// failingDeployLockStore is a DeployLockStore whose reads always fail, used to
// verify that an unreadable lock state is treated as locked.
type failingDeployLockStore struct{}

func (failingDeployLockStore) State() (lock.DeployLockState, error) {
	return lock.DeployLockState{}, errors.New("database is unreachable")
}
func (failingDeployLockStore) Lock() error { return errors.New("database is unreachable") }
func (failingDeployLockStore) Release(_ time.Time) error {
	return errors.New("database is unreachable")
}

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
		action       func(t *testing.T, l *Lockdown)
		expectedLock bool
	}{
		{
			name: "test setting the lock",
			action: func(t *testing.T, l *Lockdown) {
				require.NoError(t, l.SetLock())
			},
			expectedLock: true,
		},
		{
			name: "test releasing the lock",
			action: func(t *testing.T, l *Lockdown) {
				require.NoError(t, l.SetLock())
				require.NoError(t, l.ReleaseLock())
			},
			expectedLock: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLockdown(t, "")
			tt.action(t, l)
			isLocked := l.IsLocked()
			assert.Equal(t, tt.expectedLock, isLocked)
		})
	}
}

func TestLockdown_ReleaseLockOverride(t *testing.T) {
	t.Run("release overrides an active scheduled lockdown", func(t *testing.T) {
		l := newTestLockdown(t, "")
		l.Schedules = []LockdownSchedule{scheduleAround(-2*time.Minute, 2*time.Minute)}
		require.True(t, l.IsLocked(), "the schedule should lock the system")

		require.NoError(t, l.ReleaseLock())
		assert.False(t, l.IsLocked(), "the release should suppress the scheduled lockdown")
	})

	t.Run("the schedule takes effect again once the override expires", func(t *testing.T) {
		l := newTestLockdown(t, "")
		l.Schedules = []LockdownSchedule{scheduleAround(-2*time.Minute, 2*time.Minute)}
		l.overrideDuration = 10 * time.Millisecond

		require.NoError(t, l.ReleaseLock())
		require.False(t, l.IsLocked())

		time.Sleep(20 * time.Millisecond)
		assert.True(t, l.IsLocked(), "the scheduled lockdown should resume after the override deadline")
	})

	t.Run("no override is created without an active schedule", func(t *testing.T) {
		l := newTestLockdown(t, "")
		require.NoError(t, l.SetLock())
		require.NoError(t, l.ReleaseLock())

		state, err := l.store.State()
		require.NoError(t, err)
		assert.True(t, state.OverrideUntil.IsZero())
	})

	t.Run("setting the lock again drops the override", func(t *testing.T) {
		l := newTestLockdown(t, "")
		l.Schedules = []LockdownSchedule{scheduleAround(-2*time.Minute, 2*time.Minute)}

		require.NoError(t, l.ReleaseLock())
		require.False(t, l.IsLocked())

		require.NoError(t, l.SetLock())
		assert.True(t, l.IsLocked())
	})
}

// TestLockdown_IsLockedFailsClosed verifies that an unreadable shared lock state
// rejects deployments instead of silently letting them through during a freeze.
func TestLockdown_IsLockedFailsClosed(t *testing.T) {
	l := &Lockdown{store: failingDeployLockStore{}, overrideDuration: defaultOverrideDuration}
	assert.True(t, l.IsLocked())
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
			endHour:   10,
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
			_, err := NewLockdown(tt.schedules, lock.NewInMemoryDeployLockStore())
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLockdown_ConcurrentAccess(t *testing.T) {
	l := newTestLockdown(t, "")
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// assert, not require: require calls t.FailNow, which is only
			// valid on the goroutine running the test.
			for j := 0; j < 100; j++ {
				assert.NoError(t, l.SetLock())
				_ = l.IsLocked()
				assert.NoError(t, l.ReleaseLock())
				_ = l.IsLocked()
			}
		}()
	}

	wg.Wait()

	// No race conditions should have occurred (test passes if no data race detected with -race flag)
	assert.False(t, l.IsLocked())
}

func TestLockdown_WatchTransitions(t *testing.T) {
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
		l := newTestLockdown(t, "")
		stop := make(chan struct{})
		defer close(stop)

		// Establish a locked baseline, then let the watcher record it before
		// mutating state, so the transitions below are detected deterministically.
		require.NoError(t, l.SetLock())

		msgs := make(chan string, 4)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its baseline

		require.NoError(t, l.ReleaseLock())
		assert.Equal(t, "unlocked", recv(t, msgs))

		require.NoError(t, l.SetLock())
		assert.Equal(t, "locked", recv(t, msgs))
	})

	t.Run("notifies when another replica releases the lock", func(t *testing.T) {
		// The watcher is how clients connected to a replica that did not serve
		// the request learn about a lock change: it observes only the shared
		// state, not the local call.
		store := lock.NewInMemoryDeployLockStore()
		l, err := NewLockdown("", store)
		require.NoError(t, err)
		require.NoError(t, store.Lock())

		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 4)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its locked baseline

		require.NoError(t, store.Release(time.Time{}))

		assert.Equal(t, "unlocked", recv(t, msgs))
	})

	t.Run("notifies on a schedule-derived transition", func(t *testing.T) {
		l := newTestLockdown(t, "")
		l.Schedules = []LockdownSchedule{scheduleAround(-2*time.Minute, 2*time.Minute)}
		assert.True(t, l.IsLocked(), "schedule should lock the system at baseline")

		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 4)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })
		time.Sleep(20 * time.Millisecond) // allow the watcher to capture its locked baseline

		// An override suppresses the scheduled window, simulating its boundary.
		require.NoError(t, l.ReleaseLock())

		assert.Equal(t, "unlocked", recv(t, msgs))
	})

	t.Run("does not notify without a transition", func(t *testing.T) {
		l := newTestLockdown(t, "")
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 1)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })

		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification: %q", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("does not notify while the lock state is unreadable", func(t *testing.T) {
		// Enforcement fails closed on a read error, but the watcher must not
		// treat "unknown" as a transition — that would flap the banner between
		// locked and unlocked on every transient database error.
		l := &Lockdown{store: failingDeployLockStore{}, overrideDuration: defaultOverrideDuration}
		stop := make(chan struct{})
		defer close(stop)

		msgs := make(chan string, 1)
		go l.WatchTransitions(stop, 5*time.Millisecond, func(m string) { msgs <- m })

		select {
		case m := <-msgs:
			t.Fatalf("unexpected notification while the store was failing: %q", m)
		case <-time.After(50 * time.Millisecond):
		}
	})

	t.Run("stops when stop channel is closed", func(t *testing.T) {
		l := newTestLockdown(t, "")
		stop := make(chan struct{})
		done := make(chan struct{})

		go func() {
			l.WatchTransitions(stop, 5*time.Millisecond, func(string) {})
			close(done)
		}()

		close(stop)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("WatchTransitions did not return after stop was closed")
		}
	})
}

func TestLockdown_IsLockedWith(t *testing.T) {
	now := time.Now()
	openWindow := scheduleAround(-2*time.Minute, 2*time.Minute)
	closedWindow := scheduleAround(-4*time.Minute, -2*time.Minute)

	testCases := []struct {
		name      string
		schedules []LockdownSchedule
		state     lock.DeployLockState
		expected  bool
	}{
		{
			name:     "locked when the manual lock is set",
			state:    lock.DeployLockState{ManualLock: true},
			expected: true,
		},
		{
			name:      "unlocked while an override is pending",
			schedules: []LockdownSchedule{openWindow},
			state:     lock.DeployLockState{OverrideUntil: now.Add(time.Minute)},
			expected:  false,
		},
		{
			name:      "the manual lock outranks a pending override",
			schedules: []LockdownSchedule{openWindow},
			state:     lock.DeployLockState{ManualLock: true, OverrideUntil: now.Add(time.Minute)},
			expected:  true,
		},
		{
			name:      "the schedule takes effect again once the override expired",
			schedules: []LockdownSchedule{openWindow},
			state:     lock.DeployLockState{OverrideUntil: now.Add(-time.Minute)},
			expected:  true,
		},
		{
			// The override deadline is not a promise to re-lock: if the window
			// closed meanwhile, the system stays unlocked.
			name:      "unlocked when the schedule window closed during the override",
			schedules: []LockdownSchedule{closedWindow},
			state:     lock.DeployLockState{OverrideUntil: now.Add(-time.Minute)},
			expected:  false,
		},
		{
			name:     "unlocked with no manual lock and no schedule",
			expected: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLockdown(t, "")
			l.Schedules = tt.schedules
			assert.Equal(t, tt.expected, l.isLockedWith(tt.state, now))
		})
	}
}
