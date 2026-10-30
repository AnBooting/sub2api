# 验收记录：安全整改（2026-10-30）

- 分支：`fix/security-remediation-20261030`
- 基线提交：`f9d8bc855`（= `develop` HEAD，本分支切出点）
- 记录人：verifier（独立验收，非实现者）
- 状态：**已定稿**（冻结快照 21:35，最终数字见 §2.4c / §3.2a）
- 文档结构：§0 结论速览 / §1 环境 / §2 后端 / §3 前端 / §4 变异验证 / §5 部署一致性 / §6 线上回归 / §7 缺陷→测试→变异 三列表 / §8 未验证项 / §9 可复核性清单

> 本文所有数字均来自本机（或明确注明的 Lead 实跑）命令输出；没跑过的项目一律在 §8 标注「未跑过」，不推断。

## 0. 结论速览

| 验收项 | 结论 |
|---|---|
| 后端构建 `go build -o bin/server.exe ./cmd/server` | ✅ 退出码 **0** |
| 后端单测 `go test -tags=unit -count=1 ./...`（冻结快照） | ⚠️ 退出码 **1**，但**非本轮失败数 = 0**：仅 3 条 `TestPgDumper*`（Windows 无 `sh`）+ 2 条满载 flaky（隔离 `-count=3` 均通过）。见 §2.4c |
| 红线：**缺价不得放行** | 🟠 **曾经回归 → 已修复 → 复验通过**：21:0x 实测失败（实际打到上游返 502），21:26 单点复测 **ok**，§2.4c 全量复验通过。**回归事实保留在案**（详见 §2.4 红线小节） |
| 前端类型 `vue-tsc -b` | ✅ 退出码 **0** |
| 前端单测 `vitest run`（冻结快照） | ✅ 退出码 **0**，**435 files / 3456 tests 全过**（基线 427 / 3419，新增 8 文件 37 用例）。本文作者与 Lead 各跑一次，数字一致 |
| 前端构建 `corepack pnpm@9 run build` | ✅ 退出码 **0**（基线阶段实测；最终阶段未重跑，原因见 §3.2） |
| 变异验证 | ⚠️ 本文作者完成 **3 条**（F-01 ×2、F-03）；其余按 Lead 分工交由 reviewer，**尚未并入本文** |
| 部署一致性脚本 | ⚠️ 退出码 **1**（DRIFT 由本次验证自身重建 dist 造成；并暴露脚本判据强度局限，见 §5.2） |
| 线上回归 | ⛔ **未跑过**（未收到部署通知） |

**一句话**：红线项经历过一次真实回归（已留痕），修复后复验通过；冻结快照下除 3 条环境性失败 + 2 条已知/满载 flaky 外零失败。

## 1. 环境

| 项 | 值 | 采集命令 |
|---|---|---|
| OS | Microsoft Windows 11 家庭版 中文版（NT 10.0.26300.0） | `[System.Environment]::OSVersion.VersionString` |
| Go | `go1.27.3 windows/amd64`（GOROOT=`F:\tools\go1.27.3\go`，GOTOOLCHAIN=local） | `go version` |
| Node | `v24.18.0` | `node --version` |
| Vitest | `vitest/2.1.9 win32-x64 node-v24.18.0` | `node node_modules/vitest/vitest.mjs --version` |
| pnpm（经 corepack） | `9.15.9` | `corepack pnpm@9 --version` |
| 仓库 | `F:\sub2`，分支 `fix/security-remediation-20261030` | `git rev-parse --short HEAD` / `git branch --show-current` |

环境变量（每次命令前显式设置）：

```powershell
$env:GOROOT='F:\tools\go1.27.3\go'; $env:GOPATH='F:\tools\gopath'
$env:GOTOOLCHAIN='local'; $env:PATH="$env:GOROOT\bin;$env:PATH"
```

## 2. 后端

### 2.1 基线（当前分支，源码 = develop，无任何修复落盘）

工作树状态（`git status --short`，仅未跟踪目录，无已修改文件）：

```
?? .deployprobe/
?? project-docs/reviews/2026-10-30-security-remediation/
?? sub2api-pr-review-tests-20261028/
```

即：**基线 = develop @ f9d8bc855 的纯源码状态**，未包含本轮任何修复。

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 产物 `bin/server.exe` 164,310,016 B，2026-10-30 19:39:12 |
| `go test -tags=unit ./...` | **1** | 见下方分解 |

`go test -tags=unit ./...` 基线分解（从 `-json` 事件流统计）：

```
PASS_TESTS=25908  FAIL_TESTS=3  SKIP_TESTS=34
PKG_PASS=61       PKG_FAIL=1   PKG_NO_TEST_FILES=52
```

**唯一失败的包**：`github.com/Wei-Shaw/sub2api/internal/repository`，3 条用例：

```
--- FAIL: TestPgDumperHoldsMigrationLockThroughReaderClose (0.03s)
--- FAIL: TestPgDumperReleasesMigrationLockWhenProcessFails (0.02s)
--- FAIL: TestPgDumperReportsUnlockFailureAndDiscardsConnection (0.02s)
```

失败原因（原始输出，`backup_pg_dumper_test.go`）：

```
Error: Received unexpected error:
       start pg_dump: exec: "sh": executable file not found in %PATH%
       release backup migration lock: release migrations lock: all expectations were already fulfilled,
       call to ExecQuery 'SELECT pg_advisory_unlock($1)' with args [{Name: Ordinal:1 Value:694208311321144027}] was not expected
```

判定：**已知环境性失败（Windows 无 `sh`），与本轮 diff 无关**。

证据链（三条都可复核）：

1. `Get-Command sh` 无结果；`C:\Program Files\Git\usr\bin\sh.exe` **不存在**（`Test-Path` = False）。
2. 测试文件来自提交 `95023e7d4`（`fix(backup): serialize pg_dump/psql with the migrations advisory lock`），且 `git cat-file -e develop:backend/internal/repository/backup_pg_dumper_test.go` 通过 —— **该文件存在于 `develop`**，即基线自带，不是本轮新增。
3. 同包的另外 3 条 `TestPgDumper*`（`ReleasesMigrationLockWhenStdoutPipeSetupFails`、`ReleasesMigrationLockWhenProcessStartFails`、`DoesNotStartProcessWhenMigrationLockFails`、`RejectsNilDatabase`）**通过** —— 说明只有真正需要拉起 `sh` 的用例受影响，不是包级崩溃。

**基线对比口径（后续所有全量都按这条判定）**：

> 除 `internal/repository` 的 `TestPgDumperHoldsMigrationLockThroughReaderClose`、`TestPgDumperReleasesMigrationLockWhenProcessFails`、`TestPgDumperReportsUnlockFailureAndDiscardsConnection` 这 3 条**已知环境性失败**外，**不得新增任何失败**。

也就是说，最终全量若出现 `FAIL_TESTS > 3` 或 `PKG_FAIL > 1`，新增部分一律计为本轮引入，必须定位到具体改动。

### 2.2 过程记录：一次 interim 全量（2026-10-30 19:58–20:00，**非最终**）

