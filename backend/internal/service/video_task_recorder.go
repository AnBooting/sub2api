package service

// PRD-06 / TS-06：创建路径登记视频任务归属。
//
// 为什么用「注册表 + 包级入口」而不是给 handler 增加构造参数：
//   - 任务登记只是创建路径的一行旁路记录，失败绝不能影响创建响应；
//   - 走包级入口可以把对 handler 的改动压到最小（单点调用），
//     也不必改动 OpenAIGatewayHandler 的构造签名与既有测试。
//
// 未注册时入口是安全的 no-op：单测、图片链路、未装配任务表的部署都不会受影响。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// VideoTaskCreateParams 是创建路径能提供的全部归属事实。
//
// 字段刻意与 PRD-06 §2.3 的「本站任务 ↔ 用户 ↔ 实际分组 ↔ 上游任务/账号」一一对应：
// 后续 worker 查询必须回到这里的 GroupID / AccountID，不得重新调度选号。
type VideoTaskCreateParams struct {
	// TaskKey 是客户端可见的任务标识，也是结算占位（SetNX）使用的同一个 key，
	// 保证客户端轮询与 worker 争抢的是同一把锁。
	TaskKey string
	// UpstreamTaskID 是上游返回的原始任务号（用于拼上游查询 URL）。
	UpstreamTaskID string
	UserID         int64
	APIKeyID       int64
	GroupID        int64
	Platform       string
	Model          string
	BillingModel   string
	BillingMode    string
	// AccountID 是实际承载创建的账号；0 表示创建时未绑定。
	AccountID int64
	// Pending 是创建时计费快照，与 Redis pending 同源。
	Pending *GrokVideoPendingBilling
	// CreatedAt 是上游接受创建的时点；零值取当前时间。
	CreatedAt time.Time
}

// VideoTaskRecorder 把创建事实写入本站任务表。
type VideoTaskRecorder struct {
	tasks *VideoTaskService
}

// NewVideoTaskRecorder 构造登记器；tasks 为 nil 时所有调用安全 no-op。
func NewVideoTaskRecorder(tasks *VideoTaskService) *VideoTaskRecorder {
	return &VideoTaskRecorder{tasks: tasks}
}

// RecordCreate 登记一条任务。
//
// 幂等：重复登记（客户端重试、上游重放）由任务表的唯一约束收敛为一行。
// 任何失败只记日志，绝不向创建路径返回错误——任务登记是旁路，不能影响
// 已经成功的上游创建响应。
func (r *VideoTaskRecorder) RecordCreate(ctx context.Context, params VideoTaskCreateParams) {
	if r == nil || r.tasks == nil {
		return
	}
	taskKey := strings.TrimSpace(params.TaskKey)
	if taskKey == "" || params.UserID <= 0 || params.APIKeyID <= 0 || params.GroupID <= 0 {
		return
	}
	snapshot := json.RawMessage("{}")
	if params.Pending != nil {
		if encoded, err := json.Marshal(params.Pending); err == nil {
			snapshot = encoded
		}
	}
	var accountID *int64
	if params.AccountID > 0 {
		value := params.AccountID
		accountID = &value
	}
	_, err := r.tasks.RecordCreatedTask(ctx, &VideoTask{
		TaskNo:          videoTaskNoFromKey(taskKey),
		UserID:          params.UserID,
		APIKeyID:        params.APIKeyID,
		GroupID:         params.GroupID,
		Platform:        params.Platform,
		Model:           params.Model,
		BillingModel:    params.BillingModel,
		BillingMode:     params.BillingMode,
		UpstreamTaskID:  firstNonEmptyString(params.UpstreamTaskID, taskKey),
		AccountID:       accountID,
		BillingSnapshot: snapshot,
		Status:          VideoTaskStatusProcessing,
	})
	if err != nil {
		logger.L().Warn("video_task.record_create_failed",
			zap.Int64("user_id", params.UserID),
			zap.Int64("api_key_id", params.APIKeyID),
			zap.String("task_key", taskKey),
			zap.Error(err),
		)
	}
}

// videoTaskNoFromKey 由客户端任务 key 派生本站任务号（列宽 64）。
//
// 超长 key 不能只做裸截断：两个仅尾部不同的 key 会截断成同一个 task_no，
// 撞上 task_no 唯一约束后第二个任务根本登记不进来（静默丢任务）。
// 因此截断时保留可读前缀并追加内容指纹，保证不同 key 一定得到不同 task_no。
//
// 注意：task_no 只是**可读标识**，不参与结算占位。结算/快照一律用完整的
// VideoTask.BillingKey()（UpstreamTaskID），与客户端轮询的 key 逐字一致。
func videoTaskNoFromKey(taskKey string) string {
	normalized := strings.TrimSpace(taskKey)
	if normalized == "" {
		return ""
	}
	const maxLen = 64
	if len(normalized) <= maxLen {
		return normalized
	}
	sum := sha256.Sum256([]byte(normalized))
	suffix := hex.EncodeToString(sum[:])[:8]
	// 前缀 55 + "-" + 8 位指纹 = 64，按字节截断即可（hex/ASCII 后缀安全）。
	prefix := normalized[:maxLen-len(suffix)-1]
	return prefix + "-" + suffix
}

// defaultVideoTaskRecorder 是进程级登记器。
//
// 与 SetCodexCanonicalUserAgentResolver 同一模式：创建路径（handler）无法方便地
// 拿到 service 装配图，故由 wire 在装配时注入。
var defaultVideoTaskRecorder atomic.Pointer[VideoTaskRecorder]

// SetDefaultVideoTaskRecorder 注入进程级登记器（由 wire 调用）。
func SetDefaultVideoTaskRecorder(recorder *VideoTaskRecorder) {
	defaultVideoTaskRecorder.Store(recorder)
}

// RecordVideoTaskCreate 是创建路径（handler）调用的包级入口。
//
// 未注册时安全 no-op，调用方无需判空、也无需为单测额外装配。
func RecordVideoTaskCreate(ctx context.Context, params VideoTaskCreateParams) {
	if recorder := defaultVideoTaskRecorder.Load(); recorder != nil {
		recorder.RecordCreate(ctx, params)
	}
}
