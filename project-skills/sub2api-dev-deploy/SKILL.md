---
name: sub2api-dev-deploy
description: "Use when working on the Sub2API fork at F:\\sub2: Go/Vue code, migrations, the 国模 provider port, local compose or deployment."
---

# Sub2API 二开开发与部署

## 项目身份

| 项 | 值 |
|---|---|
| 仓库 | `F:\sub2` |
| 上游 | `Wei-Shaw/sub2api`（LGPL-3.0），基线 `v0.2.15` / `3a6fd1c9` |
| 分支 | `main` = 上游镜像（**禁止提交**）；`develop` = 二开集成 |
| 技术栈 | Go 1.27.2 + Gin + Ent + PostgreSQL 18 + Redis 8；Vue 3.5 + Vite 5 + pnpm 9 |
| 文档 | `F:\sub2\project-docs\`（先读 `00-overview.md`，规则读 `F:\sub2\AGENTS.md`） |

## 仓库导航

跨模块、公共契约、计费、调度、迁移或影响面不明确的任务，先读 `project-skills/repository-navigation/SKILL.md`，使用 `tools/repo-intelligence.mjs` 和 `tools/repo-map.mjs` 生成最小上下文包，再进入实现和验证。

## 环境（本机已就绪，勿重复安装）

| 工具 | 位置 / 用法 |
|---|---|
| Go 1.27.2 | `F:\tools\go1.27.2\go\bin\go.exe`（`GOROOT`/`GOPATH`/`PATH` 已持久化） |
| pnpm | **必须** `corepack pnpm@9`；全局 pnpm 11 与锁文件不兼容 |
| ocr | `F:\tools\ocr\opencodereview.exe`（= `ocr.exe`，阿里 OpenCodeReview v1.12.13）。**只有 `delegate preview/rule` 可用**：它只选文件与取规则、不调 LLM。`ocr review` 走 LLM，实测 454 次 HTTP 429、0 文件完成，**不要指望它做整库审查**；深审由宿主代理读 diff 完成 |
| Docker | 需用户手动安装（见 `project-docs\03-dev-environment.md`） |

## 命令速查

### 后端
```powershell
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server
go test -tags=unit ./...
go test -tags=integration ./...     # 需 PostgreSQL + Redis
go generate ./ent                   # 改 ent schema 后必须执行
```
首次拉依赖：`$env:GOPROXY='https://goproxy.cn,direct'`

### 前端
```powershell
cd F:\sub2\frontend
corepack pnpm@9 install --frozen-lockfile --registry=https://registry.npmmirror.com
corepack pnpm@9 run typecheck
corepack pnpm@9 exec vitest run          # 直接跑 vitest，绕过嵌套 pnpm
corepack pnpm@9 exec vue-tsc -b          # 绕过 npm script 里的 pnpm 调用
corepack pnpm@9 exec vite build
```

> ⚠️ **不要**用 `corepack pnpm@9 run build`：`package.json` 的 build 脚本内部再调 `pnpm`，
> 会解析到全局 pnpm 11 并失败（`ERR_PNPM_ABORTED_REMOVE_MODULES_DIR_NO_TTY`）。
> 手动按顺序执行三步：`check:i18n` → `vue-tsc -b` → `vite build`。

## 绝对红线

1. **不在 `main` 提交**。
2. **不整文件覆盖** `_reference/domestic-provider-kit/reference/` 里的共享文件；按
   `_reference/domestic-provider-kit/docs/MERGE-INDEX.md` 的行号摘录**逐段**提取国模片段。
3. **不提交凭据**：`.env`、`data/`、`postgres_data/`、`redis_data/`、API Key、SSH 信息。
4. **不碰** `dwin-builder`（192.168.43.119）。
5. **缺价不放行**：未定价模型必须在请求上游前拒绝，不得零价或回落 GPT 兜底价。
6. **迁移编号**接在上游最新之后（当前最新 **250**，新增从 **251** 起），不改写已有迁移。
   已用编号：243–245 国模移植、246 分组 HA、247 视频价目、248 云猫订单、249 视频任务、250 放宽 group-link 授权 CHECK。
7. **不推送到 `upstream`**。其 push URL 已置哨兵值 `DISABLED-upstream-is-read-only`，误推会立即失败。
   `gh` 在本目录**未设默认仓库**，`gh pr view <n>` 会解析到上游同名 PR；一律显式 `--repo AnBooting/sub2api`。

## 关键陷阱

| 陷阱 | 表现 | 处理 |
|---|---|---|
| 全局 pnpm 11 | `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH` | 用 `corepack pnpm@9` |
| npm script 内嵌 pnpm | build 失败 `no TTY` | 直接 `exec` 子命令 |
| GitHub 直连 | `Connection was reset` | `git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877` |
| npm 官方源慢 | 10–26 s/请求 | `--registry=https://registry.npmmirror.com` |
| `docs/*` 被 gitignore | 文档提交不了 | 项目文档放 `project-docs/` |
| `AGENTS.md` 被 gitignore | 提交不了 | `git add -f AGENTS.md` |
| i18n 检查 | `build` 前必跑 | 新增文案同步 `zh` 与 `en` |
| 鉴权缓存 | 分组新字段绕过价格命名空间 | 同步 `api_key_auth_cache.go` 并失效旧缓存 |
| **ent 的 OnDelete 只在 `edge.To` 生效** | 写在 `edge.From` 上被静默忽略，生成 `NoAction` | 外键级联必须写在**父**边的 `edge.To(...)`，并与迁移里的 `ON DELETE` 对齐 |
| 迁移改 CHECK 的锁窗 | `DROP CONSTRAINT` + `ADD CONSTRAINT` 在 PG 上全表校验 + ACCESS EXCLUSIVE | 大表考虑 `NOT VALID` + `VALIDATE CONSTRAINT`；本机无 PG，**未实测** |
| 测试锁住 bug | 全绿但断言的是错误行为（4k 档曾被三个测试断言非法） | 修缺陷前先写**能失败**的测试；修复后**变异验证**（回退修复必须让测试失败） |
| 测试照抄被测逻辑 | 改坏生产代码测试仍绿 | 抽出生产函数再断言（如 `mergeSearchSurcharge`），不要重写组合过程 |
| 审查者读到过期工作树 | 行号/内容与当前源码不符 | 以当前源码为准并复核实证，不直接采信结论 |
| 本地分支引用过期 | 误判提交「未推送」 | 判定「仅本地提交」用 `git rev-list --count --all --not --remotes`；`git branch -d` 只比较当前 HEAD，不是安全判据 |

