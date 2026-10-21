//go:build unit

package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestVideoTaskSummaryNeverLeaksInternalDiagnostics 锁定用户侧视频任务视图的隔离边界：
// 内部诊断（承载账号 ID、仓储错误、主机名/SQL 片段等）一律不得出现在用户响应里，
// 只允许稳定错误码 + 可公开文案。
func TestVideoTaskSummaryNeverLeaksInternalDiagnostics(t *testing.T) {
	cases := []struct {
		name      string
		lastError string
		wantCode  string
		wantText  string
	}{
		{
			name:      "carrying account load failure",
			lastError: "load carrying account 918273 failed: database temporarily unavailable",
			wantCode:  VideoTaskErrorCodeAccountLoadFailed,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeAccountLoadFailed],
		},
		{
			name:      "carrying account not bound",
			lastError: "carrying account is not bound; refusing to re-schedule account selection",
			wantCode:  VideoTaskErrorCodeAccountNotBound,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeAccountNotBound],
		},
		{
			name:      "internal dependency unavailable",
			lastError: "upstream querier or account repository is unavailable",
			wantCode:  VideoTaskErrorCodeDependencyUnavailable,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeDependencyUnavailable],
		},
		{
			name:      "settlement deferred",
			lastError: "settlement deferred: upstream succeeded but billing did not complete",
			wantCode:  VideoTaskErrorCodeSettlementFailed,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeSettlementFailed],
		},
		{
			name:      "upstream transport failure with host detail",
			lastError: "upstream status transport error: dial tcp 10.0.0.7:443: connect: connection refused",
			wantCode:  VideoTaskErrorCodeUpstreamFailed,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeUpstreamFailed],
		},
		{
			name:      "unclassifiable internal failure",
			lastError: "panic-ish internal detail sql: SELECT secret FROM accounts",
			wantCode:  VideoTaskErrorCodeInternal,
			wantText:  videoTaskPublicErrorMessages[VideoTaskErrorCodeInternal],
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := &service.VideoTask{TaskNo: "task-1", Status: "failed", LastError: tc.lastError}
			dto := ToVideoTaskSummaryDTO(task)

			require.Equal(t, tc.wantCode, dto.LastErrorCode, "错误码必须稳定可枚举")
			require.Equal(t, tc.wantText, dto.LastError, "用户侧只给可公开文案")

			payload, err := json.Marshal(dto)
			require.NoError(t, err)
			body := string(payload)
			require.NotContains(t, body, "918273", "不得暴露内部承载账号 ID")
			require.NotContains(t, body, "database temporarily unavailable", "不得暴露仓储错误细节")
			require.NotContains(t, body, "10.0.0.7", "不得暴露内部主机名")
			require.NotContains(t, body, "SELECT secret", "不得暴露 SQL 片段")
			require.NotContains(t, body, "carrying account", "不得暴露内部诊断文案")

			// 源对象不被改写：管理端排障路径仍看得到完整 last_error。
			require.Equal(t, tc.lastError, task.LastError)
		})
	}
}

// TestVideoTaskSummaryWithoutErrorHasNoErrorFields 没有失败原因时不出现错误码与错误文案。
func TestVideoTaskSummaryWithoutErrorHasNoErrorFields(t *testing.T) {
	dto := ToVideoTaskSummaryDTO(&service.VideoTask{TaskNo: "task-2", Status: "processing"})
	require.Empty(t, dto.LastErrorCode)
	require.Empty(t, dto.LastError)
	payload, err := json.Marshal(dto)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "last_error")
}

// TestVideoTaskPublicErrorMessageFallsBackToInternal 未知错误码回落到通用文案（不泄露、不空）。
func TestVideoTaskPublicErrorMessageFallsBackToInternal(t *testing.T) {
	require.Equal(t, videoTaskPublicErrorMessages[VideoTaskErrorCodeInternal], VideoTaskPublicErrorMessage("brand_new_code"))
	require.Empty(t, VideoTaskPublicErrorMessage(""))
}
