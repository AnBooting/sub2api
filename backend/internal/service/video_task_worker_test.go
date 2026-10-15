//go:build unit

package service

// PRD-06/TS-06 的硬性不变量测试。
//
// 覆盖任务要求明确点名的四条：
//   1. 幂等 / 不重复扣费；
//   2. 客户端轮询与 worker 同时到达终态只结算一次；
//   3. 上游状态不明（传输错误 / 非 2xx / 无法解析）不得当失败，保持 processing；
//   4. 不重新调度选号（查询必须用原承载账号）。
//
// 全部走假实现，不需要 PostgreSQL / Redis / 真实上游。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── 假仓储 ────────────────────────────────────────────────

type fakeVideoTaskRepo struct {
	mu       sync.Mutex
	tasks    []*VideoTask
	dueErr   error
	checked  int
	failed   int
	complete int
}

func (f *fakeVideoTaskRepo) Create(_ context.Context, task *VideoTask) (*VideoTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.tasks {
		if existing.UserID == task.UserID && existing.APIKeyID == task.APIKeyID &&
			existing.UpstreamTaskID == task.UpstreamTaskID {
			return existing, nil
		}
	}
	clone := *task
	clone.ID = int64(len(f.tasks) + 1)
	f.tasks = append(f.tasks, &clone)
	return &clone, nil
}

func (f *fakeVideoTaskRepo) GetByTaskNo(_ context.Context, taskNo string) (*VideoTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.TaskNo == taskNo {
			return task, nil
		}
	}
	return nil, nil
}

func (f *fakeVideoTaskRepo) ListDue(_ context.Context, _ time.Time, limit int) ([]*VideoTask, error) {
	if f.dueErr != nil {
		return nil, f.dueErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*VideoTask, 0, len(f.tasks))
	for _, task := range f.tasks {
		if task.Status != VideoTaskStatusProcessing {
			continue
		}
		out = append(out, task)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeVideoTaskRepo) MarkChecked(_ context.Context, id int64, attempts int, _ time.Time, lastError string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked++
	for _, task := range f.tasks {
		if task.ID == id {
			task.Attempts = attempts
			task.LastError = lastError
		}
	}
	return nil
}

func (f *fakeVideoTaskRepo) AttachBilling(_ context.Context, userID, apiKeyID int64, upstreamTaskID string, patch VideoTaskBillingPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.UserID == userID && task.APIKeyID == apiKeyID && task.UpstreamTaskID == upstreamTaskID {
			if patch.Model != "" {
				task.Model = patch.Model
			}
			if patch.BillingModel != "" {
				task.BillingModel = patch.BillingModel
			}
			if len(patch.BillingSnapshot) > 0 {
				task.BillingSnapshot = patch.BillingSnapshot
			}
		}
	}
	return nil
}

func (f *fakeVideoTaskRepo) MarkCompleted(_ context.Context, id int64, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.ID != id {
			continue
		}
		if task.Status != VideoTaskStatusProcessing {
			return false, nil
		}
		task.Status = VideoTaskStatusCompleted
		f.complete++
		return true, nil
	}
	return false, nil
}

func (f *fakeVideoTaskRepo) MarkFailed(_ context.Context, id int64, _ time.Time, reason string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.ID != id {
			continue
		}
		if task.Status != VideoTaskStatusProcessing {
			return false, nil
		}
		task.Status = VideoTaskStatusFailed
		task.LastError = reason
		f.failed++
		return true, nil
	}
	return false, nil
}

func (f *fakeVideoTaskRepo) statusOf(taskNo string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, task := range f.tasks {
		if task.TaskNo == taskNo {
			return task.Status
		}
	}
	return ""
}

// ── 假结算端口（模拟 Redis SetNX 占位语义）──────────────────

