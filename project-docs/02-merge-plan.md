# 国模移植包 → 上游合并方案

> 本文件是**分析产物**，不是已执行的改动。所有行数差异为实测（`git diff --no-index --numstat`）。
> 基线：上游 `3a6fd1c9`（v0.2.15） vs 移植包源快照（主站 1.0.65，2026-09-20）。

## 0. 结论摘要

| 类别 | 文件数 | 处理方式 |
|---|---|---|
| `core/` 新增文件 | 7 | **直接新增**（上游全部不存在） |
| 迁移 SQL | 3 | **重编号** 196/197/198 → **243/244/245**（上游最新为 242） |
| `reference/shared/` | 42 | **逐段合并**，禁止整文件覆盖（差异极大） |
| `reference/protocol/` | 6 | **重新定位**：上游这些文件已大幅重构 |
| `reference/test-support/` | 3 | 按上游现有测试接口适配 |
| `tests/` | 14 | 适配上游测试辅助后并入 |

**核心判断**：移植包是**真实的净增量**（7 个 core 文件上游全无，`domestic_provider` 代码搜索 0 命中），
但它引用的**共享文件已被上游重构过**（上游 1.0.65 → v0.2.15 之间有数千次提交），因此**不能照抄**，
必须按「新增 / 合并 / 重写 / 放弃」逐个决策。

## 1. 命名冲突预警（**合并前必须先决策**）

上游已经存在两个与「domestic」语义相近的概念，与移植包**不是同一件事**：

| 上游已有 | 位置 | 作用 | 与移植包的关系 |
|---|---|---|---|
| `cn_provider` | `backend/internal/service/cn_provider_balance_service.go`、`ratelimit_cn_providers.go`、`domain/platforms.go`（`CNProvider bool`）、`config.go` `gateway.cn_providers.*` | 国产厂商**余额检查 + 限流 + 调度停调** | **功能不同**（一个是调度/风控，一个是分类+定价），但名字近似 |
| `domestic`（Key 分组分类） | `frontend/src/utils/keyGroupProviders.ts`：`KeyGroupProvider = 'anthropic' \| 'openai' \| 'domestic' \| 'other'` | 用户 API Key 列表按厂商筛选 | 前端已有 `domestic` 概念，与 `domesticProviders.ts` 需统一 |
| `domestic`（仪表盘文案） | `frontend/src/i18n/locales/{zh,en}/dashboard.ts` | 仪表盘「国产模型」分类 | 文案层，需保持一致 |

**决策项**：新字段是否沿用 `domestic_provider`，还是改名以避开歧义？→ 见 [07-decisions.md](07-decisions.md) ADR-005。

## 2. core/ — 7 个新增文件（直接新增）

| 移植包路径 | 目标路径 | 上游状态 | 依赖 |
|---|---|---|---|
| `core/backend/internal/service/domestic_provider.go` | `backend/internal/service/domestic_provider.go` | **不存在** | `PlatformOpenAI` / `PlatformAnthropic` 常量（需核对上游常量名） |
| `core/backend/internal/service/domestic_pricing.go` | `backend/internal/service/domestic_pricing.go` | **不存在** | `ModelPricing` 类型、平台常量、错误辅助 |
| `core/backend/internal/repository/domestic_pricing_repo.go` | `backend/internal/repository/domestic_pricing_repo.go` | **不存在** | SQL DB、目标项目 repository 约定 |
| `core/backend/internal/handler/admin/domestic_pricing_handler.go` | `backend/internal/handler/admin/domestic_pricing_handler.go` | **不存在** | Gin、响应辅助、AdminHandlers |
| `core/frontend/src/utils/domesticProviders.ts` | `frontend/src/utils/domesticProviders.ts` | **不存在** | 无 |
| `core/frontend/src/components/admin/group/DomesticProviderPicker.vue` | 同路径 | **不存在** | 上游 `Select.vue` / `Icon.vue` 的 props |
| `core/frontend/src/api/admin/domesticPricing.ts` | 同路径 | **不存在** | 上游 `api/client.ts` 的 `apiClient` |

**风险**：移植包用 `github.com/Wei-Shaw/sub2api` 作 import 前缀——与上游一致，**无需改**。
但 `PlatformOpenAI` 等常量名需与上游 `backend/internal/domain/platforms.go` 核对。

## 3. migrations/ — 3 个迁移（重编号）

