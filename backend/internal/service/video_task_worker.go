package service

// PRD-06 / TS-06：后台 worker 主动推进视频任务终态。
//
// 解决的核心问题（PRD-06 §4）：「不允许客户端是否查询决定是否扣费」。
// 此前结算完全由客户端轮询驱动，客户端不查 → 永不结算。本 worker 周期扫描
// 本站任务表里未终态的任务，**用原承载账号**查询上游状态并推进终态。
//
// 硬约束（逐条对应）：
//   - §2.3 不重新调度选号：查询只用任务行固化的 account_id，
//     绝不调用任何选号/调度入口。
//   - §4 上游状态不明/超时不得当失败：只有上游**明确**报告终态才推进；
//     传输错误、非 2xx、无法解析一律保持 processing 并记录，等待下次。
//   - 幂等：结算复用 ClaimGrokVideoBilling（SetNX 占位），客户端轮询与 worker
//     抢的是同一把锁，先到者结算、后到者不重复扣费。
//   - 不新写计费：成功一律走既有 RecordUsage；失败只释放占位、不扣费。
//
// 可测性：上游查询与结算原语各自收敛为一个端口（videoTaskStatusQuerier /
// videoTaskBillingPort），生产实现直接委托既有 OpenAIGatewayService，
// 单测用假实现即可验证幂等与「不明不误判」。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// SettingKeyVideoTaskWorker 是后台视频任务推进 worker 的 settings 键。
const SettingKeyVideoTaskWorker = "video_task_worker"

const (
	videoTaskWorkerDefaultInterval = 60 * time.Second
	videoTaskWorkerMinInterval     = 15 * time.Second
	videoTaskWorkerMaxInterval     = 30 * time.Minute

	// 退避：视频生成通常数分钟完成。首查快、之后逐步拉开，避免对承载账号
	// 造成轮询风暴；上限封顶保证长任务不会被无限拖慢。
	videoTaskWorkerBaseBackoff = 30 * time.Second
	videoTaskWorkerMaxBackoff  = 5 * time.Minute

	// videoTaskWorkerSettleWindow 是任务的最长推进窗口（与 Redis pending 快照
	// 24h TTL 对齐）。超过该窗口仍未拿到上游终态的任务不再继续查询：收尾为
	// failed 但**不扣费**。这只是生命周期收尾，不触发任何重生成。
	videoTaskWorkerSettleWindow = 24 * time.Hour

	videoTaskWorkerLeaderLockKey = "video_task_worker:leader"
	videoTaskWorkerLeaderLockTTL = 5 * time.Minute
)

// VideoTaskWorkerSettings 是管理员可配的 worker 开关与周期。
//
// 默认值见 defaultVideoTaskWorkerSettings：**默认开启**且周期保守。理由：
// PRD-06 §4 把「服务端推进终态」定为必须能力，默认关闭等于把该缺口留在
// 默认配置里；周期保守（60s）保证即使开启也不会对上游造成压力。
type VideoTaskWorkerSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds,omitempty"`
	BatchSize       int  `json:"batch_size,omitempty"`
}

func defaultVideoTaskWorkerSettings() *VideoTaskWorkerSettings {
	return &VideoTaskWorkerSettings{
		Enabled:         true,
		IntervalSeconds: int(videoTaskWorkerDefaultInterval.Seconds()),
		BatchSize:       defaultVideoTaskBatchSize,
	}
}

func normalizeVideoTaskWorkerSettings(settings *VideoTaskWorkerSettings) {
	if settings == nil {
		return
	}
	if settings.IntervalSeconds <= 0 {
		settings.IntervalSeconds = int(videoTaskWorkerDefaultInterval.Seconds())
	}
	if settings.IntervalSeconds < int(videoTaskWorkerMinInterval.Seconds()) {
		settings.IntervalSeconds = int(videoTaskWorkerMinInterval.Seconds())
	}
	if settings.IntervalSeconds > int(videoTaskWorkerMaxInterval.Seconds()) {
		settings.IntervalSeconds = int(videoTaskWorkerMaxInterval.Seconds())
	}
	if settings.BatchSize <= 0 {
		settings.BatchSize = defaultVideoTaskBatchSize
	}
	if settings.BatchSize > 500 {
		settings.BatchSize = 500
	}
}

