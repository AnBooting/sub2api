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
| [architecture/README.md](architecture/README.md) | 架构侦察索引（含架构图） |
