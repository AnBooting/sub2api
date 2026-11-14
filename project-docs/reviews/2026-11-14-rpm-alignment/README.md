# RPM/TPM 口径统一（2026-11-14）

三个页面显示同名 RPM 但数字对不上。本记录说明根因、修复与未改动项。

## 根因

| 页面 | 窗口 | 分母 | token 组成 |
|---|---|---|---|
| 管理端仪表盘 | 固定最近 5 分钟 | **写死除以 5** | input + output |
| 渠道监控 V2 | 用户选的 90m/24h/7d/30d | `coveredMinutes`（实际覆盖时长） | input + output + cacheCreation + cacheRead |
| API Key 仪表盘 | 固定最近 5 分钟 | 写死除以 5 | 含缓存 |

主因是窗口长度不同（5 分钟 vs 90 分钟），这本身不是缺陷。真正的缺陷是**分母写死**：

窗口内只有 2 分钟有数据时，旧实现仍除以 5。4 次请求应报 2 RPM，旧实现报 0；800 token 应报 400 TPM，旧实现报 160。新库、数据恢复、保留策略清理后重新积累时都会命中。

## 修复

`backend/internal/repository/usage_log_repo_dashboard.go`：

- 新增 `dashboardPerformanceCoveredMinutes()`：分母取「窗口 ∩ 实际覆盖区间」。边界来自 `usage_stats_repair_control.raw_safe_from`。
- 边界早于窗口起点（稳态）→ 分母仍是 5，与旧实现逐位一致。
- 边界落在窗口内（新库/恢复）→ 分母收缩到实际覆盖时长。
- 边界为零值（控制行缺失，例如迁移 260 未执行）→ 回落整窗口，保持旧行为。
- 新增 `queryPerformanceStats()`，管理员/用户/API Key 三条路径共用同一分母口径。

`usage_log_types.go` 的字段注释同步为「窗口内实际覆盖分钟数折算」。

## 验证

新增 `dashboard_performance_window_test.go`（5 个用例）：

| 用例 | 断言 |
|---|---|
| 部分覆盖 | 4 次请求 / 2 分钟 → 2 RPM（旧实现 0） |
| 整窗口 | 50 次请求 → 10 RPM，与旧实现一致 |
| 空窗口 | 0 RPM / 0 TPM，不因分母塌缩产生噪声 |
| 边界语义 | 未知/早于窗口/恰好整窗口 均为 5.0 |
| API Key 路径 | 9 次请求 / 3 分钟 → 3 RPM |

**变异验证**：把分母改回 `dashboardPerformanceWindow.Minutes()`（等价旧实现的写死除以 5），`TestDashboardPerformanceAPIKeyUsesCoveredMinutes` 失败（期望 3，实际 1），退出码 1；恢复后通过。

`dashboard_partial_test.go` 的 mock 同步为三列（新增 `coverage_start`），原有 `rpm == 2` 断言不变。

## 前端窗口标注

同名不同义是这次误解的直接来源，UI 上补标注：

- 管理端仪表盘：标题后加「· 近 5 分钟」（`admin.dashboard.performanceWindow`）
- 渠道监控：RPM 副标题改为「每分钟请求数（当前窗口均值）」（`channelMonitorV2.metrics.rpmDetailWindowed`）
- i18n 同时补 zh 与 en，`check:i18n` 通过

## 未改动：token 组成

仪表盘 TPM 只算 `input + output`，渠道监控含 `cacheCreation + cacheRead`。缓存率 86% 的场景下，两边 TPM 差异会比 RPM 更大。

**本次未统一**，原因：

1. 仪表盘 TPM 的语义是「生成侧吞吐」，缓存读取不计入生成成本，改动会改变已有指标含义。
2. 统一会让历史趋势图与告警阈值全部跳变。
3. 当前 UI 已分别标注窗口，用户可判断差异来源。

如需统一，建议单独一轮并同步调整趋势图与告警基线。

## 门禁

- `-Scope frontend -Mode full`：exit 0，5 项全 PASS
- `-Scope backend -Mode full`：exit 0，4 PASS + 1 WARN（PgDumper 系列，本机无 sh，已登记为已知环境失败）