修复正在陆续落盘时跑的一次全量，**只作过程记录，不作结论**（那几分钟工作树仍在被其他成员改动）。

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit ./...`（`-json`） | **1** | `PASS_TESTS=25928 FAIL_TESTS=12 SKIP_TESTS=34` / `PKG_PASS=58 PKG_FAIL=4` |

4 个失败包 12 条失败：

```
internal/pkg/ip               TestGetClientIPNeverReturnsEmptyForUnparseableRemoteAddr (+4 subtests)
internal/server/middleware    TestLogger_AccessLogUsesForwardedClientIPFromTrustedProxy
internal/server/middleware    TestSessionBindingContextSnapshotsForwardedModeAndHeaders
internal/repository           TestPgDumper* ×3（已知环境性失败）
internal/repository           TestInflightReservation_RenewalKeepsStreamingReservationAlive
internal/service              TestLogCodexCLIOnlyDetection_RejectedIncludesRequestDetails
```

**立即复跑（20:06–20:11，同一工作树、无改动）**：

```
go test -tags=unit ./internal/pkg/ip/                     → ok   exit 0（cached）
go test -tags=unit ./internal/server/middleware/          → ok   exit 0（cached）
go test -tags=unit ./internal/service/ -run TestLogCodexCLIOnlyDetection → ok exit 0
go test -tags=unit ./internal/service/                    → ok   exit 0（217.996s）
```

**判定**：interim 那 12 条失败是「其他成员正在改一半 + 包级构建缓存/时序」造成的**瞬时状态**，不是稳定缺陷。三个包复跑全绿，`internal/repository` 只剩已知的 3 条 PgDumper（另加下面 §2.3 单独核实的 flaky 用例）。**最终结论以 §2.4 的全量为准。**

### 2.2b 三分法取证：`TestLogCodexCLIOnlyDetection_RejectedIncludesRequestDetails` 是**本轮修复暴露的过时测试**，不是既有失败

这条用例在 interim 与 run-A 全量里都红，一度被记为「既有失败」。本文用 A/B/C 三分法取证，**推翻了那个判断**。

**副本构造（先证明副本可信）**：

```powershell
git -C F:\sub2 archive develop backend | tar -x -C F:\mut-verify-verifier\develop-pristine
# 用 git hash-object 与 git rev-parse develop:<path> 逐文件比对，证明副本是 develop 的逐字节还原
```

```
internal\pkg\ip\ip.go                                          match=True
internal\server\routes\auth.go                                 match=True
internal\service\openai_gateway_service_codex_cli_only_test.go  match=True
internal\repository\backup_pg_dumper_test.go                    match=True
internal\middleware\rate_limiter.go                             match=True
（未跟踪文件检查：auth_account_rate_limit_test.go 在该副本中不存在 = False）
```

**三组对照**：

| 组 | 被测代码 | 结果 |
|---|---|---|
| **A** | **develop 原样**（逐字节副本） | **ok，exit 0**（2026-10-30 20:48:24） |
| **B** | **当前工作树（含本轮全部修复）** | **FAIL**（`A_EXIT=1`） |
| **C** | **当前工作树，但只把 `backend/internal/pkg/ip/ip.go` 换回 develop 版本**，其余修复全部保留 | **ok，exit 0**（`B_EXIT=0`） |

**结论**：红点由 `ip.go` 的 F-01 信任方向反转引起 → **不是既有缺陷，是本轮修复暴露的过时测试**。

**根因（源码级）**：`openai_gateway_service_codex_cli_only_test.go:182` 设 `c.Request.RemoteAddr = "172.18.0.1:54321"`（私网、**非受信对端**）并 `Header.Set("X-Real-IP", "203.0.113.42")`，然后 `openai_gateway_service.go:1207` 用 `ip.GetClientIP(c)` 写 `request_client_ip`，断言它等于 `203.0.113.42`。

新信任模型下对端 172.18.0.1 不在默认受信网段（只有 `127.0.0.0/8`、`::1/128`），**转发头必须被忽略**，返回 `172.18.0.1`，断言失败。实测日志行印证：

```
"request_client_ip":"172.18.0.1","request_remote_addr":"172.18.0.1:54321", ... "x-real-ip":"203.0.113.42"
```

即：这条既有测试把旧的「任意客户端都能用 `X-Real-IP` 指定自己的 IP」语义**固定住了**——正是 AGENTS.md 验证纪律第 1 条「测试恰好把 bug 固定住」的典型。正确做法是改测试（改成受信对端场景，或显式 `SetTrustedProxyPolicy`），**不是**改生产代码迁就它，否则 F-01 的修复会被挡回去。

同类适配在本轮已有先例：`server/middleware/session_binding_test.go` 把 `RemoteAddr` 从 `9.9.9.9` 改成 `127.0.0.1` 并显式 `ip.SetTrustedProxyPolicy`。

### 2.3 已知 flaky 核实：`TestInflightReservation_RenewalKeepsStreamingReservationAlive`

interim 全量里它失败过一次，按 AGENTS.md「已知 flaky」纪律单独核实（`-count=3`，隔离运行）：

```
go test -tags=unit ./internal/repository/ \
  -run TestInflightReservation_RenewalKeepsStreamingReservationAlive -count=3 -v
```

```
=== RUN   TestInflightReservation_RenewalKeepsStreamingReservationAlive
--- PASS: TestInflightReservation_RenewalKeepsStreamingReservationAlive (2.54s)
=== RUN   TestInflightReservation_RenewalKeepsStreamingReservationAlive
--- PASS: TestInflightReservation_RenewalKeepsStreamingReservationAlive (2.51s)
=== RUN   TestInflightReservation_RenewalKeepsStreamingReservationAlive
--- PASS: TestInflightReservation_RenewalKeepsStreamingReservationAlive (2.51s)
PASS
ok  github.com/Wei-Shaw/sub2api/internal/repository  12.409s
INFLIGHT_EXIT=0
```

**结论**：隔离运行 3/3 通过 → **判定为「满载时偶发」的 flaky，与 AGENTS.md 里 `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 同族**（内存/时序敏感，全量套件满载时偶发），**非本轮 diff 引入**，不计入「新增失败」。

### 2.4 最终全量