type fakeVideoBilling struct {
	mu            sync.Mutex
	claimed       map[string]bool
	recordCalls   int
	releaseCalls  int
	recordErr     error
	pending       *GrokVideoPendingBilling
	hasInputVideo bool
	onRecord      func()
}

func newFakeVideoBilling() *fakeVideoBilling {
	return &fakeVideoBilling{claimed: map[string]bool{}}
}

func (f *fakeVideoBilling) LoadPendingBilling(_ context.Context, _ string, _, _ int64) (*GrokVideoPendingBilling, error) {
	if f.pending == nil {
		return nil, errors.New("no pending snapshot")
	}
	return f.pending, nil
}

func (f *fakeVideoBilling) ClaimBilled(_ context.Context, requestID string, _, _ int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed[requestID] {
		return false, nil // SetNX 未抢到：别人已结算
	}
	f.claimed[requestID] = true
	return true, nil
}

func (f *fakeVideoBilling) ReleaseBilled(_ context.Context, requestID string, _, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	delete(f.claimed, requestID)
	return nil
}

func (f *fakeVideoBilling) PendingHasInputVideo(_ context.Context, _ string, _, _ int64) bool {
	return f.hasInputVideo
}

func (f *fakeVideoBilling) RecordUsage(_ context.Context, _ *OpenAIRecordUsageInput) error {
	f.mu.Lock()
	f.recordCalls++
	hook := f.onRecord
	err := f.recordErr
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func (f *fakeVideoBilling) recordCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recordCalls
}

// ── 假上游查询 ────────────────────────────────────────────

type fakeVideoStatusQuerier struct {
	state     videoTaskUpstreamState
	body      []byte
	err       error
	mu        sync.Mutex
	accountID int64
	calls     int
	onQuery   func()
}

func (f *fakeVideoStatusQuerier) QueryStatus(_ context.Context, _ *VideoTask, account *Account) (videoTaskUpstreamState, []byte, error) {
	f.mu.Lock()
	f.calls++
	f.accountID = account.ID
	hook := f.onQuery
	state, body, err := f.state, f.body, f.err
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return state, body, err
}

func (f *fakeVideoStatusQuerier) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// ── 假账号 / Key / 用户仓储 ───────────────────────────────

type fakeVideoAccountRepo struct {
	account *Account
	err     error
}

func (f *fakeVideoAccountRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	return f.account, f.err
}

type fakeVideoAPIKeyRepo struct {
	key *APIKey
	err error
}

func (f *fakeVideoAPIKeyRepo) GetByID(_ context.Context, _ int64) (*APIKey, error) {
	return f.key, f.err
}

// ── 测试装配 ──────────────────────────────────────────────

const (
	videoTaskTestUserID    = int64(42)
	videoTaskTestAPIKeyID  = int64(7)
	videoTaskTestGroupID   = int64(3)
	videoTaskTestAccountID = int64(99)
	videoTaskTestTaskKey   = "vid-task-abc"
)

func videoTaskTestGroup() *Group {
	groupID := videoTaskTestGroupID
	return &Group{ID: groupID, Platform: PlatformGrok}
}

func videoTaskTestAPIKey() *APIKey {
	groupID := videoTaskTestGroupID
	return &APIKey{
		ID:      videoTaskTestAPIKeyID,
		UserID:  videoTaskTestUserID,
		GroupID: &groupID,
		Group:   videoTaskTestGroup(),
		User:    &User{ID: videoTaskTestUserID},
	}
}

func videoTaskTestAccount() *Account {
	return &Account{ID: videoTaskTestAccountID, Platform: PlatformGrok}
}

// newTestVideoTaskWorker 组装一个完全脱离 DB/Redis/上游的 worker。
func newTestVideoTaskWorker(t *testing.T, repo *fakeVideoTaskRepo, billing *fakeVideoBilling, querier *fakeVideoStatusQuerier) *VideoTaskWorker {
	t.Helper()
	worker := NewVideoTaskWorker(NewVideoTaskService(repo), nil, nil, nil, nil, nil, nil, nil)
	worker.billing = billing
	worker.upstream = querier
	worker.accountRepo = &fakeVideoAccountRepo{account: videoTaskTestAccount()}
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: videoTaskTestAPIKey()}
	return worker
}