// GetVideoTaskWorkerSettings 返回生效的 worker 设置（缺省即默认值）。
//
// 与 ProvideUpstreamBillingProbeService 的 settings 口径一致：设置缺失、
// 解析失败都回落到默认值，绝不因为配置脏而停掉服务端推进能力。
func (s *SettingService) GetVideoTaskWorkerSettings(ctx context.Context) (*VideoTaskWorkerSettings, error) {
	defaults := defaultVideoTaskWorkerSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyVideoTaskWorker)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get video task worker settings: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return defaults, nil
	}
	settings := *defaults
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("parse video task worker settings: %w", err)
	}
	normalizeVideoTaskWorkerSettings(&settings)
	return &settings, nil
}

// SetVideoTaskWorkerSettings 校验并持久化 worker 设置。
func (s *SettingService) SetVideoTaskWorkerSettings(ctx context.Context, settings *VideoTaskWorkerSettings) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("setting repository is unavailable")
	}
	if settings == nil {
		return infraerrors.BadRequest("INVALID_VIDEO_TASK_WORKER_SETTINGS", "settings cannot be nil")
	}
	if settings.IntervalSeconds != 0 &&
		(settings.IntervalSeconds < int(videoTaskWorkerMinInterval.Seconds()) ||
			settings.IntervalSeconds > int(videoTaskWorkerMaxInterval.Seconds())) {
		return infraerrors.BadRequest(
			"INVALID_VIDEO_TASK_WORKER_INTERVAL",
			fmt.Sprintf("interval_seconds must be between %d and %d",
				int(videoTaskWorkerMinInterval.Seconds()), int(videoTaskWorkerMaxInterval.Seconds())),
		)
	}
	if settings.BatchSize < 0 || settings.BatchSize > 500 {
		return infraerrors.BadRequest("INVALID_VIDEO_TASK_WORKER_BATCH_SIZE",
			"batch_size must be between 0 and 500")
	}
	normalizeVideoTaskWorkerSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal video task worker settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyVideoTaskWorker, string(data))
}

// videoTaskUpstreamState 是上游状态的分类结果。
//
// unknown 是**默认**且必须保持 processing：传输失败、非 2xx、无法解析都归到
// 这里，绝不当成失败（PRD-06 §4）。
type videoTaskUpstreamState int

const (
	videoTaskUpstreamUnknown videoTaskUpstreamState = iota
	videoTaskUpstreamPending
	videoTaskUpstreamSucceeded
	videoTaskUpstreamFailed
)

// videoTaskBillingPort 是 worker 依赖的**既有**结算原语集合。
//
// 刻意只暴露这几个方法：worker 不新写计费，所有金额结算都经由
// ClaimGrokVideoBilling（SetNX 幂等占位）+ RecordUsage 完成。
type videoTaskBillingPort interface {
	LoadPendingBilling(ctx context.Context, requestID string, userID, apiKeyID int64) (*GrokVideoPendingBilling, error)
	ClaimBilled(ctx context.Context, requestID string, userID, apiKeyID int64) (bool, error)
	ReleaseBilled(ctx context.Context, requestID string, userID, apiKeyID int64) error
	PendingHasInputVideo(ctx context.Context, requestID string, userID, apiKeyID int64) bool
	RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error
}

// videoTaskStatusQuerier 用**原承载账号**查询上游任务状态。
//
// 返回 unknown 的语义是「状态不明，保持 processing」，调用方不得据此判定失败。
type videoTaskStatusQuerier interface {
	QueryStatus(ctx context.Context, task *VideoTask, account *Account) (videoTaskUpstreamState, []byte, error)
}

// 以下四个窄端口只声明 worker 真正用到的方法。
//
// 为什么不直接依赖 AccountRepository/APIKeyRepository 这类宽接口：宽接口把
// 仓储的全部能力（选号、迁移、批量操作）都暴露给 worker，既扩大耦合，也让
// 「worker 绝不重新调度选号」这条硬约束无法在类型层面自证。窄端口只给
// 「按 ID 读一条」，选号入口在类型上就不可达。
type videoTaskAccountReader interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
}

type videoTaskAPIKeyReader interface {
	GetByID(ctx context.Context, id int64) (*APIKey, error)
}

type videoTaskUserReader interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type videoTaskSubscriptionReader interface {
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
}