最终全量由 **Lead 亲自执行**（本文作者在同一时刻的全量见 §2.4b，两者互相印证）：

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit -count=1 ./...` | **1** | 3 个 FAIL 包 / 其余全 ok |

**FAIL 包（3 个）与归属**：

| 包 | 耗时 | 失败用例 | 实际 vs 期望 | 归属与状态 |
|---|---|---|---|---|
| `internal/handler` | 269s | `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream`（5 子用例：unpriced / missing_input / missing_output / missing_cache_read / missing_cache_write） | 实际 **502 upstream_error**（**上游已被调用**），期望 503 `pricing_unavailable` | **🔴 红线回归：缺价请求穿透到上游。截至本报告未修复，修复中。** |
| `internal/handler/admin` | 28.9s | `TestAllAdminUsageStatsEndpointsShareContract`（4 子用例：GET /dashboard/users-ranking、POST /dashboard/users-usage、POST /dashboard/api-keys-usage、GET /dashboard/user-breakdown） | 实际 500，期望 503 | 测试 cases 与「删 4 处死代码」的裁决矛盾（Lead 已裁决删 cases）；属**测试与实现口径未对齐**，非生产缺陷 |
| `internal/repository` | 79.8s | `TestPgDumperHoldsMigrationLockThroughReaderClose` 等 3 条 | 见 §2.1 | **已知环境性失败**（本机 Windows 无 `sh`），非本轮引入 |

**ok 包（其余全部）**，含关键包与耗时：

```
internal/service              313s     ok
internal/server/routes         68s     ok
internal/server/middleware     46.5s   ok
internal/middleware            38s     ok
internal/pkg/ip                 5.6s   ok
internal/server                47.3s   ok
internal/config                30.8s   ok
```

#### 🔴 红线项单列：缺价不得放行（先红 → 后修复 → 复验通过）

**时间线（三段都要留痕，不能只写最后一段）**：

| 时刻 | 执行者 | `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` | 说明 |
|---|---|---|---|
| 21:0x | **Lead** | **FAIL**：5 子用例全部实际 **502 upstream_error**（**上游已被调用**），期望 503 `pricing_unavailable` | **本轮一度违反项目绝对红线第 6 条「缺价不得放行」**。这是真实发生过的回归，修复前不得视为通过 |
| 21:26:40 | **本文作者** | **ok，exit 0，3.214s** | 修复已落盘；同一次复测 `TestAllAdminUsageStatsEndpointsShareContract` 也 **ok**（3.174s） |
| 21:29 起 | 本文作者 | 见 §2.4c 最终全量 | 全量级复验 |

同批复测命令与输出：

```
=== REDLINE CHECK 2026-10-30T21:26:40.7779254+08:00
ok  	github.com/Wei-Shaw/sub2api/internal/handler	3.214s
EXIT=0
--- handler/admin contract ---
ok  	github.com/Wei-Shaw/sub2api/internal/handler/admin	3.174s
EXIT=0
```

**Lead 21:0x 的原始失败输出（原文，保留）**：

```
--- FAIL: TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream/unpriced
--- FAIL: TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream/missing_input
--- FAIL: TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream/missing_output
--- FAIL: TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream/missing_cache_read
--- FAIL: TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream/missing_cache_write
（5 子用例全部实际 502 upstream_error，期望 503 pricing_unavailable）
```

**完整时间线（并列留痕，不省略任何一段）**：

> 21:0x 发现缺价请求穿透上游（502 `upstream_error`，上游 callList 非空）→ 定位到 F-06 的 `refinePricingRejection` 单行调用 → 该函数整体移除 → 21:26 单点复测 **ok** → 冻结后全量 **ok**。**红线曾被实质违反，已在合入前修复；F-06 本轮不交付。**

**结论**：红线回归**确实存在过**（Lead 21:0x 的原始输出为准），**现已修复**（本文作者 21:26 单点复测 + 冻结快照全量复验）。按验收纪律，最终判定以**修复后**的复验为准，但回归事实必须保留在记录里。

**F-06 的交付状态**：本轮**不交付**。`refinePricingRejection` 整体移除，`handler/gateway_handler.go` 的未知模型 404 / 未定价 503 文案改进**退回基线行为**。

**很重要的一点（reviewer 的变异结论，转记）**：把 `refinePricingRejection` **重新加回（一行注入）→ 既有端到端测试 5 子用例全红**。这说明**既有端到端测试本身就是下一轮 F-06 的 mandatory 护栏，不需要新写测试**——下一轮做 F-06 时直接以这 5 条子用例为验收判据即可。

### 2.4a 本文作者第二次全量（2026-10-30 21:28–21:34，红线修复后）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit -count=1 -json ./...` | **1** | `PASS_TESTS=26002 FAIL_TESTS=6 SKIP_TESTS=34` / `PKG_PASS=60 PKG_FAIL=2` |

只剩 2 个 FAIL 包，**红线已消失**：

```
internal/server      TestAPIContracts（/api/v1/admin/settings 2 子用例 + 父用例）  ← 此时 api-surface 正在修（Lead 已确认 21:35 绿）
internal/repository  TestPgDumper* ×3（已知环境性失败）
```

**对比本次与 §2.4b 的失败数**：17 → 6，且 `internal/handler`（红线）与 `internal/handler/admin`、`internal/server/routes`、`internal/service` 全部转绿。

### 2.4c 冻结快照最终全量（**定稿数字**）

工作树自 **21:35 冻结**（Lead 通知：任何人不再修改 `backend/` 与 `frontend/src/`）。本文作者在冻结后独立跑了一次全量，并在跑前跑后各取一次 `git status --porcelain` 做比对：

```
TREE_UNCHANGED_DURING_RUN=True      ← 跑测试期间工作树零变化，数字可信
```

#### A. 本文作者实跑（2026-10-30 21:37:58 – 21:45:05，HEAD=`f9d8bc855`）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit -count=1 -json ./...` | **1** | `PASS_TESTS=26004 FAIL_TESTS=4 SKIP_TESTS=34` / `PKG_PASS=60 PKG_FAIL=2` |

2 个 FAIL 包、4 条失败，**全部非本轮引入**：

```
internal/repository   TestPgDumperHoldsMigrationLockThroughReaderClose      ← 已知环境性（Windows 无 sh）
internal/repository   TestPgDumperReleasesMigrationLockWhenProcessFails     ← 同上
internal/repository   TestPgDumperReportsUnlockFailureAndDiscardsConnection ← 同上
internal/securityaudit TestOpenAICompatibleScannerRequestContract           ← 满载 flaky（见下）
```

`internal/securityaudit` 这条的取证（**已隔离验证**）：

```
失败原文（全量里）：
    prompt_outbound_security_test.go:65: Received unexpected error: prompt_guard_unavailable

隔离复跑（-count=3）：
--- PASS: TestOpenAICompatibleScannerRequestContract (0.01s)
--- PASS: TestOpenAICompatibleScannerRequestContract (0.00s)
--- PASS: TestOpenAICompatibleScannerRequestContract (0.00s)
ok  	github.com/Wei-Shaw/sub2api/internal/securityaudit	3.610s
EXIT=0
```

`git status --short backend/internal/securityaudit/` **为空** → 该包本轮**未被修改**；失败是满载下 httptest 服务端在 1000ms 超时内没响应导致的 `prompt_guard_unavailable`，隔离 3/3 通过 → **判定为满载 flaky，非本轮引入**。

#### B. Lead 实跑（21:38–21:45，同一冻结快照）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit -count=1 ./...` | **1** | 两类红，均非本轮引入 |

Lead 那次的两类红：

1. `internal/repository`（33.9s）：3 条 `TestPgDumper*` —— 本机 Windows 无 `sh`（既有）。
2. `internal/service`（330.5s）：`TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` —— **AGENTS.md 明文记录的已知 flaky**（满载偶发）。Lead 隔离跑 `-count=3` → **ok，EXIT=0**，证实是 flaky 不是回归。

其余**全部 ok**，包括此前红过的：`internal/handler`（含缺价红线端到端 **5/5**）、`internal/handler/admin`（含 `TestAllAdminUsageStatsEndpointsShareContract`）、`internal/server`（含 `TestAPIContracts`）、`internal/server/routes`、`internal/server/middleware`、`internal/middleware`、`internal/pkg/ip`。

#### C. 两次冻结全量的差异（如实并列，不择其一）

| 失败项 | 本文作者那次 | Lead 那次 | 归属 |
|---|---|---|---|
| `TestPgDumper*` ×3 | 红 | 红 | 环境性（Windows 无 `sh`），**稳定** |
| `TestOpenAICompatibleScannerRequestContract` | 红（隔离 3/3 绿） | 绿 | **满载 flaky**，非本轮 |
| `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` | 绿 | 红（隔离 3/3 绿） | **AGENTS.md 已知 flaky**，非本轮 |

**两条 flaky 各自只在其中一次出现、隔离都通过** —— 这本身就是 flaky 的典型特征（互相错开），也说明两次全量的「非环境性失败数」都是 **0**。

**定稿口径**：

> 冻结快照下，除 **3 条 `TestPgDumper*`（环境性）** 与 **2 条满载 flaky（`TestOpenAICompatibleScannerRequestContract`、`TestInflightEstimate_AccountMappingNoDBAndBoundedMemory`，均已隔离 `-count=3` 复跑通过）** 外，**零失败**。

