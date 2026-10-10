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
