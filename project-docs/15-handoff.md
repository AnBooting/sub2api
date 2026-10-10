# 交接文档（给新会话）

> **读这一份就够上手。** 最后更新：2026-10-10
> 当前阶段：**P1 需求澄清进行中**——已确认决策见 [06-requirements-backlog.md](06-requirements-backlog.md) REQ-009~014；剩余问题已整理为团队问卷 [16-requirements-questionnaire.md](16-requirements-questionnaire.md)。未写任何业务代码。

---

## 0. 一句话说明项目

在团队账号 **AnBooting/sub2api**（= 上游 Wei-Shaw/sub2api v0.2.15 + 2 个魔改提交）基础上继续二开，
实现 6 项需求，最终用 Docker 部署到一台真实服务器。仓库在 `F:\sub2`。

---

## 1. 五分钟上手（按顺序读）

| 顺序 | 文档 | 作用 |
|---|---|---|
| 1 | `F:\sub2\AGENTS.md` | **项目规则与红线**（会自动加载） |
| 2 | [13-requirements-mapping.md](13-requirements-mapping.md) | **6 项需求 × 现有代码的映射**（最重要） |
| 3 | [architecture/README.md](architecture/README.md) | 架构侦察结论速览 |
| 4 | [architecture/diagrams/00-architecture.html](architecture/diagrams/00-architecture.html) | 交互式架构图（浏览器打开） |
| 5 | 需要细节时 → `architecture/01`~`05` | 五份深度侦察（各 300–550 行） |

---

## 2. 当前状态

### 已完成并验证

| 项 | 证据 |
|---|---|
| Git 基线 | `main` 跟踪 `origin/main`（`0e6f5efb`）；`develop` 在其上 3 个提交 |
| 标签 | `baseline-fork-2026-10-09` = `0e6f5efb`；`baseline-upstream-v0.2.15` = `3a6fd1c9` |
| 移植包恢复 | 曾被误删，已从回收站恢复，**SHA256 89/89 通过**，在 `_reference/domestic-provider-kit/` |
| Go 工具链 | `F:\tools\go1.27.2\go`（便携版，环境变量已持久化） |
| 后端构建 | `go build ./cmd/server` ✅ 退出码 0，产物 153.3 MB |
| 后端单测 | `go test -tags=unit ./...` 运行完成，**5 个失败为基线既有**（见 §7） |
| 前端构建 | `vue-tsc --noEmit` ✅、i18n 检查 ✅、`vite build` ✅ |
| 架构图 | archify **9/9 校验 + 浏览器视觉检查通过** |
| 架构侦察 | 5 份文档，**源码零改动**（`git diff HEAD -- backend frontend deploy` 为空） |
| 代码审查工具 | `ocr`（阿里 OpenCodeReview v1.12.13，SHA256 已核对） |

### 未完成

| 项 | 阻塞原因 |
|---|---|
| Docker 本地验证 | **Docker Desktop 未安装**（安装包在 `F:\tools\downloads\DockerDesktopInstaller.exe`，需你手动装） |
| 推送到团队仓库 | ✅ 已就绪（`yylovehhy` = maintain 协作者，push dry-run 通过）。**`develop` 尚未推送** |
| 全部业务需求 | **等需求澄清 + PRD 评审** |

---

## 3. 环境（照抄，别重新探索）

### 工具链

| 工具 | 位置 | 注意 |
|---|---|---|
| Go | `F:\tools\go1.27.2\go\bin\go.exe` | `GOROOT`/`GOPATH`/`GOTOOLCHAIN=local` 已持久化 |
| pnpm | **必须** `corepack pnpm@9` | 全局 pnpm 是 11.x，**与锁文件不兼容** |
| ocr | `F:\tools\ocr\ocr.exe` | 阿里 OpenCodeReview |
| Docker | ⏳ 待安装 | |

### 命令

```powershell
# 后端
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server
go test -tags=unit ./...
go generate ./ent                    # 改 ent schema 后必须执行

# 前端（三步分开跑，别用 pnpm run build）
cd F:\sub2\frontend
corepack pnpm@9 exec vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts
corepack pnpm@9 exec vue-tsc -b
corepack pnpm@9 exec vite build
```

### ⚠️ 三个已踩过的坑

1. **`corepack pnpm@9 run build` 会失败** —— `package.json` 的 build 脚本内部再调裸 `pnpm`，
   解析到全局 11.x，报 `ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY`。**手动跑三步。**
