# Sub2API 二开项目 — 总览

> 状态：**安全整改已全部合入 develop（#18 / #19 / #21），尚未部署上线**
> 最后更新：2026-11-10（PR 状态用 `gh pr list/view --repo AnBooting/sub2api` + `git fetch origin` + `tools/verify-deployed-build.ps1` 实测；工作区与 Git 流程已按 04-git-workflow.md 收口）

## 1. 项目定位

在开源项目 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的基础上做二次开发（魔改），最终用 Docker 部署到一台真实服务器上对外提供服务。

- **上游**：`https://github.com/Wei-Shaw/sub2api`（LGPL-3.0）
- **本仓库**：`F:\sub2`，本地 git 仓库，`origin` 为私有远端（待建），`upstream` 指向上游
- **基线**：`v0.2.15`，commit `3a6fd1c9db07203ca308aaba69e502bc1f35b307`
- **技术栈**：Go 1.27.2 + Gin + Ent + PostgreSQL 18 + Redis 8；前端 Vue 3.5 + Vite 5 + TailwindCSS + pnpm

## 2. 目标（分阶段）

| 阶段 | 目标 | 状态 |
|---|---|---|
| P0 | 本地可编译、可测试、可运行的基线工程 | 进行中 |
| P1 | 补齐国模（国内大模型厂商）分类与绑定校验 | 待评审 |
| P2 | 按 PRD 实现功能 | 见 `project-docs/prd/` |
| P3 | 本地 Docker 全栈跑通 + 端到端验收 | 未开始 |
| P4 | 部署到生产服务器 | 未开始（缺服务器信息） |

## 3. 明确边界

**做**：
- 在上游代码上增量开发，保持与上游可合并
- 国模（国内大模型厂商）分组、账号、定价能力
- Docker 化部署与运维脚本

**不做**（除非需求明确要求）：
- 不整体覆盖上游共享文件
- 不把参考材料里无关功能（支付、画布、渠道监控）带进来
- 不提交任何真实凭据、生产数据、SSH 信息
- 不改动上游 `main` 分支

## 4. 许可证与合规

上游为 **LGPL-3.0**。二次开发与分发需遵守该许可证：
- 保留 `LICENSE` 与版权声明
- 若通过网络提供服务，LGPL 的源码提供义务需评估
- 参考材料若含第三方资源，需保留其许可证与来源声明

## 5. 目录说明

| 路径 | 说明 |
|---|---|
| `backend/` | Go 后端（Ent + Gin），本次二开主体 |
| `frontend/` | Vue 3 前端 |
| `deploy/` | Docker Compose、部署脚本、`.env.example` |
| `docs/` | **上游文档**（`.gitignore` 只放行白名单，勿在此放我们的文档） |
| `project-docs/` | **本项目文档**（本目录） |
| `skills/sub2api-admin/` | 上游自带的管理 API CLI 技能 |

## 6. 相关文档

| 文档 | 内容 |
|---|---|
| [01-baseline.md](01-baseline.md) | 基线取证：版本、依赖、构建要求 |
| [03-dev-environment.md](03-dev-environment.md) | 开发环境与常用命令 |
| [04-git-workflow.md](04-git-workflow.md) | Git 分支模型、提交/PR/合并、撤销恢复、行尾属性、本地门禁（配套 `tools/pre-push-check.ps1` 与 `.github/PULL_REQUEST_TEMPLATE.md`） |
| [05-deploy-docker.md](05-deploy-docker.md) | Docker 部署手册 |
| [08-runbook.md](08-runbook.md) | 运维手册 |
| [11-collaboration-norms.md](11-collaboration-norms.md) | 协作规范与质量门 |
| [12-worktree-strategy.md](12-worktree-strategy.md) | 并行开发与 worktree 策略 |
| [14-prd-techspec-template.md](14-prd-techspec-template.md) | PRD 与技术规格模板 |
| [用户使用手册.md](用户使用手册.md) | 站点用户手册（计费口径、充值兑换、峰谷、视频任务页） |
| [管理员使用手册.md](管理员使用手册.md) | 管理员手册（价格体系、缺价红线、调度、对账排障） |
| [上游同步说明.md](上游同步说明.md) | 上游 0.2.17（133 提交）同步的审计、验证清单与决策项 D1–D7 |
| [architecture/README.md](architecture/README.md) | 架构侦察索引（含架构图） |
| [22-repository-status-2026-10-19.md](22-repository-status-2026-10-19.md) | 仓库状态与全量审计执行记录（2026-10-19） |
| [23-handoff-security-remediation-2026-10-30.md](23-handoff-security-remediation-2026-10-30.md) | **安全整改交接（单份自包含）**：本轮结论、验收状态、9 项待办与人工事项；接手同事的开工入口 |
| [24-state-verification-2026-11-08.md](24-state-verification-2026-11-08.md) | 对 `22-*` 状态快照的逐条实证核实：6 条断言中 5 条一致、1 条快照过时；每条附命令与原始输出 |
| [reviews/2026-11-01-pr18-review/](reviews/2026-11-01-pr18-review/) | PR #18 复审原始证据、三项修复与变异/集成验收记录 |
| [reviews/2026-11-08-pr19-review/](reviews/2026-11-08-pr19-review/) | PR #19 评审：F-05/F-04 公共契约、I-01b SMTP 拒绝发码的发现与 Lead 汇总裁决 |

