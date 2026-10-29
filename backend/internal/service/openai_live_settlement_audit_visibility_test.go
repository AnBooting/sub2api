//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// liveAuditAdminRepo 把审计写入的行按生产 SQL 的存储映射还原成
// OpsErrorLog / OpsErrorLogDetail，供管理端列表与管理端详情两条真实读路径使用：
//   - error_message          -> OpsErrorLog.Message
//   - upstream_error_message -> OpsErrorLogDetail.UpstreamErrorMessage
//   - upstream_error_detail  -> OpsErrorLogDetail.UpstreamErrorDetail
//
// 用户投影与管理端投影都由真实的 OpsService / ToUserErrorRequest* 执行，
// 本文件不复制任何投影逻辑。
type liveAuditAdminRepo struct {
	liveRecordingOpsRepo
}

func (r *liveAuditAdminRepo) ListErrorLogs(_ context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	rows := []*OpsErrorLog{}
	for _, entry := range r.snapshot() {
		if filter.UserID == nil || entry.UserID == nil || *filter.UserID != *entry.UserID {
			continue
		}
		rows = append(rows, &OpsErrorLog{
			ID: int64(len(rows) + 1), CreatedAt: entry.CreatedAt, UserID: entry.UserID,
			APIKeyID: entry.APIKeyID, AccountID: entry.AccountID, Model: entry.Model,
			Phase: entry.ErrorPhase, Type: entry.ErrorType, StatusCode: entry.StatusCode,
			Message: entry.ErrorMessage, Platform: entry.Platform,
		})
	}
	return &OpsErrorLogList{Errors: rows, Total: len(rows), Page: 1, PageSize: 20}, nil
}

func (r *liveAuditAdminRepo) GetErrorLogByID(_ context.Context, id int64) (*OpsErrorLogDetail, error) {
	entries := r.snapshot()
	if id <= 0 || int(id) > len(entries) {
		return nil, errors.New("ops error log not found")
	}
	entry := entries[id-1]
	detail := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID: id, CreatedAt: entry.CreatedAt, UserID: entry.UserID,
			APIKeyID: entry.APIKeyID, AccountID: entry.AccountID, Model: entry.Model,
			Phase: entry.ErrorPhase, Type: entry.ErrorType, StatusCode: entry.StatusCode,
			Message: entry.ErrorMessage, Platform: entry.Platform,
		},
		UpstreamStatusCode: entry.UpstreamStatusCode,
	}
	if entry.UpstreamErrorMessage != nil {
		detail.UpstreamErrorMessage = *entry.UpstreamErrorMessage
	}
	if entry.UpstreamErrorDetail != nil {
		detail.UpstreamErrorDetail = *entry.UpstreamErrorDetail
	}
	return detail, nil
}

// TestLiveSettlementAuditKeepsFullDiagnosticsForAdmin 是隐私修复的正向面：
// 用户侧只拿固定公开说明的同时，管理员侧必须仍能拿到完整诊断（尝试次数 + 账本
// 错误原文）。这条用例走的是管理端读路径同源的 OpsService.GetErrorLogByID ——
// 管理端错误详情用的就是它，且它不做任何脱敏。
func TestLiveSettlementAuditKeepsFullDiagnosticsForAdmin(t *testing.T) {
	const secret = "ledger at 10.23.45.67:5432: relation \"usage_billing_dedup\" is unavailable"
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	audit := &liveAuditAdminRepo{}
	svc.SetOpsRepository(audit)
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New(secret)
	svc.finalizeLiveCall(record)

	entries := audit.snapshot()
	require.Len(t, entries, 1, "结算失败必须留下一条审计事件")
	written := entries[0]

	// 写入侧的分层：公开说明里没有原文，原文在管理员字段里。
	require.Equal(t, liveSettlementPendingPublicMessage, written.ErrorMessage,
		"error_message 会经用户投影原样下发，只能是固定公开说明")
	require.NotContains(t, written.ErrorMessage, "10.23.45.67")
	require.NotContains(t, written.ErrorMessage, "usage_billing_dedup")
	require.NotNil(t, written.UpstreamErrorDetail, "完整诊断必须留在管理员字段")
	require.Contains(t, *written.UpstreamErrorDetail, secret, "管理员字段必须保留账本错误原文")
	require.NotNil(t, written.UpstreamErrorMessage)
	require.Contains(t, *written.UpstreamErrorMessage, "1 attempt", "管理员侧摘要必须带尝试次数")

	// 管理员读路径（管理端错误详情用的同一个服务方法）拿到完整诊断。
	ops := &OpsService{opsRepo: audit}
	adminDetail, err := ops.GetErrorLogByID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, adminDetail)
	require.Contains(t, adminDetail.UpstreamErrorDetail, secret, "管理员详情必须能看到账本错误原文")
	require.Contains(t, adminDetail.UpstreamErrorMessage, "1 attempt")
	require.Equal(t, liveSettlementPendingPublicMessage, adminDetail.Message,
		"管理端读同一列：公开说明在管理端保持稳定，排障靠 upstream_* 字段")
	adminJSON, err := json.Marshal(adminDetail)
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), "10.23.45.67", "管理员 JSON 必须仍带完整诊断")

	// 同一行经用户投影后只剩稳定错误码 + 固定公开说明。
	userOps := &OpsService{opsRepo: audit}
	userList, err := userOps.ListUserErrorRequests(context.Background(), record.UserID, nil)
	require.NoError(t, err)
	require.Len(t, userList.Items, 1)
	item := userList.Items[0]
	require.Equal(t, "internal_error", item.Code, "用户侧只认稳定错误码")
	require.Equal(t, liveSettlementPendingPublicMessage, item.Message)
	require.Equal(t, "internal", item.Category)
	userJSON, err := json.Marshal(userList)
	require.NoError(t, err)
	for _, forbidden := range []string{"10.23.45.67", "usage_billing_dedup", "ledger at"} {
		require.False(t, strings.Contains(string(userJSON), forbidden),
			"用户响应不得包含内部诊断 %q: %s", forbidden, string(userJSON))
	}

	// 用户侧还有第二条读路径：详情接口（GetUserErrorRequestDetail → ToUserErrorRequestDetail，
	// 会额外带 error_body）。它同样不能成为内部诊断的出口。
	userDetail, err := userOps.GetUserErrorRequestDetail(context.Background(), record.UserID, 1)
	require.NoError(t, err)
	require.NotNil(t, userDetail)
	require.Equal(t, "internal_error", userDetail.Code)
	require.Equal(t, liveSettlementPendingPublicMessage, userDetail.Message)
	userDetailJSON, err := json.Marshal(userDetail)
	require.NoError(t, err)
	for _, forbidden := range []string{"10.23.45.67", "usage_billing_dedup", "ledger at"} {
		require.False(t, strings.Contains(string(userDetailJSON), forbidden),
			"用户详情不得包含内部诊断 %q: %s", forbidden, string(userDetailJSON))
	}
}