// seedVideoTask 写入一条 processing 任务。
func seedVideoTask(t *testing.T, repo *fakeVideoTaskRepo, mutate ...func(*VideoTask)) *VideoTask {
	t.Helper()
	accountID := videoTaskTestAccountID
	task := &VideoTask{
		TaskNo:         videoTaskTestTaskKey,
		UserID:         videoTaskTestUserID,
		APIKeyID:       videoTaskTestAPIKeyID,
		GroupID:        videoTaskTestGroupID,
		Platform:       VideoTaskPlatformGrok,
		Model:          "grok-imagine-video",
		BillingModel:   "grok-imagine-video",
		UpstreamTaskID: videoTaskTestTaskKey,
		AccountID:      &accountID,
		Status:         VideoTaskStatusProcessing,
		CreatedAt:      time.Now(),
	}
	for _, fn := range mutate {
		fn(task)
	}
	created, err := repo.Create(context.Background(), task)
	require.NoError(t, err)
	return created
}

func grokDoneBody() []byte {
	return []byte(`{"status":"done","video":{"url":"https://up.example/v.mp4"},"model":"grok-imagine-video","video_config":{"resolution":"720p","duration":8}}`)
}

func videoTaskPendingSnapshot() *GrokVideoPendingBilling {
	return &GrokVideoPendingBilling{
		Model:                "grok-imagine-video",
		BillingModel:         "grok-imagine-video",
		VideoResolution:      "720p",
		VideoDurationSeconds: 8,
		CreatedAt:            time.Now().Add(-time.Minute).Format(time.RFC3339Nano),
	}
}

// ── 1. 终态成功结算一次 ────────────────────────────────────

func TestVideoTaskWorker_SettlesOnceOnSuccess(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, 1, billing.recordCount(), "成功任务必须结算一次")
	require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
}

// ── 2. 幂等：重复推进不重复扣费 ─────────────────────────────

func TestVideoTaskWorker_RepeatedRunsDoNotDoubleCharge(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	for i := 0; i < 3; i++ {
		_, _, err := worker.RunDue(context.Background())
		require.NoError(t, err)
	}
	require.Equal(t, 1, billing.recordCount(), "重复推进只能结算一次")
	require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
}

// ── 3. 客户端与 worker 竞争：只结算一次 ─────────────────────

func TestVideoTaskWorker_ClientRaceSettlesOnce(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	// 模拟客户端轮询抢在 worker 之前拿到 SetNX 占位。
	claimed, err := billing.ClaimBilled(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.NoError(t, err)
	require.True(t, claimed)

	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled, "客户端已结算时 worker 仍应把任务行收尾为终态")
	require.Equal(t, 0, billing.recordCount(), "客户端已结算，worker 不得再扣一次")
	require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
}

func TestVideoTaskWorker_ConcurrentSettlementChargesOnce(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()

	var started sync.WaitGroup
	started.Add(2)
	release := make(chan struct{})
	// 让两路查询同时返回终态，最大化竞争窗口。
	querier := &fakeVideoStatusQuerier{
		state: videoTaskUpstreamSucceeded,
		body:  grokDoneBody(),
		onQuery: func() {
			started.Done()
			<-release
		},
	}
	seedVideoTask(t, repo)

	workerA := newTestVideoTaskWorker(t, repo, billing, querier)
	workerB := newTestVideoTaskWorker(t, repo, billing, querier)

	var settledA, settledB int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, settledA, _ = workerA.RunDue(context.Background())
	}()
	go func() {
		defer wg.Done()
		_, settledB, _ = workerB.RunDue(context.Background())
	}()
	started.Wait()
	close(release)
	wg.Wait()

	require.Equal(t, 1, billing.recordCount(),
		"两个并发推进者到达终态时只能结算一次（SetNX 幂等占位）")
	require.LessOrEqual(t, settledA+settledB, 2)
}