2. **GitHub 直连不稳定** —— 仓库已配 `http.proxy=http://127.0.0.1:7877`（Clash）。
3. **npm 官方源慢（10–26 s/请求）** —— 加 `--registry=https://registry.npmmirror.com`。

---

## 4. Git 结构（最终）

```
upstream/main  Wei-Shaw/sub2api     只读，仅跟随 main
     └─ origin/main = main          AnBooting 团队基线（0e6f5efb）
          └─ develop  ★ 我们的集成分支（当前 HEAD）
```

远端：`origin` = AnBooting/sub2api（团队）、`upstream` = Wei-Shaw/sub2api（原始）。
详细操作见 [04-git-workflow.md](04-git-workflow.md)。

---

## 5. 六项需求归属（核心结论）

| # | 需求 | 归属 | 关键证据 |
|---|---|---|---|
| 1 | 国模 RMB / Claude·GPT USD | **fork 已实现** → 验证补漏 | commit `11109acd`；`service/pricing_native_cny.go` |
| 2 | 调度并发（部分不并发/部分过大） | **我们** | 根因见下 ↓ |
| 3 | 卡网嵌入 + 自动充值 | **我们** | 有抓手，缺 3 样 |
| 4 | 兜底分组 AWSB | **我们**（多为配置） | 机制已存在 |
| 5 | DeepSeek 峰谷 | **fork 已实现** → 验证补漏 | `ratesAt()` 支持星期/时段/跨天/时区 |
| 6 | 图片/视频兼容 | **我们** | 视频有 4 个缺口 |

### 需求 2 根因（已核实源码）

**「两个数」分裂**：抢槽硬上限用 `account.Concurrency`（`gateway_scheduling.go:177...`），
但选号负载率**分母**用 `account.EffectiveLoadFactor()`（`:441`），后者**优先取 `load_factor`**。

- `load_factor > concurrency` → 负载率虚低 → 被优先选中 → **并发贴顶**
- `load_factor < concurrency` → 负载率虚高 → 被 `LoadRate < 100` 过滤 → **"不并发"**

补充：`concurrency <= 0` = **完全不限制**；`concurrency` 默认 3，`load_factor` **无默认值**；
粘性权重 `previous_response=5.0` vs `load=1.0`。

### 需求 6 视频四个缺口

| 缺口 | 证据 |
|---|---|
| 硬编码 Grok 专属 | `migrations/220:1` |
| 无独立 `allow_video_generation` 开关 | `migrations/220:3` |
| **无任务表 + 被动结算**（不轮询不扣费） | `grok_media.go:431-440`；`docs/seedance-api.md:41` |
| 未知视频模型**回落 2K 图片价** | `billing_service.go:2385` |

### 额外发现（与红线冲突）

**USD 模型缺价 = 记 0 放行**（`openai_gateway_usage.go:259-272`），
国模路径才是 fail-closed。这是**上游既有行为**，要不要改需决策。

---

## 6. 决策状态（2026-10-10 更新）

**已确认**：范围以 6 项为主；顺序 需求2 → 4 → 6 → 3；价格方案 D（ADR-009）；USD 缺价拒绝带开关（ADR-010）；需求 1/5 只验证不改；卡网=云猫寄售 catfk.com；兜底=可替换有序分组链；媒体对标 new-api 能力对齐；线上为上游官方版，先部署测试服务器。详见 06 号 REQ-009~014。

**仍待团队回答**：见 16 号问卷，关键阻塞题 **A1（云猫 API 能力）**、**D2（媒体报错样例）**、B1（问题账号实值）。

### 原问题列表（已归档）

| # | 问题 | 影响 |
|---|---|---|
| 1 | 需求 3：「卡网」是发卡平台还是卡密站？哪一家？"嵌入"是谁嵌谁？ | 决定方案 |
| 2 | 需求 4：「AWSB」是什么缩写？"默认兜底"指哪种场景（4 种语义）？ | 决定是否要写代码 |
| 3 | 需求 6：对标基准 "new" 是哪个项目？主要用哪些视频模型？当前具体报什么错？ | 决定范围 |
| 4 | 价格口径：fork 的 CNY 方案 vs 国模移植包方案，选哪个（4 选项见 13 号文档 §7） | **架构级冲突** |
| 5 | USD 缺价要不要改成 fail-closed？ | 破坏性变更 |

---

## 7. 已知基线问题（不是我们引入的）

`go test -tags=unit ./...` 有 **5 个失败用例**：

