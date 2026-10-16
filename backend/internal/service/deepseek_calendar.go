package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
)

const SettingKeyDeepseekHolidays = "deepseek.holidays"

var ErrInvalidDeepseekHolidays = errors.New("invalid DeepSeek holiday calendar")
var ErrDeepseekCalendarUnavailable = errors.New("DeepSeek holiday calendar does not cover billing year; peak pricing disabled")

type deepseekHolidayCalendar struct {
	dates    []string
	holidays map[string]bool
	years    map[int]bool
}

// 2026 official holiday periods, including days off in the published arrangement.
// Source: 国办发明电〔2025〕7号. Weekend make-up workdays remain off-peak.
var defaultDeepseekCalendar = func() *deepseekHolidayCalendar {
	var dates []string
	for _, period := range [][2]string{{"2026-01-01", "2026-01-03"}, {"2026-02-15", "2026-02-23"},
		{"2026-04-04", "2026-04-06"}, {"2026-05-01", "2026-05-05"}, {"2026-06-19", "2026-06-21"},
		{"2026-09-25", "2026-09-27"}, {"2026-10-01", "2026-10-07"}} {
		start, _ := time.Parse(time.DateOnly, period[0])
		end, _ := time.Parse(time.DateOnly, period[1])
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			dates = append(dates, d.Format(time.DateOnly))
		}
	}
	calendar, _ := newDeepseekCalendar(dates)
	return calendar
}()

func newDeepseekCalendar(dates []string) (*deepseekHolidayCalendar, error) {
	if len(dates) > 3660 {
		return nil, fmt.Errorf("too many holiday dates")
	}
	c := &deepseekHolidayCalendar{holidays: make(map[string]bool), years: make(map[int]bool)}
	for _, date := range dates {
		d, err := time.Parse(time.DateOnly, date)
		if err != nil || d.Format(time.DateOnly) != date {
			return nil, fmt.Errorf("invalid holiday date %q; use YYYY-MM-DD", date)
		}
		if !c.holidays[date] {
			c.dates = append(c.dates, date)
		}
		c.holidays[date], c.years[d.Year()] = true, true
	}
	sort.Strings(c.dates)
	if c.dates == nil {
		c.dates = []string{}
	}
	return c, nil
}

// Both admin APIs publish the same immutable calendar. An explicit empty year
// remains covered; omitted years use the built-in table where available.
func calendarByYear(calendar *deepseekHolidayCalendar) map[string][]string {
	result := make(map[string][]string, len(calendar.years))
	for year := range calendar.years {
		result[fmt.Sprintf("%04d", year)] = []string{}
	}
	for _, date := range calendar.dates {
		result[date[:4]] = append(result[date[:4]], date)
	}
	return result
}

func deepseekCalendarFromYears(holidays map[string][]string) *deepseekHolidayCalendar {
	calendar := &deepseekHolidayCalendar{dates: []string{}, holidays: make(map[string]bool), years: make(map[int]bool)}
	for year, days := range holidays {
		y, _ := time.Parse("2006", year) // The shared validator checked canonical years.
		calendar.years[y.Year()] = true
		for _, day := range days {
			calendar.dates = append(calendar.dates, day)
			calendar.holidays[day] = true
		}
	}
	sort.Strings(calendar.dates)
	return calendar
}

func deepseekPricingPeriodWithCalendar(at time.Time, calendar *deepseekHolidayCalendar) DeepSeekPricingPeriod {
	year := at.In(deepSeekLocation).Year()
	calendar = effectiveDeepseekCalendar(calendar, year)
	return chooseDeepSeekPricingPeriod(at, calendarByYear(calendar))
}

func (s *PricingService) deepseekCalendar() *deepseekHolidayCalendar {
	if s == nil {
		return defaultDeepseekCalendar
	}
	s.calendarMu.RLock()
	defer s.calendarMu.RUnlock()
	if s.holidayCalendar == nil {
		return defaultDeepseekCalendar
	}
	return s.holidayCalendar
}