> 交接与核实文档按轮次累加：`23-*` 来自安全整改，`24-*` 是状态快照的核实报告；两份使用手册（用户 / 管理员）也在本目录下。

---

## 7. 当前 PR 状态（2026-11-10 用 `gh pr list --repo AnBooting/sub2api` 实测）

> **读文档前先看这张表**：**当前没有 OPEN PR**（`gh pr list --state open` 返回 `[]`）。
> 标 **MERGED** 的都已进入 `develop`，但**不等于已上线**——上线状态单看 §7 末尾那条部署结论；
> 标 **CLOSED（未合入）** 的是关掉的历史行，任何文档都不得把它们写成"已上线"。

| PR | 标题 | 状态 | 说明 |
|---|---|---|---|
| #11 | chore(sync): merge upstream v0.2.17 (133 commits) into develop | **MERGED（2026-10-28）** | 上游 0.2.17 同步 + 本轮修掉的四个缺陷 + 新增能力 + 三轮评审材料与审计台账入库。审计、验证清单与 D1–D7 决策状态见 [上游同步说明.md](上游同步说明.md) §3.8。**2026-10-21 复审第 1 项（P1：改名迁移在旧库上重放导致启动失败）已修**：登记改名迁移 + 旧文件校验和 + 允许的重复错误码，受限容错，迁移文件本身零改动 |
| #12 | audit: 全量审查修复（安全/健壮性/前端 API 类型）与审计文档 | **MERGED（2026-10-29）** | 已包含上游 0.2.17 同步（迁移文件与 #11 逐字一致）。**2026-10-21 复审第 2/3/4 项已修**：observer 自身收尾要交出 pending 重试、closed 标记不再阻断结算、失败审计与最终费用行分开，并补了冻结时长的封顶口径 |
| #13 | feat(product): 补齐 PRD 功能的用户/管理端可见性、单位口径与站内帮助页 | **MERGED（2026-10-29）** | 品牌额度标签、站内帮助页、以及本目录下的**两份手册**都在这个 PR 里；已包含上游 0.2.17 同步。**2026-10-21 复审第 5 项已修**：用户侧视频任务只给稳定错误码 + 可公开文案，内部诊断留在管理端 |
| #14 | build: move the Go toolchain to 1.27.3 | **MERGED（2026-10-21）** | go 指令 + 三个 workflow 的版本断言 + **三个 Dockerfile**（含根 Dockerfile）一起升到 1.27.3，解除 `backend-security` 的标准库公告 GO-2026-6809；新增契约测试把 go.mod 与所有构建入口的版本绑在一起。合并顺序上最先合 |
| #18 | fix(security): 转发头信任反转、限流账号维度与部署可验证性整改 | **MERGED（2026-11-08）** | 合并提交 `4a0064318`，进入 `develop`。含 PR #18 复审的三项修复（Compose 固定网络与网关信任、认证身份严格解析、SMTP GET/PUT 一致），见 [reviews/2026-11-01-pr18-review/](reviews/2026-11-01-pr18-review/)。**代码已合入但线上仍运行 `f9d8bc855`，尚未部署** |
| #19 | fix(security): 收敛用户响应字段并阻止 SMTP 未配置时发码 | **MERGED（2026-11-08）** | 作者 Akanyi，合并提交 `4965ca5e9`。对应交接文档 §3.1（F-05 用户面分组字段收敛、F-04 监控快照 config 白名单）与 I-01 后半（SMTP 未配置时拒绝发码）。评审结论 Approve / 0 Critical / 0 Required，证据见 [reviews/2026-11-08-pr19-review/](reviews/2026-11-08-pr19-review/) |
| #20 | docs(reviews): correct PR status after #18 merge and record PR19 review | **MERGED（2026-11-08）** | 状态文档订正 + PR #19 评审证据入库。做法是**直接快进推入 develop**（`4a0064318..7c2848bed`，项目规则允许直接推集成分支），GitHub 随即自动把 #20 标为 MERGED —— 不是通过 PR 页面合并 |
| #21 | fix(auth): localize SMTP-not-configured message and close PR19 review nits | **MERGED（2026-11-09）** | 评审方（非 #19 作者）收口 #19 的 5 条 Nit：`EMAIL_NOT_CONFIGURED` 的 zh/en 映射与可行动提示、两处过时注释/测试名、`IsConfigured` reason 口径、异步用例空转断言。作者 `yylovehhy`，**由 `Akanyi` 合并**，合并提交 `32ed7e6ee`（`--merge` 保留合并提交，base `develop` @ `29a0f0125`）；共 9 文件 +63/−6，**不含迁移**。合并后分支 `fix/pr19-review-nits-20261108` 已本地与远端双删 |
| #7 | feat(frontend): 暖色电影感设计体系（替换 Fluent 视觉层，保留组件结构与边界加固） | **CLOSED（未合入）** | ⚠️ **前端视觉重设计尚未上线**。它写在上游 0.2.17 同步之前，而本轮 PR 改动了同样的界面文件。分支 `feat/frontend-fluent-redesign-clean` 已于 2026-11-10 归档为标签 `archive/feat-fluent-redesign`（指向 `d52241fdb`，含 5 个提交）后从远端删除，**提交一个没丢**：`git switch -c feat/frontend-fluent-redesign-clean archive/feat-fluent-redesign` 即可恢复工作分支。重做前置条件写在 PR #7 的评论里。**任何文档都不得把它描述为已上线** |
| #6 | refactor(frontend): 迁移到 Fluent/Azure 设计体系并加固边界场景 | **CLOSED（未合入）** | 已被 PR #7 取代 |
| #1–#4 | 基线、CNY 计费与节假日、视频异步锁价、PRD 交付 | **MERGED** | 已进入 develop |

