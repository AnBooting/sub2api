package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
)

const SettingKeyDeepseekHolidays = "deepseek.holidays"

var ErrInvalidDeepseekHolidays = errors.New("invalid DeepSeek holiday calendar")

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

var missingDeepseekCalendarYear sync.Map

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

func (s *PricingService) deepseekCalendar() *deepseekHolidayCalendar {
	if s == nil {
		return defaultDeepseekCalendar
	}
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
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
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	if s.settingsRepo != nil {
		body, _ := json.Marshal(calendar.dates)
		if err := s.settingsRepo.Set(ctx, SettingKeyDeepseekHolidays, string(body)); err != nil {
			return err
		}
	}
	s.holidayCalendar = calendar
	return nil
}

func (s *PricingService) DeepseekHolidayDates() []string {
	return slices.Clone(s.deepseekCalendar().dates)
}

func deepseekOffPeakDay(at time.Time, calendar *deepseekHolidayCalendar) bool {
	if calendar == nil {
		calendar = defaultDeepseekCalendar
	}
	beijing := at.In(time.FixedZone("Asia/Shanghai", 8*3600))
	if !calendar.years[beijing.Year()] {
		if _, seen := missingDeepseekCalendarYear.LoadOrStore(beijing.Year(), struct{}{}); !seen {
			slog.Warn("DeepSeek holiday calendar has no dates for billing year; using weekday schedule", "year", beijing.Year())
		}
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