func (s *PricingService) UpdateDeepseekHolidays(ctx context.Context, dates []string) error {
	calendar, err := newDeepseekCalendar(dates)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDeepseekHolidays, err)
	}
	// An empty list explicitly restores the built-in calendar.
	if len(dates) == 0 {
		calendar = defaultDeepseekCalendar
	} else if !calendar.years[time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Year()] {
		return fmt.Errorf("%w: dates must cover the current Beijing billing year", ErrInvalidDeepseekHolidays)
	}
	if err := s.updateControls(ctx, PricingControls{DeepSeekHolidays: calendarByYear(calendar)}, true); err != nil {
		return err
	}
	s.recordPricingSyncResult("DeepSeek calendar", nil)
	for year := range calendar.years {
		s.recordPricingSyncResult(fmt.Sprintf("DeepSeek calendar %d", year), nil)
	}
	return nil
}

// Missing custom years use the built-in table. Years absent from both tables
// remain off-peak to avoid charging a holiday at the weekday peak rate.
func effectiveDeepseekCalendar(calendar *deepseekHolidayCalendar, year int) *deepseekHolidayCalendar {
	if calendar != nil && calendar.years[year] {
		return calendar
	}
	return defaultDeepseekCalendar
}

func (s *PricingService) deepseekCalendarAt(at time.Time) *deepseekHolidayCalendar {
	year := at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Year()
	return effectiveDeepseekCalendar(s.deepseekCalendar(), year)
}

// Only startup and the existing scheduler perform notification I/O. Pricing
// reads remain pure, including when delivery fails or SMTP is slow.
func (s *PricingService) checkDeepseekCalendarCoverage(at time.Time) {
	year := at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Year()
	if !s.deepseekCalendarAt(at).years[year] {
		s.recordPricingSyncResult(fmt.Sprintf("DeepSeek calendar %d", year), ErrDeepseekCalendarUnavailable)
	} else {
		s.recordPricingSyncResult(fmt.Sprintf("DeepSeek calendar %d", year), nil)
	}
}

func (s *PricingService) deepseekCalendarStatus() ([]int, string) {
	calendar := s.deepseekCalendar()
	years := make(map[int]bool)
	for year := range defaultDeepseekCalendar.years {
		years[year] = true
	}
	for year := range calendar.years {
		years[year] = true
	}
	covered := make([]int, 0, len(years))
	for year := range years {
		covered = append(covered, year)
	}
	sort.Ints(covered)
	s.calendarMu.RLock()
	detail := s.calendarError
	s.calendarMu.RUnlock()
	if !years[time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Year()] {
		detail = ErrDeepseekCalendarUnavailable.Error()
	}
	return covered, detail
}

func (s *PricingService) DeepseekHolidayDates() []string {
	return slices.Clone(s.deepseekCalendar().dates)
}

func deepseekOffPeakDay(at time.Time, calendar *deepseekHolidayCalendar) bool {
	if calendar == nil {
		calendar = defaultDeepseekCalendar
	}
	beijing := at.In(time.FixedZone("Asia/Shanghai", 8*3600))
	calendar = effectiveDeepseekCalendar(calendar, beijing.Year())
	if !calendar.years[beijing.Year()] {
		return true
	}
	return calendar.holidays[beijing.Format(time.DateOnly)] || beijing.Weekday() == time.Saturday || beijing.Weekday() == time.Sunday
}

func deepseekPeakMultiplierWithCalendar(at time.Time, calendar *deepseekHolidayCalendar) float64 {
	if deepseekOffPeakDay(at, calendar) {
		return 1
	}
	beijing := at.In(time.FixedZone("Asia/Shanghai", 8*3600))
	if h := beijing.Hour(); (h >= 9 && h < 12) || (h >= 14 && h < 18) {
		return 2
	}
	return 1
}
