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

## 8. 限流绕过自检（转发头信任模型）

### 8.1 固定网络与两步切换

两个 Compose 默认固定 `sub2api-network=172.30.18.0/29`、网关 `172.30.18.1`。
同宿主 Caddy 通过端口映射访问 Sub2API 时，应用只看到该网关，因此
`SERVER_TRUSTED_PROXIES=172.30.18.1/32`；不要信任整个 `/29`、其他 Docker 网段或
EdgeOne 回源段。若 Caddy 容器直连 `sub2api` 服务名，对端是 Caddy 容器而非网关，
必须固定其 `ipv4_address` 并只信该 `/32`。固定网段冲突时，Compose subnet、gateway
和 trusted CIDR 必须一起更换。

既有实例第 1 步保持 `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=true`，先观察
Caddy 与应用的 client IP；第 2 步再把 env 和后台数据库设置同时改为 `false`。
数据库已有值优先于 env。回滚第 2 步时先在后台恢复 `true`，再同步 env；需要应用
env 变更时运行 `docker compose up -d --force-recreate sub2api`，不能使用只会复用
旧容器 env 的 `docker compose restart`。保留源站防火墙与网关 `/32` 信任。

在有 Docker 的主机上，配置后必须核对实际生效值，不能只用静态文件推断 DNAT 对端：

```bash
docker compose -f deploy/docker-compose.yml config
docker network inspect <项目名>_sub2api-network --format '{{json .IPAM.Config}}'
docker inspect sub2api --format '{{range $k,$v := .Config.Env}}{{println $v}}{{end}}' | grep '^SERVER_TRUSTED_PROXIES='
```

期望 IPAM 为 `172.30.18.0/29` / `172.30.18.1`，应用 env 仅含 `172.30.18.1/32`。
没有 Docker 的环境只能做静态契约和应用层模拟，不能据此声称真实 Caddy/DNAT 链路已验证。

### 8.2 限流检查

改过 `SERVER_TRUSTED_PROXIES` / `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL`、
换过 Caddyfile、或换过 CDN 之后，**必须**跑完这 5 条。它们只读，不写业务数据。

判断标准只有一句话：**轮换伪造的转发头，限流阈值不能跟着变**。
如果换了 `X-Real-IP` 就能多刷 N 次，说明伪造头被采信，加固无效。

前置：把 `$BASE` 换成本站地址；`$N` 取一个比目标端点阈值大的数（下文按
login=20/min、`/api/v1/settings/public` 的 public_ip_rpm 默认 300/min 说明；
实际阈值以管理后台「安全设置 → 面板限流」为准）。

```bash
BASE=https://sub2.lansn.icu
```

### 1. 伪造 X-Real-IP 轮换打登录接口 → 第 21 次必须 429

login 阈值是 20 次/分钟/（IP 或账号维度）。每次换一个假的公网 IP：

```bash
for i in $(seq 1 25); do
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    -H "X-Real-IP: 203.0.113.$i" \
    -H "X-Forwarded-For: 203.0.113.$i" \
    -d "{\"email\":\"nobody$i@example.invalid\",\"password\":\"wrong-password-$i\"}")
  echo "$i $code"
done
```

**期望**：前 20 次是 `400/401/404` 之类的业务错误，**第 21 次起出现 `429`**，
且后续持续 429（`Retry-After` 头给出剩余秒数）。
**失败形态**：25 次全部不是 429 —— 说明每个假 IP 都被当成独立客户端，
即伪造头被采信。

注意：即使 IP 维度被绕过，账号维度（同 email）仍应独立命中 429；
用**同一个** email 配不同假 IP 再跑一遍，同样必须在第 21 次前看到 429。
（账号维度是主防线：即使所有 IP 坍缩进同一个桶，按账号限流仍然生效。）

### 2. 伪造头必须不影响公开接口的 IP 桶

`/api/v1/settings/public` 走 `panelRateLimiter.PublicIP()`。用一个固定的假 IP
连打 `public_ip_rpm + 5` 次：

```bash
for i in $(seq 1 310); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/settings/public" \
    -H 'X-Real-IP: 198.51.100.7' -H 'X-Forwarded-For: 198.51.100.7')
  [ "$code" = "429" ] && { echo "first 429 at #$i"; break; }
done
```

**期望**：在 `public_ip_rpm` 附近（默认 300）出现第一次 429。
**失败形态**：跑到 310 次都没有 429 —— 桶没生效（通常是客户端 IP 被解析成
内网/回环地址，PublicIP 中间件会跳过计数，日志里有
`panel public rate limit is inactive because the client IP is not publicly routable`
的 WARN）。

### 3. 反向验证：不带伪造头也要同样被限

同 2 但不带任何 `X-Real-IP` / `X-Forwarded-For`。两次的"首次 429 位置"
必须基本一致（同一分钟内跑，或分别在不同分钟跑）。

**判据**：带伪造头与不带伪造头，首次 429 的序号差不超过个位数。
差得很大（例如不带头第 300 次 429、带头 310 次都不 429）就是被绕过了。

