//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// TS-02 S5（C-5）故障分类器测试
//
// 覆盖：
//  1. 五类映射（table-driven，理由优先于状态码）；
//  2. nil 安全；
//  3. 硬不变量：Classify().IsRetryable() == ShouldRetryNextAccount()；
//  4. fail_* 不产生第二次发送（同号重试标志必须关闭）；
//  5. 主要出口（传输 / 预算 / Grok 凭据）真的写出了分类与结构化日志字段；
//  6. 既有行为不回退（401/403/429/5xx/零值 NextAccountLegacyRetry 仍可换号）。
// ---------------------------------------------------------------------------

// TestFailureClassContractValues 钉死五个契约字符串：它们是对外契约（C-5 表），
// 拼写错误必须被测试拦住。
func TestFailureClassContractValues(t *testing.T) {
	require.Equal(t, FailureClass("retry_account"), FailureClassRetryAccount)
	require.Equal(t, FailureClass("retry_transient"), FailureClassRetryTransient)
	require.Equal(t, FailureClass("fail_client"), FailureClassFailClient)
	require.Equal(t, FailureClass("fail_policy"), FailureClassFailPolicy)
	require.Equal(t, FailureClass("fail_uncertain"), FailureClassFailUncertain)

	for _, class := range []FailureClass{
		FailureClassRetryAccount, FailureClassRetryTransient,
		FailureClassFailClient, FailureClassFailPolicy, FailureClassFailUncertain,
	} {
		require.Truef(t, class.IsValid(), "%q 必须是合法分类", class)
	}
	require.False(t, FailureClass("").IsValid())
	require.False(t, FailureClass("retry_account ").IsValid())
	require.False(t, FailureClass("fail_unknown").IsValid())

	require.True(t, FailureClassRetryAccount.IsRetryable())
	require.True(t, FailureClassRetryTransient.IsRetryable())
	require.False(t, FailureClassFailClient.IsRetryable())
	require.False(t, FailureClassFailPolicy.IsRetryable())
	require.False(t, FailureClassFailUncertain.IsRetryable())
	require.False(t, FailureClass("").IsRetryable())
}