// ── 4. 上游状态不明不得当失败 ──────────────────────────────

func TestVideoTaskWorker_UnknownUpstreamStateKeepsProcessing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		state     videoTaskUpstreamState
		body      []byte
		err       error
		wantError string
	}{
		{
			name:      "transport error",
			state:     videoTaskUpstreamUnknown,
			err:       errors.New("upstream status transport error: dial tcp: i/o timeout"),
			wantError: "timeout",
		},
		{
			name:      "non-2xx status",
			state:     videoTaskUpstreamUnknown,
			body:      []byte(`{"error":"rate limited"}`),
			err:       errors.New("upstream status returned 429"),
			wantError: "429",
		},
		{
			name:  "unparseable body",
			state: videoTaskUpstreamUnknown,
			body:  []byte("<html>not json</html>"),
		},
		{
			name:  "pending",
			state: videoTaskUpstreamPending,
			body:  []byte(`{"status":"pending"}`),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := &fakeVideoTaskRepo{}
			billing := newFakeVideoBilling()
			billing.pending = videoTaskPendingSnapshot()
			querier := &fakeVideoStatusQuerier{state: tc.state, body: tc.body, err: tc.err}
			worker := newTestVideoTaskWorker(t, repo, billing, querier)
			seedVideoTask(t, repo)

			_, settled, err := worker.RunDue(context.Background())
			require.NoError(t, err)
			require.Equal(t, 0, settled, "状态不明不得推进终态")
			require.Equal(t, 0, billing.recordCount(), "状态不明绝不能扣费")
			require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey),
				"上游状态不明/超时必须保持 processing（PRD-06 §4）")
			require.Equal(t, 1, repo.checked, "必须记录一次尝试")
			if tc.wantError != "" {
				require.Contains(t, repo.tasks[0].LastError, tc.wantError)
			}
		})
	}
}

// ── 5. 明确失败才终态，且不扣费 ────────────────────────────

func TestVideoTaskWorker_ExplicitUpstreamFailureChargesNothing(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{
		state: videoTaskUpstreamFailed,
		body:  []byte(`{"status":"failed"}`),
	}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, 0, billing.recordCount(), "失败任务不扣费")
	require.Equal(t, VideoTaskStatusFailed, repo.statusOf(videoTaskTestTaskKey))
	require.Equal(t, 1, billing.releaseCalls, "失败必须释放占位")
}

// ── 6. 不重新调度选号：查询只用原承载账号 ───────────────────

func TestVideoTaskWorker_QueriesWithOriginalAccountOnly(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamPending, body: []byte(`{"status":"pending"}`)}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, querier.callCount())
	require.Equal(t, videoTaskTestAccountID, querier.accountID,
		"必须复用任务行固化的承载账号，不得重新调度选号（PRD-06 §2.3）")
}

func TestVideoTaskWorker_MissingAccountNeverReschedules(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo, func(task *VideoTask) { task.AccountID = nil })

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, settled)
	require.Equal(t, 0, querier.callCount(), "无承载账号时不得发起任何查询/选号")
	require.Equal(t, 0, billing.recordCount())
	require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey))
}

// ── 7. 结算失败释放占位、保持 processing 以便重试 ───────────

func TestVideoTaskWorker_RecordUsageFailureReleasesClaim(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	billing.recordErr = errors.New("billing repository unavailable")
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, settled)
	require.Equal(t, 1, billing.recordCount())
	require.Equal(t, 1, billing.releaseCalls, "结算失败必须释放占位以便重试")
	require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey),
		"结算未完成必须保持 processing，不得静默丢费")

	// 下一轮（上游仍成功、结算恢复）应能完成结算——占位已被释放。
	billing.mu.Lock()
	billing.recordErr = nil
	billing.mu.Unlock()
	_, settled, err = worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
}

