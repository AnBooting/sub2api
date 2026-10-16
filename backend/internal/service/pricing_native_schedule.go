package service

import "strings"

// The complete base rates provide default/offpeak coverage. Overnight overrides
// are anchored to their listed starting weekday and continue into the next day.
func validNativeScheduleWindows(schedule *NativeSchedule) bool {
	var covered [7][1440]bool
	for _, window := range schedule.Windows {
		if len(window.Weekdays) == 0 || len(window.HourRanges) == 0 {
			return false
		}
		for _, day := range window.Weekdays {
			if day < 0 || day > 6 {
				return false
			}
			for _, hours := range window.HourRanges {
				parts := strings.Split(hours, "-")
				if len(parts) != 2 {
					return false
				}
				start, okStart := nativeMinute(parts[0])
				end, okEnd := nativeMinute(parts[1])
				if !okStart || !okEnd || start == end || start == 1440 {
					return false
				}
				stop := end
				if start > end {
					stop += 1440
				}
				for minute := start; minute < stop; minute++ {
					d, m := (day+minute/1440)%7, minute%1440
					if covered[d][m] {
						return false
					}
					covered[d][m] = true
				}
			}
		}
	}
	return true
}

func nativeWindowMatches(window NativeWindow, weekday, minute int) bool {
	for _, day := range window.Weekdays {
		for _, hours := range window.HourRanges {
			parts := strings.Split(hours, "-")
			start, _ := nativeMinute(parts[0])
			end, _ := nativeMinute(parts[1])
			if start < end && weekday == day && minute >= start && minute < end {
				return true
			}
			if start > end && ((weekday == day && minute >= start) || (weekday == (day+1)%7 && minute < end)) {
				return true
			}
		}
	}
	return false
}