### 4. 应用侧的客户端 IP 与 Caddy 日志一致

```bash
# 线上：Caddy JSON 日志里 remote_ip 应落在 EdgeOne 回源网段内，client_ip 应为真实客户端
jq -r 'select(.client_ip != null) | [.request.remote_ip, .client_ip] | @tsv' \
  /var/log/caddy/sub2api.log | tail -20
```

**期望**：`remote_ip` 全部落在 EdgeOne 回源网段；`client_ip` 是你自己的公网地址，
且**不等于** `remote_ip`。
**失败形态**：两者恒等且都是 EdgeOne 地址 → Caddy 的 `trusted_proxies` 没生效
（占位符网段没替换，是最常见的原因）。

### 5. 确认兼容模式确实关掉了

```bash
curl -s "$BASE/api/v1/settings/public" | jq '.data.version'   # 顺带确认服务正常
```

然后在管理后台「安全设置」确认 `trust_forwarded_ip_for_api_key_acl` 为 **false**。

- 两种模式都先校验 `server.trusted_proxies` 中的直连对端；`true` 保留旧转发头
  优先级和自定义头，`false` 使用 Gin 的可信代理解析链。非受信对端在两种模式下
  都不能靠伪造头换桶，但仍应关闭兼容模式并限制源站直连以完成加固。
- **DB 优先于 env**：数据库里已持久化 `true` 的实例，只设
  `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false` 不会生效，必须在后台关掉。

### 附：源站不可直连（第 0 道防线）

在 EdgeOne 网段之外的机器上直连源站 IP 的 80/443，必须超时/拒绝。能连通就说明
攻击者可以完全跳过 EdgeOne 与 Caddy，直接向应用发伪造头，上面 5 条全部失去意义。

```bash
timeout 5 curl -sv --resolve sub2.lansn.icu:443:<源站IP> https://sub2.lansn.icu/health
```

## 9. EdgeOne 回源网段怎么取、怎么换、怎么验

`deploy/Caddyfile.edgeone` 里的回源网段是**占位符**（RFC 5737 / RFC 3849 文档网段），
仓库里不硬编码真实网段——它 3–6 个月轮换一次，写死只会更快过期。

### 9.1 怎么取

1. 登录腾讯云控制台，进「边缘安全加速平台 EO」。
2. 服务总览 -> 选中站点 -> 站点详情。
3. 左侧 **安全防护 -> 源站防护**，页面会显示当前回源 IP 列表（含 IPv4 与 IPv6）。
4. 复制**全部**网段（IPv4 + IPv6 都要，只抄一半会导致 IPv6 用户全部折叠成一个 IP）。

自动化（可选项，避免漏通知）：调用「查询源站防护详情」接口，每 3 天一次，
比对返回里的 `Version` 字段；`NextOriginACL` 非空即表示有新网段待更新。

### 9.2 轮换后改哪里

**唯一需要改的地方**是可搜索标记那一行：

```bash
grep -n REPLACE_WITH_EDGEONE_ORIGIN_CIDRS deploy/Caddyfile.edgeone
# 74:  trusted_proxies static 198.51.100.0/24 2001:db8:beef::/48 # REPLACE_WITH_EDGEONE_ORIGIN_CIDRS
```

只替换地址部分，**保留行尾标记**（`caddy validate` 支持行尾 `#` 注释，已实测）：

```caddyfile
trusted_proxies static <新网段1> <新网段2> ... # REPLACE_WITH_EDGEONE_ORIGIN_CIDRS
```

同时要改的还有两处（漏改会退化成"配得太松"或"全站 429"）：

| 位置 | 改什么 | 漏改后果 |
|---|---|---|
| 源站防火墙 / 安全组 | 入站只放行新网段访问 80/443 | 旧网段删掉后**全部回源失败**（站点直接不可用） |
| 应用侧 `SERVER_TRUSTED_PROXIES` | **不要动**，它填的是 Caddy 地址，不是 EO 网段 | 误填 EO 网段 -> 对端是 Caddy 不受信 -> 全站 429 |

顺序很重要：**先加新网段到防火墙**，再 reload Caddy，最后删旧网段。反过来会丢流量。

### 9.3 改完怎么验证

**第 1 步：Caddy 语法与信任配置生效**

```bash
caddy validate --config /etc/caddy/Caddyfile.edgeone --adapter caddyfile
# 确认运行态真的带上了新网段
curl -s localhost:2019/config/ | jq '.apps.http.servers.srv0.trusted_proxies'
```

**第 2 步：日志里 client_ip 是真客户端、remote_ip 是 EO**

```bash
jq -r '[.request.remote_ip, .client_ip] | @tsv' /var/log/caddy/sub2api.log \
  | sort | uniq -c | sort -rn | head -20
```

判据：

- `remote_ip` 必须落在**新**网段内；
- `client_ip` 必须**不等于** `remote_ip`，且是不同的公网地址（真实用户各不相同）；
- 若所有行 `client_ip == remote_ip` 且都是 EO 地址 -> 网段没生效（占位符没换，或 reload 失败）。