// ── 8. 归属不一致不结算（防止错扣到别的分组/用户）────────────

func TestVideoTaskWorker_RefusesMismatchedOwnership(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)
	// Key 已迁移到别的分组。
	otherGroupID := int64(555)
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: &APIKey{
		ID:      videoTaskTestAPIKeyID,
		UserID:  videoTaskTestUserID,
		GroupID: &otherGroupID,
		Group:   &Group{ID: otherGroupID},
	}}

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, settled)
	require.Equal(t, 0, billing.recordCount(), "分组不一致时不得扣费")
	require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey))
}

// ── 9. 归属校验：他人任务不可读 ────────────────────────────

func TestVideoTaskService_GetForOwnerRejectsOthers(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	service := NewVideoTaskService(repo)
	_, err := service.RecordCreatedTask(context.Background(), &VideoTask{
		TaskNo:         videoTaskTestTaskKey,
		UserID:         videoTaskTestUserID,
		APIKeyID:       videoTaskTestAPIKeyID,
		GroupID:        videoTaskTestGroupID,
		UpstreamTaskID: videoTaskTestTaskKey,
		Status:         VideoTaskStatusProcessing,
	})
	require.NoError(t, err)

	own, err := service.GetForOwner(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.NoError(t, err)
	require.Equal(t, videoTaskTestTaskKey, own.TaskNo)

	_, err = service.GetForOwner(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID+1, videoTaskTestAPIKeyID)
	require.ErrorIs(t, err, ErrVideoTaskForbidden)

	_, err = service.GetForOwner(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID, videoTaskTestAPIKeyID+1)
	require.ErrorIs(t, err, ErrVideoTaskForbidden)

	_, err = service.GetForOwner(context.Background(), "no-such-task", videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.ErrorIs(t, err, ErrVideoTaskNotFound)
}

// ── 10. 创建登记幂等 ──────────────────────────────────────

func TestVideoTaskService_RecordCreatedTaskIsIdempotent(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	service := NewVideoTaskService(repo)
	params := func() *VideoTask {
		accountID := videoTaskTestAccountID
		return &VideoTask{
			TaskNo:         videoTaskTestTaskKey,
			UserID:         videoTaskTestUserID,
			APIKeyID:       videoTaskTestAPIKeyID,
			GroupID:        videoTaskTestGroupID,
			UpstreamTaskID: videoTaskTestTaskKey,
			AccountID:      &accountID,
			Status:         VideoTaskStatusProcessing,
		}
	}
	first, err := service.RecordCreatedTask(context.Background(), params())
	require.NoError(t, err)
	second, err := service.RecordCreatedTask(context.Background(), params())
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "重放创建不得产生第二行")
	require.Len(t, repo.tasks, 1)
}

// ── 11. 上游状态分类：unknown 是默认 ───────────────────────

func TestClassifyVideoTaskUpstreamState(t *testing.T) {
	t.Parallel()
	require.Equal(t, videoTaskUpstreamUnknown, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, nil))
	require.Equal(t, videoTaskUpstreamUnknown, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, []byte("<html>")))
	require.Equal(t, videoTaskUpstreamUnknown, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, []byte(`{"status":"weird"}`)))
	require.Equal(t, videoTaskUpstreamSucceeded, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, grokDoneBody()))
	// done 但没有 video.url：不可结算，保持 pending。
	require.Equal(t, videoTaskUpstreamPending, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, []byte(`{"status":"done"}`)))
	require.Equal(t, videoTaskUpstreamFailed, classifyVideoTaskUpstreamState(VideoTaskPlatformGrok, []byte(`{"status":"failed"}`)))

	require.Equal(t, videoTaskUpstreamSucceeded, classifyVideoTaskUpstreamState(VideoTaskPlatformSeedance, []byte(`{"status":"succeeded"}`)))
	require.Equal(t, videoTaskUpstreamFailed, classifyVideoTaskUpstreamState(VideoTaskPlatformSeedance, []byte(`{"status":"expired"}`)))
	require.Equal(t, videoTaskUpstreamPending, classifyVideoTaskUpstreamState(VideoTaskPlatformSeedance, []byte(`{"status":"running"}`)))
	require.Equal(t, videoTaskUpstreamUnknown, classifyVideoTaskUpstreamState(VideoTaskPlatformSeedance, []byte(`{}`)))
}

