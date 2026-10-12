//go:build unit

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TS-02 S2 接线完整性：按源码文本锁定白名单，防「漏接线」与「误接线」。
// 先例：backend/internal/server/routes/prompt_audit_route_coverage_test.go、
// backend/internal/repository/affiliate_repo_test.go 都直接读源码断言。
const (
	budgetHookSendMarker  = "sendUpstreamWithBudget("
	budgetHookAllowMarker = "allowUpstreamSend("
	// budgetHookDefinitionFile 定义这两个钩子的文件，自身当然包含标识符，不参与反向扫描。
	budgetHookDefinitionFile = "gateway_request_budget_upstream.go"
)

type budgetHookWiringCase struct {
	file   string
	marker string
	count  int
}

// budgetHookWiringTable 是 S2 发送点白名单（行号基准 23a4e24b8）。
// OpenAI 家族走唯一汇聚点 doOpenAIUpstream 的 allowUpstreamSend，其余走
// sendUpstreamWithBudget；profile 参数保持原样。
//
// TS-02 S3b：文本类 embedding/alpha-search 也经 doOpenAIUpstream 发送，
// 因此自动被 allowUpstreamSend 计数，无需新增白名单条目；媒体
// openai_images.go / grok_media.go、codex 元数据 openai_codex_models_handler.go
// 与 count_tokens 仍刻意不挂预算（见排除清单与下方隐式依赖注释）。
var budgetHookWiringTable = []budgetHookWiringCase{
	{"openai_plugin_transport.go", budgetHookAllowMarker, 1},
	{"gateway_forward.go", budgetHookSendMarker, 4},
	{"gateway_forward_as_chat_completions.go", budgetHookSendMarker, 1},
	{"gateway_forward_as_responses.go", budgetHookSendMarker, 1},
	{"gateway_anthropic_passthrough.go", budgetHookSendMarker, 1},
	{"gateway_bedrock.go", budgetHookSendMarker, 1},
	{"gateway_systemone.go", budgetHookSendMarker, 1},
	{"gateway_service.go", budgetHookSendMarker, 1},
	{"gemini_chat_completions_compat_service.go", budgetHookSendMarker, 1},
	{"gemini_messages_compat_service.go", budgetHookSendMarker, 3},
	{"antigravity_gateway_gemini.go", budgetHookSendMarker, 1},
	{"antigravity_gateway_retry.go", budgetHookSendMarker, 3},
	{"antigravity_gateway_upstream.go", budgetHookSendMarker, 1},
	{"openai_ws_forwarder_ingress.go", budgetHookAllowMarker, 1},
	{"openai_ws_turn_budget_frame_conn.go", budgetHookAllowMarker, 1},
}

// budgetHookExcludedFiles 是明确不接线的探测/额度/目录/账号测试类文件；
// account_test_service*.go 由 glob 覆盖。
//
// TS-02 S3b：媒体、codex 元数据与 count_tokens 同样登记为不消耗预算。
// openai_images.go / grok_media.go 属于 PRD-06 图片/视频副作用路径；
// openai_codex_models_handler.go 只下发模型元数据，不执行推理。
//
// 只列本包（internal/service）内的文件：claude_usage_service.go 在
// internal/repository/ 下，不属于本包扫描范围，故不在此列。
var budgetHookExcludedFiles = []string{
	"cline_usage.go",
	"command_code_usage.go",
	"ollama_cloud_usage.go",
	"opencode_go_usage.go",
	"cn_provider_balance_service.go",
	"cn_provider_quota_service.go",
	"grok_quota_service.go",
	"grok_observed_models.go",
	"model_protocol_catalog.go",
	"upstream_models.go",
	"openai_codex_models_service.go",
	"openai_apikey_responses_probe.go",
	"upstream_billing_probe.go",
	"openai_images_b64_backfill.go",
	"grok_media.go",
	"seedance.go",
	"grok_audio.go",
	"gateway_count_tokens.go",
	"openai_gateway_count_tokens.go",
}