// TestClassifyMapsFiveClasses 是 C-5 的 table-driven 五类映射。每行的理由写在
// name 里，断言分类与「是否允许换号」两个维度。
func TestClassifyMapsFiveClasses(t *testing.T) {
	tests := []struct {
		name string
		err  *UpstreamFailoverError
		want FailureClass
	}{
		// ---- retry_account：账号鉴权 / 账号限流 / 满载 / 账号余额 ----
		{
			name: "401 账号鉴权失败",
			err:  &UpstreamFailoverError{StatusCode: http.StatusUnauthorized},
			want: FailureClassRetryAccount,
		},
		{
			name: "403 账号被拒",
			err:  &UpstreamFailoverError{StatusCode: http.StatusForbidden},
			want: FailureClassRetryAccount,
		},
		{
			name: "429 账号限流",
			err:  &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests},
			want: FailureClassRetryAccount,
		},
		{
			name: "529 上游满载",
			err:  &UpstreamFailoverError{StatusCode: 529},
			want: FailureClassRetryAccount,
		},
		{
			name: "凭据阶段失败（Stage=account_auth，状态码 5xx）",
			err: &UpstreamFailoverError{
				StatusCode: http.StatusBadGateway,
				Stage:      GatewayFailureStageAccountAuth,
				Scope:      GatewayFailureScopeAccount,
				Reason:     TypeSafeCredentialRejectedReason,
			},
			want: FailureClassRetryAccount,
		},
		{
			name: "Grok 账号凭据需要处理（账号级，可换号）",
			err: &UpstreamFailoverError{
				Stage:             GatewayFailureStageAccountAuth,
				Scope:             GatewayFailureScopeAccount,
				Reason:            GrokCredentialReasonRevoked,
				NextAccountAction: NextAccountRetry,
			},
			want: FailureClassRetryAccount,
		},
		{
			name: "402 上游账号余额不足但允许换号",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusPaymentRequired,
				Reason:            OpenAIImagesInsufficientBalanceReason,
				NextAccountAction: NextAccountRetry,
			},
			want: FailureClassRetryAccount,
		},

		// ---- retry_transient：连接失败 / 上游临时 5xx ----
		{
			name: "零值（legacy 重试语义）",
			err:  &UpstreamFailoverError{},
			want: FailureClassRetryTransient,
		},
		{
			name: "500 上游内部错误",
			err:  &UpstreamFailoverError{StatusCode: http.StatusInternalServerError},
			want: FailureClassRetryTransient,
		},
		{
			name: "502 传输层失败（上游无状态码）",
			err:  &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: gatewayTransportFailoverBody},
			want: FailureClassRetryTransient,
		},
		{
			name: "503 上游不可用",
			err:  &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
			want: FailureClassRetryTransient,
		},
		{
			name: "529 请求级瞬时降载仍属账号级状态",
			err: &UpstreamFailoverError{
				StatusCode:             529,
				RequestScopedTransient: true,
				RetryableOnSameAccount: true,
			},
			want: FailureClassRetryAccount,
		},
		{
			name: "504 未声明终止时按可重试处理",
			err:  &UpstreamFailoverError{StatusCode: http.StatusGatewayTimeout},
			want: FailureClassRetryTransient,
		},

		// ---- fail_client：本站侧客户端错误（仅在已声明不换号时成立）----
		{
			name: "400 参数错误（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusBadRequest,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "404 模型/路径无效（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusNotFound,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "413 请求体过大（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusRequestEntityTooLarge,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "422 参数不可处理（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusUnprocessableEntity,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "402 本站余额不足（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusPaymentRequired,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "400 模型名无效（账号/提供方能力问题，可换号）",
			err: &UpstreamFailoverError{
				StatusCode:   http.StatusBadRequest,
				ResponseBody: []byte(`{"error":{"code":"model_not_found","message":"The model does not exist"}}`),
			},
			want: FailureClassRetryAccount,
		},

		// ---- fail_policy：内容审核 / 明确业务拒绝 / 本站策略终止 ----
		{
			name: "预算耗尽（RequestAttemptBudgetReason）",
			err:  NewRequestAttemptBudgetFailoverError(),
			want: FailureClassFailPolicy,
		},
		{
			name: "内容审核 cyber_policy（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusForbidden,
				Reason:            GatewayFailureReason("cyber_policy"),
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "451 合规/审核拒绝（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusUnavailableForLegalReasons,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "Grok provider 配置不可用（终止，非账号故障）",
			err: &UpstreamFailoverError{
				Stage:             GatewayFailureStageAccountAuth,
				Scope:             GatewayFailureScopeProvider,
				Reason:            GrokCredentialReasonProviderConfig,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "Grok 凭据 failover 预算耗尽（终止）",
			err: &UpstreamFailoverError{
				Stage:             GatewayFailureStageAccountAuth,
				Scope:             GatewayFailureScopeRequest,
				Reason:            GrokCredentialReasonFailoverTimeout,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "显式终止但无具体原因（兜底 fail_policy）",
			err: &UpstreamFailoverError{
				StatusCode:        418,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},

		// ---- fail_uncertain：超时且无法确认上游是否执行 / 流已向客户端输出 ----
		{
			name: "nil 错误",
			err:  nil,
			want: FailureClassFailUncertain,
		},
		{
			name: "504 上游超时（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusGatewayTimeout,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
		{
			name: "408 请求超时（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusRequestTimeout,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
		{
			name: "reason 表示 timeout（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusServiceUnavailable,
				Reason:            GatewayFailureReason("upstream_timeout"),
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
		{
			name: "reason 表示 deadline（终止）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusBadGateway,
				Reason:            GatewayFailureReason("first_output_deadline_exceeded"),
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
		{
			name: "无超时信号的终止型错误 → 兜底 fail_policy",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusBadGateway,
				Reason:            GatewayFailureReason("stream_already_committed"),
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "超时且已向客户端输出（不确定优先）",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusGatewayTimeout,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Classify()
			require.Equalf(t, tt.want, got, "Classify() = %q, want %q", got, tt.want)
			require.Truef(t, got.IsValid(), "分类必须是五个契约取值之一")

			// 硬不变量：分类的可重试性与 ShouldRetryNextAccount() 恒等。
			retryable := tt.err != nil && tt.err.ShouldRetryNextAccount()
			require.Equalf(t, retryable, got.IsRetryable(),
				"Classify().IsRetryable() 必须与 ShouldRetryNextAccount() 一致")
			if tt.err != nil {
				require.Equal(t, retryable, tt.err.IsRetryableClass())
			}
		})
	}
}

// TestClassifyNilIsSafeAndConservative 钉死 nil 语义：nil 归入 fail_uncertain
// （没有证据说明换号有帮助），并且所有 nil 访问都不 panic、一律不可重试。
func TestClassifyNilIsSafeAndConservative(t *testing.T) {
	var nilErr *UpstreamFailoverError

	require.Equal(t, FailureClassFailUncertain, nilErr.Classify())
	require.False(t, nilErr.Classify().IsRetryable())
	require.False(t, nilErr.IsRetryableClass())
	require.False(t, nilErr.ShouldRetryNextAccount())
	require.Nil(t, gatewayFailureClassLogFields(nilErr, "test.event"))

	// 出口日志 helper 必须容忍 nil（否则出口在无错误上下文时会 panic）。
	require.NotPanics(t, func() { logGatewayFailureClass(nil, "test.event") })
}

// TestClassifyHonoursStoredSnapshotButKeepsInvariant 钉死「出口快照优先 + 不变量兜底」：
// 合法且与决策一致的快照原样采用；与决策冲突（陈旧/写错）时回退动态推导，
// 保证 ShouldRetryNextAccount()==false 的错误永远不会被判成 retry_*。
func TestClassifyHonoursStoredSnapshotButKeepsInvariant(t *testing.T) {
	// 快照与决策一致：采用快照（即便状态码本身会推出别的类）。
	consistent := &UpstreamFailoverError{
		StatusCode:   http.StatusUnauthorized, // 动态推导是 retry_account
		FailureClass: FailureClassRetryTransient,
	}
	require.True(t, consistent.ShouldRetryNextAccount())
	require.Equal(t, FailureClassRetryTransient, consistent.Classify())

	// 快照与决策冲突：终止型错误被写成 retry_transient，必须回退推导。
	conflicting := &UpstreamFailoverError{
		StatusCode:        http.StatusBadRequest,
		NextAccountAction: NextAccountStop,
		FailureClass:      FailureClassRetryTransient,
	}
	require.False(t, conflicting.ShouldRetryNextAccount())
	require.Equal(t, FailureClassFailClient, conflicting.Classify(),
		"冲突快照必须回退动态推导，硬不变量不可被字段破坏")

	// 非法快照同样回退。
	invalid := &UpstreamFailoverError{StatusCode: http.StatusInternalServerError, FailureClass: FailureClass("bogus")}
	require.Equal(t, FailureClassRetryTransient, invalid.Classify())
}

// TestClassifyRetryabilityMatchesShouldRetryNextAccountAcrossCorpus 是硬不变量的
// 属性测试：遍历状态码 × 动作 × 阶段 × reason 的组合，分类的可重试性必须与
// ShouldRetryNextAccount() 完全一致，且 fail_* 与 retry_* 不得交叉。
func TestClassifyRetryabilityMatchesShouldRetryNextAccountAcrossCorpus(t *testing.T) {
	statuses := []int{0, 400, 401, 402, 403, 404, 408, 413, 422, 429, 451, 500, 502, 503, 504, 529}
	actions := []NextAccountAction{NextAccountLegacyRetry, NextAccountRetry, NextAccountStop}
	stages := []GatewayFailureStage{"", GatewayFailureStageInference, GatewayFailureStageAccountAuth}
	reasons := []GatewayFailureReason{
		"", "cyber_policy", "content_policy_violation", "upstream_timeout",
		RequestAttemptBudgetReason, GrokCredentialReasonProviderDown, openAIRequestBodyTooLargeReason,
	}

	checked := 0
	for _, status := range statuses {
		for _, action := range actions {
			for _, stage := range stages {
				for _, reason := range reasons {
					err := &UpstreamFailoverError{
						StatusCode:        status,
						NextAccountAction: action,
						Stage:             stage,
						Reason:            reason,
					}
					class := err.Classify()
					retryable := err.ShouldRetryNextAccount()

					require.Truef(t, class.IsValid(), "status=%d action=%d stage=%q reason=%q → 非法分类 %q",
						status, action, stage, reason, class)
					require.Equalf(t, retryable, class.IsRetryable(),
						"status=%d action=%d stage=%q reason=%q：Classify=%q 与 ShouldRetryNextAccount=%v 不一致",
						status, action, stage, reason, class, retryable)
					if !retryable {
						require.Containsf(t,
							[]FailureClass{FailureClassFailClient, FailureClassFailPolicy, FailureClassFailUncertain},
							class,
							"status=%d action=%d stage=%q reason=%q：终止型错误不得判成 retry_*",
							status, action, stage, reason)
					} else {
						require.Containsf(t,
							[]FailureClass{FailureClassRetryAccount, FailureClassRetryTransient},
							class,
							"status=%d action=%d stage=%q reason=%q：可换号错误不得判成 fail_*",
							status, action, stage, reason)
					}
					checked++
				}
			}
		}
	}
	require.Equal(t, len(statuses)*len(actions)*len(stages)*len(reasons), checked)
}

// TestFailureClassNoSecondSend 钉死「fail_client / fail_policy / fail_uncertain
// 不产生第二次发送」：这三类必须同时满足
//   - ShouldRetryNextAccount() == false（handler 的 HandleFailoverError 立即
//     返回 FailoverExhausted，既不换号也不重试；见 handler/failover_loop.go:236）；
//   - RetryableOnSameAccount == false（双保险：不得再携带同号重试标志）。
func TestFailureClassNoSecondSend(t *testing.T) {
	tests := []struct {
		name string
		err  *UpstreamFailoverError
		want FailureClass
	}{
		{
			name: "预算耗尽不换号",
			err:  NewRequestAttemptBudgetFailoverError(),
			want: FailureClassFailPolicy,
		},
		{
			name: "本站 400 参数错误不换号",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusBadRequest,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailClient,
		},
		{
			name: "内容审核不换渠道",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusForbidden,
				Reason:            GatewayFailureReason("cyber_policy"),
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailPolicy,
		},
		{
			name: "超时不确定不重放",
			err: &UpstreamFailoverError{
				StatusCode:        http.StatusGatewayTimeout,
				NextAccountAction: NextAccountStop,
			},
			want: FailureClassFailUncertain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.err.Classify())
			require.False(t, tt.err.IsRetryableClass(), "fail_* 分类不得允许换号/换组")
			require.False(t, tt.err.ShouldRetryNextAccount(), "fail_* 分类必须终止重试")
			require.False(t, tt.err.RetryableOnSameAccount, "fail_* 分类不得携带同号重试标志")
			require.False(t, tt.err.Classify().IsRetryable())
		})
	}
}

// TestFailureClassLogFields 钉死结构化日志载荷：分类、可重试性、决策与分类依据
// 必须全部可见，否则排障时无法复盘「为什么这一类」。
func TestFailureClassLogFields(t *testing.T) {
	err := &UpstreamFailoverError{
		StatusCode:        http.StatusGatewayTimeout,
		NextAccountAction: NextAccountStop,
		Reason:            GatewayFailureReason("upstream_timeout"),
		Scope:             GatewayFailureScopeRequest,
	}
	fields := gatewayFailureClassLogFields(err, "gateway.unit_test")
	require.NotEmpty(t, fields)

	// zap.Field 的取值字段未导出，直接用等值比较钉死每个字段（顺序无关）。
	require.Contains(t, fields, zap.String("event", "gateway.unit_test"))
	require.Contains(t, fields, zap.String("failure_class", string(FailureClassFailUncertain)))
	require.Contains(t, fields, zap.Bool("failure_class_retryable", false))
	require.Contains(t, fields, zap.Bool("retry_next_account", false))
	require.Contains(t, fields, zap.Int("upstream_status", http.StatusGatewayTimeout))
	require.Contains(t, fields, zap.String("failure_scope", string(GatewayFailureScopeRequest)))
	require.Contains(t, fields, zap.String("failure_reason", "upstream_timeout"))
	require.Contains(t, fields, zap.String("failure_stage", ""))
	require.Contains(t, fields, zap.Bool("safe_to_failover_after_write", false))
	require.Contains(t, fields, zap.Bool("request_scoped_transient", false))
}

// ---------------------------------------------------------------------------
// 出口级验证：主要传输 / 预算 / 凭据出口必须写出可测试的分类
// ---------------------------------------------------------------------------

// TestUpstreamTransportExitClassifiesAsRetryTransient 钉死 Anthropic/Bedrock
// 传输出口：连接类失败仍可换号（retry_transient），并且出口真的写入了字段。
func TestClassifyUpstreamTransportExitRetryTransient(t *testing.T) {
	for _, rawErr := range []error{
		errors.New("Post \"http://upstream/v1/messages?beta=true\": EOF"),
		errors.New("Post \"https://upstream/v1/messages\": socks connect tcp: username/password authentication failed"),
	} {
		s := &GatewayService{accountRepo: &transportTempUnschedRepoStub{}}
		c := newTransportErrorTestGin(t)
		account := &Account{ID: 149, Name: "acc", Platform: PlatformAnthropic}

		err := s.handleUpstreamTransportError(context.Background(), c, account, rawErr, OpsUpstreamErrorEvent{})
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)

		require.Equal(t, FailureClassRetryTransient, failoverErr.Classify())
		require.Equal(t, FailureClassRetryTransient, failoverErr.FailureClass,
			"出口必须写出分类字段，而不是只靠动态推导")
		require.True(t, failoverErr.ShouldRetryNextAccount(), "传输失败必须仍可换号（既有行为不变）")
		require.True(t, failoverErr.IsRetryableClass())
	}
}

// TestOpenAIUpstreamTransportExitClassifiesAsRetryTransient 钉死 OpenAI 传输出口。
func TestClassifyOpenAIUpstreamTransportExitRetryTransient(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: &openaiTransportAccountRepoStub{}}
	c, _ := newOpenAITransportErrTestContext()
	account := &Account{ID: 4627, Name: "acc", Platform: PlatformOpenAI}

	err := svc.handleOpenAIUpstreamTransportError(context.Background(), c, account,
		errors.New("read tcp: connection reset by peer"), false)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, FailureClassRetryTransient, failoverErr.Classify())
	require.Equal(t, FailureClassRetryTransient, failoverErr.FailureClass)
	require.True(t, failoverErr.ShouldRetryNextAccount())
}

