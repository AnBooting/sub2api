package service

import "context"

// ChannelMonitorV2PublicConfig exposes only the refresh cadence to users.
// Admin snapshots and /config retain ChannelMonitorV2Config.
type ChannelMonitorV2PublicConfig struct {
	RefreshIntervalSeconds int `json:"refresh_interval_seconds"`
}

type ChannelMonitorV2PublicSnapshot struct {
	Config   ChannelMonitorV2PublicConfig `json:"config"`
	Coverage ChannelMonitorV2Coverage     `json:"coverage"`
	Metrics  ChannelMonitorV2Metric       `json:"metrics"`
	Health   ChannelMonitorV2Health       `json:"health"`
	Trend    []ChannelMonitorV2TrendPoint `json:"trend"`
}

func (s *ChannelMonitorV2Service) SnapshotPublic(ctx context.Context, filter ChannelMonitorV2Filter) (*ChannelMonitorV2PublicSnapshot, error) {
	snapshot, err := s.Snapshot(ctx, filter, false)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return &ChannelMonitorV2PublicSnapshot{
		Config:   ChannelMonitorV2PublicConfig{RefreshIntervalSeconds: snapshot.Config.RefreshIntervalSeconds},
		Coverage: snapshot.Coverage, Metrics: snapshot.Metrics,
		Health: snapshot.Health, Trend: snapshot.Trend,
	}, nil
}
