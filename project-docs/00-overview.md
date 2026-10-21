# Sub2API 二开项目 — 总览

> 状态：**开发前准备阶段**（尚未开始功能开发）
> 最后更新：2026-10-10

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
| [04-git-workflow.md](04-git-workflow.md) | Git 分支模型与操作规范 |
| [05-deploy-docker.md](05-deploy-docker.md) | Docker 部署手册 |
| [08-runbook.md](08-runbook.md) | 运维手册 |
| [11-collaboration-norms.md](11-collaboration-norms.md) | 协作规范与质量门 |
| [12-worktree-strategy.md](12-worktree-strategy.md) | 并行开发与 worktree 策略 |
| [14-prd-techspec-template.md](14-prd-techspec-template.md) | PRD 与技术规格模板 |
| [用户使用手册.md](用户使用手册.md) | 站点用户手册（计费口径、充值兑换、峰谷、视频任务页） |
| [管理员使用手册.md](管理员使用手册.md) | 管理员手册（价格体系、缺价红线、调度、对账排障） |
| [上游同步说明.md](上游同步说明.md) | 上游 0.2.17（133 提交）同步的审计、验证清单与决策项 D1–D7 |

> **两份手册随 PR #8 一起进入 develop**，在 PR #8 合入之前，上面指向它们的两条链接在 develop 上暂时不可达（文件本身在 PR #8 的分支上）。
| [architecture/README.md](architecture/README.md) | 架构侦察索引（含架构图） |

---

## 7. 当前 PR 状态（2026-10-21 用 `gh pr list --repo AnBooting/sub2api` 实测）

> **读文档前先看这张表**：本轮的功能 PR 与工具链 PR 已全部合入 develop；下面标 **CLOSED（未合入）** 的**尚未上线**，文档里不要把它们写成"已上线"。

| PR | 标题 | 状态 | 说明 |
|---|---|---|---|
| #10 | build: move the Go toolchain to 1.27.3 | **MERGED** | go 指令、三个 workflow 的版本断言与两个 Dockerfile 一起升到 1.27.3，解除 `backend-security` 的标准库公告 GO-2026-6809 |
| #9 | chore(sync): merge upstream v0.2.17 (133 commits) into develop | **MERGED** | 上游 0.2.17 同步。审计、验证清单与 D1–D7 决策状态、以及本轮追加的修正与新能力见 [上游同步说明.md](上游同步说明.md) §3.8 |
| #8 | feat(product): 补齐 PRD 功能的用户/管理端可见性、单位口径与站内帮助页 | **MERGED** | 品牌额度标签、站内帮助页，以及本目录下的**两份手册**都在这个 PR 里；已包含上游 0.2.17 同步 |
| #5 | audit: 全量审查修复（安全/健壮性/前端 API 类型）与审计文档 | **MERGED** | 已包含上游 0.2.17 同步（迁移文件与 #9 逐字一致）。合并顺序：#9 → #5 → #8（已按此执行） |
| #7 | feat(frontend): 暖色电影感设计体系（替换 Fluent 视觉层，保留组件结构与边界加固） | **CLOSED（未合入）** | ⚠️ **前端视觉重设计尚未上线**。它写在上游 0.2.17 同步之前，而本轮三条 PR 已改动同样的界面文件；分支 `feat/frontend-fluent-redesign-clean` **完整保留**，重做的前置条件写在 PR #7 的评论里。**任何文档都不得把它描述为已上线** |
| #6 | refactor(frontend): 迁移到 Fluent/Azure 设计体系并加固边界场景 | **CLOSED（未合入）** | 已被 PR #7 取代 |
| #1–#4 | 基线、CNY 计费与节假日、视频异步锁价、PRD 交付 | **MERGED** | 已进入 develop |