// VideoTaskWorker 周期推进未终态的视频任务。
type VideoTaskWorker struct {
	tasks        *VideoTaskService
	upstream     videoTaskStatusQuerier
	billing      videoTaskBillingPort
	apiKeyRepo   videoTaskAPIKeyReader
	userRepo     videoTaskUserReader
	userSubRepo  videoTaskSubscriptionReader
	accountRepo  videoTaskAccountReader
	apiKeySvc    *APIKeyService
	settingsRepo SettingRepository
	lockCache    LeaderLockCache
	db           *sql.DB

	mu        sync.Mutex
	started   bool
	stopped   bool
	parentCtx context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewVideoTaskWorker 构造 worker。
//
// gateway 为 nil 时仍返回可用对象（扫描/收尾照常），只是无法查询上游与结算：
// 此时任务保持 processing 并记录，绝不误判失败。
func NewVideoTaskWorker(
	tasks *VideoTaskService,
	gateway *OpenAIGatewayService,
	apiKeyRepo videoTaskAPIKeyReader,
	userRepo videoTaskUserReader,
	userSubRepo videoTaskSubscriptionReader,
	accountRepo videoTaskAccountReader,
	apiKeySvc *APIKeyService,
	settingsRepo SettingRepository,
) *VideoTaskWorker {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &VideoTaskWorker{
		tasks:        tasks,
		apiKeyRepo:   apiKeyRepo,
		userRepo:     userRepo,
		userSubRepo:  userSubRepo,
		accountRepo:  accountRepo,
		apiKeySvc:    apiKeySvc,
		settingsRepo: settingsRepo,
		parentCtx:    ctx,
		cancel:       cancel,
	}
	if gateway != nil {
		worker.billing = &gatewayVideoTaskBilling{gateway: gateway}
		worker.upstream = &gatewayVideoTaskStatusQuerier{gateway: gateway}
	}
	return worker
}

// SetLeaderLock 注入跨实例互斥（多副本部署时同一轮只跑一个实例）。
func (s *VideoTaskWorker) SetLeaderLock(cache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = cache
	s.db = db
}

// Start 启动后台循环；重复调用安全。
func (s *VideoTaskWorker) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runLoop()
}