### 2.4b 本文作者第一次全量（交叉验证，2026-10-30 20:56–21:10）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `go build -o bin/server.exe ./cmd/server` | **0** | 通过 |
| `go test -tags=unit -count=1 -json ./...` | **1** | `PASS_TESTS=26004 FAIL_TESTS=17 SKIP_TESTS=34` / `PKG_PASS=57 PKG_FAIL=5` |

5 个 FAIL 包 17 条失败（比 Lead 那次多 2 个包，因为跑的时刻更早、其他成员仍在改）：

```
internal/handler/admin   TestAllAdminUsageStatsEndpointsShareContract ×5（含父用例）
internal/handler         TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream ×6（含父用例）  ← 红线
internal/repository      TestPgDumper* ×3（已知环境性失败）
internal/server/routes   TestAuthLoginAccountFailureCounterIsWired（期望 401 实际 404）
internal/service         TestLogCodexCLIOnlyDetection_RejectedIncludesRequestDetails（§2.2b 已取证：本轮修复暴露的过时测试）
internal/service         TestLiveSettlementRetryReconcilesZeroCostUsageRow
```

**与 Lead 那次的差异**（如实记录，不掩饰）：

- `internal/server/routes`、`internal/service` 两个包在本文作者跑完之后被继续改动（`TestAuthLoginAccountFailureCounterIsWired` 是当时新落盘的用例；`TestLogCodexCLIOnlyDetection` 已由 sec-ip 按 §2.2b 的方式修好），所以 Lead 的最终那次这两个包已 ok。
- `internal/handler/admin` 在本文作者复跑时一度出现**测试编译失败**：`setting_smtp_configured_test.go:48:13: undefined: settingsAdminRepoStub`（21:12 那次）。**21:15 复跑 `go vet -tags=unit ./internal/handler/admin/` 已 exit 0**，说明是落盘中间态，已自行消失。这正是本轮多次出现的「多文件改动未一次落盘」中间态。

**两轮全量共同确认的结论**：`go build` = 0；`internal/pkg/ip`、`internal/middleware`、`internal/server/middleware`、`internal/server`、`internal/config`、`internal/service`（修复后）全部 ok；**唯一红线是 `internal/handler` 的缺价穿透**。

## 3. 前端

### 3.1 基线

| 命令 | 退出码 | 结果 |
|---|---|---|
| `.\node_modules\.bin\vue-tsc.cmd -b` | **0** | 无输出（通过） |
| `node node_modules/vitest/vitest.mjs run` | **0** | 见下 |

vitest 汇总行（原文）：

```
 Test Files  427 passed (427)
      Tests  3419 passed (3419)
   Start at  19:32:27
   Duration  139.70s (transform 117.47s, setup 156.86s, collect 368.84s, tests 116.13s, environment 1002.43s, prepare 151.74s)
```

（`KeysView.spec.ts` 运行期输出了 jsdom XHR `AggregateError` / `Failed to load HA groups` 的 stderr 噪声，但用例本身全部 passed、退出码 0，属既有测试的噪声输出。）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `corepack pnpm@9 run build` | **0** | `check:i18n` 3/3 通过 → `vue-tsc -b` → `vite build` `✓ 1147 modules transformed` / `✓ built in 1m 11s` |

```
> sub2api-frontend@1.0.0 build F:\sub2\frontend
> pnpm run check:i18n && vue-tsc -b && vite build

 ✓ src/i18n/__tests__/localeKeyCompleteness.spec.ts (3 tests) 51ms
 Test Files  1 passed (1)
      Tests  3 passed (3)

vite v5.4.21 building for production...
✓ 1147 modules transformed.
(!) Some chunks are larger than 500 kB after minification. ...
✓ built in 1m 11s
FEBUILD_EXIT=0
```

**构建过程的两点如实记录**：

1. **环境陷阱：直接 `corepack pnpm@9 run build` 会失败**（第 1 次实测 `FEBUILD_EXIT=1`）。

   `package.json` 的 build 脚本是 `pnpm run check:i18n && vue-tsc -b && vite build` —— 内层又调了一次 `pnpm`。而 PATH 上的 `pnpm` 是**全局 11.x**（`C:\Users\ASUS\AppData\Roaming\npm\pnpm.cmd`），它与上游锁文件不兼容，实测报：

   ```
   [WARN] The "pnpm" field in package.json is no longer read by pnpm. ...
   [ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY] Aborted removal of modules directory due to no TTY
   [ERROR] Command failed with exit code 1: "...\npm\node_modules\pnpm\bin\pnpm.mjs" install
   FEBUILD_EXIT=1
   ```

   **正确做法（可复现）**：在 PATH **最前面**插入一个只含 pnpm9 的 shim 目录，让内层 `pnpm` 也解析到 9.15.9。

   ```powershell
   # 1) 建 shim 目录（放临时目录，不要进仓库）
   $shim = "$env:TEMP\verify-sec\pnpmshim"
   New-Item -ItemType Directory -Force -Path $shim | Out-Null
   # 2) shim 文件内容（就两行）
   #    @echo off
   #    corepack pnpm@9 %*
   '@echo off
   corepack pnpm@9 %*
   '@ | Out-File "$shim\pnpm.cmd" -Encoding ascii
   # 3) 前置到 PATH 最前（关键：必须在全局 pnpm 11 之前）
   $env:PATH = "$shim;$env:PATH"
   # 4) 再跑构建
   cd F:\sub2\frontend
   corepack pnpm@9 run build      # → exit 0
   ```

   shim 自检：`& "$shim\pnpm.cmd" --version` → `9.15.9`。该 shim 只在 `%TEMP%` 下，**不入仓库、不进任何提交**。
2. `vite.config.ts:107` `outDir: '../backend/internal/web/dist'`，`emptyOutDir: true`。因此这次构建**就地重建了 `backend/internal/web/dist/`**（19:43:04，此前是 2026-10-21 的陈旧产物）。这是验证的副作用，不是源码改动（该目录被 `.gitignore`）。

### 3.2 最终全量

#### 3.2a 冻结快照（2026-10-30 21:37:58 起，**定稿数字**）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `.\node_modules\.bin\vue-tsc.cmd -b` | **0** | 无输出（通过），21:37:58 起跑 |
| `node node_modules/vitest/vitest.mjs run` | **0** | 见下 |

```
 Test Files  435 passed (435)
      Tests  3456 passed (3456)
   Start at  21:39:40
   Duration  169.83s (transform 170.81s, setup 212.33s, collect 611.89s, tests 113.97s, environment 962.72s, prepare 183.54s)

VITEST_EXIT=0
```

Lead 在同一冻结快照上实跑的结果**完全一致**（`Test Files 435 passed (435)` / `Tests 3456 passed (3456)` / `EXIT=0`），frontend 队友报 `vue-tsc -b` EXIT=0、`check:i18n` 3 passed。三方一致。

#### 3.2b 本文作者第二次全量（2026-10-30 20:56–21:02，冻结前，作过程记录）

| 命令 | 退出码 | 结果 |
|---|---|---|
| `.\node_modules\.bin\vue-tsc.cmd -b` | **0** | 无输出（通过） |
| `node node_modules/vitest/vitest.mjs run` | **0** | 见下 |
| `corepack pnpm@9 run build` | 见 §3.1（基线同源构建；最终阶段未再重跑，原因见下） | — |

vitest 汇总行（原文，2026-10-30 20:58 起跑）：

```
 Test Files  435 passed (435)
      Tests  3456 passed (3456)
   Start at  20:58:21
   Duration  266.84s (transform 341.53s, setup 288.61s, collect 1559.20s, tests 306.14s, environment 820.91s, prepare 207.60s)

VITEST_EXIT=0
```