// ── 12. 开关与周期 ────────────────────────────────────────

type fakeVideoSettingsRepo struct {
	value string
	err   error
}

func (f *fakeVideoSettingsRepo) Get(_ context.Context, _ string) (*Setting, error) { return nil, nil }
func (f *fakeVideoSettingsRepo) GetValue(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.value, nil
}
func (f *fakeVideoSettingsRepo) Set(_ context.Context, _, _ string) error { return nil }
func (f *fakeVideoSettingsRepo) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeVideoSettingsRepo) SetMultiple(_ context.Context, _ map[string]string) error { return nil }
func (f *fakeVideoSettingsRepo) GetAll(_ context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeVideoSettingsRepo) Delete(_ context.Context, _ string) error { return nil }

func TestVideoTaskWorker_DisabledBySettings(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)

	disabled, err := json.Marshal(VideoTaskWorkerSettings{Enabled: false})
	require.NoError(t, err)
	worker.settingsRepo = &fakeVideoSettingsRepo{value: string(disabled)}

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, settled)
	require.Equal(t, 0, querier.callCount())
	require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey))
}

func TestVideoTaskWorker_SettingsClampInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, videoTaskWorkerDefaultInterval, func() time.Duration {
		settings := &VideoTaskWorkerSettings{Enabled: true}
		normalizeVideoTaskWorkerSettings(settings)
		return time.Duration(settings.IntervalSeconds) * time.Second
	}())
	settings := &VideoTaskWorkerSettings{Enabled: true, IntervalSeconds: 1}
	normalizeVideoTaskWorkerSettings(settings)
	require.Equal(t, int(videoTaskWorkerMinInterval.Seconds()), settings.IntervalSeconds)
	settings = &VideoTaskWorkerSettings{Enabled: true, IntervalSeconds: 1 << 30}
	normalizeVideoTaskWorkerSettings(settings)
	require.Equal(t, int(videoTaskWorkerMaxInterval.Seconds()), settings.IntervalSeconds)
}

// ── 13. 超窗口收尾不扣费 ──────────────────────────────────

func TestVideoTaskWorker_SettleWindowExpiryChargesNothing(t *testing.T) {
	t.Parallel()
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamPending, body: []byte(`{"status":"pending"}`)}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.CreatedAt = time.Now().Add(-(videoTaskWorkerSettleWindow + time.Hour))
	})

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, 0, billing.recordCount(), "超窗口收尾不得扣费")
	require.Equal(t, 0, querier.callCount(), "超窗口不再查询上游")
	require.Equal(t, VideoTaskStatusFailed, repo.statusOf(videoTaskTestTaskKey))
}

// ── 14. 退避有界 ─────────────────────────────────────────

func TestVideoTaskBackoffIsBounded(t *testing.T) {
	t.Parallel()
	require.Equal(t, videoTaskWorkerBaseBackoff, videoTaskBackoff(0))
	require.Equal(t, videoTaskWorkerBaseBackoff*2, videoTaskBackoff(1))
	var seen atomic.Int64
	for i := 0; i < 20; i++ {
		seen.Store(int64(videoTaskBackoff(i)))
	}
	require.LessOrEqual(t, seen.Load(), int64(videoTaskWorkerMaxBackoff))
}

