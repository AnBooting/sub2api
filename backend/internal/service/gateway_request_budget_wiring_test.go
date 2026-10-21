//go:build unit

package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TS-02 S2 接线完整性：所有真实上游发送都必须经过请求级发送预算闸门。
// 先例：backend/internal/server/routes/prompt_audit_route_coverage_test.go、
// backend/internal/repository/affiliate_repo_test.go 都直接读源码断言。
//
// 断言方式（2026-10-21 修订）：**不再按「某文件里钩子出现几次」计数**。
// 计数把断言绑死在具体文件与次数上：上游 0.2.17 把 models 端点从
// gemini_messages_compat_service.go 搬到 gemini_models_endpoints.go，计数立刻假红；
// 而真正的缺口（新增一个直接 httpUpstream.Do 的发送点）反而数不出来。
// 现在的口径是按行为立护栏：
//  1. 反向扫描全包（AST + 控制流判定，判定引擎见
//     gateway_request_budget_wiring_scan_test.go）：一次直发上游调用只有在被一条
//     「失败即中止」的 allowUpstreamSend 闸门检查**支配**时才算受预算约束；
//     同函数内「一处经闸门 + 一处直发」的混用、闸门之外的直发，都会报出
//     file:function:line。注释不参与判定（只看真实调用）；
//  2. 正向登记：budgetHookWiringTable 里的文件必须真的用到闸门（漏接线检测），
//     且不得出现未经闸门的直发调用。
//
// 行为级断言见 gemini_models_endpoints_budget_test.go（预算耗尽时 models 端点必须被拒）。
const (
	budgetHookSendMarker  = "sendUpstreamWithBudget("
	budgetHookAllowMarker = "allowUpstreamSend("
	// budgetHookDefinitionFile 定义这两个钩子的文件，自身当然包含标识符，不参与反向扫描。
	budgetHookDefinitionFile = "gateway_request_budget_upstream.go"
)

type budgetHookWiringCase struct {
	file   string
	marker string
}

// budgetHookWiringTable 是 S2 发送点登记表（只登记「允许接线」的文件，不绑定调用次数）。
// OpenAI 家族走唯一汇聚点 doOpenAIUpstream 的 allowUpstreamSend，其余走
// sendUpstreamWithBudget；profile 参数保持原样。
//
// TS-02 S3b：文本类 embedding/alpha-search 也经 doOpenAIUpstream 发送，
// 因此自动被 allowUpstreamSend 计数，无需新增登记条目；媒体
// openai_images.go / grok_media.go、codex 元数据 openai_codex_models_handler.go
// 与 count_tokens 仍刻意不挂预算（见排除清单与下方隐式依赖注释）。
//
// 2026-10-21：新增 gemini_models_endpoints.go —— 上游 0.2.17 把 models 端点
// 搬到独立文件后曾直接调用 httpUpstream.Do，绕过发送预算；现已改回预算闸门。
var budgetHookWiringTable = []budgetHookWiringCase{
	{"openai_plugin_transport.go", budgetHookAllowMarker},
	{"gateway_forward.go", budgetHookSendMarker},
	{"gateway_forward_as_chat_completions.go", budgetHookSendMarker},
	{"gateway_forward_as_responses.go", budgetHookSendMarker},
	{"gateway_anthropic_passthrough.go", budgetHookSendMarker},
	{"gateway_bedrock.go", budgetHookSendMarker},
	{"gateway_systemone.go", budgetHookSendMarker},
	{"gateway_service.go", budgetHookSendMarker},
	{"gemini_chat_completions_compat_service.go", budgetHookSendMarker},
	{"gemini_messages_compat_service.go", budgetHookSendMarker},
	{"gemini_models_endpoints.go", budgetHookSendMarker},
	{"antigravity_gateway_gemini.go", budgetHookSendMarker},
	{"antigravity_gateway_retry.go", budgetHookSendMarker},
	{"antigravity_gateway_upstream.go", budgetHookSendMarker},
	{"openai_ws_forwarder_ingress.go", budgetHookAllowMarker},
	{"openai_ws_forwarder_support.go", budgetHookAllowMarker},
	{"openai_ws_forwarder_v2.go", budgetHookAllowMarker},
	{"openai_ws_turn_budget_frame_conn.go", budgetHookAllowMarker},
}