**与基线对比**：`427 files / 3419 tests` → `435 files / 3456 tests`，**新增 8 个 spec 文件 / 37 条用例**，全部通过，零失败。

新增的 8 个 spec（均为本轮修复配套）：

```
frontend/src/components/admin/settings/__tests__/SmtpNotConfiguredNotice.spec.ts
frontend/src/components/common/__tests__/UsageStatsIncompleteNotice.spec.ts
frontend/src/components/user/__tests__/GatewayFailureBadge.spec.ts
frontend/src/components/user/__tests__/gatewayFailure.spec.ts
frontend/src/components/user/__tests__/UserErrorRequestsTable.gatewayFailure.spec.ts
frontend/src/utils/__tests__/smtp.spec.ts
frontend/src/utils/__tests__/usageStatsAvailability.spec.ts
（+ src/api/__tests__/client.spec.ts 等既有文件的用例增补）
```

**关于 `corepack pnpm@9 run build` 的如实说明**：

- **基线阶段跑过并成功**（§3.1，exit 0，`check:i18n` 3/3 → `vue-tsc -b` → `vite build` `✓ built in 1m 11s`），证明**构建链路本身可跑通**。
- **冻结快照阶段没有重跑 `pnpm build`**。原因：`vite.config.ts:107` 的 `outDir` 直指 `backend/internal/web/dist/`（`emptyOutDir: true`），重跑会就地重建该目录；而该目录是部署一致性判断（§5）的输入，重跑会改变别人正在核对的 dist hash。基线那次重建已造成过一次 DRIFT（§5.1）。
- 因此，**「最终前端代码能否成功构建」在本文中属于「未跑过」**，只有「最终前端代码能通过 `vue-tsc -b` 与 `vitest run`」是实测的。**不虚报为通过。**

## 4. 变异验证

### 4.0 方法

- **不碰他人工作树**：把 `F:\sub2\backend` 完整复制到独立目录 `F:\mut-verify-verifier\backend`（`robocopy /E /XD bin .git`），所有回退/恢复都在副本内进行。`F:\sub2` 全程只读（除了 `VERIFICATION.md` 本身与 §3.1 那次 build 产出的 `backend/internal/web/dist/`）。
- 每次变异：`Copy-Item` 备份原文件 → 定点替换锚点 → 跑目标包测试 → `Copy-Item` 还原 → 断言锚点已还原（输出 `restored=True`）。
- 变异运行环境与 §1 完全一致（GOROOT 1.27.3 / GOTOOLCHAIN=local）。
- 副本更新时间戳：2026-10-30 19:47 前后（此时工作树已落盘 ip.go / ip_test.go / auth_service.go / domain_constants.go / setting_public.go / http.go / Dockerfile 等，**其余队友的改动尚未落盘**）。

### 4.1 已完成的变异

| # | 修复点 | 回退了什么 | 命令 | 变红的测试 | 退出码 | 恢复后 |
|---|---|---|---|---|---|---|
| M1 | F-01 转发头信任方向 | `pkg/ip/ip.go` `GetClientIP` 里的 `if !isTrustedDirectPeer(peer) { return peer }` 守卫改成 `if false { ... }`（等价于回到「头里的值非私网即采信」） | `go test -tags=unit ./internal/pkg/ip/ -v` | `TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPeer`<br>`TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPrivatePeer`<br>`TestGetSecurityClientIPRotatingForwardedHeadersCannotChangeIdentity`<br>`TestGetSecurityClientIPForwardedHeaderCounterExamples`（含 6 个子用例：IPv4-mapped IPv6、带端口、逗号链、重复头、小写头名、admin 自定义头） | **1**（FAIL） | 已还原，`restored=True`；后续 M2 复跑同包通过 |
| M2 | F-03 `sub2api.local` 整域保留 | `service/auth_service.go` `isReservedEmail` 里 `isSub2APIInternalEmail(normalized)` 替换为 `false` | `go test -tags=unit ./internal/service/ -run 'TestIsReservedEmail|TestAuthService_Register_RejectsSub2APIInternalDomain' -v` | `TestIsReservedEmail_Sub2APIInternalDomain`<br>`TestAuthService_Register_RejectsSub2APIInternalDomain` | **1**（FAIL） | 已还原，`restored=True` |

M1 原始输出（节选）：

```
mutated: guard disabled

--- FAIL: TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPeer (0.00s)
--- FAIL: TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPrivatePeer (0.00s)
--- FAIL: TestGetSecurityClientIPRotatingForwardedHeadersCannotChangeIdentity (0.00s)
--- FAIL: TestGetSecurityClientIPForwardedHeaderCounterExamples (0.00s)
    --- FAIL: TestGetSecurityClientIPForwardedHeaderCounterExamples/untrusted_peer_with_IPv4-mapped_IPv6_header_value
    --- FAIL: .../untrusted_peer_with_port_in_header_value
    --- FAIL: .../untrusted_peer_with_comma_chain
    --- FAIL: .../untrusted_peer_with_duplicated_X-Real-IP_headers
    --- FAIL: .../untrusted_peer_with_lower-case_header_name
    --- FAIL: .../untrusted_peer_with_admin-configured_custom_header
FAIL	github.com/Wei-Shaw/sub2api/internal/pkg/ip	5.573s
GO_TEST_EXIT=1
restored; originalGuardPresent=True
```

M2 原始输出（节选）：

```
=== RUN   TestIsReservedEmail_Sub2APIInternalDomain
--- FAIL: TestIsReservedEmail_Sub2APIInternalDomain (0.00s)
=== RUN   TestIsReservedEmail_Sub2APILookalikesAreAllowed
--- PASS: TestIsReservedEmail_Sub2APILookalikesAreAllowed (0.00s)
=== RUN   TestAuthService_Register_RejectsSub2APIInternalDomain
--- FAIL: TestAuthService_Register_RejectsSub2APIInternalDomain (0.15s)
--- PASS: TestIsReservedEmail_DingTalkDomain (0.00s)
GO_TEST_EXIT=1
restored=True
```

注：M2 里 `TestIsReservedEmail_Sub2APILookalikesAreAllowed` 仍然 PASS 是**正确**的——这条用例锁定的是「不得误伤形似域名」，与「整域保留」互为反向约束，禁用保留域后它本来就该继续通过。

| M3 | F-01 账号维度限流（`RateLimitOptions.AccountKey`） | `middleware/rate_limiter.go` `rateLimitDimensions` 里 `dimensions = append(dimensions, key+":acct:"+account)` 改成 `_ = account`（等价于只有 IP 维度，回到「轮换 IP 即可绕过」） | `go test -tags=unit ./internal/middleware/ -run 'TestRateLimiterAccountKey|TestRateLimiterNeverAllowsRequestWithZeroDimensions' -v` | `TestRateLimiterAccountKeyBlocksIPRotation`<br>`TestRateLimiterAccountKeyIsolatesAccounts` | **1**（FAIL） | 已还原，`restored=True` |

M3 原始输出：

```
mutated: account dimension disabled

=== RUN   TestRateLimiterAccountKeyBlocksIPRotation
--- FAIL: TestRateLimiterAccountKeyBlocksIPRotation (0.00s)
=== RUN   TestRateLimiterAccountKeyIsolatesAccounts
--- FAIL: TestRateLimiterAccountKeyIsolatesAccounts (0.00s)
=== RUN   TestRateLimiterAccountKeyPreservesRequestBody
--- PASS: TestRateLimiterAccountKeyPreservesRequestBody (0.00s)
=== RUN   TestRateLimiterNeverAllowsRequestWithZeroDimensions
--- PASS: TestRateLimiterNeverAllowsRequestWithZeroDimensions (1.83s)
FAIL	github.com/Wei-Shaw/sub2api/internal/middleware	7.706s
GO_TEST_EXIT=1
restored=True
```

