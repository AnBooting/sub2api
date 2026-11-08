# Docker 部署手册（草案）

> 状态：**草案**。生产服务器的信息尚未提供，本文件先固化「本地验证 → 生产部署」的流程与检查点。

## 1. 部署架构

上游官方 compose 提供三个容器：

| 容器 | 镜像 | 说明 |
|---|---|---|
| `sub2api` | **自建**（二开代码） | 应用主体，内嵌前端静态资源，监听 8080 |
| `sub2api-postgres` | `postgres:18-alpine` | 数据库，不对外暴露端口 |
| `sub2api-redis` | `redis:8-alpine` | 缓存/队列，AOF 持久化 |

数据落本地目录（`docker-compose.local.yml`）：`./data`、`./postgres_data`、`./redis_data`，
便于整体打包迁移。

## 2. 镜像策略

| 方案 | 说明 | 采用 |
|---|---|---|
| 用官方镜像 `weishaw/sub2api:latest` | **不含二开代码** | ❌ |
| 本地/CI 构建自建镜像 | 用仓库根目录 `Dockerfile` | ✅ |

Dockerfile 三阶段：`node:24-alpine` 构建前端 → `golang:1.27.2-alpine` 交叉编译后端（`-tags embed` 内嵌前端）→ `alpine:3.21` 运行时。

## 3. 本地验证流程（P3 阶段执行）

```bash
# 1. 准备部署目录
cd F:\sub2\deploy
cp .env.example .env

# 2. 生成必需密钥
openssl rand -hex 32     # JWT_SECRET
openssl rand -hex 32     # TOTP_ENCRYPTION_KEY
openssl rand -hex 32     # POSTGRES_PASSWORD
# 填入 .env；同时设置 ADMIN_EMAIL / ADMIN_PASSWORD

# 3. 构建自建镜像
cd F:\sub2
docker build -t sub2api-local:dev .

# 4. 起服务（把 compose 里的 image 指向 sub2api-local:dev）
docker compose -f deploy/docker-compose.local.yml up -d

# 5. 健康检查
curl http://localhost:8080/health
docker compose -f deploy/docker-compose.local.yml ps
```

### 验收点
- [ ] 三容器 `healthy`
- [ ] `/health` 返回 200
- [ ] 能用 `.env` 里的管理员账号登录后台
- [ ] 国模分组/账号功能可用（P1 完成后）
- [ ] 一次真实上游调用成功，并在账本里核对扣费
- [ ] `docker compose down` 后数据仍在，`up` 后服务恢复

### 陈旧 dist 陷阱（本地构建前必读）

`backend/internal/web/dist/` 是 `.gitignore` 掉的**构建产物**，仓库里不存在"正确版本"。
Dockerfile 会自己跑 `pnpm build` 再 `COPY` 进后端阶段，所以**容器构建不受影响**；
受影响的是**本地** `go build -tags embed`：

```text
本地 dist 是 2026-10-21 的旧产物（index--depgilV.js）
线上是 index-DEkhF0Ff.js
→ 直接 go build -tags embed 会内嵌旧前端，二进制的后端是新的、前端是旧的
→ 现象：功能"改完没生效"，但 /api/v1/settings/public 的 version 显示新提交
```

正确顺序（前端产物必须是最新的）：

```powershell
cd F:\sub2\frontend
corepack pnpm@9 run build            # 产物直接写进 ../backend/internal/web/dist
cd F:\sub2\backend
go build -tags embed -o bin\server.exe ./cmd/server
```

判断 dist 是否陈旧：`tools/verify-deployed-build.ps1` 会把 dist 的 `index.html`
时间与"前端源码最后一次提交时间"比较，早于源码就标 `[DRIFT] 本地 HEAD ... dist 陈旧`。

## 4. 生产部署流程（P4 阶段，等服务器信息）

### 前置信息（**尚未提供，不做假设**）
1. SSH 用户名 / 密码或密钥
2. 服务器 IP / 域名
3. 操作系统与版本、架构（amd64 / arm64）
4. 是否已装 Docker 与 Compose 版本
5. 是否已有存量数据（是否为全新部署）
6. 端口规划（是否用 80/443、是否走反向代理）
7. TLS：用 Caddy 自动证书（上游带 `Caddyfile`）还是自有证书