// Stop 优雅停止：取消父 ctx 并等待循环退出（复用既有 cancel+wg 模式）。
func (s *VideoTaskWorker) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *VideoTaskWorker) runLoop() {
	defer s.wg.Done()
	// 首次启动稍作延迟：给应用完成装配、避免与启动期其它任务抢资源。
	select {
	case <-s.parentCtx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	for {
		interval := s.effectiveInterval()
		if _, _, err := s.RunDue(s.parentCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.L().Warn("video_task_worker.run_due_failed", zap.Error(err))
		}
		select {
		case <-s.parentCtx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// effectiveInterval 解析生效周期：settings 覆盖 → 默认值。
func (s *VideoTaskWorker) effectiveInterval() time.Duration {
	settings := s.loadSettings()
	if settings == nil || settings.IntervalSeconds <= 0 {
		return videoTaskWorkerDefaultInterval
	}
	return time.Duration(settings.IntervalSeconds) * time.Second
}

func (s *VideoTaskWorker) loadSettings() *VideoTaskWorkerSettings {
	defaults := defaultVideoTaskWorkerSettings()
	if s == nil || s.settingsRepo == nil {
		return defaults
	}
	ctx, cancel := context.WithTimeout(s.parentCtx, 5*time.Second)
	defer cancel()
	raw, err := s.settingsRepo.GetValue(ctx, SettingKeyVideoTaskWorker)
	if err != nil || strings.TrimSpace(raw) == "" {
		return defaults
	}
	settings := *defaults
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		logger.L().Warn("video_task_worker.settings_parse_failed", zap.Error(err))
		return defaults
	}
	normalizeVideoTaskWorkerSettings(&settings)
	return &settings
}

// RunDue 执行一轮扫描；供测试直接驱动（不需要起 goroutine）。
//
// 返回值语义：(处理数, 结算成功数, error)。
func (s *VideoTaskWorker) RunDue(ctx context.Context) (int, int, error) {
	if s == nil || s.tasks == nil {
		return 0, 0, nil
	}
	settings := s.loadSettings()
	if settings != nil && !settings.Enabled {
		return 0, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// 多副本部署时同一轮只让一个实例跑；单实例/无 Redis 时 tryAcquire 直接放行。
	release, acquired := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, videoTaskWorkerLeaderLockKey, "video_task_worker", videoTaskWorkerLeaderLockTTL)
	if !acquired {
		return 0, 0, nil
	}
	defer release()

	batchSize := defaultVideoTaskBatchSize
	if settings != nil && settings.BatchSize > 0 {
		batchSize = settings.BatchSize
	}
	tasks, err := s.tasks.ListDue(ctx, time.Now(), batchSize)
	if err != nil {
		return 0, 0, err
	}
	processed, settled := 0, 0
	for _, task := range tasks {
		if task == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return processed, settled, ctx.Err()
		default:
		}
		processed++
		if s.processTask(ctx, task) {
			settled++
		}
	}
	return processed, settled, nil
}

// processTask 推进单个任务；返回是否已写入终态（结算或失败收尾）。
func (s *VideoTaskWorker) processTask(ctx context.Context, task *VideoTask) bool {
	// 超出推进窗口：收尾为 failed，不扣费，也不触发重生成。
	if !task.CreatedAt.IsZero() && time.Since(task.CreatedAt) > videoTaskWorkerSettleWindow {
		s.finalizeFailed(ctx, task, "settle_window_exceeded: no upstream terminal state within the settlement window")
		return true
	}
	// 缺少承载账号：不能重新选号（§2.3），只能记录并保持 processing。
	if task.AccountID == nil || *task.AccountID <= 0 {
		s.deferTask(ctx, task, "carrying account is not bound; refusing to re-schedule account selection")
		return false
	}
	if s.upstream == nil || s.accountRepo == nil {
		s.deferTask(ctx, task, "upstream querier or account repository is unavailable")
		return false
	}
	account, err := s.accountRepo.GetByID(ctx, *task.AccountID)
	if err != nil || account == nil {
		s.deferTask(ctx, task, fmt.Sprintf("load carrying account %d failed: %v", *task.AccountID, err))
		return false
	}
	state, body, err := s.upstream.QueryStatus(ctx, task, account)
	if err != nil {
		s.deferTask(ctx, task, err.Error())
		return false
	}
	switch state {
	case videoTaskUpstreamSucceeded:
		if s.settleSuccess(ctx, task, account, body) {
			return true
		}
		// 结算未完成（缺价、RecordUsage 失败等）→ 释放占位并保持 processing，
		// 让下一轮或客户端轮询重试，绝不静默丢弃这笔费用。
		s.deferTask(ctx, task, "settlement deferred: upstream succeeded but billing did not complete")
		return false
	case videoTaskUpstreamFailed:
		s.finalizeFailed(ctx, task, "upstream reported a terminal failure state")
		return true
	default:
		// pending 或 unknown：都保持 processing（§4）。
		s.deferTask(ctx, task, "")
		return false
	}
}

// deferTask 记录一次尝试并安排下次检查，保持 processing。
func (s *VideoTaskWorker) deferTask(ctx context.Context, task *VideoTask, reason string) {
	if s.tasks == nil {
		return
	}
	backoff := videoTaskBackoff(task.Attempts)
	if err := s.tasks.MarkChecked(ctx, task, time.Now().Add(backoff), reason); err != nil {
		logger.L().Warn("video_task_worker.mark_checked_failed",
			zap.String("task_no", task.TaskNo), zap.Error(err))
	}
}

// videoTaskBackoff 指数退避并封顶。
func videoTaskBackoff(attempts int) time.Duration {
	backoff := videoTaskWorkerBaseBackoff
	for i := 0; i < attempts && backoff < videoTaskWorkerMaxBackoff; i++ {
		backoff *= 2
	}
	if backoff > videoTaskWorkerMaxBackoff {
		backoff = videoTaskWorkerMaxBackoff
	}
	return backoff
}

// finalizeFailed 写入失败终态：释放结算占位（不扣费），不触发任何重生成。
func (s *VideoTaskWorker) finalizeFailed(ctx context.Context, task *VideoTask, reason string) {
	if s.billing != nil {
		// 用完整任务标识释放占位：必须与客户端 ClaimGrokVideoBilling 的 key 一致，
		// 否则释放的不是同一把锁（见 VideoTask.BillingKey 的说明）。
		if err := s.billing.ReleaseBilled(ctx, task.BillingKey(), task.UserID, task.APIKeyID); err != nil {
			logger.L().Warn("video_task_worker.release_claim_failed",
				zap.String("task_no", task.TaskNo), zap.Error(err))
		}
	}
	if s.tasks == nil {
		return
	}
	if _, err := s.tasks.MarkFailed(ctx, task, time.Now(), reason); err != nil {
		logger.L().Warn("video_task_worker.mark_failed_failed",
			zap.String("task_no", task.TaskNo), zap.Error(err))
	}
}

// settleSuccess 结算一次成功任务。
//
// 幂等（关键）：ClaimBilled 是 SetNX 占位，与客户端轮询使用同一个 key
// （userID, apiKeyID, taskKey）。worker 与客户端同时到达终态时，只有先到者
// 拿到占位并结算，后到者直接返回——这就是「只结算一次」。
func (s *VideoTaskWorker) settleSuccess(ctx context.Context, task *VideoTask, account *Account, statusBody []byte) bool {
	if s.billing == nil || s.apiKeyRepo == nil {
		return false
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, task.APIKeyID)
	if err != nil || apiKey == nil {
		logger.L().Warn("video_task_worker.load_api_key_failed",
			zap.String("task_no", task.TaskNo), zap.Error(err))
		return false
	}
	// 归属一致性复核：任务行固化的用户/分组必须与当前 Key 事实一致，
	// 否则不结算（防止 Key 被迁移/删除后错误扣费到别的分组）。
	if apiKey.UserID != task.UserID {
		logger.L().Warn("video_task_worker.api_key_owner_mismatch",
			zap.String("task_no", task.TaskNo),
			zap.Int64("task_user_id", task.UserID),
			zap.Int64("api_key_user_id", apiKey.UserID))
		return false
	}
	if apiKey.Group == nil || apiKey.GroupID == nil || *apiKey.GroupID != task.GroupID {
		logger.L().Warn("video_task_worker.api_key_group_mismatch",
			zap.String("task_no", task.TaskNo),
			zap.Int64("task_group_id", task.GroupID))
		return false
	}

	pending := s.loadPendingSnapshot(ctx, task)
	result := buildVideoTaskSettlementResult(task, statusBody, pending)
	if result == nil {
		return false
	}
	// Seedance per_token 计划需要 token 用量；缺失时不结算（保持 processing），
	// 与 handler 的 fail-closed 门槛一致。
	if strings.EqualFold(strings.TrimSpace(task.Platform), VideoTaskPlatformSeedance) &&
		SeedanceCompletionRequiresTokens(apiKey.Group, result.BillingModel) && result.Usage.OutputTokens <= 0 {
		logger.L().Warn("video_task_worker.seedance_missing_token_usage",
			zap.String("task_no", task.TaskNo))
		return false
	}

	// 关键：用完整任务标识（UpstreamTaskID）占位，与客户端轮询路径逐字一致。
	// 若这里误用被截断的 TaskNo，超长 key 会让两边各抢到一把锁 → 重复扣费。
	claimed, err := s.billing.ClaimBilled(ctx, task.BillingKey(), task.UserID, task.APIKeyID)
	if err != nil {
		logger.L().Warn("video_task_worker.claim_failed", zap.String("task_no", task.TaskNo), zap.Error(err))
		return false
	}
	if !claimed {
		// 客户端轮询（或并发 worker）已经结算过：不重复扣费，仅收尾任务行。
		s.markCompleted(ctx, task)
		return true
	}

	user := s.loadUser(ctx, task.UserID, apiKey)
	subscription := s.loadSubscription(ctx, task.UserID, task.GroupID)

	// Seedance per_token 的第二档维度（请求是否含视频输入）：优先 Redis 快照，
	// Redis 过期后回落到任务表快照。只在有明确证据时才置 true（不打折猜测）。
	if canonicalSeedanceVideoPriceFamily(result.BillingModel) != "" || canonicalSeedanceVideoPriceFamily(result.Model) != "" {
		hasInputVideo := s.billing.PendingHasInputVideo(ctx, task.BillingKey(), task.UserID, task.APIKeyID)
		if !hasInputVideo && pending != nil {
			hasInputVideo = pending.HasInputVideo
		}
		if hasInputVideo {
			ctx = WithVideoPlanInputVideoFact(ctx, true)
		}
	}

	if err := s.billing.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             apiKey,
		User:               user,
		Account:            account,
		Subscription:       subscription,
		InboundEndpoint:    videoTaskInboundEndpoint(task.Platform),
		UpstreamEndpoint:   videoTaskUpstreamEndpoint(task.Platform),
		RequestPayloadHash: HashUsageRequestPayload([]byte(task.BillingKey())),
		APIKeyService:      s.apiKeySvc,
		QuotaPlatform:      QuotaPlatform(ctx, apiKey),
	}); err != nil {
		// 结算失败必须释放占位，否则这笔费用永远无法再结算（与 handler 同语义）。
		if releaseErr := s.billing.ReleaseBilled(ctx, task.BillingKey(), task.UserID, task.APIKeyID); releaseErr != nil {
			logger.L().Warn("video_task_worker.release_after_record_failure",
				zap.String("task_no", task.TaskNo), zap.Error(releaseErr))
		}
		logger.L().Error("video_task_worker.record_usage_failed",
			zap.String("task_no", task.TaskNo),
			zap.Int64("user_id", task.UserID),
			zap.Int64("api_key_id", task.APIKeyID),
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
		return false
	}
	s.markCompleted(ctx, task)
	logger.L().Info("video_task_worker.settled",
		zap.String("task_no", task.TaskNo),
		zap.Int64("user_id", task.UserID),
		zap.Int64("api_key_id", task.APIKeyID),
		zap.Int64("account_id", account.ID),
		zap.String("model", result.BillingModel),
	)
	return true
}

func (s *VideoTaskWorker) markCompleted(ctx context.Context, task *VideoTask) {
	if s.tasks == nil {
		return
	}
	if _, err := s.tasks.MarkCompleted(ctx, task, time.Now()); err != nil {
		logger.L().Warn("video_task_worker.mark_completed_failed",
			zap.String("task_no", task.TaskNo), zap.Error(err))
	}
}

// loadPendingSnapshot 取创建时计费快照：优先 Redis（新），回落任务表
// （Redis 24h 过期后仍可按原价结算，而不是猜价）。
func (s *VideoTaskWorker) loadPendingSnapshot(ctx context.Context, task *VideoTask) *GrokVideoPendingBilling {
	if s.billing != nil {
		if pending, err := s.billing.LoadPendingBilling(ctx, task.BillingKey(), task.UserID, task.APIKeyID); err == nil && pending != nil {
			return pending
		}
	}
	if len(task.BillingSnapshot) == 0 {
		return nil
	}
	var pending GrokVideoPendingBilling
	if err := json.Unmarshal(task.BillingSnapshot, &pending); err != nil {
		return nil
	}
	return &pending
}

func (s *VideoTaskWorker) loadUser(ctx context.Context, userID int64, apiKey *APIKey) *User {
	if apiKey != nil && apiKey.User != nil {
		return apiKey.User
	}
	if s.userRepo == nil {
		return nil
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil
	}
	return user
}

func (s *VideoTaskWorker) loadSubscription(ctx context.Context, userID, groupID int64) *UserSubscription {
	if s.userSubRepo == nil || groupID <= 0 {
		return nil
	}
	subscription, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil
	}
	return subscription
}

