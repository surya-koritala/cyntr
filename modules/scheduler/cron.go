package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule represents a parsed cron expression (5-field: min hr day month weekday).
type CronSchedule struct {
	minutes  []int // 0-59
	hours    []int // 0-23
	days     []int // 1-31
	months   []int // 1-12
	weekdays []int // 0-6 (0=Sunday)
}

// ParseCron parses a 5-field cron expression.
func ParseCron(expr string) (*CronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	cs := &CronSchedule{}
	var err error

	cs.minutes, err = parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	cs.hours, err = parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	cs.days, err = parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day: %w", err)
	}
	cs.months, err = parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	cs.weekdays, err = parseField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("weekday: %w", err)
	}

	return cs, nil
}

func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		vals := make([]int, max-min+1)
		for i := range vals {
			vals[i] = min + i
		}
		return vals, nil
	}

	var result []int
	for _, part := range strings.Split(field, ",") {
		if strings.Contains(part, "/") {
			// Step: */5 or 1-30/5
			parts := strings.SplitN(part, "/", 2)
			step, err := strconv.Atoi(parts[1])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step: %s", part)
			}
			start, end := min, max
			if parts[0] != "*" {
				r, err := parseRange(parts[0], min, max)
				if err != nil {
					return nil, err
				}
				start, end = r[0], r[len(r)-1]
			}
			for i := start; i <= end; i += step {
				result = append(result, i)
			}
		} else if strings.Contains(part, "-") {
			r, err := parseRange(part, min, max)
			if err != nil {
				return nil, err
			}
			result = append(result, r...)
		} else {
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid value: %s", part)
			}
			if v < min || v > max {
				return nil, fmt.Errorf("value %d out of range %d-%d", v, min, max)
			}
			result = append(result, v)
		}
	}
	return result, nil
}

func parseRange(s string, min, max int) ([]int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range: %s", s)
	}
	start, err1 := strconv.Atoi(parts[0])
	end, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("invalid range: %s", s)
	}
	if start < min || end > max || start > end {
		return nil, fmt.Errorf("range %d-%d out of bounds %d-%d", start, end, min, max)
	}
	vals := make([]int, end-start+1)
	for i := range vals {
		vals[i] = start + i
	}
	return vals, nil
}

func contains(vals []int, v int) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

// Matches returns true if the given time matches this schedule.
func (cs *CronSchedule) Matches(t time.Time) bool {
	return contains(cs.minutes, t.Minute()) &&
		contains(cs.hours, t.Hour()) &&
		contains(cs.days, t.Day()) &&
		contains(cs.months, int(t.Month())) &&
		contains(cs.weekdays, int(t.Weekday()))
}

// Next returns the next time after 'from' that matches this schedule.
func (cs *CronSchedule) Next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 525960; i++ { // max ~1 year of minutes
		if cs.Matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return t // fallback
}
