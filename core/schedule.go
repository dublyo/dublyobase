package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxScheduleSearch = 366 * 24 * time.Hour

// NextScheduledTime supports @every durations and standard five-field cron
// expressions. It intentionally evaluates at minute precision, matching the
// operational cron use case while keeping the scheduler dependency-free.
func NextScheduledTime(schedule string, timezone string, after time.Time) (time.Time, error) {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return time.Time{}, fmt.Errorf("%w: schedule is required", ErrValidation)
	}
	if strings.HasPrefix(schedule, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(schedule, "@every ")))
		if err != nil || d < time.Minute {
			return time.Time{}, fmt.Errorf("%w: @every schedule must be at least 1m", ErrValidation)
		}
		return after.UTC().Add(d), nil
	}
	loc := time.UTC
	if strings.TrimSpace(timezone) != "" && timezone != "UTC" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: invalid timezone", ErrValidation)
		}
		loc = loaded
	}
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("%w: cron schedule must have 5 fields", ErrValidation)
	}
	minutes, err := parseScheduleField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid minute field", ErrValidation)
	}
	hours, err := parseScheduleField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid hour field", ErrValidation)
	}
	dom, err := parseScheduleField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid day-of-month field", ErrValidation)
	}
	months, err := parseScheduleField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid month field", ErrValidation)
	}
	dow, err := parseScheduleField(fields[4], 0, 7)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: invalid day-of-week field", ErrValidation)
	}
	cursor := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	deadline := cursor.Add(maxScheduleSearch)
	for !cursor.After(deadline) {
		weekday := int(cursor.Weekday())
		if dow[7] && weekday == 0 {
			weekday = 7
		}
		if minutes[cursor.Minute()] &&
			hours[cursor.Hour()] &&
			dom[cursor.Day()] &&
			months[int(cursor.Month())] &&
			(dow[int(cursor.Weekday())] || dow[weekday]) {
			return cursor.UTC(), nil
		}
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("%w: cron schedule has no run within one year", ErrValidation)
}

func parseScheduleField(field string, min int, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty part")
		}
		step := 1
		if base, rawStep, ok := strings.Cut(part, "/"); ok {
			part = base
			parsed, err := strconv.Atoi(rawStep)
			if err != nil || parsed < 1 {
				return nil, fmt.Errorf("invalid step")
			}
			step = parsed
		}
		start, end := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			parsedStart, errA := strconv.Atoi(a)
			parsedEnd, errB := strconv.Atoi(b)
			if errA != nil || errB != nil || parsedStart > parsedEnd {
				return nil, fmt.Errorf("invalid range")
			}
			start, end = parsedStart, parsedEnd
		default:
			parsed, err := strconv.Atoi(part)
			if err != nil {
				return nil, err
			}
			start, end = parsed, parsed
		}
		if start < min || end > max {
			return nil, fmt.Errorf("out of range")
		}
		for i := start; i <= end; i += step {
			out[i] = true
		}
	}
	return out, nil
}