// buildVideoTaskSettlementResult 把上游成功状态 + 计费快照合成结算结果。
//
// 与 handler 的 prepareGrokVideoCompletionBilling / prepareSeedanceCompletionBilling
// 同口径：分辨率优先取快照（上游状态常不返回），时长取上游/快照，RequestID 强制为
// 稳定任务号以命中既有 usage_billing_dedup。
func buildVideoTaskSettlementResult(task *VideoTask, statusBody []byte, pending *GrokVideoPendingBilling) *OpenAIForwardResult {
	if task == nil || len(statusBody) == 0 {
		return nil
	}
	isSeedance := strings.EqualFold(strings.TrimSpace(task.Platform), VideoTaskPlatformSeedance)
	// billingKey 是完整任务标识：结算结果里的 ResponseID/RequestID 必须与客户端
	// 路径同源，否则 usage_billing_dedup 也会因截断而错开。
	billingKey := task.BillingKey()
	var result *OpenAIForwardResult
	if isSeedance {
		result = &OpenAIForwardResult{
			ResponseID:    billingKey,
			Model:         firstNonEmptyString(task.Model, task.BillingModel),
			BillingModel:  firstNonEmptyString(task.BillingModel, task.Model),
			UpstreamModel: strings.TrimSpace(gjson.GetBytes(statusBody, "model").String()),
			Usage: OpenAIUsage{
				OutputTokens: max(0, int(gjson.GetBytes(statusBody, "usage.completion_tokens").Int())),
			},
			VideoDurationSeconds: SeedanceDurationSecondsFromStatusBody(statusBody),
			VideoResolution:      SeedanceResolutionFromStatusBody(statusBody),
		}
	} else {
		result = ExtractGrokVideoBillingFromStatusBody(statusBody, pending, billingKey)
		if result == nil {
			return nil
		}
	}
	if pending != nil {
		if strings.TrimSpace(result.Model) == "" {
			result.Model = firstNonEmptyString(pending.BillingModel, pending.Model, pending.OriginalModel)
		}
		if strings.TrimSpace(result.BillingModel) == "" {
			result.BillingModel = firstNonEmptyString(pending.BillingModel, pending.Model, result.Model)
		}
		if strings.TrimSpace(result.UpstreamModel) == "" {
			result.UpstreamModel = pending.UpstreamModel
		}
		if strings.TrimSpace(pending.VideoResolution) != "" {
			result.VideoResolution = pending.VideoResolution
		}
		if result.VideoDurationSeconds <= 0 {
			result.VideoDurationSeconds = pending.VideoDurationSeconds
		}
		if e2e := GrokVideoE2EDuration(pending.CreatedAt, time.Now()); e2e > 0 {
			result.Duration = e2e
		}
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = firstNonEmptyString(task.Model, task.BillingModel)
	}
	if strings.TrimSpace(result.BillingModel) == "" {
		result.BillingModel = firstNonEmptyString(task.BillingModel, result.Model)
	}
	// 强制稳定任务号：worker 与客户端共享同一去重键（完整标识，不截断）。
	result.RequestID = StableGrokVideoBillingRequestID(firstNonEmptyString(result.ResponseID, billingKey))
	result.ResponseID = firstNonEmptyString(result.ResponseID, billingKey)
	if !isSeedance {
		result.VideoCount = 1
		result.ImageCount = 0
		result.VideoResolution = NormalizeVideoBillingResolutionOrDefault(result.VideoResolution)
		result.VideoDurationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(result.VideoDurationSeconds)
	}
	return result
}