| 包 | 用例 |
|---|---|
| `internal/repository` | `TestPgDumperHoldsMigrationLockThroughReaderClose` |
| `internal/repository` | `TestPgDumperReleasesMigrationLockWhenProcessFails` |
| `internal/repository` | `TestPgDumperReportsUnlockFailureAndDiscardsConnection` |
| `internal/repository` | `TestServerTimingConnectorRecordsDriverCallsWithoutRowLifetime` |
| `internal/service` | `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` |

→ 验收以「**不新增失败**」为准。

其他：
- 根 `Makefile` 的 `FRONTEND_CRITICAL_VITEST`（29 条）**不含** fork 新增的 5 个 spec。
- `api/admin/pricing.ts` 未注册进 `adminAPI` barrel。
- 迁移编号：**fork 已占 243**，新增从 **244** 起。

---

## 8. 下一步怎么做

### 立刻可做（不依赖决策）

1. **装 Docker Desktop** → 验证本地 compose 起服务。
2. **要协作者权限** → `Settings → Collaborators and teams → Add people → yylovehhy → Write`。
3. **验证需求 1/5**（fork 已实现的）：端到端跑一次国模调用，核对 CNY 计价与峰谷时段。

### 需要先出文档再动手

按 [14-prd-techspec-template.md](14-prd-techspec-template.md) 为每项需求产出 PRD + 技术规格，评审通过后：
```powershell
git checkout develop
git checkout -b feat/REQ-002-scheduling-concurrency
```

### 质量门（合并前必须全过）

| 门 | 命令 |
|---|---|
| 后端编译 | `cd backend && go build ./cmd/server` |
| 后端单测 | `go test -tags=unit ./...`（不新增失败） |
| 后端静态检查 | `go vet ./...` |
| 前端类型 | `corepack pnpm@9 exec vue-tsc --noEmit` |
| 前端测试 | `corepack pnpm@9 exec vitest run` |
| i18n | `corepack pnpm@9 exec vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts` |
| 代码审查 | `ocr review --from main --to <branch>` |

---

## 9. 文档地图

```
F:\sub2\
├── AGENTS.md                     项目规则（自动加载）
├── project-docs\
│   ├── 00-overview.md            项目定位/目标/边界/许可
│   ├── 01-baseline.md            全部取证事实与版本
│   ├── 02-merge-plan.md          移植包逐文件合并方案
│   ├── 03-dev-environment.md     环境与命令
│   ├── 04-git-workflow.md        Git 操作手册
│   ├── 05-deploy-docker.md       Docker 部署手册（草案）
│   ├── 06-requirements-backlog.md 需求待办池
│   ├── 07-decisions.md           ADR（8 条）
│   ├── 08-runbook.md             运维手册（草案）
│   ├── 09-preflight-report.md    移植包预检报告
│   ├── 10-preparation-report.md  准备阶段报告
│   ├── 11-collaboration-norms.md 协作规范
│   ├── 12-worktree-strategy.md   worktree 策略（结论：暂不开）
│   ├── 13-requirements-mapping.md ★ 需求映射（最重要）
│   ├── 14-prd-techspec-template.md PRD/技术规格模板
│   ├── 15-handoff.md             本文档
│   └── architecture\
│       ├── README.md             侦察结论速览
│       ├── 01-request-billing.md 请求链路与计费
│       ├── 02-scheduling.md      调度与并发
│       ├── 03-media.md           图片与视频
│       ├── 04-payment-fallback.md 支付与兜底分组
│       ├── 05-frontend.md        前端
│       └── diagrams\
│           ├── 00-architecture.html  交互式架构图
│           └── 00-architecture.json  archify 源（可重新生成）
├── _reference\
│   └── domestic-provider-kit\    国模移植包（只读参考）
└── project-skills\
    └── sub2api-dev-deploy\       项目级 skill
```

---

## 10. 可用能力

| 能力 | 用途 |
|---|---|
| skill `sub2api-dev-deploy` | 本项目开发与部署速查 |
| `ocr`（`F:\tools\ocr\ocr.exe`） | 阿里 OpenCodeReview |
| skill `code-review-and-quality` / `differential-review` | 代码审查 |
| skill `security-audit` / `security-and-hardening` | 安全审查 |
| skill `webapp-testing` | Playwright 端到端验证 |
| skill `archify` | 生成架构图（源文件在 `diagrams/`） |
| `skills/sub2api-admin` | 上游自带管理 API CLI |