// budgetHookExcludedFiles 是明确不接线的探测/额度/目录/账号测试类文件；
// account_test_service*.go 由 glob 覆盖。
//
// TS-02 S3b：媒体、codex 元数据与 count_tokens 同样登记为不消耗预算。
// openai_images.go / grok_media.go 属于 PRD-06 图片/视频副作用路径；
// openai_codex_models_service.go 只下发模型元数据，不执行推理。
//
// 2026-10-21 新增三项上游 0.2.17 / 本 fork 的非推理发送：
//   - antigravity_credits_overages.go：查询 Antigravity credits 用量，不是推理发送；
//   - gemini_cached_content.go：显式缓存的增删改（管理类调用），不产生模型推理；
//   - video_task_worker.go：PRD-06 异步视频任务执行侧，计费走独立的任务链路。
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
	"antigravity_credits_overages.go",
	"gemini_cached_content.go",
	"video_task_worker.go",
}

// budgetUngatedSendFunctionExemptions 登记同一文件内「按契约不消耗发送预算」的函数：
// 这些函数所在的文件整体仍受预算约束，只有被点名的函数允许直发上游。
//
// openai_plugin_transport.go 的 doOpenAIAccountTestUpstream 是账号测试路径
// （AccountTestService），与 account_test_service*.go 同一类：账号测试不是
// 单逻辑请求的真实推理发送，不占用其 16 次发送预算。
var budgetUngatedSendFunctionExemptions = map[string][]string{
	"openai_plugin_transport.go": {"doOpenAIAccountTestUpstream"},
}

// 隐式依赖（TS-02 S3 已确认，刻意不接线）：count_tokens 路由
// （routes/gateway.go:58 → handler/openai_gateway_count_tokens.go:176）经
// service/openai_gateway_count_tokens.go:86/:326 的 doOpenAIUpstream 发送，会被
// allowUpstreamSend 计数；但该路由**不挂预算**（ctx 无预算 → nil → 不计数也不拦截），
// 因此 count_tokens 不占用单逻辑请求的 16 次发送预算。
// 旁证：routes/prompt_audit_route_coverage_test.go:55 明确 count_tokens 是
// "tokenization only; it does not execute a model request"。
// 若将来给 count_tokens 路由挂预算，必须同时更新本注释与排除清单口径。
func readBudgetHookSource(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(name)
	require.NoErrorf(t, err, "读取 %s 失败（测试工作目录应为 backend/internal/service）", name)
	return string(source)
}

// budgetExcludedFileSet 返回「明确不消耗发送预算」的文件集合（含账号测试 glob）。
func budgetExcludedFileSet(t *testing.T) map[string]struct{} {
	t.Helper()
	excluded := make(map[string]struct{}, len(budgetHookExcludedFiles)+4)
	for _, f := range budgetHookExcludedFiles {
		excluded[f] = struct{}{}
	}
	accountTestFiles, err := filepath.Glob("account_test_service*.go")
	require.NoError(t, err)
	for _, f := range accountTestFiles {
		excluded[f] = struct{}{}
	}
	return excluded
}