// videoTaskInboundEndpoint / videoTaskUpstreamEndpoint 为 worker 结算行提供端点归因。
//
// worker 没有 HTTP 请求上下文，用与既有异步媒体路径一致的常量标签，
// 保证用量行与客户端结算行同维度（否则按端点统计会漏掉 worker 结算的那部分）。
func videoTaskInboundEndpoint(platform string) string {
	if strings.EqualFold(strings.TrimSpace(platform), VideoTaskPlatformSeedance) {
		return "/api/v3/contents/generations/tasks"
	}
	return "/v1/videos"
}

func videoTaskUpstreamEndpoint(platform string) string {
	if strings.EqualFold(strings.TrimSpace(platform), VideoTaskPlatformSeedance) {
		return "/api/v3/contents/generations/tasks"
	}
	return "/v1/videos"
}

// ──────────────────────────────────────────────────────────
// 生产适配器：直接委托既有 OpenAIGatewayService
// ──────────────────────────────────────────────────────────

// gatewayVideoTaskBilling 把既有结算原语适配为 videoTaskBillingPort。
type gatewayVideoTaskBilling struct {
	gateway *OpenAIGatewayService
}

func (g *gatewayVideoTaskBilling) LoadPendingBilling(ctx context.Context, requestID string, userID, apiKeyID int64) (*GrokVideoPendingBilling, error) {
	return g.gateway.LoadGrokVideoPendingBilling(ctx, requestID, userID, apiKeyID)
}

