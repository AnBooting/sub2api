package usagestats

import "encoding/json"

// Each flag describes completeness, independently of aggregation freshness.
// Unavailable usage values serialize as null, never a plausible zero or stale sum.
type DashboardUsageAvailability struct {
	Total       bool `json:"total"`
	Today       bool `json:"today"`
	Hourly      bool `json:"hourly"`
	Performance bool `json:"performance"`
}

func (s DashboardStats) MarshalJSON() ([]byte, error) {
	type plain DashboardStats
	data, err := json.Marshal(plain(s))
	if err != nil || s.UsageAvailability == nil {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	clear := func(available bool, names ...string) {
		if !available {
			for _, name := range names {
				fields[name] = json.RawMessage("null")
			}
		}
	}
	a := s.UsageAvailability
	clear(a.Total, "total_requests", "total_input_tokens", "total_output_tokens", "total_cache_creation_tokens", "total_cache_read_tokens", "total_tokens", "total_cost", "total_actual_cost", "total_account_cost", "average_duration_ms")
	clear(a.Today, "today_requests", "today_input_tokens", "today_output_tokens", "today_cache_creation_tokens", "today_cache_read_tokens", "today_tokens", "today_cost", "today_actual_cost", "today_account_cost", "active_users")
	clear(a.Hourly, "hourly_active_users")
	clear(a.Performance, "rpm", "tpm")
	return json.Marshal(fields)
}