**合并顺序（已完成）**：**#14（10-21）→ #11（10-28）→ #12（10-29）→ #13（10-29）→ #18（11-08）→ #19（11-08）→ #21（11-09）**；#20（文档/证据）以直接快进入 develop 的方式完成。`origin/develop` 已含 #21 的合并提交 `32ed7e6ee`，本地 `develop` 与远端同点，此后只追加文档提交。#21 是纯代码/文案收口，未新增迁移，迁移文件在 #11/#12/#13 之间仍逐字一致，243–250 未复活。

**PR 编号沿革（避免后来人疑惑）**：PR 编号由 GitHub 按仓库自增分配，**不可选、不可复用**（关闭未合并的 PR 也永久占号）。因此 #20 是文档/证据 PR（已合入），#21 才是 Nit 收口 PR；两者顺序不代表重要性，只代表创建时间。

**当前没有开放 PR**（#21 已于 2026-11-09T10:50:38Z 合入）。**部署仍未发生**（2026-11-10 用 `tools/verify-deployed-build.ps1` 实测）：
线上 `https://sub2.lansn.icu/` 的 `/api/v1/settings/public` 返回值里的 version 是 `0.2.17-fork.f9d8bc855`，
与本地 `develop` 顶端不一致（探测时 HEAD 为 `32ed7e6ee`），脚本报 `[DRIFT]` 并以**退出码 1** 结束——即 #18/#19/#21 的代码都还没上线，线上仍是 PR #18 之前的基线。
同一次探测还确认：线上首页 HTML 内联的 `__APP_CONFIG__.version` 是**空串**（已知缺陷 D-02，修复在 `setting_public.go` + `Dockerfile`，需下次构建才生效）。
上线前的人工事项见 [23-handoff](23-handoff-security-remediation-2026-10-30.md) §4 与 [ACTION-ITEMS.md](reviews/2026-10-30-security-remediation/ACTION-ITEMS.md)。