// budgetRemainingSendViolations 返回文件内未受预算约束的直发发送点
// （已扣除显式登记的函数级豁免），每条带函数名与行号。
func budgetRemainingSendViolations(file string, source string) []budgetSendViolation {
	exempt := budgetUngatedSendFunctionExemptions[file]
	out := make([]budgetSendViolation, 0)
	for _, v := range budgetUngatedSendViolations(file, source) {
		if slices.Contains(exempt, v.Func) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// TestRequestAttemptBudgetHookWiringIsComplete 登记表里的每个文件必须真的用到预算闸门
// （漏接线检测），且文件内不得出现未经闸门的直发上游调用。
//
// 只断言「用到闸门」而不是「用到几次」：接线次数是实现细节（上游把某个发送点
// 搬到新文件就会改变次数），护栏要守住的是「这条发送受预算约束」这个行为。
func TestRequestAttemptBudgetHookWiringIsComplete(t *testing.T) {
	for _, tc := range budgetHookWiringTable {
		t.Run(tc.file, func(t *testing.T) {
			source := readBudgetHookSource(t, tc.file)
			require.Containsf(t, source, tc.marker,
				"%s 必须接入发送预算闸门 %s（漏接线）", tc.file, tc.marker)

			sites := budgetRemainingSendViolations(tc.file, source)
			require.Emptyf(t, sites,
				"%s 内部存在绕过发送预算的直发上游调用：%v", tc.file, sites)
		})
	}
}

// TestRequestAttemptBudgetEveryUpstreamSendIsGated 反向扫描整个 service 包：
// 每个直发上游调用都必须受预算闸门约束，否则就是「新增一个绕过预算的发送点」。
//
// 与 TestRequestAttemptBudgetHookHasNoUnregisteredCallSites 的分工：
//   - 本用例抓「漏接线」（真实发送点没走闸门）；
//   - 那个用例抓「误接线」（闸门接到了未登记的文件）。
//
// 显式豁免只有两处：budgetHookExcludedFiles（整文件为非推理发送）与
// budgetUngatedSendFunctionExemptions（同文件内的个别非推理函数）。
func TestRequestAttemptBudgetEveryUpstreamSendIsGated(t *testing.T) {
	excluded := budgetExcludedFileSet(t)

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	violations := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == budgetHookDefinitionFile {
			continue
		}
		source := readBudgetHookSource(t, name)
		if _, ok := excluded[name]; ok {
			continue
		}
		for _, v := range budgetRemainingSendViolations(name, source) {
			violations = append(violations, v.String())
		}
	}
	require.Emptyf(t, violations,
		"以下上游发送点绕过了请求发送预算：%v（必须改用 sendUpstreamWithBudget/allowUpstreamSend，"+
			"或在排除清单里显式登记为非推理发送）", violations)
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
		// 文档口径是「排除文件不得出现任何 hook」：allowUpstreamSend 同样是 hook，
		// 出现即意味着该文件被纳入了发送预算。
		require.NotContainsf(t, source, budgetHookAllowMarker,
			"%s 属于排除清单（探测/额度/目录/账号测试），不得接入发送预算", file)
	}
}

// TestRequestAttemptBudgetExemptedFunctionsHaveNoHook 函数级豁免登记的是「这一处
// 确实不受预算约束的直发函数」：豁免函数里不得出现任何发送 hook
// （sendUpstreamWithBudget / allowUpstreamSend），否则它就不是纯直发函数，
// 豁免登记会掩盖真实的漏接线。
func TestRequestAttemptBudgetExemptedFunctionsHaveNoHook(t *testing.T) {
	require.NotEmpty(t, budgetUngatedSendFunctionExemptions, "函数级豁免清单不应为空")
	for file, funcs := range budgetUngatedSendFunctionExemptions {
		source := readBudgetHookSource(t, file)
		for _, fn := range funcs {
			body := budgetFuncBodySource(t, file, source, fn)
			require.NotContainsf(t, body, budgetHookSendMarker,
				"%s 的豁免函数 %s 不得接入发送预算闸门", file, fn)
			require.NotContainsf(t, body, budgetHookAllowMarker,
				"%s 的豁免函数 %s 不得接入发送预算闸门", file, fn)
		}
	}
}

// TestRequestAttemptBudgetHookHasNoUnregisteredCallSites 反向扫描整个 service 包：
// 任何出现发送钩子的非测试文件都必须在登记表里，防止把预算误接到未登记的文件。
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