// TestLiveSettlementAuditPublicMessageIsStable 固定公开说明不能随 cause 变化，
// 否则用户侧文案就变成内部错误的回显通道。
func TestLiveSettlementAuditPublicMessageIsStable(t *testing.T) {
	causes := []string{
		"ledger at 10.23.45.67:5432: relation \"usage_billing_dedup\" is unavailable",
		"connection refused",
		"temporary ledger outage",
	}
	messages := map[string]bool{}
	for _, cause := range causes {
		price := 0.1
		svc, store, _, billing := newLiveSettlementTestService()
		audit := &liveAuditAdminRepo{}
		svc.SetOpsRepository(audit)
		record := newLiveSettlementRecord(t, store, 0, &price)
		billing.err = errors.New(cause)
		svc.finalizeLiveCall(record)
		entries := audit.snapshot()
		require.Len(t, entries, 1)
		messages[entries[0].ErrorMessage] = true
	}
	require.Len(t, messages, 1, "不同 cause 必须产生同一条公开说明: %v", messages)
	require.True(t, messages[liveSettlementPendingPublicMessage])
}

// TestLiveSettlementAuditWritesOneRowPerSession 锁定「一条卡住的会话只留一条审计」。
//
// ops_error_logs 没有 (request_id, error_type) 唯一键，重试每失败一次就写一行；
// 而用户错误列表按 user_id 查这张表。若逐次留痕，一条卡住的会话会在用户自己的
// 列表里出现 N 条一模一样的「请求失败」，用户会以为请求失败了 N 次。
func TestLiveSettlementAuditWritesOneRowPerSession(t *testing.T) {
	// 把退避压到 1ms：这条用例要跑满整轮重试，真实退避合计约 7.5 分钟。
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	audit := &liveAuditAdminRepo{}
	svc.SetOpsRepository(audit)
	record := newLiveSettlementRecord(t, store, 0, &price)

	// 账本持续故障：finalize 一次 + 重试整轮，attempts 会累加到多次。
	billing.err = errors.New("temporary ledger outage")
	svc.finalizeLiveCall(record)
	svc.retryPendingLiveSettlement(record)

	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, current.Settlement)
	require.Greater(t, current.Settlement.Attempts, 1, "这条用例要求重试确实跑了多轮")

	require.Len(t, audit.snapshot(), 1,
		"同一条会话无论重试多少次，审计只能留一条（否则用户列表会出现重复行）")
}

// TestLiveSettlementAuditIsNotCountedAsSLAError 锁定审计行不污染运营看板的 SLA。
//
// 看板判据是 status>=400 AND NOT is_business_limited；审计行 status=500，
// 若不置 is_business_limited，一次内部账本故障会按重试次数表现为多次服务端错误。
func TestLiveSettlementAuditIsNotCountedAsSLAError(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	audit := &liveAuditAdminRepo{}
	svc.SetOpsRepository(audit)
	record := newLiveSettlementRecord(t, store, 0, &price)

	billing.err = errors.New("temporary ledger outage")
	svc.finalizeLiveCall(record)

	entries := audit.snapshot()
	require.Len(t, entries, 1)
	require.True(t, entries[0].IsBusinessLimited,
		"内部账本故障属业务侧暂时不可用，必须置 is_business_limited 以免拉低 SLA")
}

// TestLiveSettlementPublicMessageDoesNotPromiseRetry 锁定公开文案不承诺自动重试。
//
// 重试只在 observer 存活且预算未耗尽时进行（约 7.5 分钟），预算耗尽后没有后台
// sweeper 接手。承诺「会自动重试」会把泄露问题换成误导问题。
func TestLiveSettlementPublicMessageDoesNotPromiseRetry(t *testing.T) {
	lower := strings.ToLower(liveSettlementPendingPublicMessage)
	for _, promise := range []string{"automatically", "will be retried", "retried automatically"} {
		require.NotContains(t, lower, promise,
			"公开文案不得承诺自动重试：预算耗尽后没有后台 sweeper 接手")
	}
	require.NotEmpty(t, liveSettlementPendingPublicMessage)
}