// TestBudgetExhaustedExitIsFailPolicyAndNeverFailsOver 钉死预算耗尽出口：
// 终止型（NextAccountStop）+ fail_policy，且预算哨兵在传输出口被转成该错误时
// 不记 Ops 上游错误、不临时封禁账号（S3 既有语义），分类与之一致。
func TestClassifyBudgetExhaustedExitFailPolicy(t *testing.T) {
	repo := &transportTempUnschedRepoStub{}
	s := &GatewayService{accountRepo: repo}
	c := newTransportErrorTestGin(t)
	account := &Account{ID: 149, Name: "acc", Platform: PlatformAnthropic}

	budgetErr := fmt.Errorf("upstream send denied: %w", ErrRequestAttemptBudgetExhausted)
	require.True(t, IsRequestAttemptBudgetError(budgetErr))

	err := s.handleUpstreamTransportError(context.Background(), c, account, budgetErr, OpsUpstreamErrorEvent{})
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)

	require.Equal(t, FailureClassFailPolicy, failoverErr.Classify())
	require.Equal(t, FailureClassFailPolicy, failoverErr.FailureClass)
	require.False(t, failoverErr.ShouldRetryNextAccount(), "预算耗尽必须终止：不重试、不换号")
	require.False(t, failoverErr.IsRetryableClass())
	require.Equal(t, 0, repo.calls, "预算拒绝不得临时封禁账号")
	if _, ok := c.Get(OpsUpstreamErrorsKey); ok {
		t.Fatal("预算拒绝不得记 Ops 上游错误（上游从未被调用）")
	}
}