注：M3 里 `TestRateLimiterAccountKeyPreservesRequestBody` 与 `TestRateLimiterNeverAllowsRequestWithZeroDimensions` 仍 PASS 是**正确**的——前者锁的是「读体后回填，下游仍能读」，后者锁的是「不能出现零维度放行」，二者都只依赖 AccountKey 回调本身、不依赖账号桶是否真的开出来，所以禁用账号维度不影响它们。这也说明这两个测试**没有**把「账号维度生效」这件事固定住，真正锁住 F-01 的是前两条。

### 4.2 尚未做的变异（如实标注）

| 修复点 | 状态 | 原因 |
|---|---|---|
| 账号维度限流（`RateLimitOptions.AccountKey`） | **未做变异验证** | 截至本记录时刻 `backend/internal/middleware/rate_limiter.go` 与 `routes/auth.go` 尚未落盘（`git status` 无该文件），无修复可回退 |
| `/redeem` 独立限流 | **未做变异验证（本文作者）** | 实现见 `routes/user.go` 的 `panelRateLimiter.RedeemGuard()`（5/min per user + 10 次失败/10min 收紧），用例 `TestRedeemRouteRateLimitPerUser`、`TestRedeemRouteRateLimitedBeforeHandlerRuns`。按 Lead 20:47 的分工，变异由 **reviewer** 负责，本文不重复；reviewer 结论尚未落入本文。 |
| `/usage/:id` 同码同体（F-07） | **未做变异验证** | 用例 `handler/usage_get_by_id_ownership_test.go` 已落盘；变异由 reviewer 负责 |
| 未知模型 404 / 未定价 503（F-06/N-01） | **未做变异验证** | 用例 `handler/gateway_pricing_error_message_test.go` 已落盘；**且该项当前处于红线回归状态（见 §2.4）**，即便有变异验证也不能算通过 |
| N-02 统计未就绪契约 | **未做变异验证** | 用例 `handler/admin/dashboard_usage_stats_unavailable_test.go` 等已落盘；变异由 reviewer 负责 |

**本文作者未参与其余变异的原因（Lead 20:47 分工指令原文要点）**：「reviewer 已完成 ①IsTrustedDirectPeer 恒 true 全红、并已解除阻塞开始 ②③④。你不要与 reviewer 重复做同一批变异——你专注『最终全量数字 + 文档』」。因此 M1–M3 之后的变异由 reviewer 执行，其结论应并入本文后续版本。

## 5. 部署一致性

脚本：`F:\sub2\tools\verify-deployed-build.ps1`（由 deploy-ops 于本轮新增，未跟踪文件）。
执行：`pwsh -NoProfile -File .\tools\verify-deployed-build.ps1`（默认 `-BaseUrl https://sub2.lansn.icu`），采集时间 **2026-10-30 19:45 前后**，仓库 HEAD=`f9d8bc855`。

**退出码 1（FAIL）**，完整输出：

```
================ 部署一致性校验 ================
站点          : https://sub2.lansn.icu
仓库          : F:\sub2

--- 线上后端 ---
  /api/v1/settings/public version : 0.2.17-fork.f9d8bc855
  从中解析出的提交                : f9d8bc855
--- 线上前端 ---
  首页入口脚本                    : index-DEkhF0Ff.js
  HTML 内联 __APP_CONFIG__.version: <空串>
--- 本地 ---
  git HEAD                        : f9d8bc855e9e (2026-10-29T22:50:00+08:00)
  dist/index.html                 : F:\sub2\backend\internal\web\dist\index.html
  dist 入口脚本                   : index-BbceXYHS.js
  dist 生成时间                   : 2026-10-30 19:43:04
  前端源码最后提交                : 2026-10-29 22:50:00

[OK]   线上后端 : 0.2.17-fork.f9d8bc855 == 本地 HEAD f9d8bc855
[DRIFT] 线上前端 : index-DEkhF0Ff.js != 本地 dist index-BbceXYHS.js（本地 dist 已重建但未部署，或线上是另一次构建）
[OK]   本地 HEAD : f9d8bc855e9ef2a5d60974e15572880559a878cf
       HTML 内联 version 为空串（前端首屏拿不到版本，属已知缺陷）

RESULT: FAIL（见上方 [DRIFT] 行）
VERIFY_DEPLOY_EXIT=1
```

### 5.1 结论与归因（本机实测，不推断）

1. 三行结论结构、解析逻辑、退出码语义（0/1/2）都按设计工作：线上后端 version 解析出 `f9d8bc855` 并与本地 HEAD 判定一致；HTML 内联 `__APP_CONFIG__.version` 空串这一已知缺陷被正确指出（对应 D-02）；判定失败仅来自「本地 dist 入口脚本 ≠ 线上入口脚本」。
2. 该 DRIFT 由**本次验证自身的副作用**造成：§3.1 的 `corepack pnpm@9 run build` 把 `backend/internal/web/dist/` 从 10-21 的陈旧产物重建为 19:43:04 的新产物（vite `outDir` 直指该目录，`emptyOutDir: true`）。也就是说 §5 这次执行是「本地已重建、线上仍是旧构建」的正常表现，**不是部署漂移**。
3. 若要得到 PASS 的端到端演示，需要「本地 dist 与线上同一构建」或「线上部署本机这次构建」——本任务不部署、不动线上，故未做。

### 5.2 判据强度：必须分成两级（重要，脚本当前把它们混在一行）

**实测反例**：同一提交 `f9d8bc855`，前端工作树**无任何改动**，本机重建的入口脚本是 `index-BbceXYHS.js`，而线上是 `index-DEkhF0Ff.js` —— **两者不等**。所以「入口脚本 hash 相等」不能用来断定「线上构建自同一提交」。

| 级别 | 判据 | 能证明什么 | 不能证明什么 | 本次结果 |
|---|---|---|---|---|
| **强判据** | `/api/v1/settings/public` 的 `data.version` 里解析出的 commit **==** 本地 `git rev-parse HEAD` | 线上**后端进程**确实由该提交构建（版本串由构建期 ldflags 注入 `main.Version`，无法从源码伪造） | 不含前端任何信息 | **成立**：`0.2.17-fork.f9d8bc855` == `f9d8bc855e9ef2a5d60974e15572880559a878cf` |
| **弱判据** | 线上首页 `/assets/index-*.js` **==** 本地 `dist/index.html` 里的入口脚本 | 线上前端**就是本机这次构建的产物**（同一提交 + 同一工具链 + 刚构建） | 线上是否由同一提交构建；本机 dist 是否刷新过 | **不成立**（且本机 dist 是本轮刚重建的，天然不可能与线上相等） |

**给 deploy-ops 的脚本改进建议（口径层面）**：

- 强判据单独决定「后端是否一致」，**只有它决定退出码**；
- 弱判据只能作为**提示**，且必须先减去「本地 dist 是否刚被重建过」这个前提：脚本已算出 `dist 生成时间` 与 `前端源码最后提交`，可以在 dist 比「前端源码最后提交」新时把弱判据降级为 `[INFO]` 而不是 `[DRIFT]`；
- 若确实需要「线上前端构建自某提交」的证据，得用**产物串检索**（在线上 `/assets/*` 里找该提交独有的标识符），也就是 `EVIDENCE.md` §1.2 已经用过的方法，而不是比对 hash。

以上为口径建议，**脚本本身我没有改**（不在我的写域内）。

## 6. 线上回归

