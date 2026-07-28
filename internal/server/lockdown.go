package server

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/shini4i/argo-watcher/internal/lock"
)

// defaultOverrideDuration is how long releasing the lock suppresses an active
// scheduled lockdown before it takes effect again.
const defaultOverrideDuration = 15 * time.Minute

// Lockdown resolves whether deployments are currently frozen, combining the
// shared manual lock and override deadline held in the store with the schedules
// each replica evaluates locally.
type Lockdown struct {
	// store holds the manual lock and override deadline. With the Postgres
	// implementation those are shared by every replica, so a lock set through
	// one of them rejects deployments on all of them.
	store lock.DeployLockStore
	// Schedules are parsed from configuration, which is identical on every
	// replica, and are therefore evaluated locally rather than stored.
	Schedules []LockdownSchedule
	// overrideDuration is the lifetime of the override created by ReleaseLock.
	// It is a field so tests can shorten it.
	overrideDuration time.Duration
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

	slog.Debug("parsed lockdown schedules", "schedules", l.Schedules)

	return nil
}

// IsLocked reports whether the system is under lockdown: true when a manual
// lock is active, or when the current time falls within a scheduled lockdown
// that is not currently overridden.
//
// A failure to read the shared state is treated as locked. Rejecting a
// deployment while the state is unknown is recoverable; letting one through
// during a freeze because the database blinked is not.
func (l *Lockdown) IsLocked() bool {
	locked, err := l.resolve()
	if err != nil {
		slog.Error("failed to read deploy lock state, assuming locked", "error", err)
	}

	return locked
}

// resolve reports the current lock state along with any store read error. On a
// read error it returns the fail-closed answer (locked) *and* the error, so the
// enforcement path can act on the safe default while the watcher can tell an
// unknown state apart from a genuine transition.
func (l *Lockdown) resolve() (bool, error) {
	state, err := l.store.State()
	if err != nil {
		return true, err
	}

	return l.isLockedWith(state, time.Now()), nil
}

// isLockedWith resolves the lock state for a given store state and instant. It
// is separate from IsLocked so the precedence rules can be tested without a
// store or a clock.
func (l *Lockdown) isLockedWith(state lock.DeployLockState, now time.Time) bool {
	if state.ManualLock {
		return true
	}

	if now.Before(state.OverrideUntil) {
		return false
	}

	return l.scheduleActive(now)
}

// scheduleActive reports whether now falls within any configured lockdown window.
func (l *Lockdown) scheduleActive(now time.Time) bool {
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
func (l *Lockdown) SetLock() error {
	return l.store.Lock()
}

// ReleaseLock cancels the manual lockdown. If a scheduled lockdown is active, it
// is temporarily overridden for overrideDuration; once that deadline passes the
// schedule takes effect again. The deadline is persisted with the lock state, so
// the override ends at the same instant on every replica and survives a restart.
// Clients learn about the expiry from the lockdown watcher, which polls the
// resolved state.
func (l *Lockdown) ReleaseLock() error {
	var overrideUntil time.Time
	if l.scheduleActive(time.Now()) {
		overrideUntil = time.Now().Add(l.overrideDuration)
	}

	return l.store.Release(overrideUntil)
}

// WatchTransitions polls the lock state on the given interval and invokes notify
// with "locked" or "unlocked" whenever the computed state changes. Scheduled
// lockdowns and shared locks set by other replicas are only observable by
// polling, so this is the mechanism that informs clients about them. The initial
// state is recorded without notifying, so only genuine transitions produce a
// notification. It runs until stop is closed.
//
// A tick whose store read fails is skipped rather than reported: unlike the
// enforcement path, which must fail closed, an unreadable state is not a
// transition, and treating it as one would flap the banner on every transient
// error. The failure is logged at debug level because a database outage is
// already reported loudly by the reachability probe.
func (l *Lockdown) WatchTransitions(stop <-chan struct{}, interval time.Duration, notify func(string)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	last, _ := l.resolve()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			current, err := l.resolve()
			if err != nil {
				slog.Debug("skipping lockdown transition check, lock state is unreadable", "error", err)
				continue
			}
			if current == last {
				continue
			}
			last = current

			if current {
				notify("locked")
			} else {
				notify("unlocked")
			}
		}
	}
}

// NewLockdown initializes a new Lockdown structure backed by the given store and
// parses the lockdown schedules if provided. If the schedule parsing is
// successful, it returns the new Lockdown. Otherwise, it returns an error.
func NewLockdown(schedules string, store lock.DeployLockStore) (*Lockdown, error) {
	lockdown := &Lockdown{
		store:            store,
		overrideDuration: defaultOverrideDuration,
	}
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
	currDay := now.Weekday()
	currHour := now.Hour()
	currMin := now.Minute()

	// If it's the same day
	if startDay == endDay {
		return currDay == startDay &&
			timeAtOrAfter(currHour, currMin, startHour, startMin) &&
			timeBefore(currHour, currMin, endHour, endMin)
	}

	// For different days
	if !dayInRange(currDay, startDay, endDay) {
		return false
	}

	// Check times for start and end day
	switch currDay {
	case startDay:
		return timeAtOrAfter(currHour, currMin, startHour, startMin)
	case endDay:
		return timeBefore(currHour, currMin, endHour, endMin)
	default:
		return true
	}
}

// timeAtOrAfter reports whether hour:min is at or after ref hour:min on the same day.
func timeAtOrAfter(hour, min, refHour, refMin int) bool {
	return hour > refHour || (hour == refHour && min >= refMin)
}

// timeBefore reports whether hour:min is strictly before ref hour:min on the same day.
func timeBefore(hour, min, refHour, refMin int) bool {
	return hour < refHour || (hour == refHour && min < refMin)
}

// dayInRange reports whether day falls within [start, end], wrapping to the next
// week when end is before start (e.g. Fri→Mon).
func dayInRange(day, start, end time.Weekday) bool {
	if end >= start {
		return day >= start && day <= end
	}
	return day >= start || day <= end
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
