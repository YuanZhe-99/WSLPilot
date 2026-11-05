// Package scheduler runs backup plans when they are due: from the GUI
// while it is open, and from wslpilot-agent (started by a per-user Task
// Scheduler task) while it is not. Plans only produce backups; the
// backups belong to their locations (see internal/backup).
package scheduler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Every is how often a plan runs.
type Every string

// Frequencies.
const (
	Hourly  Every = "hourly"
	Daily   Every = "daily"
	Weekly  Every = "weekly"
	Monthly Every = "monthly"
	Manual  Every = "manual" // only when started by hand
)

// Schedule is when a plan runs.
type Schedule struct {
	Every Every `json:"every"`
	// At is "HH:MM" local time (daily, weekly, monthly); for hourly only
	// the minutes count.
	At string `json:"at,omitempty"`
	// Weekday for weekly plans (0 = Sunday).
	Weekday time.Weekday `json:"weekday,omitempty"`
	// Day of the month for monthly plans (1–28, so every month has it).
	Day int `json:"day,omitempty"`
}

// Validate checks the schedule's fields.
func (s Schedule) Validate() error {
	switch s.Every {
	case Hourly, Daily, Weekly, Monthly, Manual:
	default:
		return fmt.Errorf("scheduler: unknown frequency %q", s.Every)
	}
	if s.Every != Manual {
		if _, _, err := parseAt(s.At); err != nil {
			return err
		}
	}
	if s.Every == Monthly && (s.Day < 1 || s.Day > 28) {
		return errors.New("scheduler: day of month must be 1–28")
	}
	return nil
}

func parseAt(at string) (h, m int, err error) {
	if at == "" {
		return 0, 0, nil
	}
	parts := strings.Split(at, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("scheduler: time %q is not HH:MM", at)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("scheduler: time %q is not HH:MM", at)
	}
	return h, m, nil
}

// Next returns the first run time strictly after t (zero for Manual).
// Notes: computed in t's location, so daylight-saving changes keep the
// wall-clock time.
func (s Schedule) Next(t time.Time) time.Time {
	h, m, _ := parseAt(s.At)
	loc := t.Location()
	switch s.Every {
	case Hourly:
		n := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
		for !n.After(t) {
			n = n.Add(time.Hour)
		}
		return n
	case Daily:
		n := time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, loc)
		if !n.After(t) {
			n = time.Date(t.Year(), t.Month(), t.Day()+1, h, m, 0, 0, loc)
		}
		return n
	case Weekly:
		days := (int(s.Weekday) - int(t.Weekday()) + 7) % 7
		n := time.Date(t.Year(), t.Month(), t.Day()+days, h, m, 0, 0, loc)
		if !n.After(t) {
			n = time.Date(t.Year(), t.Month(), t.Day()+days+7, h, m, 0, 0, loc)
		}
		return n
	case Monthly:
		n := time.Date(t.Year(), t.Month(), s.Day, h, m, 0, 0, loc)
		if !n.After(t) {
			n = time.Date(t.Year(), t.Month()+1, s.Day, h, m, 0, 0, loc)
		}
		return n
	}
	return time.Time{}
}

// Retry is how a failed run is retried.
type Retry struct {
	Max     int           `json:"max"`     // attempts after the first (0: none)
	Initial time.Duration `json:"initial"` // first delay; doubles each time
	Limit   time.Duration `json:"limit"`   // largest delay
}

// Delay returns the wait before retry number n (1-based).
func (r Retry) Delay(n int) time.Duration {
	d := r.Initial
	if d <= 0 {
		d = 5 * time.Minute
	}
	for i := 1; i < n; i++ {
		d *= 2
		if r.Limit > 0 && d >= r.Limit {
			return r.Limit
		}
	}
	if r.Limit > 0 && d > r.Limit {
		return r.Limit
	}
	return d
}
