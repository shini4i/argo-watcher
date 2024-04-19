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

// Parse parses the lockdown schedules from a string and stores them in the Lockdown struct.
// Expected format: "Mon 08:00 - Tue 08:00, Wed 08:00 - Thu 08:00"
func (l *Lockdown) Parse(schedules string) error {
	timeFramesSplit := strings.Split(schedules, ",")

	for _, tf := range timeFramesSplit {
		times := strings.Split(strings.TrimSpace(tf), "-")
		if len(times) != 2 {
			return fmt.Errorf("invalid timeframe format")
		}

		startParts := strings.Split(strings.TrimSpace(times[0]), " ")
		endParts := strings.Split(strings.TrimSpace(times[1]), " ")

		if len(startParts) != 2 || len(endParts) != 2 {
			return fmt.Errorf("invalid timeframe format")
		}

		startDay, err := dayToWeekday(startParts[0])
		if err != nil {
			return err
		}

		startTimeParts := strings.Split(startParts[1], ":")
		startHour, err := strconv.Atoi(startTimeParts[0])
		if err != nil {
			return err
		}
		startMin, err := strconv.Atoi(startTimeParts[1])
		if err != nil {
			return err
		}

		endDay, err := dayToWeekday(endParts[0])
		if err != nil {
			return err
		}
		endTimeParts := strings.Split(endParts[1], ":")
		endHour, err := strconv.Atoi(endTimeParts[0])
		if err != nil {
			return err
		}
		endMin, err := strconv.Atoi(endTimeParts[1])
		if err != nil {
			return err
		}

		l.Schedules = append(l.Schedules, LockdownSchedule{
			StartDay:  startDay,
			StartHour: startHour,
			StartMin:  startMin,
			EndDay:    endDay,
			EndHour:   endHour,
			EndMin:    endMin,
		})
	}

	log.Debug().Msgf("Parsed lockdown schedules: %v", l.Schedules)

	return nil
}

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

func (l *Lockdown) SetLock() {
	l.ManualLock = true
}

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

func NewLockdown(schedules string) (*Lockdown, error) {
	lockdown := &Lockdown{}
	if schedules != "" {
		if err := lockdown.Parse(schedules); err != nil {
			return nil, err
		}
	}
	return lockdown, nil
}

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