### 流程
```
只读探测远端环境 → 备份（若有存量）→ 传输 compose 与 .env → 拉取/导入镜像
→ 起服务 → 迁移自动执行 → 健康检查 → 端到端验证 → 记录回滚点
```

### 发布后自检

每次发布后跑一次部署一致性校验，把"到底部署没"变成机器判定，而不是看界面猜：

```powershell
cd F:\sub2
pwsh -NoProfile -File tools\verify-deployed-build.ps1
if ($LASTEXITCODE -ne 0) {
    # 0 = 强判据通过；1 = 强判据漂移；2 = 取数失败（都按发布失败处理）
    Write-Error "部署校验未通过（exit=$LASTEXITCODE），执行回滚"
    # docker compose -f deploy/docker-compose.local.yml down
    # docker tag sub2api:<上一个可用 tag> sub2api:current
    # docker compose -f deploy/docker-compose.local.yml up -d
}
```

#### 强判据与弱判据（为什么不是"hash 全相等才算过"）

| 级别 | 判据 | 是否决定退出码 | 说明 |
|---|---|---|---|
| **强判据** | 线上 `/api/v1/settings/public` 的 `version` 解析出的 commit == 本地 `git HEAD` | ✅ 是 | 版本号由构建期 ldflags 注入，与提交一一对应，是"线上跑的是不是这个提交"的可信证据 |
| **弱判据** | 线上入口 hash == 本地 dist 入口 hash | ❌ 默认否 | vite 产物 hash 受构建环境影响（node/pnpm 版本、模块排序、时间戳注入），**同一提交在同机重建也会不同 hash**（本项目实测：线上 `index-DEkhF0Ff.js` vs 本地 `index-BbceXYHS.js`，同一 HEAD）。hash 不同只能证明"不是同一次构建"，**不能**证明"线上构建自不同提交" |

需要把弱判据也当失败条件时（例如严格要求"线上就是这次构建的产物"）加 `-StrictDistHash`。

**退出码语义（对外契约，改脚本时不要改）**：

只有两个退出码，但 `0`/`1` 各自覆盖多个分支。逐分支列出，便于对日志定位：

| 退出码 | 分支 | 触发条件 | 输出的行 | 处置 |
|---|---|---|---|---|
| **0** | 0-a 全绿 | 强判据通过 + 弱判据 hash 相同 + 内联版本一致 | 四行全 `[OK]` | 通过 |
| **0** | 0-b 强判据通过但弱判据弱 | 强判据通过，hash 不同（默认模式）或内联版本为空/不同 | 含 `[INFO]`/`[WARN]` | **通过**，但必须肉眼确认 `[WARN]` 行（现状即此分支：D-02 未构建） |
| **0** | 0-c 本地无 dist | 强判据通过，`backend/internal/web/dist` 缺失，弱判据跳过 | 前端行为 `[INFO] 本地 dist 缺失` | 通过；但要意识到本地无法做 `-tags embed` 验证 |
| **1** | 1-a 后端提交漂移 | `version` 解析出的 commit != 本地 HEAD | `[DRIFT] 线上后端` | 发布失败 -> 回滚 |
| **1** | 1-b 后端版本缺失/不可解析 | `version` 为空串，或不匹配 `<版本>-fork.<短提交>` | `[DRIFT] 线上后端 : version 为空串 / 无法解析出提交` | 检查 `-X main.Version` 注入链 |
| **1** | 1-c 严格模式 hash 不符 | 加了 `-StrictDistHash` 且 hash 不同 | `[DRIFT] 线上前端（-StrictDistHash 已开启）` | 仅严格发布门禁使用 |
| **2** | 2-a 取数失败 | 网络不通 / 非 200 / 返回体无 `data` | `FAIL 取 /api/v1/settings/public 失败` 等 | **无资格判断漂移**，先修取数 |
| **2** | 2-b HTML 不可解析 | 首页 200 但找不到 `/assets/index-*.js` | `FAIL 首页 HTML 里找不到 ... 入口脚本` | 不是内嵌前端构建，或被反代改写 HTML |
| **2** | 2-c 前置缺失 | 不是 git 工作树 / 仓库根不存在 | `FAIL ... 不是 git 工作树` | 修调用参数（`-RepoRoot`） |