| 原编号 | 文件 | 新编号 | 内容 |
|---|---|---|---|
| 196 | `196_group_domestic_provider.sql` | **243** | `groups` 表增加 `domestic_provider` 列 |
| 197 | `197_domestic_model_pricing_overrides.sql` | **244** | 覆盖价表 |
| 198 | `198_group_domestic_provider_constraint.sql` | **245** | 约束 |

**注意**：上游已到 **242**，且 `backend/migrations/` 下有 25+ 个 `*_migration_test.go`。
移植包自带的 `tests/backend/migrations/group_domestic_provider_migration_test.go` 需同步改名与编号。

**已知兼容差异**（移植包自己声明）：
> Go 的 `NormalizeDomesticProvider` 允许 [OI] 和 Anthropic，而 196/198 的数据库约束**仅允许 [OI]**。

→ 若只做 [OI] 国模，保持现状即可；若要支持 Anthropic 国模，需**另写升级迁移**统一约束。

## 4. reference/shared/ — 42 个共享文件（逐段合并）

按差异规模排序（`+新增/-删除` 行）：

| 文件 | 差异 | 合并重点 |
|---|---|---|
| `backend/internal/handler/openai_gateway_handler.go` | +1840 / -433 | ⚠️ **最大**。上游 118KB → 参考 178KB。**不要整文件覆盖**，只提取国模相关分支 |
| `frontend/src/views/admin/GroupsView.vue` | +1184 / -635 | 国模平台选项、厂商选择器、价格目录 |
| `frontend/src/components/account/EditAccountModal.vue` | +1256 / -350 | 厂商回填、协议选项、extra 合并 |
| `frontend/src/components/account/CreateAccountModal.vue` | +1029 / -456 | 国模入口、Base URL 预设、extra 写入 |
| `backend/internal/service/billing_service.go` | +992 / -306 | 厂商隔离计费 |
| `backend/internal/service/gateway_usage_billing.go` | +386 / -501 | Anthropic 路径（按需） |
| `frontend/src/views/admin/AccountsView.vue` | +601 / -175 | 厂商图标展示 |
| `backend/internal/service/openai_gateway_usage.go` | +452 / -263 | 用量与计费 |
| `backend/internal/service/admin_group.go` | +377 / -217 | 分组校验 |
| `backend/internal/service/account.go` | +520 / -55 | 账号厂商匹配 |
| `backend/internal/service/admin_account.go` | +274 / -236 | 账号 CRUD 校验 |
| `backend/internal/handler/admin/group_handler.go` | +360 / -97 | 请求结构体 |
| `backend/internal/service/model_pricing_resolver.go` | +162 / -205 | ⚠️ **上游已重构**（16KB vs 参考 16KB，但内容不同），需重新定位 |
| `backend/internal/service/wire.go` | +275 / -68 | ProviderSet 装配 |
| `backend/internal/repository/group_repo.go` | +151 / -149 | 增删改查字段 |
| `backend/internal/service/openai_gateway_chat_completions.go` | +242 / -47 | 协议选择 |
| `frontend/src/types/index.ts` | +235 / -43 | 类型定义 |
| `frontend/src/components/common/GroupBadge.vue` | +121 / -148 | 国模徽章 |
| `backend/internal/handler/dto/types.go` | +181 / -50 | DTO |
| `backend/internal/service/group.go` | +156 / -71 | 分组服务 |
| `backend/internal/server/routes/admin.go` | +140 / -39 | 价格目录路由 |
| `backend/internal/service/admin_service.go` | +129 / -49 | — |
| `backend/internal/handler/openai_chat_completions.go` | +99 / -67 | 请求前价格检查 |
| `backend/internal/handler/dto/mappers.go` | +118 / -25 | 映射 |
| `backend/internal/repository/api_key_repo.go` | +80 / -41 | 鉴权缓存字段 |
| `backend/internal/service/api_key_auth_cache.go` | +97 / -15 | ⚠️ 缓存必须携带 `domestic_provider`，否则绕过价格命名空间 |
| `backend/ent/schema/group.go` | +73 / -12 | ent schema 新字段（需 `go generate`） |
| `backend/internal/handler/openai_alpha_search.go` | +62 / -21 | 价格检查 |
| `backend/internal/service/admin_group_duplicate.go` | +40 / -27 | 复制分组 |
| `frontend/src/components/common/Select.vue` | +62 / -5 | 补 props |
| `backend/internal/service/account_service.go` | +42 / -20 | 通用更新入口校验 |
| `backend/internal/handler/wire.go` | +44 / -15 | Handler 装配 |
| `backend/internal/handler/openai_embeddings.go` | +32 / -20 | 价格检查 |
| `frontend/src/components/common/PlatformIcon.vue` | +46 / -0 | 图标 |
| `frontend/src/components/common/GroupSelector.vue` | +22 / -16 | 厂商过滤 |
| `backend/internal/repository/wire.go` | +22 / -4 | — |
| `backend/internal/handler/openai_live.go` | +15 / -11 | — |
| `frontend/src/api/admin/index.ts` | +12 / -6 | 导出新 API |
| `backend/internal/handler/handler.go` | +7 / -5 | — |
| `frontend/src/components/icons/Icon.vue` | +2 / -0 | 图标名 |
| `frontend/src/components/account/AccountGroupsCell.vue` | +0 / -2 | 显示 |
| `frontend/src/components/common/GroupOptionItem.vue` | +47 / -86 | 显示 |
| `frontend/src/components/common/PlatformTypeBadge.vue` | +91 / -30 | 显示 |
| **合计** | **+12,798 / -5,136** | 47 个文件 |

