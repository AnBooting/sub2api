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
type VideoTaskSummaryDTO struct {
	TaskNo      string  `json:"task_no"`
	Status      string  `json:"status"`
	Model       string  `json:"model,omitempty"`
	Platform    string  `json:"platform,omitempty"`
	BillingMode string  `json:"billing_mode,omitempty"`
	CreatedAt   string  `json:"created_at"`
	SettledAt   *string `json:"settled_at,omitempty"`
	LastError   string  `json:"last_error,omitempty"`
}

// ToVideoTaskSummaryDTO 把任务行映射成用户侧摘要。
func ToVideoTaskSummaryDTO(task *service.VideoTask) VideoTaskSummaryDTO {
	if task == nil {
		return VideoTaskSummaryDTO{}
	}
	return VideoTaskSummaryDTO{
		TaskNo:      task.TaskNo,
		Status:      task.Status,
		Model:       task.Model,
		Platform:    task.Platform,
		BillingMode: task.BillingMode,
		CreatedAt:   formatVideoTaskTime(task.CreatedAt),
		SettledAt:   formatVideoTaskTimePtr(task.SettledAt),
		LastError:   task.LastError,
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