// 隐式依赖（TS-02 S3 已确认，刻意不接线）：count_tokens 路由
// （routes/gateway.go:58 → handler/openai_gateway_count_tokens.go:176）经
// service/openai_gateway_count_tokens.go:86/:326 的 doOpenAIUpstream 发送，会被
// allowUpstreamSend 计数；但该路由**不挂预算**（ctx 无预算 → nil → 不计数也不拦截），
// 因此 count_tokens 不占用单逻辑请求的 16 次发送预算。
// 旁证：routes/prompt_audit_route_coverage_test.go:55 明确 count_tokens 是
// "tokenization only; it does not execute a model request"。
// 若将来给 count_tokens 路由挂预算，必须同时更新本注释与白名单口径。
func readBudgetHookSource(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(name)
	require.NoErrorf(t, err, "读取 %s 失败（测试工作目录应为 backend/internal/service）", name)
	return string(source)
}

// TestRequestAttemptBudgetHookWiringIsComplete 每个白名单文件必须恰好出现预期次数的
// 发送钩子：漏接线（少）或重复接线（多）都会失败。
func TestRequestAttemptBudgetHookWiringIsComplete(t *testing.T) {
	for _, tc := range budgetHookWiringTable {
		t.Run(tc.file, func(t *testing.T) {
			source := readBudgetHookSource(t, tc.file)
			require.Equalf(t, tc.count, strings.Count(source, tc.marker),
				"%s 的 %s 出现次数必须恰好为 %d", tc.file, tc.marker, tc.count)
		})
	}
}

// TestRequestAttemptBudgetHookIsNotWiredIntoExcludedFiles 排除清单里的文件不得出现
// 发送钩子：探测、额度查询、模型目录、账号测试都不是真实推理发送，不能计入预算。
func TestRequestAttemptBudgetHookIsNotWiredIntoExcludedFiles(t *testing.T) {
	excluded := append([]string{}, budgetHookExcludedFiles...)
	accountTestFiles, err := filepath.Glob("account_test_service*.go")
	require.NoError(t, err)
	require.NotEmpty(t, accountTestFiles, "排除清单引用的 account_test_service*.go 必须存在，否则清单已过期")
	excluded = append(excluded, accountTestFiles...)

	for _, file := range excluded {
		// 显式校验清单条目真实存在：清单过期（文件被重命名/移动/删除）时
		// 必须立刻失败，而不是让 readBudgetHookSource 报出难以定位的 os 错误。
		_, statErr := os.Stat(file)
		require.NoErrorf(t, statErr, "排除清单条目 %s 不存在，清单已过期", file)

		source := readBudgetHookSource(t, file)
		require.NotContainsf(t, source, budgetHookSendMarker,
			"%s 属于排除清单（探测/额度/目录/账号测试），不得接入发送预算", file)
	}
}

// TestRequestAttemptBudgetHookHasNoUnregisteredCallSites 反向扫描整个 service 包：
// 任何出现发送钩子的非测试文件都必须在白名单表里，防止把预算误接到未登记的文件。
//
// 局限（TS-02 S3 已确认）：本用例只能发现「误接线」，**发现不了真实发送点被整段
// 漏接线**——某个发送点若从未调用这两个钩子，源码里就没有对应 marker，扫描自然
// 也不会报。防漏接线依赖上面的正向计数表（budgetHookWiringTable）与人工核对，
// 不得声称本用例能防漏接线。
func TestRequestAttemptBudgetHookHasNoUnregisteredCallSites(t *testing.T) {
	registered := make(map[string]struct{}, len(budgetHookWiringTable))
	for _, tc := range budgetHookWiringTable {
		registered[tc.file] = struct{}{}
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	stray := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == budgetHookDefinitionFile {
			continue
		}
		if _, ok := registered[name]; ok {
			continue
		}
		source := readBudgetHookSource(t, name)
		if strings.Contains(source, budgetHookSendMarker) || strings.Contains(source, budgetHookAllowMarker) {
			stray = append(stray, name)
		}
	}
	require.Emptyf(t, stray, "以下文件接入了发送预算但未登记在白名单表：%v", stray)
}

// TestRequestAttemptBudgetHookDoesNotOverrideRequestContext 锁定「不给请求 ctx 加
// context.WithTimeout」的硬约束：预算只查询 deadline，加 WithTimeout 会破坏流式
// detach 计费约定。
func TestRequestAttemptBudgetHookDoesNotOverrideRequestContext(t *testing.T) {
	source := readBudgetHookSource(t, budgetHookDefinitionFile)
	require.NotContains(t, source, "context.WithTimeout", "发送钩子不得给请求 ctx 加超时")
}