> ⚠️ **警告**：参考文件的差异中**混杂了大量与国模无关的上游演进**（支付、渠道监控、画布等）。
> 参考文件是「主站 1.0.65」的完整文件，不是「国模 diff」。**必须用 MERGE-INDEX.md 里的行号摘录定位国模片段**，
> 只提取国模相关代码，其余一律以上游为准。

## 5. reference/protocol/ — 6 个协议文件（需重新定位）

| 文件 | 上游状态 | 说明 |
|---|---|---|
| `backend/internal/service/openai_gateway_chat_completions.go` | 存在，差异 +242/-47 | Chat Completions 直转 |
| `backend/internal/service/openai_gateway_chat_completions_raw.go` | 存在，差异 +159/-45 | 原始转发 |
| `backend/internal/service/openai_gateway_responses_chat_fallback.go` | 存在，差异 +279/-45 | Responses → Chat 回退 |
| `backend/internal/service/openai_apikey_responses_probe.go` | 存在，差异 +120/-4 | 自动探测 |
| `backend/internal/pkg/openai_compat/upstream_capability.go` | 存在（4,953 B） | 上游能力判定 |
| `backend/internal/service/account.go` | 存在，差异 +520/-55 | — |

**移植包给出的关键兼容提示**：
> 源版创建弹窗的 `selectPlatformFamily` 会把本地协议状态设为 `force_chat_completions`，
> 但 `buildOpenAIExtra` 的国模分支**提前返回，没有把协议状态持久化**。
> 目标已有自动探测时保留并验证；没有时建议显式写 `extra.openai_responses_mode = "force_chat_completions"`。

→ **待决策**：上游已有自动探测（`openai_apikey_responses_probe.go` 存在），保留探测还是强制 Chat Completions？见 ADR-006。

## 6. tests/ — 14 个测试文件

| 类别 | 文件数 | 说明 |
|---|---|---|
| 后端 service | 4 | 定价、中国标准、集成、厂商校验 |
| 后端 repository | 1 | SQL mock |
| 后端 handler / dto | 2 | — |
| 后端 migrations | 1 | 需同步重编号 |
| 前端 | 5 | domesticProviders、domesticPricing、GroupBadge、GroupOptionItem、GroupSelector |
| test-support | 3 | `model_pricing_resolver_test.go`（+164/-21）、`vitest.config.ts`、`setup.ts` |

**依赖**：这些测试依赖原项目的测试辅助 mock。上游当前已有自己的测试辅助（`backend/internal/testutil/`、`frontend/src/**/__tests__/`），
需按上游接口改写，**不能直接照搬**。

## 7. 执行顺序（P1 阶段）

1. 先跑 `node _reference/domestic-provider-kit/tools/preflight.mjs F:\sub2` 生成冲突报告（只读）。
2. 读 `MERGE-INDEX.md`（162 KB，4,130 行）逐段定位国模片段。
3. **决策命名冲突**（第 1 节）→ 更新 ADR。
4. 新增 7 个 core 文件 + 3 个迁移（重编号 **259+**；0.2.17 同步后本 fork 的迁移已占 251–258，上游占 243/244/245，246–250 留空）。
5. 合并后端共享文件（ent → repository → service → handler → wire → routes），
   每层合并后立即 `go build` 验证，不要一次改完再编译。
6. `go generate ./ent` + wire 生成。
7. 合并前端共享文件，每层合并后 `vue-tsc` 验证。
8. 适配并运行 14 个测试。
9. 按 `_reference/domestic-provider-kit/docs/ACCEPTANCE.md` 的 21 项逐条验收。
