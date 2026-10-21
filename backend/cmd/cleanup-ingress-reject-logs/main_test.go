package main

import (
	"strings"
	"testing"
	"time"
)

func TestHistoricalIngressRejectReason(t *testing.T) {
	tests := []struct {
		name   string
		item   candidate
		reason string
		match  bool
	}{
		{name: "standard invalid key", item: candidate{body: `{"code":"INVALID_API_KEY","message":"Invalid API key"}`}, reason: "invalid_key", match: true},
		{name: "google missing key", item: candidate{body: `{"error":{"code":401,"message":"API key is required","status":"UNAUTHENTICATED"}}`}, reason: "missing_key", match: true},
		{name: "google group deleted", item: candidate{body: `{"error":{"code":403,"message":"API Key 所属分组已删除","status":"PERMISSION_DENIED"}}`}, reason: "group_deleted", match: true},
		{name: "ip acl", item: candidate{body: `{"code":"ACCESS_DENIED","message":"Access denied. Your IP is 192.0.2.1"}`}, reason: "ip_acl_denied", match: true},
		{name: "user not found remains", item: candidate{body: `{"code":"USER_NOT_FOUND","message":"User associated with API key not found"}`}, match: false},
		{name: "quota remains", item: candidate{body: `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"quota"}`}, match: false},
		{name: "database failure remains", item: candidate{statusCode: 500, message: "Failed to validate API key", body: `{"code":"INTERNAL_ERROR","message":"Failed to validate API key"}`}, match: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := historicalIngressRejectReason(tt.item)
			if ok != tt.match || reason != tt.reason {
				t.Fatalf("got (%q, %v), want (%q, %v)", reason, ok, tt.reason, tt.match)
			}
		})
	}
}

// Q7: 删除谓词必须同时满足 created_at <= now() 与保留窗口两个条件，
// 避免未来时间戳的记录被当作历史数据清掉，也避免误删仍在保留窗口内的日志。
func TestResolveCutoffClampsFutureBeforeToNow(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	future := now.Add(48 * time.Hour)

	cutoff, err := resolveCutoff(future, now, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cutoff.After(now) {
		t.Fatalf("cutoff must never exceed now(); got %s", cutoff)
	}
	want := now.AddDate(0, 0, -30)
	if !cutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want retention floor %s", cutoff, want)
	}
}

func TestResolveCutoffAppliesRetentionWindow(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	before := now.Add(-1 * time.Hour)

	cutoff, err := resolveCutoff(before, now, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.AddDate(0, 0, -30)
	if !cutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want retention floor %s (retention wins over a too-recent --before)", cutoff, want)
	}
}

func TestResolveCutoffKeepsOlderBeforeWhenRetentionIsZero(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	before := now.AddDate(0, 0, -90)

	cutoff, err := resolveCutoff(before, now, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cutoff.Equal(before) {
		t.Fatalf("cutoff = %s, want %s when retention window is disabled", cutoff, before)
	}
}

func TestResolveCutoffRejectsNegativeRetention(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if _, err := resolveCutoff(now, now, -1); err == nil {
		t.Fatal("negative retention days must be rejected")
	}
}

// dry-run 必须输出可核对的计数：would_delete 是「将要删除」的数量，
// execute 模式下 deleted 必须等于该数量。
func TestSummarizeReportsDryRunCounts(t *testing.T) {
	counts := map[string]int64{"invalid_key": 3, "missing_key": 2}
	line := summarize("dry-run", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), 100, 5, 0, counts)

	for _, want := range []string{"mode=dry-run", "scanned=100", "matched=5", "would_delete=5", "deleted=0", "reasons=2"} {
		if !strings.Contains(line, want) {
			t.Fatalf("dry-run summary %q missing %q", line, want)
		}
	}
}

func TestSummarizeExecuteReportsDeletedCount(t *testing.T) {
	line := summarize("execute", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), 100, 5, 5, nil)
	for _, want := range []string{"mode=execute", "matched=5", "would_delete=5", "deleted=5"} {
		if !strings.Contains(line, want) {
			t.Fatalf("execute summary %q missing %q", line, want)
		}
	}
}