**第 3 步：带伪造头的限流对照（端到端证明没被绕过）**

这是最关键的一条——它同时验证 Caddy 信任、应用信任、限流维度三层：

```bash
# 3a. 伪造 X-Real-IP 轮换打登录，第 21 次必须 429
for i in $(seq 1 25); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST https://sub2.lansn.icu/api/v1/auth/login \
    -H 'Content-Type: application/json' \
    -H "X-Real-IP: 203.0.113.$i" -H "X-Forwarded-For: 203.0.113.$i" \
    -d '{"email":"nobody@example.invalid","password":"x"}'
done | sort | uniq -c
# 期望：出现 429（前 20 次业务错误，第 21 次起 429）
```

```bash
# 3b. 对照：把伪造值固定在同一个假 IP 上，必须更早触发（证明按 IP 计数真的在用）
for i in $(seq 1 25); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST https://sub2.lansn.icu/api/v1/auth/login \
    -H 'Content-Type: application/json' \
    -H 'X-Real-IP: 203.0.113.200' -H 'X-Forwarded-For: 203.0.113.200' \
    -d '{"email":"nobody@example.invalid","password":"x"}'
done | sort | uniq -c
```

判据：**3a 与 3b 的 429 出现位置必须一致**（都在第 21 次附近）。
如果 3a 一直不 429 而 3b 很快 429 —— 说明轮换伪造头成功了，即伪造头被采信，
必须回到 9.2 检查 `trusted_proxies` 与应用的 `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL`。

**第 4 步：部署一致性（确认线上跑的是你以为的那次构建）**

```powershell
cd F:\sub2
pwsh -NoProfile -File tools\verify-deployed-build.ps1
# exit 0 = 强判据通过；1 = 后端提交漂移；2 = 取数失败
# 注意 [WARN]/[INFO] 行不影响退出码，但要肉眼确认
```

> 发布后一键自检（PowerShell，失败即回滚）：
> ```powershell
> pwsh -NoProfile -File tools\verify-deployed-build.ps1; if ($LASTEXITCODE -ne 0) { Write-Error "部署校验失败 exit=$LASTEXITCODE，执行回滚" }
> ```

### 9.4 头语义回归检查（EO 是否开始覆盖 EO-Connecting-IP）

本仓库的 `client_ip_headers` 是 **`X-Forwarded-For` 在前、`EO-Connecting-IP` 兜底**。
这个顺序基于一条实测结论（详见 `deploy/Caddyfile.edgeone` 顶部注释）：

> Caddy 只取 `client_ip_headers` 里**第一个非空头**。腾讯云文档写明 EdgeOne
> "透传客户端的所有请求头部"，且从未承诺覆盖客户端自带的 `EO-Connecting-IP`。
> 实测（Caddy 2.10.2，同一输入 `EO=198.51.100.5(伪造)` + `XFF=203.0.113.66`）：
> EO 在前 -> `{client_ip}` = **198.51.100.5（采信伪造值，绕过成功）**；
> XFF 在前 -> `{client_ip}` = 203.0.113.66（取到真实值）。

**这条结论会随 EdgeOne 行为变化而失效**，所以每季度或每次 EO 侧变更后跑一次：

```bash
# 故意带一个假的 EO-Connecting-IP 打一次（只读接口，不产生业务数据）
curl -s -o /dev/null -H 'EO-Connecting-IP: 198.51.100.5' \
  https://sub2.lansn.icu/api/v1/settings/public

# 立刻看最新一条访问日志的 client_ip
jq -r 'select(.client_ip != null) | [.request.remote_ip, .client_ip] | @tsv' \
  /var/log/caddy/sub2api.log | tail -1
```

- `client_ip` 是**你自己的真实公网 IP**（不是 198.51.100.5）-> EO 未透传/已覆盖，
  当前顺序继续可用。
- `client_ip` 显示 **198.51.100.5** -> EO 改成了覆盖语义（或当前部署把 EO 头放在了
  第一位）。前者可以安全地把 `client_ip_headers` 调成 `EO-Connecting-IP X-Forwarded-For`；
  后者立刻改回来。

⚠️ 注意：如果哪天 `client_ip` 出现伪造值，**不要只改 Caddy 顺序就以为修好了**——
先确认 `remote_ip` 落在 EO 网段（第 2 步），否则问题在第一条防线（源站可直连）。

## 10. 安全基线

- [ ] `.env` 权限 600，不进版本库
- [ ] `POSTGRES_PASSWORD` / `JWT_SECRET` / `TOTP_ENCRYPTION_KEY` 均为 `openssl rand -hex 32` 生成
- [ ] 管理员邮箱**不用** `admin@example.com` 这类可猜值
- [ ] PostgreSQL / Redis **不暴露**宿主端口
- [ ] `security_opt: no-new-privileges:true`（compose 已含）
- [ ] 反代启用 TLS
- [ ] 定期备份并**验证可恢复**