## 国模移植包要点

- 位置：`F:\sub2\_reference\domestic-provider-kit\`（只读参考，已校验 89/89 SHA256）。
- **7 个 core 文件上游全无** → 直接新增。
- **47 个共享参考文件全部有差异**（+12,798/-5,136 行），差异里混杂大量无关的上游演进 → 逐段合并。
- 迁移 196/197/198 → 重编号 **243/244/245**。
- 上游已有 `cn_provider`（余额/限流/调度）与前端 `keyGroupProviders.ts` 的 `domestic` 分类，
  与移植包的 `domestic_provider` 语义重叠 → 见 `project-docs/07-decisions.md` ADR-005。
- 详细方案见 `F:\sub2\project-docs\02-merge-plan.md` 与 `09-preflight-report.md`。

## 部署

本地：见 `project-docs/05-deploy-docker.md`。
生产：**等用户提供服务器信息**（用户名/密码或密钥、IP、系统、是否已装 Docker、有无存量数据、TLS 方案），
不做假设。部署前必须 `pg_dump` 备份并记录回滚点。

## 审查

```powershell
cd F:\sub2
# 只列可审文件（不调 LLM，安全、快）
& F:\tools\ocr\opencodereview.exe delegate preview --from origin/develop --to HEAD
# 取该文件适用的规则
& F:\tools\ocr\opencodereview.exe delegate rule <files>
```
**不要用 `ocr review`**：它走 LLM，本机端点限流严重（实测 454 次 HTTP 429、约 2 小时 0 文件完成）。

深审由宿主代理读 diff 完成。本项目已证明「套件全绿」不足以作为正确性证据，审查必须包含：
① 先写能失败的测试复现缺陷；② 修复后变异验证；③ 用 `git blame` 区分上游既有缺陷与本轮引入；
④ 对审查结论本身复核（两次遇到审查者基于过期工作树给出已修复的问题）。

## 验证标准

- 后端 `go build` 退出码 0 + `go test -tags=unit ./...` 通过
- 前端 `vue-tsc --noEmit` 退出码 0 + vitest 通过 + `vite build` 成功
- 功能按 `_reference/domestic-provider-kit/docs/ACCEPTANCE.md` 21 项逐条验收
- 部署 `/health` 200 + 三容器 healthy + 端到端调用与扣费核对

## 描述补充

description 只保留触发语义；完整说明如下（原文压入正文以守住 140 字符的常驻目录预算，内容不删减）：

Develop, build, test and Docker-deploy the Sub2API secondary-development fork at F:\sub2 (upstream Wei-Shaw/sub2api, baseline v0.2.15). Use when working on the sub2api codebase: Go backend (Ent/Gin), Vue 3 frontend, database migrations, the domestic-provider (国模) feature port, local compose bring-up, or production deployment. Covers the exact local toolchain, the pnpm-9 trap, migration renumbering, merge rules for the reference kit, and deploy/rollback steps.
