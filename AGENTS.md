# 项目规则（Sub2API 二开）

> 本文件是**本仓库**的代理工作规则。上游仓库自带的 `AGENTS.md` 不存在；
> 本文件被上游 `.gitignore` 忽略，提交需 `git add -f AGENTS.md`。

## 项目身份

- 上游：`Wei-Shaw/sub2api`（LGPL-3.0），基线 **v0.2.15 / 3a6fd1c9**
- 本仓库：团队 fork（`origin`），二开集成分支 **`develop`**
- 目标：按 `project-docs/prd/` 的 PRD 实现功能，并做 Docker 生产部署
- 文档索引：`project-docs/`（需求与规格见 `project-docs/prd/`）

## 绝对红线

1. **不在 `main` 分支提交**。`main` 是上游镜像，只用于 `git fetch upstream`。
2. **不整文件覆盖上游共享文件**。参考材料是参考，不是补丁；必须按行号摘录逐段提取所需片段。
3. **不提交凭据**：`.env`、`data/`、`postgres_data/`、`redis_data/`、任何 API Key、密码、SSH 信息。
4. **不使用任何非本机的 SSH 构建机**；构建与验证一律在本地完成。
5. **不迁移无关功能**：支付、画布、渠道监控不在本期范围内。
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
⚠️ 全局 pnpm 是 11.x，与上游锁文件**不兼容**（`ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`）。**永远用 `corepack pnpm@9`**（实测 `corepack pnpm@9 --version` → 9.15.9，可用）。

代理会话里若不希望每次走 corepack 解析，可直接调用已安装的二进制（等价且更快）：
```powershell
cd F:\sub2\frontend
.\node_modules\.bin\vue-tsc.cmd -b        # typecheck（等价 run typecheck）
node node_modules/vitest/vitest.mjs run     # 测试（等价 run test:run）
```
两者都必须退出码 0；不要因为绕过 corepack 就跳过验证。

### 网络
GitHub 需代理：`git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877`（Clash 端口变化时同步改）。

### 远端与分支

- `origin` = 团队 fork（可推送）；`upstream` = 上游，**只读**。
  `upstream` 的 push URL 已设为哨兵值 `DISABLED-upstream-is-read-only`，误推会**立即失败**而不是推到上游。
  若需恢复默认行为：`git remote set-url --push upstream https://github.com/Wei-Shaw/sub2api.git`。
- **提 PR 前必须先确认目标仓库**：`gh` 在本目录未设默认仓库，`gh pr view <n>` 会解析到上游同名 PR。
  一律显式传 `--repo AnBooting/sub2api`。
- **不要用本地分支是否「已合并」判断能否删除**：`git branch -d` 只比较当前 HEAD。
  用 `git rev-list --count --all --not --remotes` 判定是否存在**仅本地**的提交；为 0 才可删。

## 架构要点

| 层 | 位置 | 说明 |
|---|---|---|
| 入口 | `backend/cmd/server` | `-tags embed` 时内嵌 `internal/web/dist` 前端产物 |
| 路由 | `backend/internal/server/routes/` | `admin.go` 是管理路由 |
| 处理 | `backend/internal/handler/` | Gin handler；`dto/` 放请求/响应类型与 mapper |
| 服务 | `backend/internal/service/` | 业务逻辑；`wire.go` 是依赖装配 |
| 仓储 | `backend/internal/repository/` | 数据访问；`wire.go` 装配 |
| 模型 | `backend/ent/schema/` | Ent schema，改后必须 `go generate ./ent` |
| 迁移 | `backend/migrations/` | 顺序编号 SQL，**当前最新 250**，新增从 **251** 起 |
| 前端 | `frontend/src/` | Vue 3 + Pinia + vue-i18n + Tailwind |

## 特殊边界

- **迁移编号**：新增必须接在上游最新编号之后，**不改写已存在的迁移**。
- **i18n**：前端 `build` 会先跑 `check:i18n`（locale key 完整性），新增文案必须同步 `zh` 与 `en`。
- **ent / wire 生成物不手改**：改 schema 或 ProviderSet 后重新生成。
- **API Key 鉴权缓存**：分组快照若新增字段（如 `domestic_provider`），必须同步缓存结构并使旧缓存失效。
- **定价口径**：以项目 ADR 与 PRD 的业务规则为准；界面说明、后端计费、测试三者必须一致。
- **`.gitignore` 陷阱**：`docs/*` 与 `AGENTS.md` 被忽略；项目文档放 `project-docs/`。

