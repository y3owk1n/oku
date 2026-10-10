package manifest

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Schedule makes a service a job that the service manager runs at set times,
// and that ends on its own. It runs every Every, or each day at At, or at At on
// Weekdays only.
type Schedule struct {
	Every    string   `toml:"every"`
	At       string   `toml:"at"`
	Weekdays []string `toml:"weekdays"`
	// Interval, Hour, Minute and Days are what Parse reads from the keys.
	Interval     time.Duration  `toml:"-"`
	Hour, Minute int            `toml:"-"`
	Days         []time.Weekday `toml:"-"`
}

// weekdays are the names a schedule takes, in time.Weekday order.
var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Parse checks s and fills Interval, or Hour, Minute and Days.
func (s *Schedule) Parse() error {
	switch {
	case s.Every != "" && s.At != "":
		return errors.New("schedule takes every or at, not both")
	case s.Every == "" && s.At == "":
		return errors.New(`schedule takes every, such as every = "15m", or at, such as at = "03:00"`)
	case s.Every != "" && len(s.Weekdays) > 0:
		return errors.New("schedule.weekdays goes with at")
	case s.Every != "":
		units := map[string]time.Duration{"m": time.Minute, "h": time.Hour}

		n, err := strconv.Atoi(s.Every[:max(len(s.Every)-1, 0)])
		unit, ok := units[s.Every[max(len(s.Every)-1, 0):]]

		if s.Interval = time.Duration(n) * unit; err != nil || !ok || s.Interval < time.Minute || s.Interval > 24*time.Hour {
			return fmt.Errorf("schedule.every %q must be minutes or hours from 1m to 24h, such as 15m or 6h", s.Every)
		}

		return nil
	}

	hour, minute, ok := strings.Cut(s.At, ":")
	h, hErr := strconv.Atoi(hour)
	m, mErr := strconv.Atoi(minute)

	if !ok || len(hour) != 2 || len(minute) != 2 || hErr != nil || mErr != nil || h > 23 || m > 59 {
		return fmt.Errorf("schedule.at %q must be a time of day as HH:MM, such as 03:00", s.At)
	}

	s.Hour, s.Minute = h, m

	for _, day := range s.Weekdays {
		i := slices.Index(weekdays, day)

		switch {
		case i < 0:
			return fmt.Errorf("schedule.weekdays: %q is not a day, use %s", day, strings.Join(weekdays, ", "))
		case slices.Contains(s.Days, time.Weekday(i)):
			return fmt.Errorf("schedule.weekdays names %s twice", day)
		}

		s.Days = append(s.Days, time.Weekday(i))
	}

	slices.Sort(s.Days)

	return nil
}
