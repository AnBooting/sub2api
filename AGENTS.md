# 项目规则（Sub2API 二开）

> 本文件是**本仓库**的代理工作规则。上游仓库自带的 `AGENTS.md` 不存在；
> 本文件被上游 `.gitignore` 忽略，提交需 `git add -f AGENTS.md`。

## 项目身份

- 上游：`Wei-Shaw/sub2api`（LGPL-3.0），基线 **v0.2.15 / 3a6fd1c9**
- 本仓库：`F:\sub2`，二开集成分支 **`develop`**
- 目标：国模（国内大模型厂商）分组/账号/定价能力 + Docker 生产部署
- 文档索引：[project-docs/00-overview.md](project-docs/00-overview.md)

## 绝对红线

1. **不在 `main` 分支提交**。`main` 是上游镜像，只用于 `git fetch upstream`。
2. **不整文件覆盖共享参考文件**。`_reference/domestic-provider-kit/reference/` 是参考，不是补丁；
   必须按 `docs/MERGE-INDEX.md` 的行号摘录逐段提取国模片段。
3. **不提交凭据**：`.env`、`data/`、`postgres_data/`、`redis_data/`、任何 API Key、密码、SSH 信息。
4. **不碰** `dwin-builder`（192.168.43.119）—— 用户明确指示。
5. **不迁移无关功能**：支付、画布、账号智商检测、渠道监控不在国模移植范围内。
6. **缺价不得放行**：未定价模型必须在上游请求前拒绝，不得按零价或回落 GPT 兜底价。

## 真实命令（本机实测）

### 后端（Go 便携版）
```powershell
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server
go test -tags=unit ./...
go test -tags=integration ./...      # 需 PostgreSQL + Redis
go generate ./ent                    # 改 ent schema 后必须执行
```
Go 环境：`GOROOT=F:\tools\go1.27.2\go`，`GOPATH=F:\tools\gopath`，`GOTOOLCHAIN=local`。
首次拉依赖需 `GOPROXY=https://goproxy.cn,direct`。

### 前端（**必须 pnpm 9**）
```powershell
cd F:\sub2\frontend
corepack pnpm@9 install --frozen-lockfile --registry=https://registry.npmmirror.com
corepack pnpm@9 run typecheck
corepack pnpm@9 run test:run
corepack pnpm@9 run build
```
⚠️ 全局 pnpm 是 11.x，与上游锁文件**不兼容**（`ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`）。**永远用 `corepack pnpm@9`**。

### 网络
GitHub 需代理：`git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877`（Clash 端口变化时同步改）。

## 架构要点

| 层 | 位置 | 说明 |
|---|---|---|
| 入口 | `backend/cmd/server` | `-tags embed` 时内嵌 `internal/web/dist` 前端产物 |
| 路由 | `backend/internal/server/routes/` | `admin.go` 是管理路由 |
| 处理 | `backend/internal/handler/` | Gin handler；`dto/` 放请求/响应类型与 mapper |
| 服务 | `backend/internal/service/` | 业务逻辑；`wire.go` 是依赖装配 |
| 仓储 | `backend/internal/repository/` | 数据访问；`wire.go` 装配 |
| 模型 | `backend/ent/schema/` | Ent schema，改后必须 `go generate ./ent` |
| 迁移 | `backend/migrations/` | 顺序编号 SQL，**当前最新 242**，新增从 **243** 起 |
| 前端 | `frontend/src/` | Vue 3 + Pinia + vue-i18n + Tailwind |

## 特殊边界

- **迁移编号**：新增必须接在上游最新编号之后，**不改写已存在的迁移**。
- **i18n**：前端 `build` 会先跑 `check:i18n`（locale key 完整性），新增文案必须同步 `zh` 与 `en`。
- **ent / wire 生成物不手改**：改 schema 或 ProviderSet 后重新生成。
- **API Key 鉴权缓存**：分组快照若新增字段（如 `domestic_provider`），必须同步缓存结构并使旧缓存失效。
- **定价口径**：沿用源站「1 元 = 1 美元」站内政策（ADR-003），界面说明、后端计费、测试三者必须一致。
- **`.gitignore` 陷阱**：`docs/*` 与 `AGENTS.md` 被忽略；项目文档放 `project-docs/`。

## 验证标准

- 后端：`go build` 退出码 0 + `go test -tags=unit ./...` 通过
- 前端：`typecheck` 退出码 0 + `test:run` 通过 + `build` 成功
- 功能：`_reference/domestic-provider-kit/docs/ACCEPTANCE.md` 21 项逐条有证据
- 部署：`/health` 200 + 三容器 healthy + 端到端调用与扣费核对

## 可用能力

| 能力 | 用途 |
|---|---|
| `ocr`（`F:\tools\ocr\opencodereview.exe`） | 阿里 OpenCodeReview：`ocr review --from main --to develop` |
| `skills/sub2api-admin` | 上游自带的管理 API CLI（`node scripts/sub2api-admin.js ...`） |
| skill `code-review-and-quality` / `differential-review` | 代码审查 |
| skill `security-audit` / `security-and-hardening` | 安全审查 |
| skill `git-workflow-and-versioning` | Git 流程 |
| skill `webapp-testing` | Playwright 端到端验证 |
| `project-docs/` | 项目文档（先读 00-overview） |
