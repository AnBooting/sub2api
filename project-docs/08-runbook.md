# 运维手册（草案）

> 待生产服务器确认后补全。当前为本地 Docker 部署的运维基线。

## 1. 服务拓扑

```
[用户/客户端] → :8080 (sub2api 容器)
                    ├─→ postgres:5432  (内部网络，不暴露)
                    └─→ redis:6379     (内部网络，不暴露)
```

## 2. 启停

```bash
cd <部署目录>

docker compose -f docker-compose.local.yml up -d      # 启动
docker compose -f docker-compose.local.yml ps         # 状态
docker compose -f docker-compose.local.yml stop       # 停止（保留容器）
docker compose -f docker-compose.local.yml down       # 停止并删除容器（数据保留）
docker compose -f docker-compose.local.yml logs -f sub2api   # 日志
```

## 3. 健康检查

| 检查 | 命令 | 期望 |
|---|---|---|
| 应用 | `curl -s http://localhost:8080/health` | HTTP 200 |
| 容器 | `docker compose ps` | 三个 `healthy` |
| 数据库 | `docker exec sub2api-postgres pg_isready -U sub2api` | `accepting connections` |
| Redis | `docker exec sub2api-redis redis-cli ping` | `PONG` |

## 4. 日志

| 来源 | 位置 |
|---|---|
| 容器 stdout | `docker compose logs` |
| 应用日志文件 | `./data/logs/`（取决于 `LOG_OUTPUT_TO_FILE`） |
| 管理员自动生成 | 首次启动日志中 `grep "Generated admin"` |

## 5. 备份与恢复

```bash
# 备份
docker exec sub2api-postgres pg_dump -U sub2api sub2api > backup-$(date +%F).sql
tar czf data-$(date +%F).tar.gz data/ postgres_data/ redis_data/ .env

# 恢复
docker compose down
docker exec -i sub2api-postgres psql -U sub2api -d sub2api < backup-2026-10-10.sql
docker compose up -d
```

## 6. 升级

```bash
docker compose pull          # 若用镜像仓库
docker compose up -d         # 重建容器；迁移随启动自动执行
docker compose logs -f sub2api
```

## 7. 故障排查

| 症状 | 排查 |
|---|---|
| 容器起不来 | `docker compose logs sub2api`；检查 `.env` 是否缺 `POSTGRES_PASSWORD` |
| 数据库连不上 | `pg_isready`；确认 `depends_on: service_healthy` 生效；应用有界重试 |
| 登录掉线 | `JWT_SECRET` 为空会导致每次重启换密钥 → 必须固定 |
| 2FA 失效 | `TOTP_ENCRYPTION_KEY` 为空同理 → 必须固定 |
| 反代后粘性会话失效 | Nginx 需加 `underscores_in_headers on;`（上游 README 明确） |
| 国模调用 404/400 | 核对 `base_url` 是否重复拼接 `/v1`；核对协议模式 |

## 8. 安全基线

- [ ] `.env` 权限 600，不进版本库
- [ ] `POSTGRES_PASSWORD` / `JWT_SECRET` / `TOTP_ENCRYPTION_KEY` 均为 `openssl rand -hex 32` 生成
- [ ] 管理员邮箱**不用** `admin@example.com` 这类可猜值
- [ ] PostgreSQL / Redis **不暴露**宿主端口
- [ ] `security_opt: no-new-privileges:true`（compose 已含）
- [ ] 反代启用 TLS
- [ ] 定期备份并**验证可恢复**