func (g *gatewayVideoTaskBilling) ClaimBilled(ctx context.Context, requestID string, userID, apiKeyID int64) (bool, error) {
	return g.gateway.ClaimGrokVideoBilling(ctx, requestID, userID, apiKeyID)
}

func (g *gatewayVideoTaskBilling) ReleaseBilled(ctx context.Context, requestID string, userID, apiKeyID int64) error {
	return g.gateway.ReleaseGrokVideoBilling(ctx, requestID, userID, apiKeyID)
}

func (g *gatewayVideoTaskBilling) PendingHasInputVideo(ctx context.Context, requestID string, userID, apiKeyID int64) bool {
	return g.gateway.GrokVideoPendingHasInputVideo(ctx, requestID, userID, apiKeyID)
}

func (g *gatewayVideoTaskBilling) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
	return g.gateway.RecordUsage(ctx, input)
}

// gatewayVideoTaskStatusQuerier 用原承载账号查询上游状态。
//
// 刻意不复用 ForwardGrokMedia / ForwardSeedance：那两个入口是请求作用域的
// （写客户端响应、走 failover 选号），与 worker 的「只查原账号、不写响应、
// 不选号」语义冲突。这里只做 URL 构造 + 带原账号凭据的 GET + 纯函数解析。
type gatewayVideoTaskStatusQuerier struct {
	gateway *OpenAIGatewayService
}