四行结论分别对应：**线上后端**（强判据）、**线上前端**（弱判据）、**内联版本**
（`window.__APP_CONFIG__.version`）、**本地 HEAD**（含本地 dist 陈旧性）。

`[WARN]` / `[INFO]` 行不影响退出码，但必须人工看一眼——它正是"静默通过"的反面。
两个典型例子：

```text
[INFO] 线上前端 : index-DEkhF0Ff.js != 本地 dist index-BbceXYHS.js（弱判据：hash 不同只说明不是同一次构建，不能证明线上构建自不同提交）
[WARN] 内联版本 : HTML 内联 version 为空串，前端首屏拿不到版本（缺陷 D-02；...）
```

常用参数：

```powershell
pwsh -File tools\verify-deployed-build.ps1 -BaseUrl https://sub2.lansn.icu   # 默认站点
pwsh -File tools\verify-deployed-build.ps1 -LocalDistRoot <某次构建产物目录>  # 指定要比对的本地 dist
pwsh -File tools\verify-deployed-build.ps1 -StrictDistHash                   # 弱判据也当失败条件
```

### 版本号是怎么进二进制的

**只有一个版本变量**：`main.Version`。所有输出都从它派生：

```text
-X main.Version
   └─> cmd/server 的包级变量 Version
        └─> main() 构造 handler.BuildInfo{Version}
             ├─> handler.BuildInfo.Version        -> /api/v1/settings/public 的 version
             └─> provideServiceBuildInfo()        -> service.BuildInfo.Version
                  └─> ProvideSettingService()     -> SettingService.SetVersion()
                       └─> injectedVersion()      -> HTML 内联 __APP_CONFIG__.version
```

| 注入点 | 出现位置 | 谁读 |
|---|---|---|
| `-X main.Version` | `cmd/server` 的包级变量 | CLI banner、`/api/v1/settings/public`、HTML 内联 version（经 BuildInfo 传递） |
| `ENV VITE_APP_VERSION` | 前端构建期环境变量 | 打包指纹（不参与 UI 展示） |

> ⚠️ **不要为 service 层再加第二个 `-X`**。历史设计曾在 `internal/service` 放一个
> `serviceBuildVersion` 包级变量，靠两个 Dockerfile 各传一处 `-X` 才非空。后果是
> "两个变量必须永远同步"——而实际有五条构建路径只传 `-X main.Version`
> （`backend/Makefile`、`deploy/Makefile`、`.goreleaser.yaml`、`.goreleaser.simple.yaml`、
> 任何手敲的 `go build -ldflags`），它们产出的二进制 HTML 里 version 恒为空，
> 而 `/api/v1/settings/public` 却正常，症状极难发现。
> 已改为经 `handler.BuildInfo` 单向下传，单一事实源。

版本取值优先级：`--build-arg VERSION` > 精确 git tag > `backend/cmd/server/VERSION` 文件；
三者都取不到时回落 `0.0.0-unknown`，**空串不会进入版本链**（两个 Dockerfile 都有
`if [ -z ... ]` 兜底，`resolve-version.sh` 在无 tag 的检出上返回 VERSION 文件内容）。

校验这条链是否健康：发布后 `verify-deployed-build.ps1` 的「内联版本」行应显示
`== /api/v1/settings/public version`。

### 回滚
```bash
docker compose -f docker-compose.local.yml down
docker tag sub2api:<上一个可用 tag> sub2api:current
docker compose -f docker-compose.local.yml up -d
# 数据库回滚：pg_restore 部署前的 pg_dump 备份
```

## 5. 备份策略

| 对象 | 方式 | 频率 |
|---|---|---|
| PostgreSQL | `docker exec sub2api-postgres pg_dump -U sub2api sub2api > backup.sql` | 每日 |
| 应用数据 | `./data` 目录打包 | 每日 |
| Redis | AOF 已开启，`./redis_data` 打包 | 可选 |
| `.env` | **单独安全保存**（含密钥，不进 git） | 变更时 |

## 6. 待办

- [ ] 确认生产服务器信息后补全本节
- [ ] 编写 `scripts/deploy-remote.sh` 与 `scripts/rollback.sh`
- [ ] 确定镜像仓库（本地导入 / 私有 registry / GHCR）