**未做**。截至本文定稿，本文作者**没有收到 Lead「已部署」的通知**，因此 §6 要求的六项线上回归（轮换 `X-Real-IP` 25 次的状态序列、`/redeem` 第 6 次 429、`/usage/:id` 三态、`admin@sub2api.local` 注册被拒、未知模型 404 / 未定价 503、SMTP 未配置时发码报错）**一条都没有跑过**。

线上只做过一次**只读**的部署一致性校验（§5，`GET /api/v1/settings/public` + `GET /`），以及 `EVIDENCE.md` 里 Lead 在**修复前**做的复现。修复后的线上行为需要等部署后再验。

## 7. 缺陷 → 测试 → 变异验证（三列表）

**口径说明**：`实测` 一栏只写我在**本机实际跑过并看到结果**的内容；`未做` / `未落盘` 一律照实写，不推断。

| 缺陷 | 新增/修改的测试 | 变异验证 |
|---|---|---|
| **F-01** 转发头信任方向反转，IP 限流可被伪造头绕过 | `backend/internal/pkg/ip/ip_test.go`：`TestDefaultTrustedProxyPolicyOnlyTrustsLoopback`、`TestSetTrustedProxyPolicy`、`TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPeer`、`TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPrivatePeer`、`TestGetSecurityClientIPRotatingForwardedHeadersCannotChangeIdentity`、`TestGetSecurityClientIPHonorsForwardedHeadersFromLoopbackPeer`、`TestGetSecurityClientIPUsesHeadersFromConfiguredTrustedPeer`、`TestGetSecurityClientIPForwardedHeaderCounterExamples`（6 子用例）、`TestIsTrustedDirectPeerUsesNetworkSemantics`、`TestGetClientIPNeverReturnsEmptyForUnparseableRemoteAddr`<br>`backend/internal/server/client_ip_trust_test.go`（新文件）：`TestConfigureClientIPTrustWiresTrustedProxyPolicy`、`TestShouldWarnClientIPCollapse` | **已做（M1）**：回退 `ip.go` 的 `!isTrustedDirectPeer` 闸门 → 4 条用例 + 6 子用例变红，exit 1，已还原 |
| **F-01** 账号维度限流（即使 IP 坍缩/被伪造仍生效） | `backend/internal/middleware/rate_limiter_test.go`：`TestRateLimiterAccountKeyBlocksIPRotation`、`TestRateLimiterAccountKeyIsolatesAccounts`、`TestAccountKeyFromJSONBodyFieldHashesEmail`、`TestAccountKeyFromJSONBodyFieldRejectsNonStringOrMissing`、`TestRateLimiterAccountKeyPreservesRequestBody`、`TestRateLimiterSkipPrivateClientIP`、`TestRateLimiterIgnoresForwardedIPFromUntrustedPeer`、`TestAccountKeyFromJSONBodyFieldAppliesNormalizer`、`TestAccountKeyFromJSONBodyFieldSkipsOversizedBody`、`TestRateLimiterNeverAllowsRequestWithZeroDimensions`<br>`backend/internal/server/routes/auth_account_rate_limit_test.go`（新文件）：`TestAuthLoginRateLimitCannotBeBypassedByRotatingForwardedHeader`、`TestAuthLoginAccountDimensionBlocksDistributedAttempts`、`TestAuthLoginAccountDimensionFoldsAliases`、`TestAuthAccountRateLimitsAreLooserThanIPRateLimits`、`TestAuthAccountLimitConfigIsWired` | **已做（M3）**：回退 `rateLimitDimensions` 里的账号维度追加 → `TestRateLimiterAccountKeyBlocksIPRotation`、`TestRateLimiterAccountKeyIsolatesAccounts` 变红，exit 1，已还原。**routes 层的 5 条用例未做变异验证** |
| **F-02** `/redeem` 无独立限流 | **未落盘**（`backend/internal/server/routes/user.go` 在本文定稿时`git status` 无改动） | **未做** |
| **F-03** `admin@sub2api.local` 等保留标识可注册 | `backend/internal/service/auth_reserved_email_test.go`（新文件）：`TestIsReservedEmail_Sub2APIInternalDomain`、`TestIsReservedEmail_Sub2APILookalikesAreAllowed`、`TestAuthService_Register_RejectsSub2APIInternalDomain`、`TestAuthService_SendVerifyCode_RejectsSub2APIInternalDomain` | **已做（M2）**：回退 `isSub2APIInternalEmail` 调用 → 2 条变红，exit 1，已还原 |
| **F-04** `/channel-monitor-v2/*` 对普通用户泄露配置 | **未落盘** | **未做** |
| **F-05** `/groups/available` 泄露运营字段 | **部分落盘**（`handler/admin/group_handler.go` 已改，`handler/dto/types.go` 的用户面向映射在本文定稿时未见） | **未做** |
| **F-06 / N-01** 未知模型 404 / 未定价 503 区分 | 前端口径测试已落盘：`frontend/src/components/user/__tests__/gatewayFailure.spec.ts`（7 条）、`GatewayFailureBadge.spec.ts`（4 条）、`UserErrorRequestsTable.gatewayFailure.spec.ts`（4 条）<br>后端：**F-06 本轮不交付**（`refinePricingRejection` 因红线已被整体移除） | **已做（reviewer）**：把 `refinePricingRejection` 重新加回（一行注入）→ 既有端到端测试 `TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream` **5 子用例全红**。结论：**既有端到端测试本身就是下一轮 F-06 的 mandatory 护栏，不需要新写** |
| **F-07** `/usage/:id` 403/404 差异泄露存在性 | **未落盘** | **未做** |
| **N-02** dashboard 统计参数越界返回 500 / 统计未完成时静默空数组 | `backend/internal/handler/usage_stats_unavailable_test.go`：`TestUserUsageStatsUnavailableMatchesAdminContract`<br>`backend/internal/handler/admin/dashboard_usage_stats_unavailable_test.go`：`TestUsageStatsIncompleteContractIsSingleSource`、`TestAllAdminUsageStatsEndpointsShareContract`、`TestSnapshotV2KeepsDegradedContract`、`TestAdminUsageStatsRealFailureStays500`<br>前端：`frontend/src/utils/__tests__/usageStatsAvailability.spec.ts`、`frontend/src/components/common/__tests__/UsageStatsIncompleteNotice.spec.ts` | **未做** |
| **I-01** `send-verify-code` 假成功 | **未落盘**（`service/email_service.go` 无改动）；前端 `frontend/src/utils/__tests__/smtp.spec.ts` 已落盘（4 条） | **未做** |
| **I-02** `/setup/status` 硬编码 | **未落盘** | **未做** |
| **D-02** `__APP_CONFIG__.version` 空串 | 无自动化测试；由 `tools/verify-deployed-build.ps1` 检测（§5 实测已报出「HTML 内联 version 为空串」） | **不适用**（运维脚本，非缺陷修复） |

### 7.0 最终阶段新增/变化的测试（第二次落盘后补记）

在前文 §7 表格之后，其余成员又落盘了以下测试文件/用例（已并入最终全量的判定）：