func (q *gatewayVideoTaskStatusQuerier) QueryStatus(ctx context.Context, task *VideoTask, account *Account) (videoTaskUpstreamState, []byte, error) {
	if q == nil || q.gateway == nil {
		return videoTaskUpstreamUnknown, nil, errors.New("gateway is unavailable")
	}
	if account == nil {
		return videoTaskUpstreamUnknown, nil, errors.New("carrying account is required")
	}
	upstreamTaskID := strings.TrimSpace(task.UpstreamTaskID)
	if upstreamTaskID == "" {
		return videoTaskUpstreamUnknown, nil, errors.New("upstream task id is missing")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var (
		target string
		token  string
		err    error
	)
	if strings.EqualFold(strings.TrimSpace(task.Platform), VideoTaskPlatformSeedance) {
		target, token, err = q.buildSeedanceStatusRequest(account, upstreamTaskID)
	} else {
		target, token, err = q.buildGrokStatusRequest(queryCtx, account, upstreamTaskID)
	}
	if err != nil {
		return videoTaskUpstreamUnknown, nil, fmt.Errorf("build upstream status request: %w", err)
	}

	req, err := http.NewRequestWithContext(queryCtx, http.MethodGet, target, nil)
	if err != nil {
		return videoTaskUpstreamUnknown, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := q.gateway.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		// 传输错误 = 状态不明，保持 processing（§4）。
		return videoTaskUpstreamUnknown, nil, fmt.Errorf("upstream status transport error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readVideoTaskUpstreamBody(resp.Body, q.gateway.cfg)
	if err != nil {
		return videoTaskUpstreamUnknown, nil, fmt.Errorf("read upstream status body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 非 2xx 一律视为状态不明：可能是账号/代理/上游瞬时问题，不能据此
		// 判定任务失败（§4）。记录状态码便于排障。
		return videoTaskUpstreamUnknown, body, fmt.Errorf("upstream status returned %d", resp.StatusCode)
	}
	return classifyVideoTaskUpstreamState(task.Platform, body), body, nil
}

func (q *gatewayVideoTaskStatusQuerier) buildGrokStatusRequest(ctx context.Context, account *Account, upstreamTaskID string) (string, string, error) {
	target, err := buildGrokMediaURL(account, q.gateway.cfg, GrokMediaEndpointVideoStatus, upstreamTaskID)
	if err != nil {
		return "", "", err
	}
	// GetRequestCredential 是请求路径的凭据契约收口点，且对 nil gin.Context 安全。
	token, _, err := q.gateway.GetRequestCredential(ctx, nil, account)
	if err != nil {
		return "", "", err
	}
	return target, token, nil
}

func (q *gatewayVideoTaskStatusQuerier) buildSeedanceStatusRequest(account *Account, upstreamTaskID string) (string, string, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) {
		return "", "", errors.New("seedance requires an OpenAI API key account with a custom base URL")
	}
	base, err := q.gateway.validateUpstreamBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return "", "", err
	}
	taskID := strings.TrimPrefix(upstreamTaskID, "seedance:")
	target, err := buildSeedanceURL(base, SeedanceEndpointStatus, taskID)
	if err != nil {
		return "", "", err
	}
	token := strings.TrimSpace(account.GetCredential("api_key"))
	if token == "" {
		return "", "", errors.New("seedance account missing api_key")
	}
	return target, token, nil
}

// classifyVideoTaskUpstreamState 把上游响应体归类为终态判定。
//
// 只有**明确**的终态才推进；其余一律 unknown/pending → 保持 processing。
func classifyVideoTaskUpstreamState(platform string, body []byte) videoTaskUpstreamState {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return videoTaskUpstreamUnknown
	}
	if strings.EqualFold(strings.TrimSpace(platform), VideoTaskPlatformSeedance) {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "status").String())) {
		case "succeeded", "success", "completed":
			return videoTaskUpstreamSucceeded
		case "failed", "canceled", "cancelled", "expired":
			return videoTaskUpstreamFailed
		case "queued", "running", "pending", "processing":
			return videoTaskUpstreamPending
		default:
			return videoTaskUpstreamUnknown
		}
	}
	// xAI 官方枚举：pending | done | expired | failed。
	// done 只有同时带 video.url 才算成功（与 IsGrokVideoStatusBillable 同口径）。
	if IsGrokVideoStatusBillable(body) {
		return videoTaskUpstreamSucceeded
	}
	switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "status").String())) {
	case "failed", "expired", "canceled", "cancelled":
		return videoTaskUpstreamFailed
	case "pending", "processing", "running", "queued", "done":
		// done 但没有 video.url：还不是可结算的成功，继续等。
		return videoTaskUpstreamPending
	default:
		return videoTaskUpstreamUnknown
	}
}

// readVideoTaskUpstreamBody 读取上游响应体，带与既有链路一致的体积上限。
func readVideoTaskUpstreamBody(reader io.Reader, cfg *config.Config) ([]byte, error) {
	limit := resolveUpstreamResponseReadLimit(cfg)
	return readUpstreamResponseBodyLimited(reader, limit)
}