// TestBudgetFailoverErrorConstructorClassifiesFailPolicy 直接钉死构造点分类。
func TestClassifyBudgetFailoverErrorConstructorFailPolicy(t *testing.T) {
	failoverErr := NewRequestAttemptBudgetFailoverError()
	require.Equal(t, FailureClassFailPolicy, failoverErr.Classify())
	require.Equal(t, FailureClassFailPolicy, failoverErr.FailureClass)
	require.False(t, failoverErr.IsRetryableClass())
	require.False(t, failoverErr.ShouldRetryNextAccount())
	require.Equal(t, RequestAttemptBudgetReason, failoverErr.Reason)
	require.False(t, failoverErr.ShouldReportAccountScheduleFailure(), "预算耗尽不得上报账号调度失败")
}

// TestGrokCredentialExitClassifiesByReason 钉死 Grok 凭据出口（覆盖账号级与
// provider 级两类）：账号凭据需要处理 → retry_account；provider 配置不可用 → fail_policy。
func TestClassifyGrokCredentialExitByReason(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 9901, Name: "grok-acc", Platform: PlatformGrok}

	t.Run("账号级凭据故障仍可换号", func(t *testing.T) {
		c := newTransportErrorTestGin(t)
		err := svc.newGrokCredentialFailover(c, account, grokCredentialFailureClass{
			scope:   GatewayFailureScopeAccount,
			reason:  GrokCredentialReasonRevoked,
			action:  NextAccountRetry,
			message: "Grok OAuth credentials require account action",
		})
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, FailureClassRetryAccount, failoverErr.Classify())
		require.Equal(t, FailureClassRetryAccount, failoverErr.FailureClass)
		require.True(t, failoverErr.IsRetryableClass())
	})

	t.Run("provider 配置不可用不换号", func(t *testing.T) {
		c := newTransportErrorTestGin(t)
		err := svc.newGrokCredentialFailover(c, account, grokCredentialFailureClass{
			scope:   GatewayFailureScopeProvider,
			reason:  GrokCredentialReasonProviderConfig,
			action:  NextAccountStop,
			message: "Grok OAuth provider configuration is unavailable",
		})
		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.Equal(t, FailureClassFailPolicy, failoverErr.Classify())
		require.Equal(t, FailureClassFailPolicy, failoverErr.FailureClass)
		require.False(t, failoverErr.IsRetryableClass())
		require.False(t, failoverErr.ShouldRetryNextAccount())
	})
}