| 缺陷 | 测试 | 变异验证 |
|---|---|---|
| **F-02** `/redeem` 限流 | `backend/internal/server/routes/user_redeem_rate_limit_test.go`：`TestRedeemRouteRateLimitPerUser`、`TestRedeemRouteRateLimitedBeforeHandlerRuns` | **未做**（分工归 reviewer） |
| **F-01** 账号维度（路由层补强） | `backend/internal/server/routes/auth_account_rate_limit_test.go` 追加：`TestAuthLoginAccountFailureCounterIsWired` | **未做** |
| **F-07** `/usage/:id` 同码同体 | `backend/internal/handler/usage_get_by_id_ownership_test.go` | **未做**（分工归 reviewer） |
| **F-06 / N-01** 未知模型 404 与未定价 503 文案 | `backend/internal/handler/gateway_pricing_error_message_test.go` | **未做**；且该项**当前红线回归**（§2.4） |
| **I-01** SMTP 未配置暴露 | `backend/internal/handler/admin/setting_smtp_configured_test.go` | **未做** |
| **N-02**（用户端与管理端同契约） | `backend/internal/handler/usage_stats_unavailable_test.go`、`handler/admin/dashboard_usage_stats_unavailable_test.go`（4 条） | **未做** |

### 7.1 汇总

| 统计口径 | 数量 |
|---|---|
| 本文作者亲自做过变异验证的修复点 | **3**（F-01 转发头信任 M1、F-01 账号维度 M3、F-03 保留邮箱 M2） |
| 明确「未做变异验证」的修复点（本文范围） | F-02、F-04、F-05、F-06/N-01、F-07、N-02、I-01、I-02 |
| 其中因「尚未落盘」而没有修复可回退的 | F-02、F-04、F-06/N-01、F-07、I-01、I-02（在本文第一次定稿时刻） |
| 其中已落盘但按 Lead 分工交由 reviewer 变异的 | F-02、F-07、N-02、F-01 路由层补强 |
| **合计（不虚报）** | **8 个缺陷中只有 2 个（F-01、F-03）有本文作者的变异证据**；F-01 内部分两条（信任方向 + 账号维度），故计 3 条变异 |

## 7.2 本轮工作区纪律实测记录（可复核的中间态）

本轮 8 个成员并行改动同一工作树，本文作者在验证过程中**实际观察到 4 类中间态**，逐条列出（每条都有时间戳与原始报错）：

| # | 时间 | 现象 | 是否稳定缺陷 | 处理 |
|---|---|---|---|---|
| 1 | 20:00 interim 全量 | `pkg/ip`、`server/middleware`、`service` 共 9 条失败 | 否。20:06–20:11 复跑同包全部 `ok` | 等 1–2 分钟后重跑 |
| 2 | 21:12 复跑 `handler/admin` | `setting_smtp_configured_test.go:48:13: undefined: settingsAdminRepoStub`，包级 **build failed** | 否。21:15 `go vet -tags=unit ./internal/handler/admin/` **exit 0** | 等落盘完成后重跑 |
| 3 | 21:22 前的多次 `go test` | `internal/server/routes` 3 条红（账号维度 / redeem ×2） | 否。21:22 目标包复跑 **`ok ... 15.197s`** | 等落盘 |
| 4 | 20:52 | `git status` 显示 `routes/user.go` 尚无 `RedeemGuard` | 是「尚未落盘」而非缺陷 | 记录时刻，后续再验 |

**结论**：本轮的中间态全部是「多文件/跨包改动未一次落盘」造成，没有一次是真实回归（除红线那条）。这也是 AGENTS.md 与 Lead 广播里反复强调「多文件改动必须一次性写完再落盘」的实证依据。

## 8. 未验证项与原因

| 项 | 状态 | 原因（不推断，照实写） |
|---|---|---|
| 集成测试 `go test -tags=integration ./...` | **未跑过** | 需 PostgreSQL + Redis 实例；本机未部署。任务范围只要求 unit |
| §6 全部线上回归 6 项 | **未跑过** | 未收到 Lead「已部署」通知（工作树自 21:35 冻结，本轮未部署） |
| 冻结快照的 `corepack pnpm@9 run build` | **未跑过** | 见 §3.2：`vite outDir` 直指 `backend/internal/web/dist/`，重跑会改变部署一致性判断的输入。基线阶段已实测 exit 0（构建链路可用） |
| 变异验证：F-02 / F-04 / F-05 / F-06·N-01 / F-07 / N-02 / I-01 / I-02 | **本文未做** | 见 §4.2。F-02、F-07、N-02 已落盘但按 Lead 分工交 reviewer；F-04、F-05、I-01、I-02 在本文成稿时未取到对应测试文件。**F-06 已由 reviewer 做过一行注入变异（5 子用例全红），见 §2.4 红线小节** |
| Playwright E2E（用户端 9 页 + 管理端 7 页） | **未跑**（本文作者） | 属 frontend 成员 task-3/task-10 的交付物，不在本任务写域与执行范围；产物见同目录 `E2E-REPORT.md`（本文作者未复核其内容） |
| 线上受控探测（兑换幂等、并发扣费） | **未复核** | qa-live 的产物在 `.deployprobe/qa/`，属 task-5；本文作者**未读取其内容**，不据此下任何结论 |
| Docker 镜像构建 / 容器部署 | **未跑过** | 本机无 Docker（AGENTS.md 已注明），deploy 改动只做静态检查 |
| `golangci-lint` / `govulncheck` / `pnpm audit` | **未跑过** | 属 CI 作业，不在本任务要求的验收命令清单内 |
| `TestAPIContracts` 冻结前那次红 | 已消失 | 21:28 全量里红，21:37 冻结快照里绿（Lead 确认 21:35 已修）。中间态，非稳定缺陷 |
| 并发/竞态、SSRF、兑换码熵值 | **未跑过** | 与 EVIDENCE.md 第5节 相同原因。qa-live 在线上做过受控兑换探测（.deployprobe/qa/14-redeem-idempotency.json 等），但那是 task-5 的产物，**本文作者未复核其内容，也不据此下结论** |
| 变异验证：reviewer 负责的部分 | **未并入本文** | Lead 20:47 分工：reviewer 执行、本文不重复。F-06 那条已由 Lead 转述并入 2.4 节；其余（账号锁死、/redeem、/usage/:id、N-02）**本文无 reviewer 原始产物，故不声称** |

## 9. 可复核性清单（供他人重跑）

```powershell
# 0) 环境
$env:GOROOT='F:\tools\go1.27.3\go'; $env:GOPATH='F:\tools\gopath'
$env:GOTOOLCHAIN='local'; $env:PATH="$env:GOROOT\bin;$env:PATH"

# 1) 后端
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server   # 期望 exit 0
go test -tags=unit -count=1 ./...         # 期望仅 3 条 TestPgDumper* 红（Windows 无 sh）

# 2) 前端
cd F:\sub2\frontend
.\node_modules\.bin\vue-tsc.cmd -b         # 期望 exit 0
node node_modules/vitest/vitest.mjs run    # 期望 435 files / 3456 tests 全过

# 3) 前端构建（会重建 backend/internal/web/dist/，跑之前想清楚）
$shim = "$env:TEMP\pnpmshim"; New-Item -ItemType Directory -Force -Path $shim | Out-Null
# shim 文件 pnpm.cmd 内容两行： @echo off / corepack pnpm@9 %*
$env:PATH = "$shim;$env:PATH"
corepack pnpm@9 run build                  # 期望 exit 0（含 check:i18n）

# 4) 部署一致性
cd F:\sub2
pwsh -NoProfile -File .\tools\verify-deployed-build.ps1

# 5) 变异验证（在独立副本里做，不要污染主工作区）
robocopy F:\sub2\backend F:\mut-verify-<name>\backend /E /XD bin .git /XF *.exe
```

**红线复验（最重要的一条）**：

```powershell
cd F:\sub2\backend
go test -tags=unit ./internal/handler/ -run 'TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream' -count=1 -v
# 期望：5 个子用例全 PASS；若出现 502 upstream_error 说明缺价请求又穿透到上游
```