## 8. 工作区与本地门禁（2026-11-10 收口）

| 项 | 现状 |
|---|---|
| 工作区 | `F:\sub2` 在 `develop`，与 `origin/develop` 同步（无领先/落后提交）；无残留 worktree；`git status --porcelain` 输出为**空**——两份本地杂物（见下两行）已写进 `.git/info/exclude`，文件仍在磁盘、不入库 |
| 本地分支 | 只剩 `develop` 与 `main`（镜像：与 `develop` 同一提交，只做快进，不在 `main` 上提交）；#21 的 `fix/pr19-review-nits-20261108` 已删 |
| 远端分支 | 只剩 `develop` 与 `main`（2026-11-10 清理完成）。原有三个「有内容但不在 develop 上」的分支——`feat/frontend-fluent-redesign-clean`（PR #7 关闭未合入，5 个提交）、`chore/go-toolchain-1.27.3`（1 个未合入的 docs 提交）、`backup/develop-merged-20261021`（合并前快照，2 个提交）——**先打注释标签再删**，提交无丢失（见下行「标签」与「恢复已删分支」） |
| 恢复已删分支 | 需要哪个就从标签拉回来：`git switch -c feat/frontend-fluent-redesign-clean archive/feat-fluent-redesign`（chore/backup 同理换成对应 `archive/*`），再 `git push -u origin <分支名>`。标签是注释标签，`git show <tag>` 能看到当初的归档说明 |
| 标签 | 本地 **72** 个（清理前 69 + 本次 3 个归档）、`origin` 现在 **7** 个（都经 2026-11-10 显式授权推送）：3 个取证锚 `baseline-fork-2026-10-09` / `baseline-upstream-v0.2.15` / `baseline-v0.2.15`、`prepush/pr12-pr13-merged`（#12/#13 的 squash 提交在本地**唯一**的引用）、3 个归档 `archive/feat-fluent-redesign` / `archive/chore-go-toolchain-1.27.3` / `archive/backup-develop-merged-20261021`。其余 **65** 个 `v0.x` 刻意不推：那是上游/基线的版本号命名空间，推到 fork 会让人误以为 fork 发布过这些版本 |
| main 同步 | 2026-11-10 把 `origin/main` 从 `ca8c14efc` 快进到 `develop` 顶（当时落后 **287** 个提交，`develop..main` = 0，纯快进、无合并提交）。规矩不变：`main` 永不直接提交，只从 `develop` 快进 |
| 本地杂物 | ① `9F91A156…jpg`（4.1 MB，用户放在仓库根的截图，非交付物）；② `project-skills/sub2api-dev-deploy/manifest.json`（技能生成器写的元数据，含 `generatedAt`/`fileCount`，仓库其他技能都没有这种文件）。两者都在 `.git/info/exclude` 里：不进历史，也不污染 `git status`；真要入库用 `git add -f <路径>` |
| 合并门禁 | CI 实测停摆（最近 run 2026-10-22）；以 `pwsh -File tools/pre-push-check.ps1 -Mode full` + PR 模板贴出的结果为准 |
| 新任务入口 | 先读 [04-git-workflow.md](04-git-workflow.md)；提 PR 用 [`.github/PULL_REQUEST_TEMPLATE.md`](../.github/PULL_REQUEST_TEMPLATE.md)；合并前跑门禁脚本 |

> 复用约定：门禁清单以 `04-git-workflow.md` §9 为单一事实源，`tools/pre-push-check.ps1` 是它的可执行入口；两者修改要同步。
