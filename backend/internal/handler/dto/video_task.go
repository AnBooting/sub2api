package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// VideoTaskSummaryDTO 是用户侧可见的视频任务摘要。
//
// 字段最少化：只暴露「我提交过哪些视频任务、现在什么状态」，
// 绝不包含 upstream_task_id / account_id / billing_snapshot
// （上游内部标识、承载账号与计费快照不属于用户侧视图）。
//
// last_error 只给**可公开**的失败说明，last_error_code 给稳定错误码；
// 内部诊断（承载账号 ID、仓储错误、SQL 片段…）不进入用户响应，
// 完整 last_error 仍保留在库里供管理端排障。
type VideoTaskSummaryDTO struct {
	TaskNo      string  `json:"task_no"`
	Status      string  `json:"status"`
	Model       string  `json:"model,omitempty"`
	Platform    string  `json:"platform,omitempty"`
	BillingMode string  `json:"billing_mode,omitempty"`
	CreatedAt   string  `json:"created_at"`
	SettledAt   *string `json:"settled_at,omitempty"`
	// LastError 是可公开的失败说明（不含内部诊断）。
	LastError string `json:"last_error,omitempty"`
	// LastErrorCode 是稳定错误码（可枚举、可翻译）；前端按码显示，不解析文案。
	LastErrorCode string `json:"last_error_code,omitempty"`
}

// VideoTaskPageDTO 是用户侧视频任务列表的分页响应。
//
// next_cursor 为 null 表示没有更多；非空时把它原样回传即可取下一页。
type VideoTaskPageDTO struct {
	Items      []VideoTaskSummaryDTO `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

// ToVideoTaskSummaryDTO 把任务行映射成用户侧摘要。
func ToVideoTaskSummaryDTO(task *service.VideoTask) VideoTaskSummaryDTO {
	if task == nil {
		return VideoTaskSummaryDTO{}
	}
	return VideoTaskSummaryDTO{
		TaskNo:        task.TaskNo,
		Status:        task.Status,
		Model:         task.Model,
		Platform:      task.Platform,
		BillingMode:   task.BillingMode,
		CreatedAt:     formatVideoTaskTime(task.CreatedAt),
		SettledAt:     formatVideoTaskTimePtr(task.SettledAt),
		LastError:     VideoTaskPublicErrorMessage(VideoTaskPublicErrorCode(task.LastError)),
		LastErrorCode: VideoTaskPublicErrorCode(task.LastError),
	}
}

// formatVideoTaskTime 统一按 RFC3339（UTC）输出时间。
func formatVideoTaskTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// formatVideoTaskTimePtr 缺省时间输出 nil（前端据此区分「尚未结算」）。
func formatVideoTaskTimePtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := formatVideoTaskTime(*value)
	return &formatted
}
