package service

// PRD-06 / TS-06：异步视频的本站任务账本。
//
// 为什么需要它（PRD-06 §2.3 / §2.4 / §4）：
//   - 此前结算完全由客户端轮询驱动：客户端不查 → 永不结算，直接违反 §4
//     「不允许客户端是否查询决定是否扣费」。
//   - §2.3 要求「后续查询仍回到原任务渠道，不能每次查状态重新调度」，
//     所以任务行必须固化 user_id / api_key_id / group_id / account_id，
//     worker 只能拿原承载账号去查上游。
//   - §2.4 允许「只补该批协议要求的最小生命周期」：processing/completed/failed 三态。
//
// 边界：本服务不引入第二套计费。金额结算仍由既有
// ClaimGrokVideoBilling（SetNX 幂等占位）+ RecordUsage 完成；本服务只负责
// 「谁的任务、用哪个账号、查到什么终态、有没有推进过」。
//
// 归属校验（BE-03）：查询一律按 (user_id, api_key_id) 限定，绝不跨用户泄漏；
// 上游凭据不落库（account_id 只是外键引用）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	VideoTaskStatusProcessing = "processing"
	VideoTaskStatusCompleted  = "completed"
	VideoTaskStatusFailed     = "failed"
)

// defaultVideoTaskBatchSize 限制单轮 worker 扫描的任务数，避免一次扫描把
// 上游查询风暴打进承载账号。默认保守（见 video_task_worker.go 的周期说明）。
const defaultVideoTaskBatchSize = 50

// VideoTaskPlatformGrok / VideoTaskPlatformSeedance 是任务行记录的平台标识，
// 决定 worker 用哪套上游状态协议去查。
const (
	VideoTaskPlatformGrok     = "grok"
	VideoTaskPlatformSeedance = "openai"
)

var (
	ErrVideoTaskNotFound    = infraerrors.New(404, "VIDEO_TASK_NOT_FOUND", "video task not found")
	ErrVideoTaskForbidden   = infraerrors.New(403, "VIDEO_TASK_FORBIDDEN", "video task does not belong to this API key")
	ErrVideoTaskUnavailable = infraerrors.New(503, "VIDEO_TASK_UNAVAILABLE", "video task storage is unavailable")
)

// ErrVideoTaskStoreNotConfigured 在仓储未装配（无 *sql.DB）时返回，
// 让上层 fail closed 而不是在构造期 panic。
var ErrVideoTaskStoreNotConfigured = errors.New("video task store is not configured")

