# 基线事实（取证记录）

> 全部为本机实测或直接读取官方来源得到的事实，非推测。取证时间：2026-10-09 ~ 2026-10-10。

## 1. 上游仓库

| 项 | 值 | 来源 |
|---|---|---|
| 仓库 | `Wei-Shaw/sub2api` | GitHub API |
| 描述 | Sub2API 一站式开源中转服务，Claude / OpenAI / Gemini / Grok 订阅统一接入 | GitHub API |
| 默认分支 | `main` | GitHub API |
| 许可证 | LGPL-3.0 | GitHub API |
| Star / Fork | 43,568 / 9,340 | GitHub API |
| 最新 tag | `v0.2.15` | `git ls-remote` |
| 基线 commit | `3a6fd1c9db07203ca308aaba69e502bc1f35b307` | `git rev-parse HEAD` |
| 提交时间 | 2026-10-09（`chore: sync VERSION to 0.2.15 [skip ci]`） | `git log` |
| 活跃度 | 极高，几乎每天推送 | GitHub API `pushed_at` |

## 2. 本地检出

| 项 | 值 |
|---|---|
| 路径 | `F:\sub2` |
| 跟踪文件数 | 4,274 |
| 后端 Go 文件 | 2,882 |
| 前端 Vue 文件 | 316 |
| 迁移 SQL 文件 | 292（最新编号 **242**） |
| 仓库体积（不含 .git） | 55.7 MB |
| 历史深度 | 完整（7,568 commits，63 tags） |

## 3. 构建要求（来自上游源码）

| 组件 | 要求 | 来源 |
|---|---|---|
| Go | **1.27.2**（`go.mod` 声明；CI 硬断言 `go1.27.2`） | `backend/go.mod`、`.github/workflows/backend-ci.yml` |
| golangci-lint | **v2.14.0** | `backend-ci.yml` |
| Node.js | 20（CI）/ Dockerfile 用 node:24-alpine | `backend-ci.yml`、`Dockerfile` |
| pnpm | **9**（Dockerfile `corepack prepare pnpm@9`；CI `version: 9`） | `Dockerfile`、`backend-ci.yml` |
| PostgreSQL | 18-alpine（compose） | `deploy/docker-compose.local.yml` |
| Redis | 8-alpine（compose） | `deploy/docker-compose.local.yml` |
| 构建标签 | 单元测试 `-tags=unit`，集成测试 `-tags=integration`，构建 `-tags embed` | `backend/Makefile`、`Dockerfile` |

## 4. 本机环境（实测）

| 工具 | 状态 |
|---|---|
| Windows 11 家庭中文版 22631 | — |
| CPU | i7-12650H，10 核 16 线程 |
| 内存 | 15.6 GB（**2×8GB DDR5-4800，双通道**，2 槽位全占，最大支持 64GB） |
| 磁盘 | C/D/E/F 均为 NVMe SSD；F: 剩余 459.9 GB |
| git | 2.56.0 ✅ |
| Node.js | v24.18.0 ✅ |
| pnpm | 11.18.0（**需降级到 9**，见 03） |
| gh | 已登录 `yylovehhy`，scope 含 `repo` ✅ |
| **Go** | ❌ 原本缺失 → 已装便携版 `F:\tools\go1.27.2\go`（go1.27.2） |
| **Docker** | ❌ 缺失，安装包已下载（606 MB），**待手动安装**（需管理员 + WSL2 + 重启） |
| WSL | 已启用但**无发行版** |
| 虚拟化 | CPU 支持 VT-x / EPT / VMM（`VirtualizationFirmwareEnabled=True`） |
| make / jq / psql / 7z | ❌ 缺失（本地不阻塞；构建走 Go 与 pnpm） |
| 管理员权限 | ❌ 当前会话非管理员 |

## 5. 网络环境

| 项 | 值 |
|---|---|
| 代理 | Clash 混合端口 `127.0.0.1:7877`（系统代理已启用，`ProxyEnable=1`） |
| 直连 GitHub | ❌ 不稳定（`git fetch` 直连反复 `Connection was reset`） |
| 经代理 GitHub | ✅ 稳定（`git` 配置 `http.proxy=http://127.0.0.1:7877` 后一次成功） |
| Go 模块代理 | `GOPROXY=https://goproxy.cn,direct`（已验证可下载依赖） |

> **注意**：`F:\sub2` 的 git 已写入本地配置 `http.proxy=http://127.0.0.1:7877`。若代理端口变化，需同步修改，否则 fetch 会失败。

## 6. 上游现状与移植包的关系

- 上游**没有**国模功能：移植包 7 个 core 文件路径在上游全部 404，GitHub 代码搜索 `domestic_provider` = 0 命中。
- 上游**已有**近似概念 `cn_provider`（23 处命中）：用于国产厂商**余额检查 / 限流 / 调度**（`backend/internal/service/cn_provider_balance_service.go`、`ratelimit_cn_providers.go`、`domain/platforms.go`），与移植包的「分组厂商分类 + 定价命名空间」**不是同一件事**，但存在概念重叠，合并时必须核对命名与语义，避免两套并存的混乱。
- 上游前端已有 `frontend/src/utils/keyGroupProviders.ts`，其中已有 `domestic` 分类（用于 Key 分组筛选），与本移植包的 `domesticProviders.ts` 是**不同的东西**，需合并考虑。

## 7. 上游部署资产（已具备）

`Dockerfile`（多阶段：前端 → Go 交叉编译 → alpine 运行时，内嵌前端）、`deploy/docker-compose.yml`、`deploy/docker-compose.local.yml`（本地目录版，推荐生产）、`deploy/.env.example`、`deploy/docker-deploy.sh`、`deploy/install.sh`、`deploy/Caddyfile`、`deploy/DATAMANAGEMENTD_CN.md`。

**关键点**：官方镜像 `weishaw/sub2api:latest` **不含**我们的二开代码，二开必须自建镜像。