// ── 15. 回归：超长 task key 的结算占位必须与客户端同一把锁 ──────
//
// 背景（Lead 审查发现的真实缺陷）：客户端用**完整** taskRequestID 占位，
// 而 worker 原先用被截断到 64 字符的 TaskNo。上游 ResponseID 超过 64 字符时
// 两者不是同一把锁 → 双方都抢到占位 → 同一任务扣两次。
//
// 这两条测试用 >64 字符的 key 锁定修复：无论谁先占位，都只能扣一次。

// longVideoTaskKey 构造一个长度明确超过 64 的客户端任务 key。
func longVideoTaskKey() string {
	return "vid-" + strings.Repeat("abcdefghij", 12) + "-tail"
}

func TestVideoTaskWorker_LongKeyClientClaimBlocksWorker(t *testing.T) {
	t.Parallel()
	key := longVideoTaskKey()
	require.Greater(t, len(key), 64, "该回归用例必须使用超长 key")

	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.TaskNo = videoTaskNoFromKey(key)
		task.UpstreamTaskID = key // 完整、未截断
	})

	// 客户端先占位：用完整 key（与 handler 的 ClaimGrokVideoBilling 一致）。
	claimed, err := billing.ClaimBilled(context.Background(), key, videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.NoError(t, err)
	require.True(t, claimed)

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, 0, billing.recordCount(),
		"客户端已用完整 key 占位，worker 必须复用同一把锁而不再扣费")
}

func TestVideoTaskWorker_LongKeyWorkerClaimBlocksClient(t *testing.T) {
	t.Parallel()
	key := longVideoTaskKey()
	require.Greater(t, len(key), 64, "该回归用例必须使用超长 key")

	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.TaskNo = videoTaskNoFromKey(key)
		task.UpstreamTaskID = key
	})

	_, settled, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, 1, billing.recordCount(), "worker 先到，结算一次")

	// 客户端随后用完整 key 占位：必须拿不到锁（已被 worker 占用）。
	claimed, err := billing.ClaimBilled(context.Background(), key, videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.NoError(t, err)
	require.False(t, claimed,
		"worker 已用完整 key 占位，客户端不得再抢到锁（否则重复扣费）")
	require.Equal(t, 1, billing.recordCount())
}

// BillingKey 必须返回完整未截断的标识，且与客户端使用的 key 一致。
func TestVideoTaskBillingKeyUsesUntruncatedUpstreamID(t *testing.T) {
	t.Parallel()
	key := longVideoTaskKey()
	task := &VideoTask{
		TaskNo:         videoTaskNoFromKey(key),
		UpstreamTaskID: key,
	}
	require.NotEqual(t, key, task.TaskNo, "task_no 受列宽约束会截断")
	require.Equal(t, key, task.BillingKey(),
		"结算占位必须用完整 UpstreamTaskID，与客户端逐字一致")

	// 回落：无 UpstreamTaskID 时才用 TaskNo（历史行）。
	require.Equal(t, "short-key", (&VideoTask{TaskNo: "short-key"}).BillingKey())
	require.Equal(t, "", (*VideoTask)(nil).BillingKey())
}

// 超长 key 的 task_no 派生必须无碰撞（否则第二个任务登记不进来）。
func TestVideoTaskNoFromKeyIsCollisionSafe(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("x", 80)
	first := videoTaskNoFromKey(base + "AAAA")
	second := videoTaskNoFromKey(base + "BBBB")
	require.NotEqual(t, first, second, "仅尾部不同的超长 key 不得派生出相同 task_no")
	require.LessOrEqual(t, len(first), 64)
	require.LessOrEqual(t, len(second), 64)
	// 短 key 保持原样（可读性 + 与既有行为一致）。
	require.Equal(t, "short-key", videoTaskNoFromKey("short-key"))
	require.Equal(t, "", videoTaskNoFromKey("   "))
}

