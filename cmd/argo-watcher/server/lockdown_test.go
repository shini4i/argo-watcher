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
