package server

import (
	"fmt"
	"strings"
	"time"
)

type Lockdown struct {
	ManualLock  bool
	CurrentLock string
	Schedules   []LockdownSchedule
}

type LockdownSchedule struct {
	Start         time.Time
	End           time.Time
	DisabledUntil time.Time
}

type LockdownDay struct {
	Day    time.Weekday
	Hour   int
	Minute int
}

func (l *Lockdown) Parse(schedules string) error {
	timeFramesSplit := strings.Split(schedules, ",")

	for _, tf := range timeFramesSplit {
		times := strings.Split(tf, "-")
		if len(times) != 2 {
			return fmt.Errorf("invalid timeframe format")
		}

		start, err := time.Parse(time.RFC3339, times[0])
		if err != nil {
			return err
		}

		end, err := time.Parse(time.RFC3339, times[1])
		if err != nil {
			return err
		}

		l.Schedules = append(l.Schedules, LockdownSchedule{Start: start, End: end})
	}

	return nil
}

func (l *Lockdown) IsLocked() bool {
	switch l.CurrentLock {
	case "manual":
		return true
	case "schedule":
		now := time.Now()
		for _, s := range l.Schedules {
			if now.After(s.Start) && now.Before(s.End) && now.After(s.DisabledUntil) {
				return true
			}
		}
	}
	return false
}

func (l *Lockdown) SetLock() {
	l.ManualLock = true
	l.CurrentLock = "manual"
}

func (l *Lockdown) ReleaseLock() {
	l.ManualLock = false
	l.CurrentLock = "schedule"

	now := time.Now()
	for i, s := range l.Schedules {
		if now.After(s.Start) && now.Before(s.End) {
			l.Schedules[i].DisabledUntil = now.Add(2 * time.Hour)
			break
		}
	}
}

func NewLockdown(schedules string) (*Lockdown, error) {
	lockdown := &Lockdown{}
	if err := lockdown.Parse(schedules); err != nil {
		return nil, err
	}
	return lockdown, nil
}
