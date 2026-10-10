# Sub2API 二开 — 开发前准备状态报告

> 生成时间：2026-10-10 00:40（Asia/Shanghai）
> 阶段：**开发前准备（P0）** — 尚未开始任何功能开发

## 一、已完成

### 1. 代码基线（✅ 完成）

| 项 | 值 |
|---|---|
| 仓库路径 | `F:\sub2` |
| 上游 | `Wei-Shaw/sub2api`（LGPL-3.0） |
| 基线版本 | `v0.2.15` / commit `3a6fd1c9` |
| 历史深度 | 完整（7,568 commits，63 tags） |
| 分支 | `main`（上游镜像）、`develop`（二开集成） |
| 标签 | `baseline-v0.2.15`、`baseline-upstream-v0.2.15` |
| 工作树 | 干净（除 `_reference/` 与 `project-docs/` 待提交） |
| 规模 | 4,274 跟踪文件 / 后端 2,882 Go 文件 / 前端 316 Vue 文件 / 292 迁移 |

### 2. 移植包恢复与校验（✅ 完成）

- 移植包**曾被误删**，已从回收站恢复原始压缩包 `sub2api-domestic-provider-kit-1.0.65(2).zip`。
- 按包内 `SHA256SUMS` 校验：**89/89 通过，0 不匹配，0 缺失** → 字节完整。
- 已归位到 `_reference/domestic-provider-kit/`，压缩包保留在 `_reference/domestic-provider-kit-1.0.65.zip`。

### 3. 工具链（✅ 完成 / ⏳ 待用户）

| 工具 | 状态 |
|---|---|
| Go 1.27.2 | ✅ 便携版 `F:\tools\go1.27.2\go`，环境变量已持久化 |
| pnpm 9 | ✅ 通过 `corepack pnpm@9`（9.15.9）使用；全局 pnpm 11 与锁文件不兼容 |
| ocr（阿里 OpenCodeReview） | ✅ `F:\tools\ocr\opencodereview.exe` v1.12.13，SHA256 已核对 |
| 前端依赖 | ✅ 已安装（`corepack pnpm@9 install`） |
| Docker Desktop | ⏳ 安装包已下载（606 MB），**需你手动安装**（管理员 + WSL2 + 重启） |

### 4. 构建验证（✅ 通过）

| 检查 | 结果 | 耗时 |
|---|---|---|
| `go mod download` | ✅ 退出码 0 | ~2 min |
| `go build ./cmd/server` | ✅ 退出码 0，产物 153.3 MB | 163 s |
| 前端 `typecheck`（vue-tsc） | ✅ 退出码 0 | 68.8 s |
| 前端 i18n 检查 + vite build | 🔄 进行中（绕过嵌套 pnpm 问题后重跑） |
| 后端 `go vet` + `go test -tags=unit` | 🔄 进行中 |

### 5. 文档体系（✅ 完成）

`project-docs/` 下 10 份文档：

| 文档 | 内容 |
|---|---|
| [00-overview.md](../project-docs/00-overview.md) | 项目定位、目标、边界、许可证、目录 |
| [01-baseline.md](../project-docs/01-baseline.md) | 全部取证事实与版本 |
| [02-merge-plan.md](../project-docs/02-merge-plan.md) | **移植包逐文件合并方案**（47 文件、+12,798/-5,136 行） |
| [03-dev-environment.md](../project-docs/03-dev-environment.md) | 本机环境、pnpm 陷阱、常用命令 |
| [04-git-workflow.md](../project-docs/04-git-workflow.md) | Git 分支模型与操作 |
| [05-deploy-docker.md](../project-docs/05-deploy-docker.md) | Docker 部署手册（草案） |
| [06-requirements-backlog.md](../project-docs/06-requirements-backlog.md) | 需求待办池（REQ-001~008） |
| [07-decisions.md](../project-docs/07-decisions.md) | ADR-001~008 |
| [08-runbook.md](../project-docs/08-runbook.md) | 运维手册（草案） |
| [09-preflight-report.md](../project-docs/09-preflight-report.md) | 移植包预检与差异报告 |

另有 `AGENTS.md`（项目规则，**需 `git add -f`**）。

## 二、关键发现

### 1. 移植包是真实净增量

7 个 core 文件在上游**全部不存在**（404），`domestic_provider` 代码搜索 **0 命中**。
上游从未实现国模分组/账号/定价。

### 2. 但共享文件已被上游大幅重构

47 个共享参考文件全部有差异，合计 **+12,798 / -5,136 行**，远大于国模功能本身。
差异里**混杂大量上游演进**（支付、渠道监控、画布、Codex 等）。

→ **结论：参考文件不能整文件覆盖**，必须按 `MERGE-INDEX.md`（4,130 行）的行号摘录逐段提取。

### 3. 命名冲突（需决策）

| 上游已有 | 位置 | 与移植包的关系 |
|---|---|---|
| `cn_provider` | 余额检查 / 限流 / 调度（23 处） | 功能不同（调度风控 vs 分类定价），但名字近似 |
| `domestic` | 前端 `keyGroupProviders.ts` Key 分组筛选 | 语义重叠，需统一 |
| `domestic` | 仪表盘 i18n 文案 | 文案层，需一致 |

### 4. 迁移编号

上游已到 **242**，移植包的 196/197/198 → 重编号为 **243/244/245**。

### 5. 协议模式

上游已有 Responses 自动探测（`openai_apikey_responses_probe.go`）。
移植包源版创建弹窗有 bug：设了 `force_chat_completions` 但未持久化。→ 需决策（ADR-006）。

## 三、待你完成 / 待决策

### 需要你操作（无法代劳）

1. **安装 Docker Desktop**：`F:\tools\downloads\DockerDesktopInstaller.exe`
   - 右键 → 以管理员身份运行
   - 勾选 **Use WSL 2 instead of Hyper-V**
   - 安装后**重启一次**，再启动 Docker Desktop
   - 验证：`docker --version`、`docker run --rm hello-world`

2. **确认是否创建私有 origin 仓库**（用于备份你的二开代码）：
   ```powershell
   gh repo create sub2api-fork --private --source F:\sub2 --remote origin --push
   ```

### 需要你决策

| # | 决策 | 我的建议 |
|---|---|---|
| D1 | `domestic_provider` 命名是否沿用（ADR-005） | 沿用后端名，前端统一 `domestic` 语义 |
| D2 | 国模账号协议模式（ADR-006） | 默认显式 `force_chat_completions`，编辑可切回自动 |
| D3 | 是否现在就合并国模移植包（P1），还是等你的新需求一起做 | 建议先合并（它是后续功能的基础） |
| D4 | 生产服务器信息 | 等你提供 |

### 需求录入

你的需求明确后，我会按 `project-docs/06-requirements-backlog.md` 的模板逐条展开为：
**用户故事 → 验收标准 → 技术方案 → 影响文件 → 工作量 → PRD / 技术规格文档**，然后再动代码。

## 四、下一步（等你确认后执行）

1. 提交 P0 准备工作到 `develop`（`_reference/` + `project-docs/` + `AGENTS.md`）。
2. 创建私有 origin 并推送（备份基线）。
3. 等你安装 Docker 后验证本地 compose 起服务。
4. 接收你的需求 → 出 PRD 与技术规格 → 开始 P1/P2 开发。