## 验证标准

- 后端：`go build` 退出码 0 + `go test -tags=unit ./...` 通过
- 前端：`typecheck` 退出码 0 + `test:run` 通过 + `build` 成功
- 功能：按 PRD 的验收标准逐条给出证据
- 部署：`/health` 200 + 三容器 healthy + 端到端调用与扣费核对

## 验证纪律（本项目实测教训，务必遵守）

1. **测试全绿不等于正确。** 本项目已三次遇到「测试恰好把 bug 固定住」：新增 4k 档时三个既有测试断言 4k 非法；C-1 复算测试把渠道/推理倍率设为 1 因而绕过缺陷路径；`peek(true)` 的测试只断言纯函数、与入口无绑定。**修缺陷前先写能失败的测试。**
2. **测试必须调用生产代码，不能照抄被测逻辑。** 把 bug 的组合逻辑在测试里重写一遍，改坏生产代码测试仍然绿。应抽出生产函数（如 `mergeSearchSurcharge`）再断言它。
3. **修复后做变异验证。** 把修复回退，确认测试**失败**；再恢复。没通过这一步的修复不算验证过。
4. **先复现再修。** 对每个上报的缺陷，先用失败测试/真实 SQL 复现，避免「看起来对」的推测性修改。
5. **区分上游既有缺陷与本轮引入。** 用 `git blame` / `git merge-base --is-ancestor <sha> origin/develop` 判定；不在本 diff 内的问题不要顺手改，记录并留待后续轮次。
6. **不要为了让测试通过而放宽护栏。** 若测试与契约冲突，先确认契约，再改测试。
7. **审查者可能读到过期的工作树。** 审查结论中的行号/内容若与当前源码不符，以当前源码为准并复核实证（本项目两次遇到：`peek(true)` 已在后续提交修好、本地分支引用过期导致误判提交未推送）。
8. **测试构造的配置必须与生产默认一致，否则会测出一条生产走不到的分支。** 本项目实测：`gateway.openai_ws.sticky_session_ttl_seconds` 的 viper 默认值是 **3600（非零）**，而测试用 `config.Config{}`（值为 0），于是「配置 > 0 就覆盖」的写法在测试里走策略分支、在生产里永远走配置分支——**策略接线全绿但生产完全没生效**。凡是「未配置则回落」的分支，测试必须显式建模 viper 默认值；判定「是否配置」也不能用 `> 0` 这种会被默认值污染的哨兵。同类陷阱：`openAIWSSessionStickyTTL()` 的 `openaiStickySessionTTL` 兜底同样不可达（默认 3600 恒真），因两者数值都是 1h 故无行为差异，属既有缺陷未改。

### 已知 flaky（不要误判为自己引入）

- `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory`（`billing_inflight_reservation_test.go`，8MB 堆上限）在**全量套件满载**时偶发失败；隔离运行稳定通过，且与 `origin/develop` 逐字相同。**非本 diff 引入，不要改它。**

## 可用能力

| 能力 | 用途 |
|---|---|
| `ocr`（`F:\tools\ocr\opencodereview.exe`） | `ocr delegate preview/rule --from X --to Y`：**只选文件与取规则，不调用 LLM**，可安全用于大范围筛选。`ocr review` 走 LLM，端点限流严重（实测 454 次 HTTP 429、0 文件完成），**不要依赖它做整库审查**；深审请由代理自己读 diff |
| `skills/sub2api-admin` | 上游自带的管理 API CLI（`node scripts/sub2api-admin.js ...`） |
| skill `code-review-and-quality` / `differential-review` | 代码审查 |
| skill `security-audit` / `security-and-hardening` | 安全审查 |
| skill `git-workflow-and-versioning` | Git 流程 |
| skill `webapp-testing` | Playwright 端到端验证 |
| `project-docs/` | 项目文档（先读 00-overview） |
