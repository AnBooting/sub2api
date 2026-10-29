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

**启用 Live 时，执行以下升级命令前必须先完成下方的 Live 结算协议切换预检。**

```bash
docker compose pull          # 若用镜像仓库
docker compose up -d         # 重建容器；迁移随启动自动执行
docker compose logs -f sub2api
```

### Live 结算协议切换

- 旧版实例仍可能直接覆盖共享 Redis 中的结算状态，不能让新旧版本同时处理 Live 会话。切换前停止新建 Live 请求，并隔离新版 Live 流量；确认旧实例的现存 proxy、observer、到期兜底和结算重试均已结束后，才将 Live 流量切到新版。无法确认排空时，不要启用新版 Live。
- 在旧记录过期前核对 `live:call:*` 中尚待结算、已关闭但没有结算检查点，及日志确认状态不明的记录，与持久账本和用量记录逐项对账。旧版可能已留下同请求的零费用用量行；更早版本、非标准导入或损坏的记录若缺少必要字段，可能无法通过快照校验，需人工核对；尤其是缺价但已有正额结算快照的异常记录，须先隔离并对账，不能假设程序必然自动拒绝扣费。关闭标记或用量写入返回成功不证明账本一致，不要直接重复扣费或删除记录。
- Redis 无法确认首次冻结时，新版不会按未确认的金额扣费；自动重试次数有限，进程退出或故障持续可能留下未结算会话，当前没有持久化扫描器保证最终恢复。账本失败期间不会写正式用量行；若重试耗尽，该会话可能缺少请求数、时长和费用统计，Redis 快照过期后也可能丢失对账依据。应在过期前告警、保留证据并人工对账，不能依赖 TTL 或自动重试保证不漏账。
- 创建时记录的 TTL 为会话最长时长加 5 分钟；成功关闭或冻结后设为 24 小时，后续成功的冻结确认或状态更新会刷新该期限。故障时不能假设始终有 24 小时可对账。
- 旧结算快照未记录简易模式决策时，无法仅凭快照确定历史扣费口径。旧的未完成结算尚未核对前，旧、新实例的 `RunMode` 必须保持一致，不得切换模式；无法确认原模式或账实结果时，不要自动处理或恢复 Live 流量。

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