// VideoTask 是一行本站异步视频任务。
//
// 它刻意不携带任何上游凭据：account_id 只是外键，凭据仍由
// OpenAIGatewayService 在查询时按既有账号凭据链路取得。
type VideoTask struct {
	ID             int64
	TaskNo         string
	UserID         int64
	APIKeyID       int64
	GroupID        int64
	Platform       string
	Model          string
	BillingModel   string
	BillingMode    string
	UpstreamTaskID string
	// AccountID 是实际承载创建请求的账号。worker 必须复用它查询上游，
	// 不得重新调度选号（PRD-06 §2.3）。NULL 表示创建时未能绑定账号。
	AccountID *int64
	// BillingSnapshot 是创建时计费快照（与 Redis pending 同源）。Redis 快照
	// 24h 过期后，worker 仍能按原价结算，而不是猜一个价。
	BillingSnapshot json.RawMessage
	Status          string
	Attempts        int
	LastError       string
	LastCheckedAt   *time.Time
	NextCheckAt     *time.Time
	SettledAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// VideoTaskRepository 是任务表的持久化端口（实现见 internal/repository）。
type VideoTaskRepository interface {
	// Create 插入一行；已存在同一 (user_id, api_key_id, upstream_task_id) 时返回既有行。
	Create(ctx context.Context, task *VideoTask) (*VideoTask, error)
	// GetByTaskNo 按本站任务号查询。
	GetByTaskNo(ctx context.Context, taskNo string) (*VideoTask, error)
	// ListDue 返回未终态且已到检查时刻的任务（PRD-06 §2.4 的扫描入口）。
	ListDue(ctx context.Context, now time.Time, limit int) ([]*VideoTask, error)
	// MarkChecked 记录一次尝试（不改变终态）。
	MarkChecked(ctx context.Context, id int64, attempts int, nextCheckAt time.Time, lastError string) error
	// AttachBilling 把创建时计费事实补写到已登记的任务行（幂等，仅在仍 processing 时生效）。
	AttachBilling(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string, patch VideoTaskBillingPatch) error
	// MarkCompleted / MarkFailed 写入终态；仅当任务仍处于 processing 时生效。
	MarkCompleted(ctx context.Context, id int64, settledAt time.Time) (bool, error)
	MarkFailed(ctx context.Context, id int64, settledAt time.Time, reason string) (bool, error)
}

// BillingKey 返回结算占位与计费快照使用的**完整**任务标识。
//
// 必须与客户端轮询路径逐字一致：handler 用完整 taskRequestID 去
// ClaimGrokVideoBilling / LoadGrokVideoPendingBilling（见 handler/grok_media.go）。
//
// 绝对不能用 TaskNo：task_no 受 VARCHAR(64) 列宽约束，超长 key 会被截断
// （videoTaskNoFromKey），截断后 worker 与客户端拿到的就不是同一把锁，
// 两边都会抢到占位 → 同一任务被扣两次。
//
// 任务行的 UpstreamTaskID 存的是**未截断**的原值（249 迁移里该列为 TEXT，
// 刻意不设短定长上限），因此以它为准；仅当它为空（历史/异常行）时才回落到 TaskNo。
func (t *VideoTask) BillingKey() string {
	if t == nil {
		return ""
	}
	if key := strings.TrimSpace(t.UpstreamTaskID); key != "" {
		return key
	}
	return strings.TrimSpace(t.TaskNo)
}

// VideoTaskBillingPatch 是创建路径补写入的计费事实。
type VideoTaskBillingPatch struct {
	Model           string
	BillingModel    string
	BillingMode     string
	BillingSnapshot json.RawMessage
}

// VideoTaskService 提供任务生命周期与归属校验。
type VideoTaskService struct {
	repo VideoTaskRepository
}

// NewVideoTaskService 构造服务。repo 为 nil 时所有方法 fail closed。
func NewVideoTaskService(repo VideoTaskRepository) *VideoTaskService {
	return &VideoTaskService{repo: repo}
}

func (s *VideoTaskService) ensureRepo() error {
	if s == nil || s.repo == nil {
		return ErrVideoTaskStoreNotConfigured
	}
	return nil
}

// RecordCreatedTask 在创建请求被上游接受后登记任务归属。
//
// 幂等：同一 (user_id, api_key_id, upstream_task_id) 只保留一行，重放创建
// 不会产生重复任务，也不会覆盖已有的推进进度。
func (s *VideoTaskService) RecordCreatedTask(ctx context.Context, task *VideoTask) (*VideoTask, error) {
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrVideoTaskUnavailable
	}
	normalized, err := normalizeVideoTaskForCreate(task)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, normalized)
}

func normalizeVideoTaskForCreate(task *VideoTask) (*VideoTask, error) {
	clone := *task
	clone.TaskNo = strings.TrimSpace(clone.TaskNo)
	clone.UpstreamTaskID = strings.TrimSpace(clone.UpstreamTaskID)
	clone.Platform = strings.TrimSpace(clone.Platform)
	clone.Model = strings.TrimSpace(clone.Model)
	clone.BillingModel = strings.TrimSpace(clone.BillingModel)
	clone.BillingMode = strings.TrimSpace(clone.BillingMode)
	if clone.TaskNo == "" {
		return nil, ErrVideoTaskUnavailable
	}
	if clone.UserID <= 0 || clone.APIKeyID <= 0 || clone.GroupID <= 0 {
		return nil, ErrVideoTaskUnavailable
	}
	if clone.UpstreamTaskID == "" {
		return nil, ErrVideoTaskUnavailable
	}
	if clone.Status == "" {
		clone.Status = VideoTaskStatusProcessing
	}
	if clone.Status != VideoTaskStatusProcessing {
		return nil, ErrVideoTaskUnavailable
	}
	if len(clone.BillingSnapshot) == 0 {
		clone.BillingSnapshot = json.RawMessage("{}")
	}
	return &clone, nil
}