// TestOpenAICompatibleModelNotFoundClassifiesAsRetryAccount 钉死 OpenAI 兼容 400
// 「模型名无效」：这是账号/提供方能力问题（换号可能挂载该模型），不得误判为
// fail_client。分类只读 StatusCode + ResponseBody 推导，**不改写被分类对象的
// 语义字段**——出口返回什么字段，这里就断言什么字段（行为中性）。
func TestClassifyOpenAICompatibleModelNotFoundRetryAccount(t *testing.T) {
	body := []byte("{\"error\":{\"code\":\"model_not_found\",\"message\":\"The model does not exist\"}}")
	require.True(t, isOpenAICompatibleModelNotFound400(body))

	failoverErr := newOpenAIUpstreamFailoverError(
		http.StatusBadRequest, nil, body, "The model does not exist", false,
	)
	// 出口字段保持既有形态：分类器不得反过来改写它们。
	require.Empty(t, failoverErr.Stage)
	require.Empty(t, failoverErr.Scope)
	require.Empty(t, failoverErr.Reason)
	require.Equal(t, FailureClassRetryAccount, failoverErr.Classify())
	require.True(t, failoverErr.ShouldRetryNextAccount(), "换号语义不得改变")

	// 行为中性回归：分类不改变账号健康度判定与凭据/调度归因。
	statusCode, _, eligible := classifyOpenAIAPIKeyHealthFailure(failoverErr)
	require.Equal(t, http.StatusBadRequest, statusCode)
	require.False(t, eligible, "400 模型不存在不得计入 API Key 健康度熔断")
	require.False(t, failoverErr.IsCredentialFailure())
	require.True(t, failoverErr.ShouldReportAccountScheduleFailure(),
		"Stage=Inference 的失败仍归因到所选账号（既有语义）")
}

// TestClassifyKeepsLegacyRetryBehaviour 回归：既有 401/403/429/5xx 与零值
// NextAccountLegacyRetry 的换号行为不得被分类器改变。
func TestClassifyKeepsLegacyRetryBehaviour(t *testing.T) {
	legacy := []*UpstreamFailoverError{
		{}, // 零值 = legacy retry
		{StatusCode: http.StatusUnauthorized},
		{StatusCode: http.StatusForbidden},
		{StatusCode: http.StatusTooManyRequests},
		{StatusCode: http.StatusInternalServerError},
		{StatusCode: http.StatusBadGateway},
		{StatusCode: http.StatusServiceUnavailable},
		{StatusCode: 529},
		{StatusCode: http.StatusTooManyRequests, RetryableOnSameAccount: true},
		{StatusCode: http.StatusGatewayTimeout, RetryableOnSameAccount: true},
	}
	for _, err := range legacy {
		require.Truef(t, err.ShouldRetryNextAccount(), "status=%d 的既有换号语义不得回退", err.StatusCode)
		require.Truef(t, err.Classify().IsRetryable(), "status=%d 应判为 retry_*", err.StatusCode)
	}
}
