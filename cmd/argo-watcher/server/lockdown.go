package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type Lockdown struct {
	ManualLock   bool // used to manually lock the system
	OverrideMode bool // used to temporarily disable scheduled lockdowns
	Schedules    []LockdownSchedule
}

type LockdownSchedule struct {
	StartDay  time.Weekday
	StartHour int
	StartMin  int
	EndDay    time.Weekday
	EndHour   int
	EndMin    int
}

// parseSchedule parses a single schedule from a string and returns a LockdownSchedule struct.
func parseSchedule(schedule string) (LockdownSchedule, error) {
	times := strings.Split(strings.TrimSpace(schedule), "-")
	if len(times) != 2 {
		return LockdownSchedule{}, fmt.Errorf("invalid timeframe format")
	}

	startParts := strings.Split(strings.TrimSpace(times[0]), " ")
	endParts := strings.Split(strings.TrimSpace(times[1]), " ")

	if len(startParts) != 2 || len(endParts) != 2 {
		return LockdownSchedule{}, fmt.Errorf("invalid timeframe format")
	}

	startDay, err := dayToWeekday(startParts[0])
	if err != nil {
		return LockdownSchedule{}, err
	}

	startHour, startMin, err := parseTime(startParts[1])
	if err != nil {
		return LockdownSchedule{}, err
	}

	endDay, err := dayToWeekday(endParts[0])
	if err != nil {
		return LockdownSchedule{}, err
	}

	endHour, endMin, err := parseTime(endParts[1])
	if err != nil {
		return LockdownSchedule{}, err
	}

	return LockdownSchedule{
		StartDay:  startDay,
		StartHour: startHour,
		StartMin:  startMin,
		EndDay:    endDay,
		EndHour:   endHour,
		EndMin:    endMin,
	}, nil
}

// parseTime parses a time from a string and returns the hour and minute as integers.
func parseTime(timeStr string) (int, int, error) {
	timeParts := strings.Split(timeStr, ":")
	hour, err := strconv.Atoi(timeParts[0])
	if err != nil {
		return 0, 0, err
	}
	minutes, err := strconv.Atoi(timeParts[1])
	if err != nil {
		return 0, 0, err
	}
	return hour, minutes, nil
}

// Parse parses the lockdown schedules from a string and stores them in the Lockdown struct.
func (l *Lockdown) Parse(schedules string) error {
	timeFramesSplit := strings.Split(schedules, ",")

	for _, tf := range timeFramesSplit {
		schedule, err := parseSchedule(tf)
		if err != nil {
			return err
		}
		l.Schedules = append(l.Schedules, schedule)
	}

	log.Debug().Msgf("Parsed lockdown schedules: %v", l.Schedules)

	return nil
}

// IsLocked checks if the system is under lockdown. It returns true if either
// a manual lock is active or if the current time falls within a scheduled lockdown
// and no override mode is active. Otherwise, it returns false.
func (l *Lockdown) IsLocked() bool {
	if l.ManualLock {
		return true
	}

	if l.OverrideMode {
		return false
	}

	now := time.Now()

	for _, s := range l.Schedules {
		if timeWithinSchedule(now, s.StartDay, s.EndDay, s.StartHour, s.StartMin, s.EndHour, s.EndMin) {
			return true
		}
	}

	return false
}

// SetLock immediately places the system into manual lockdown mode.
// No matter what the scheduled lockdown settings are, once this method is invoked,
// the system is considered to be under lockdown until manually released.
func (l *Lockdown) SetLock() {
	l.ManualLock = true
}

// ReleaseLock cancels the manual lockdown. If a scheduled lockdown is active,
// it temporarily overrides it for the next 15 minutes. After that period,
// if the scheduled lockdown is still in progress, the system re-locks.
func (l *Lockdown) ReleaseLock() {
	l.ManualLock = false

	if l.IsLocked() {
		l.OverrideMode = true
		go func() {
			time.Sleep(15 * time.Minute)
			l.OverrideMode = false

			// we need to re-notify clients that the lock is in action again
			notifyWebSocketClients("locked")
		}()
	}
}

// NewLockdown initializes a new Lockdown structure and parses the lockdown schedules
// if provided. If the schedule parsing is successful, it returns the new Lockdown.
// Otherwise, it returns an error.
func NewLockdown(schedules string) (*Lockdown, error) {
	lockdown := &Lockdown{}
	if schedules != "" {
		if err := lockdown.Parse(schedules); err != nil {
			return nil, err
		}
	}
	return lockdown, nil
}

// timeWithinSchedule determines if a given time is within the specified schedule interval.
// The schedule wraps around to the next week if the end day is before the start day.
// Returns true if the given time falls within the schedule, otherwise returns false.
func timeWithinSchedule(now time.Time, startDay, endDay time.Weekday, startHour, startMin, endHour, endMin int) bool {
	// if endDay < startDay, it means the period extends to the next week
	weekdaysInOrder := endDay >= startDay

	if weekdaysInOrder && now.Weekday() >= startDay && now.Weekday() <= endDay ||
		!weekdaysInOrder && (now.Weekday() >= startDay || now.Weekday() <= endDay) {
		switch now.Weekday() {
		case startDay:
			// for starting day, time needs to be after start time
			if now.Hour() < startHour || (now.Hour() == startHour && now.Minute() < startMin) {
				return false
			}
		case endDay:
			// for ending day, time needs to be before end time
			if now.Hour() > endHour || (now.Hour() == endHour && now.Minute() >= endMin) {
				return false
			}
		}
		return true
	}

	return false
}

// dayToWeekday converts a three-letter abbreviation of a weekday (e.g., "Mon") to its corresponding time.Weekday enum value.
// If the input doesn't match a valid weekday abbreviation, it returns an error.
func dayToWeekday(day string) (time.Weekday, error) {
	switch day {
	case "Sun":
		return time.Sunday, nil
	case "Mon":
		return time.Monday, nil
	case "Tue":
		return time.Tuesday, nil
	case "Wed":
		return time.Wednesday, nil
	case "Thu":
		return time.Thursday, nil
	case "Fri":
		return time.Friday, nil
	case "Sat":
		return time.Saturday, nil
	default:
		return 0, fmt.Errorf("invalid day format")
	}
}