// AttachBilling 在创建路径登记任务后补写计费快照。
//
// 分两步是因为创建路径的两次调用各自持有不同事实：绑定调用知道分组/承载
// 账号，快照调用知道模型与计费参数。两步都幂等，任一步失败都不会让任务行
// 消失（worker 仍可仅凭绑定事实推进终态）。
func (s *VideoTaskService) AttachBilling(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string, patch VideoTaskBillingPatch) error {
	if err := s.ensureRepo(); err != nil {
		return err
	}
	upstreamTaskID = strings.TrimSpace(upstreamTaskID)
	if upstreamTaskID == "" || userID <= 0 || apiKeyID <= 0 {
		return nil
	}
	if len(patch.BillingSnapshot) == 0 {
		patch.BillingSnapshot = json.RawMessage("{}")
	}
	patch.Model = strings.TrimSpace(patch.Model)
	patch.BillingModel = strings.TrimSpace(patch.BillingModel)
	patch.BillingMode = strings.TrimSpace(patch.BillingMode)
	return s.repo.AttachBilling(ctx, userID, apiKeyID, upstreamTaskID, patch)
}

// GetForOwner 返回属于该 (userID, apiKeyID) 的任务。
//
// 鉴权顺序即安全边界：先取行，再校验归属；不匹配一律返回 Forbidden，
// 不区分「不存在」与「不属于你」，避免探测他人任务是否存在。
func (s *VideoTaskService) GetForOwner(ctx context.Context, taskNo string, userID, apiKeyID int64) (*VideoTask, error) {
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	taskNo = strings.TrimSpace(taskNo)
	if taskNo == "" {
		return nil, ErrVideoTaskNotFound
	}
	task, err := s.repo.GetByTaskNo(ctx, taskNo)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrVideoTaskNotFound
	}
	if task.UserID != userID || task.APIKeyID != apiKeyID {
		return nil, ErrVideoTaskForbidden
	}
	return task, nil
}

// ListDue 供 worker 扫描到期任务。
func (s *VideoTaskService) ListDue(ctx context.Context, now time.Time, limit int) ([]*VideoTask, error) {
	if err := s.ensureRepo(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultVideoTaskBatchSize
	}
	if now.IsZero() {
		now = time.Now()
	}
	return s.repo.ListDue(ctx, now, limit)
}

// MarkChecked 记录一次上游查询尝试（保持 processing）。
func (s *VideoTaskService) MarkChecked(ctx context.Context, task *VideoTask, nextCheckAt time.Time, lastError string) error {
	if err := s.ensureRepo(); err != nil {
		return err
	}
	if task == nil {
		return ErrVideoTaskUnavailable
	}
	return s.repo.MarkChecked(ctx, task.ID, task.Attempts+1, nextCheckAt, strings.TrimSpace(lastError))
}

// MarkCompleted 写入成功终态（仅 processing → completed 生效）。
func (s *VideoTaskService) MarkCompleted(ctx context.Context, task *VideoTask, settledAt time.Time) (bool, error) {
	if err := s.ensureRepo(); err != nil {
		return false, err
	}
	if task == nil {
		return false, ErrVideoTaskUnavailable
	}
	if settledAt.IsZero() {
		settledAt = time.Now()
	}
	return s.repo.MarkCompleted(ctx, task.ID, settledAt)
}

// MarkFailed 写入失败终态（仅 processing → failed 生效）。
//
// 注意：上游状态不明/超时**不得**走这里（PRD-06 §4）。只有上游明确报告
// failed/expired/canceled，或任务超出结算窗口时才调用。
func (s *VideoTaskService) MarkFailed(ctx context.Context, task *VideoTask, settledAt time.Time, reason string) (bool, error) {
	if err := s.ensureRepo(); err != nil {
		return false, err
	}
	if task == nil {
		return false, ErrVideoTaskUnavailable
	}
	if settledAt.IsZero() {
		settledAt = time.Now()
	}
	return s.repo.MarkFailed(ctx, task.ID, settledAt, strings.TrimSpace(reason))
}
