# 02 · 调度与并发控制侦察

> 任务：task-2（只读侦察 sub2api 的账号调度与并发控制）
> 仓库：F:\sub2（分支 develop，基线 = AnBooting/sub2api main 0e6f5efb = 上游 v0.2.15 3a6fd1c9 + 2 个魔改提交）
> 产出：project-docs/architecture/02-scheduling.md

## 0. 状态 / 证据来源 / 未确认项

**状态**：已完成。本文件只做只读侦察，未修改 backend/、frontend/、deploy/ 下任何文件。

**证据来源**（全部为当前工作树实际内容）：

| 类别 | 路径 |
|---|---|
| 调度器 | backend/internal/service/gateway_scheduling.go、openai_account_scheduler.go、openai_gateway_scheduling.go、scheduler_cache.go、scheduler_snapshot_service.go、scheduler_outbox.go、scheduler_events.go |
| 并发 | backend/internal/service/concurrency_service.go、backend/internal/repository/concurrency_cache.go、backend/internal/handler/gateway_helper.go、concurrency_error_response.go、image_concurrency_limiter.go |
| 限流 | backend/internal/service/ratelimit_service.go、ratelimit_cn_providers.go、temp_unsched.go、account_scheduling_threshold_eval.go |
| 模型 | backend/ent/schema/account.go、user.go、account_group.go、group.go；backend/internal/service/account.go |
| 迁移 | backend/migrations/001_init.sql、020_add_temp_unschedulable.sql、036_scheduler_outbox.sql、062_*.sql、067_add_account_load_factor.sql、150_*.sql |
| 配置 | backend/internal/config/config.go、deploy/config.example.yaml |

**未确认项**（本文件内已逐条标注 ⚠️）：

1. gateway_scheduling.go:822-834 的 schedulingConfig() 兜底值（StickySessionWaitTimeout=45s）与 config.go:2556 的 viper 默认（120s）不一致；仅在 s.cfg == nil 时生效，生产路径未确认会走到。
2. 「部分账号不并发 / 部分账号并发过大」的**实际线上根因**未确认——本文只给出可排查位置与代码判据，不含现场数据（Redis 槽位计数、账号字段实值）。
3. account_groups.priority（分组内账号优先级）是否参与运行时调度打分，未在负载感知路径中找到消费点；调度排序使用 accounts.priority（openAIAccountSchedulingPriority，openai_account_scheduler.go:635）。⚠️ 未确认是否有其它路径消费。
4. scheduler_outbox 事件从写入到快照可见的端到端延迟未实测（配置默认 1s 轮询）。

---

## 1. 调度器全貌

调度分两条独立技术栈：**通用网关（Anthropic/Gemini/Grok 等）** 与 **[OI] 高级调度器**。两者共用同一个 ConcurrencyService 与同一套 Redis 槽位。

| 文件 | 行数 | 职责 |
|---|---|---|
| service/gateway_scheduling.go | 2476 | 通用 GatewayService 选号入口：SelectAccountWithLoadAwareness（:100）、三层调度（Layer1 模型路由 :276 / Layer1.5 粘性 :523 / Layer2 负载感知 :645 / Layer3 兜底排队 :779）、分层过滤与 LRU（:1589-:1720）、tryAcquireAccountSlot（:1181） |
| service/openai_gateway_scheduling.go | 1647 | [OI] 侧同构实现：SelectAccountWithLoadAwareness（:1129）、粘性命中 tryStickySessionHit（:955）、listSchedulableAccounts（:1500）、tryAcquireAccountSlot（:1532） |
| service/openai_account_scheduler.go | 2960 | [OI] **高级调度器**：default[OI]AccountScheduler.Select（:382）、粘性层 selectBySessionHash（:495）、打分计划 build[OI]AccountLoadPlan（:859）、TopK 抢槽 tryAcquire[OI]SelectionOrder（:1161）、负载均衡 selectByLoadBalance（:1400）、兜底 finishLoadBalanceSelectionFallback（:1668）、权重解析（:1886-:1978） |
| service/scheduler_cache.go | 110 | 调度快照缓存接口 SchedulerCache（:77）：SchedulerBucket{GroupID,Platform,Mode}（:47）、写令牌/退役/重开（:27-:45, :82-:94） |
| service/scheduler_snapshot_service.go | 1581 | 快照服务：ListSchedulableAccounts（:211，缓存未命中回源 DB 并回写）、outbox worker（:344）、全量重建（:359/:1046）、outbox 滞后检测（:1287） |
| service/scheduler_outbox.go / scheduler_events.go | 27 / 9 | outbox 事件模型与 6 类事件常量（account_changed / account_groups_changed / account_bulk_changed / account_last_used / group_changed / full_rebuild） |
| service/concurrency_service.go | 684 | 并发槽位门面（账号/用户/API Key/WS 租约/等待队列） |
| repository/concurrency_cache.go | 1138 | Redis ZSET 槽位实现（Lua 原子抢槽） |
| service/ratelimit_service.go | 2629 | 上游错误 → 限流/临时停调/阈值停调 |
| service/ratelimit_cn_providers.go | 273 | 国产供应商（kimi/zhipu/deepseek 等）响应式冷却 |
| service/temp_unsched.go | 40 | 临时不可调度状态模型与缓存接口 |
| service/account_scheduling_threshold_eval.go | 533 | 按平台用量窗口阈值判定是否自动停调 |

### 1.1 快照机制（决定「账号是否可见」）

- 快照按 SchedulerBucket = {groupID, platform, mode} 分桶（scheduler_cache.go:47-55），mode ∈ single|mixed|forced（:13-15）。
- 读取路径：SchedulerSnapshotService.ListSchedulableAccounts（:211）→ 命中 cache.GetSnapshot 直接返回（:222-230）；未命中则 CaptureBucketWriteToken（:231）→ DB 回源（:254）→ SetSnapshot（:263）。
- 账号变更通过 **outbox** 异步刷新：account_repo.go 中 enqueueSchedulerOutbox(..., SchedulerOutboxEventAccountChanged, ...) 出现 40+ 处（例如 :131、:499、:1472、:2788）；表结构见 migrations/036_scheduler_outbox.sql。
- outbox 轮询默认 1s、滞后 5s 告警、10s 强制重建（config.go:2569-2571）。
- ⚠️ 这意味着 **DB 改了账号字段到调度可见之间存在秒级延迟**；排查「改了并发数但没生效」时必须先排除这一层。

---

## 2. 账号选择算法

### 2.1 通用路径：三层调度

入口 GatewayService.SelectAccountWithLoadAwareness（gateway_scheduling.go:100）：

| 层 | 位置 | 逻辑 |
|---|---|---|
| 前置 | :112-141 | 读 schedulingConfig()；checkClaudeCodeRestriction 可能降级分组；从 Redis 读粘性绑定 GetSessionAccountID（:137） |
| Layer 1 | :276-521 | 模型路由（group.ModelRouting）命中时，**优先级高于粘性**；按「优先级 → 负载率 → LastUsedAt」排序后逐个抢槽（:460-498） |
| Layer 1.5 | :523-643 | 无模型路由时的粘性会话命中：tryAcquireAccountSlot(ctx, accountID, account.Concurrency)（:569），失败则返回 WaitPlan（:608） |
| Layer 2 | :645-777 | 负载感知：批量取负载 GetAccountsLoadBatch（:716）→ 过滤 LoadRate < 100（:730）→ 分层过滤 → LRU → 抢槽（:754） |
| Layer 3 | :779-793 | 全部满则返回 WaitPlan 兜底排队（FallbackWaitTimeout / FallbackMaxWaiting） |

**分层过滤顺序**（Layer 2 核心，:738-749）：

    1. filterByMinPriority   (:1589)  取 priority 最小集合（数值越小优先级越高）
    2. filterBySoonestReset  (:1632)  仅当 cfg.PreferSoonestReset=true
    3. filterByMinLoadRate   (:1609)  取 LoadRate 最低集合
    4. selectByLRU           (:1663)  取 LastUsedAt 最久远者（同分时随机打散）

若 concurrencyService == nil 或 !LoadBatchEnabled，退化为**纯优先级+LRU** 顺序抢槽（:164-212），此时**没有负载均衡**。

### 2.2 [OI] 高级调度器

入口 default[OI]AccountScheduler.Select（openai_account_scheduler.go:382），层级常量见 :22-28：

| 层 | Layer 值 | 位置 |
|---|---|---|
| previous_response_id 粘性 | previous_response_id | :396-436 |
| guardian_parent 影子母账号 | guardian_parent | :438-453 |
| session_hash 粘性 | session_hash | :455-470 → selectBySessionHash（:495） |
| 负载均衡 | load_balance | :472 → selectByLoadBalance（:1400） |

**打分公式**（build[OI]AccountLoadPlan，:1023-1030）：

    score = w.Priority     * priorityFactor
          + w.Load         * loadFactor        // 1 - clamp01(LoadRate/100)
          + w.Queue        * queueFactor       // 1 - clamp01(WaitingCount/maxWaiting)
          + w.ErrorRate    * errorFactor       // EWMA 错误率
          + w.TTFT         * ttftFactor        // EWMA 首 token 延迟
          + w.Reset        * resetFactor       // use-it-or-lose-it
          + w.QuotaHeadroom * quotaHeadroomFactor
          + w.UpstreamCost  * (upstreamCostFactor - 0.5)

粘性加权（:1031-1038）：req.StickyWeighted 时 previous_response 命中 +w.Previous，session_hash 命中 +w.SessionSticky。

默认权重（config.go:2494-2503）：priority 1.0、load 1.0、queue 0.7、error_rate 0.8、ttft 0.5、reset 0.0、quota_headroom 0.0、upstream_cost 0.0、previous_response **5.0**、session_sticky **3.0**。
→ 粘性权重远高于负载权重，这是「流量集中在少数账号」的**结构性原因之一**。

抢槽顺序：build[OI]SelectionOrder（:1054）取 TopK（lb_top_k，默认 7，config.go:2487）内按分数排序；tryAcquire[OI]SelectionOrderWithBudget（:1171）逐个抢，带**探测预算** openAIAccountSelectionProbeLimit = 64（:35，:322-338）。

### 2.3 粘性会话（sticky session）

| 项 | 证据 |
|---|---|
| 键与 TTL | gateway_service.go:36 stickySessionTTL = time.Hour；openai_gateway_service.go:35 openaiStickySessionTTL = time.Hour；可配 gateway.openai_ws.sticky_session_ttl_seconds（默认 3600，config.go:1351 / deploy/config.example.yaml:403） |
| 读写 | repository/gateway_cache.go:39 GetSessionAccountID；openai_sticky_compat.go:122/:154/:181/:202（读/写/刷新/删，含新旧 key 迁移回退） |
| 绑定时机 | 选号内 bindGatewayStickySessionDuringSelection（gateway_service.go:969）或利润门通过后 BindStickySessionAfterProfitAdmission（:982） |
| 会话 hash 生成 | openai_gateway_scheduling.go:164 GenerateSessionHash、:206 GenerateSessionHashWithFallback、:188 grokStickyAffinitySeed |
| 逃逸 | openai_account_scheduler.go:642 shouldEscapeStickyAccount：TTFT EWMA > sticky_escape_ttft_ms(默认 15000) 或错误率 > sticky_escape_error_rate(默认 0.5) 时跳过粘性（deploy/config.example.yaml:427-433） |
| 清理 | shouldClearStickySession（gateway_service.go:558）：账号被模型限流/不可调度时解除绑定 |

**粘性 = 同一会话固定落在同一账号**，因此单账号并发堆积是粘性的必然结果，不是 bug。

### 2.4 槽位（slot）机制

Redis ZSET，成员 = requestID，score = 时间戳：

| 键 | 常量 | 位置 |
|---|---|---|
| 账号 | concurrency:account:{id} | concurrency_cache.go:28 |
| 用户 | concurrency:user:{id} | :30 |
| API Key | concurrency:api_key:{id} | :32 |
| Live 租约（账号/用户/Key） | concurrency:live:{account,user,api_key}:{id} | :33-35 |
| WS 入站租约 | concurrency:openai_ws_ingress:api_key:{id} | :38 |
| 账号等待队列 | wait:account:{id} | :44 |
| 用户等待队列 | concurrency:wait:{id} | :42 |

**抢槽是单个 Lua 脚本原子完成**（concurrency_cache.go:71-107）：

    redis.call('ZREMRANGEBYSCORE', key, '-inf', expireBefore)   -- 清理过期槽位
    redis.call('ZREMRANGEBYSCORE', liveKey, '-inf', now - 60)   -- 清理过期 Live 租约
    local count = redis.call('ZCARD', key) + redis.call('ZCARD', liveKey)
    if count < maxConcurrency then ZADD ... return {1, now} end
    return {0, now}

要点：
- **Live 租约计入并发数**（:99），因此长期 WS 会话会占用账号并发额度（liveLeaseTTLSeconds = 60，:40）。
- 槽位 TTL：defaultSlotTTLMinutes = 15（:47），但配置默认 **30 分钟**（config.go:2548 gateway.concurrency_slot_ttl_minutes = 30，注释「支持超长请求」）。**槽位泄漏最长占位 30 分钟**。
- 后台清理 worker：ConcurrencyService.StartSlotCleanupWorker（concurrency_service.go:724），周期 gateway.scheduling.slot_cleanup_interval（默认 30s，config.go:2565）。
- 启动时清理遗留槽位：CleanupStaleProcessSlots（concurrency_service.go:212），按 requestID 前缀 RequestIDPrefix() 区分进程（:194-217）。

**负载率计算**（concurrency_cache.go:999-1002）：

    loadRate := 0
    if ac.maxConcurrency > 0 {
        loadRate = (currentConcurrency + waitingCount) * 100 / ac.maxConcurrency
    }

其中 maxConcurrency 来自调用方传入的 EffectiveLoadFactor()（见 3.1）。

---

## 3. 并发控制

### 3.1 账号级并发上限

**数据库列**：accounts.concurrency

| 层 | 位置 | 说明 |
|---|---|---|
| 建表 | migrations/001_init.sql:68 | concurrency INT NOT NULL DEFAULT 3 |
| ent schema | ent/schema/account.go:100-101 | field.Int("concurrency").Default(3)，注释「账户最大并发请求数」 |
| 新增负载因子 | migrations/067_add_account_load_factor.sql:1 | ALTER TABLE accounts ADD COLUMN IF NOT EXISTS load_factor INTEGER |
| ent schema | ent/schema/account.go:103 | field.Int("load_factor").Optional().Nillable() |
| Go 模型 | service/account.go:35 Concurrency int、:40 LoadFactor *int | |

**关键点：存在两个不同的数**

    // service/account.go:169-180
    func (a *Account) EffectiveLoadFactor() int {
        if a == nil { return 1 }
        if a.LoadFactor != nil && *a.LoadFactor > 0 { return *a.LoadFactor }
        if a.Concurrency > 0 { return a.Concurrency }
        return 1
    }

| 用途 | 使用哪个字段 | 证据 |
|---|---|---|
| **实际抢槽上限**（硬闸） | account.Concurrency | gateway_scheduling.go:177/:369/:483/:569/:754/:801；openai_account_scheduler.go:574/:1258/:1323；openai_gateway_scheduling.go:1161/:1231/:1397/:1436 |
| **负载率分母**（选号打分） | EffectiveLoadFactor() | gateway_scheduling.go:441/:712；openai_account_scheduler.go:1482/:1662；openai_gateway_scheduling.go:1317；repository/account_repo.go:2019；ops_concurrency.go:78；ops_metrics_collector.go:431 |
| **WaitPlan.MaxConcurrency** | account.Concurrency | openai_account_scheduler.go:590/:1691 明确注释：「WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待」 |

**这是最重要的一个设计点**：load_factor 只影响「被选中的倾向」，**不改变真实并发上限**。
- load_factor > concurrency → LoadRate 分母变大 → 显示负载低 → **更容易被选中**；
- load_factor < concurrency → LoadRate 虚高 → **被 Layer 2 的 LoadRate < 100 过滤掉**（gateway_scheduling.go:730，openai_gateway_scheduling.go 同构）。

**扣减/释放位置**：

- 扣减：ConcurrencyService.AcquireAccountSlot（concurrency_service.go:342）→ cache.AcquireAccountSlot（concurrency_cache.go:631，Lua）。
- 释放：返回的 ReleaseFunc（concurrency_service.go:362-368），内部用独立 5s 超时后台 context 释放（:363）。
- 释放兜底：handler/gateway_helper.go:188 wrapReleaseOnDone 用 context.AfterFunc 保证客户端断开也释放。
- 抢槽/释放点（handler）：gateway_handler.go:247/:254（用户槽）、:445/:790（账号槽经 helper）；openai_gateway_handler.go:2079（用户槽）、:2312/:2347/:2403（账号槽）。

**「无限制」语义**（重要）：

    // concurrency_service.go:343-349
    if maxConcurrency <= 0 {
        return &AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
    }

即 **concurrency <= 0 表示完全不限制**（不写 Redis）。load_factor 同理（EffectiveLoadFactor 回落到 1 或 Concurrency）。
唯一例外：Grok OAuth 账号创建/更新时会被强制抬到 1（service/admin_account.go:349-356 normalizeAccountConcurrency）。

### 3.2 用户级并发

| 项 | 证据 |
|---|---|
| 列 | users.concurrency，migrations/001_init.sql:47 concurrency INT NOT NULL DEFAULT 5 |
| ent | ent/schema/user.go:55-56 field.Int("concurrency").Default(5) |
| 传入 | middleware/api_key_auth.go:180/:278 → AuthSubject.Concurrency（middleware/auth_subject.go:9） |
| 抢槽 | handler/gateway_handler.go:247 AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, ...)；openai_gateway_handler.go:2079 |
| 服务 | ConcurrencyService.AcquireUserSlot（concurrency_service.go:381）→ concurrency_cache.go:708，键 concurrency:user:{id} |
| 系统设置默认 | gateway.default_concurrency（setting_features.go:300 GetDefaultConcurrency），按注册来源另有 auth_source_default_*_concurrency（setting_parse.go:135-165，默认 5） |
| 管理员升级 | repository/simple_mode_admin_concurrency.go:20：simple 模式把 admin 从 5 抬到 30 |

**顺序：先用户槽，后账号槽**（gateway_handler.go:247 → :445）。用户并发满会直接 429，**在选号之前**。

### 3.3 API Key 级

concurrency:api_key:{id}（concurrency_cache.go:32）**只统计不限流**：

    // concurrency_service.go:417-420
    // TrackAPIKeySlot records one active request slot for an API key without
    // applying key-level concurrency limits. It is fail-open.
    func (s *ConcurrencyService) TrackAPIKeySlot(ctx, apiKeyID) func()

调用点：gateway_helper.go:319 withAPIKeySlot（包在用户槽 release 外）。
**API Key 没有并发上限字段**（ent/schema/api_key.go 只有 quota/quota_used，无 concurrency）。
唯一 Key 级硬闸是 WS 入站租约：gateway.openai_ws.max_ingress_connections_per_api_key（默认 64，config.go:1274/:2453），实现 concurrency_service.go:257 Acquire[OI]WSIngressLease，续租 20s / TTL 60s（:74-76）。

### 3.4 请求级 / 其它并发闸

| 闸 | 位置 | 说明 |
|---|---|---|
| 图片生成进程级 limiter | handler/image_concurrency_limiter.go:9 | **进程内**，默认关闭（deploy/config.example.yaml:507-522：image_concurrency.enabled: false）；多实例总上限 ≈ 实例数 × max |
| 账号等待队列（用户级） | concurrency_service.go:495 IncrementWaitCount | 上限 = CalculateMaxWait(concurrency) = concurrency + 20（:564-569，defaultExtraWaitSlots = 20 :221） |
| 账号等待队列（账号级） | concurrency_service.go:527 IncrementAccountWaitCount | 上限由 WaitPlan.MaxWaiting 传入（sticky 默认 3 / fallback 默认 100） |
| WS 连接池上限 | openai_ws_pool.go:2219 effectiveMaxConnsByAccount | min(max_conns_per_account, ceil(account.concurrency * factor))；OAuth/APIKey 系数默认 5.0（config.go:1321/:1323，示例 128/5.0，deploy/config.example.yaml:365-379）。**这是会话数上限，不是在飞请求数上限**；命中会返回 1013 busy |
| 账号 RPM（Anthropic OAuth） | gateway_scheduling.go:1419 isAccountSchedulableForRPM、:1455 IncrementAccountRPM | 仅 IsAnthropicOAuthOrSetupToken() |
| 账号会话数（Anthropic OAuth） | gateway_scheduling.go:1467 checkAndRegisterSession、:1497 ReleaseAccountSession | GetMaxSessions() / GetSessionIdleTimeoutMinutes() |
| 用户/分组 RPM | service/billing_cache_service.go:821 checkRPM | 三层并行：(user,group) rpm_override → group.rpm_limit → user.rpm_limit（ent/schema/user.go:121、group.go:275） |
| 账号切换上限 | handler/gateway_handler.go:82-91 | maxAccountSwitches = 10、Gemini = 3（可配 gateway.max_account_switches） |
| 利润门否决上限 | handler/failover_loop.go:51 | maxProfitVetoAttempts = 10 |
| 选号探测预算 | openai_account_scheduler.go:35 | openAIAccountSelectionProbeLimit = 64 |

---

## 4. 并发超限时的行为

**不是简单拒绝，而是「等待 → 排队 → 换账号 → 兜底拒绝」四段式。**

### 4.1 选号阶段：换账号

抢槽失败不立即报错，而是：

- 通用 Layer 2：从 available 移除该账号，重新分层过滤（gateway_scheduling.go:767-776）；
- [OI]：tryAcquire[OI]SelectionOrderWithBudget 继续下一个候选（openai_account_scheduler.go:1200-1201）；
- 全部候选失败 → 返回 **WaitPlan**（不返回错误）。

### 4.2 等待阶段：指数退避轮询

handler/gateway_helper.go：

| 常量 | 值 | 位置 |
|---|---|---|
| maxConcurrencyWait | **30s** | :122 |
| initialBackoff | 100ms | :126 |
| backoffMultiplier | 1.5 | :128 |
| maxBackoff | 2s | :130 |

waitForSlotWithPingTimeout（:361）在循环里 select：ctx.Done() → 超时错误；pingCh → 流式 SSE 心跳保活；timer.C → 重试抢槽（:406-446）。
退避带 ±20% 抖动（:466）避免惊群。**流式请求在排队期间已开始发 SSE 心跳**（:417-431）。

### 4.3 排队队列

- **账号级**：handler 先 IncrementAccountWaitCount(ctx, account.ID, WaitPlan.MaxWaiting)（gateway_handler.go:424、openai_gateway_handler.go:2350）；队列满 → **429** + gateway_queue_full（concurrency_error_response.go:20-22，文案 "Too many pending requests, please retry later"）。
- **用户级**：acquireUserSlotWithWaitTimeout（gateway_helper.go:274）用 CalculateMaxWait(maxConcurrency) - maxConcurrency 作为队列上限（≥1），满则 WaitQueueFullError（:295-297）。
- 队列计数在请求结束时 defer 递减（gateway_handler.go:438-443）。

### 4.4 超时/拒绝的错误映射

handler/concurrency_error_response.go:17-38：

| 错误 | HTTP | type | code | 文案 |
|---|---|---|---|---|
| WaitQueueFullError | 429 | rate_limit_error | gateway_queue_full | Too many pending requests, please retry later |
| ConcurrencyError（超时） | 429 | rate_limit_error | gateway_concurrency_limit | Concurrency limit exceeded for {account 或 user}, please retry later |
| context.Canceled | 499 | api_error | — | context canceled |
| 其它 | 503 | api_error | — | Service temporarily unavailable, please retry later |

**无可用账号** → 503 No available accounts（gateway_handler.go:420/:765、openai_gateway_handler.go:2286/:2316）。

### 4.5 超时参数汇总

| 配置 | 默认 | 位置 | 用途 |
|---|---|---|---|
| gateway.scheduling.sticky_session_wait_timeout | 120s | config.go:2556 | 粘性账号排队等待 |
| gateway.scheduling.fallback_wait_timeout | 30s | :2557 | 兜底排队等待 |
| gateway.scheduling.sticky_session_max_waiting | 3 | :2555 | 粘性账号等待队列长度 |
| gateway.scheduling.fallback_max_waiting | 100 | :2558 | 兜底等待队列长度 |
| gateway.concurrency_slot_ttl_minutes | 30 | :2548 | 槽位泄漏兜底 TTL |
| handler 用户槽等待 | 30s | gateway_helper.go:122 | 硬编码 |

⚠️ gateway_scheduling.go:822-834 的 schedulingConfig() 兜底把 StickySessionWaitTimeout 写成 45s，与 viper 默认 120s 不一致；仅 s.cfg == nil 时生效。

---

## 5. 限流与临时不可调度

### 5.1 ratelimit_service.go（2629 行）职责

上游错误 → 状态处置的总入口 HandleUpstreamError（:332）。主要分支：

| 分支 | 位置 | 效果 |
|---|---|---|
| handle403 | :1007 | 403 分级；连续 openAI403DisableThreshold = 3（:95）次 → 永久 status=error |
| handle429 | :1186 | 写 rate_limit_reset_at；冷却 5s~7200s（:76-77） |
| handle529 | :1966 | 写 overload_until |
| handleAuthError | :971 | 401 鉴权失败 → 永久 error（CN 供应商除外） |
| HandleUpstreamModelNotFound | :2508 | 模型级冷却 30 分钟（:2492） |
| HandleStreamTimeout | :2784 | 流超时 → 临时停调/错误 |
| tryTempUnschedulable | :2647 | 按账号自定义规则停调 |

### 5.2 临时不可调度（temp unschedulable）

**数据模型** service/temp_unsched.go:9-19 TempUnschedState：until_unix、triggered_at_unix、status_code、matched_keyword、rule_index、error_message。

**落库列**（ent/schema/account.go）：

| 列 | schema 行 | 迁移 |
|---|---|---|
| temp_unschedulable_until | :174 | migrations/020_add_temp_unschedulable.sql:5 |
| temp_unschedulable_reason | :180 | :8 |

**三条触发路径**：

1. **账号自定义规则**（credentials.temp_unschedulable_enabled + temp_unschedulable_rules，service/account.go:425-473）→ matchTempUnschedulableRules（:2620）按 error_code + keywords 子串匹配 → triggerTempUnschedulable（:2712）。
   - 有 requestedModel 且非 401 时写 **模型级** 限流 SetModelRateLimit（:2745，落在 accounts.extra->'model_rate_limits'，见 model_rate_limit.go:12、account_repo.go:2413）；
   - 否则写 **账号级** SetTempUnschedulable（:2756）。
2. **按平台用量阈值自动停调**：ApplyAccountSchedulingThreshold（:165）→ EvaluateAccountSchedulingThreshold（account_scheduling_threshold_eval.go:33）。
   - 阈值来源：系统设置 account_scheduling_thresholds（setting_features.go:1129），**默认全部 100 = 关闭**（setting_update.go:565-571）；
   - 允许平台：openai / anthropic / grok / kimi / zhipu / minimax / opencode_go / command_code（domain_constants.go:138-147）；
   - 账号级覆盖：credentials.account_scheduling_threshold（account_scheduling_threshold_eval.go:29/:123）；
   - 缓存 TTL 60s（setting_gateway_runtime.go:86）。
3. **国产供应商响应式冷却**（ratelimit_cn_providers.go）：
   - Kimi 并发限制 403 → cn_concurrency_limit，停调 openAI403CooldownMinutesDefault = 10 分钟（:92-100）；
   - 余额不足 → cn_balance_low，停调 2× 余额检测周期（默认 20 分钟，:156-176），由周期任务在余额恢复后清除；
   - Coding Plan 窗口耗尽 403 → cn_quota_exhausted，冷却到快照中最早的未来窗口重置点（:73-90）。

**恢复**：ClearTempUnschedulable（ratelimit_service.go:2204）、ClearRateLimit（:2129）、RecoverAccountAfterSuccessfulTest（:2200）。

**调度可见性**：Account.IsSchedulable()（service/account.go:182-203）依次检查 status、schedulable、expires_at、overload_until、rate_limit_reset_at、temp_unschedulable_until、配额超限。

**另有一条进程内快速通道**：openai_account_runtime_block_fastpath.go:327 BlockAccountScheduling / :382 ClearAccountSchedulingBlock / :394 is[OI]AccountRuntimeBlocked，走内存 map（默认冷却 openAIStopSchedulingBridgeCooldown = 2min，:19）。**这一层不落库，重启即失效**。

---

## 6. 定位：「部分账号不并发 / 部分账号并发过大」

> 以下每条都给出**可执行排查位置**。所有判断都需要现场数据（Redis 槽位计数 + 账号字段实值）才能定论。

### 6.1 账号「不并发」（选不到 / 选得少）

按优先级从高到低排查：

| # | 可能原因 | 排查位置 | 判据 |
|---|---|---|---|
| 1 | status != 'active' 或 schedulable = false | ent/schema/account.go:117/:148；account.go:182-184 | 硬性排除 |
| 2 | temp_unschedulable_until > now | account.go:196；account.temp_unschedulable_reason | reason 前缀可区分来源：cn_balance_low / cn_concurrency_limit / cn_quota_exhausted / account_scheduling_threshold / JSON（自定义规则） |
| 3 | rate_limit_reset_at > now（429 冷却） | account.go:193 | 见 handle429（ratelimit_service.go:1186） |
| 4 | overload_until > now（529） | account.go:190 | handle529（:1966） |
| 5 | expires_at 过期 + auto_pause_on_expired | account.go:187；schema :139 | OAuth token 过期也会走这条 |
| 6 | 配额超限（APIKey/Bedrock） | account.go:199 IsQuotaExceeded | — |
| 7 | **load_factor 明显小于 concurrency** | account.go:169 EffectiveLoadFactor → concurrency_cache.go:999 | LoadRate 被放大；≥100 时被 Layer 2 过滤（gateway_scheduling.go:730） |
| 8 | priority 值偏大 | ent/schema/account.go:107（默认 50，越小越优先）；gateway_scheduling.go:1593；openai_account_scheduler.go:635 | 分层过滤先取 min(priority) 集合，只有该集合内的账号会被抢槽 |
| 9 | 粘性绑定把他账号固定住了 | Redis session_hash → account_id；openai_sticky_compat.go:122 | 同一会话只落一个账号 |
| 10 | 模型路由把模型限定到其它账号 | gateway_scheduling.go:895 routingAccountIDsForRequest、group.ModelRouting | Layer 1 优先于粘性 |
| 11 | 模型级限流（extra->'model_rate_limits'） | model_rate_limit.go:40-47；account_repo.go:2413 | 按 (账号, 模型) 粒度 |
| 12 | 渠道定价限制 | gateway_scheduling.go:124/:674；checkChannelPricingRestriction | 直接判为不可用 |
| 13 | 账号不在该分组 | account_groups 表；gateway_scheduling.go:1165 isAccountInGroup | 分组查询已过滤，缺失即不可见 |
| 14 | Anthropic OAuth 窗口费用/RPM/会话数超限 | gateway_scheduling.go:1323/:1419/:1467 | 仅 IsAnthropicOAuthOrSetupToken() |
| 15 | Grok free 软闸 | openai_account_scheduler.go:1413 filterGrokFreeQuotaAccounts | 免费档额度耗尽 |
| 16 | **槽位泄漏**：Redis ZSET 里残留 requestID | redis-cli ZCARD concurrency:account:{id} | 槽位 TTL 最长 30 分钟；释放失败/进程被杀会残留 |
| 17 | **Live 租约占位** | concurrency_cache.go:99（ZCARD 含 liveKey）；openai_live.go:180 AcquireLiveLease | 长连接 WS 会话按 60s 续租持续占并发 |
| 18 | 调度快照未刷新 | scheduler_snapshot_service.go:375 pollOutbox；outbox 默认 1s | 改了 DB 但快照桶还是旧的 |
| 19 | 进程内 runtime block 未清除 | openai_account_runtime_block_fastpath.go:394 | 内存态，重启才清 |
| 20 | Grok OAuth 并发被强制抬到 1 | service/admin_account.go:349-356 | concurrency <= 0 被 normalize 成 1 |

**快速取证命令**（只读）：

    # 账号当前槽位与等待数
    redis-cli ZCARD "concurrency:account:<ID>"
    redis-cli ZCARD "concurrency:live:account:<ID>"
    redis-cli GET  "wait:account:<ID>"
    redis-cli ZRANGE "concurrency:account:<ID>" 0 -1 WITHSCORES

    # 粘性绑定
    redis-cli --scan --pattern "*session*"

### 6.2 账号「并发过大」（被过度使用）

| # | 可能原因 | 排查位置 | 机制 |
|---|---|---|---|
| 1 | **concurrency <= 0（完全不限制）** | concurrency_service.go:343-349 | 直接返回 Acquired，不写 Redis；DB 无 CHECK 约束 |
| 2 | **load_factor > concurrency** | account.go:169 → LoadRate 分母变大 | 显示负载低 → 优先被选；真实上限仍是 concurrency，但**选择偏斜**会造成该账号长期贴顶 |
| 3 | **粘性权重过高** | config.go:2503 previous_response=5.0 / session_sticky=3.0 vs load=1.0 | 高级调度器把会话钉在少数账号 |
| 4 | **粘性 TTL 长（默认 3600s）** | config.go:1351 | 一小时内同一会话恒定落同一账号 |
| 5 | **priority 最小** | gateway_scheduling.go:1593 | 分层过滤第一刀就把其它账号全砍掉 |
| 6 | **负载读缓存 200ms** | concurrency_service.go:223、config.go:2562 | 突发流量在缓存窗口内看不到彼此，集中打同一账号（**不会突破槽位上限**） |
| 7 | **lb_top_k 偏小** | config.go:2487（默认 7） | TopK 内随机化，K 小则集中在高分行 |
| 8 | **Redis 故障 → loadMap 为空** | openai_account_scheduler.go:1489-1494（err 时 loadMap 保持空）；gateway_scheduling.go:717-722（回退 legacy 顺序） | 所有账号 LoadRate=0，负载均衡失效；**抢槽仍走 Redis 会失败，所以不会真的超限**，但选号会退化为优先级+LRU |
| 9 | **PreferSoonestReset = true** | gateway_scheduling.go:743；config.go:2560 | use-it-or-lose-it，持续优先用即将重置的账号 |
| 10 | **WS 会话数 ≠ 请求数** | openai_ws_pool.go:2219 | max_conns_per_account(128) × 系数 5.0 允许远超 concurrency 的并发会话，**在飞请求仍受账号槽限制**，但上游连接数会很大 |
| 11 | **图片并发 limiter 默认关闭** | deploy/config.example.yaml:510 enabled: false | 图片请求不额外限流，全部计入账号并发 |
| 12 | **用户并发上限偏大** | users.concurrency（默认 5；simple 模式 admin 被升到 30） | 单用户可同时占满多个账号槽 |
| 13 | **高级调度器权重被设置覆盖** | settings.openai_advanced_scheduler_*_weight_*（openai_account_scheduler.go:2018-2065）；设置键见 setting_handler_audit.go:519/:528 | 权重可被后台改而不改配置文件 |
| 14 | **多实例未共享 Redis** | repository/wire.go:24-26 | 若各实例连不同 Redis，槽位计数各自独立 → 实际并发 = 实例数 × concurrency ⚠️（未确认当前部署是否单实例） |

### 6.3 建议的定位顺序（最小成本）

1. redis-cli ZCARD concurrency:account:{id} 对比 accounts.concurrency → 判断是「真占满」还是「选不到」。
2. 若 ZCARD 远小于 concurrency 却拿不到流量 → 走 6.1（选择层问题：priority / load_factor / 粘性 / 模型路由）。
3. 若 ZCARD 长期等于 concurrency → 走 6.2（是否 concurrency<=0、load_factor 偏大、粘性集中）。
4. 检查 accounts 行的 6 个时间列与 extra->'model_rate_limits'、temp_unschedulable_reason 前缀。
5. 检查系统设置 account_scheduling_thresholds 是否被调成非 100。
6. 若 DB 已改但调度未变 → 检查 outbox 水位与快照桶（scheduler_outbox 表 + scheduler_snapshot_service.go:1287 checkOutboxLag）。

---

## 7. 相关数据库表与关键列

### 7.1 accounts（调度相关列）

| 列 | 类型/默认 | ent schema | 迁移 | 作用 |
|---|---|---|---|---|
| concurrency | INT NOT NULL DEFAULT 3 | account.go:100 | 001_init.sql:68 | **账号并发硬上限**；<=0 表示不限 |
| load_factor | INTEGER NULL | account.go:103 | 067:1 | 负载率分母；NULL 回落 concurrency |
| priority | INT NOT NULL DEFAULT 50 | account.go:107 | 001_init.sql | 越小越优先 |
| schedulable | BOOL DEFAULT true | account.go:148 | 005（schema 注释） | 手动开关 |
| status | VARCHAR(20) DEFAULT 'active' | account.go:117 | 001 | active/error/disabled |
| rate_limited_at / rate_limit_reset_at | timestamptz | account.go:153/:160 | 005 | 429 冷却 |
| overload_until | timestamptz | account.go:167 | 005 | 529 过载 |
| temp_unschedulable_until / temp_unschedulable_reason | timestamptz / text | account.go:174/:180 | 020:5/:8 | 临时停调 |
| expires_at / auto_pause_on_expired | timestamptz / BOOL DEFAULT true | account.go:133/:139 | 005 | 过期停调 |
| session_window_start/end/status | timestamptz / VARCHAR(20) | account.go:187-198 | 005 | 会话窗口（Claude Pro） |
| rate_multiplier | decimal(10,4) DEFAULT 1.0 | account.go:112 | fork 魔改 | 账号计费倍率（同时参与上游成本打分） |
| parent_account_id | BIGINT NULL | account.go:200 | 154a | spark 影子母账号 |
| extra->'model_rate_limits' | JSONB | model_rate_limit.go:12 | — | (账号,模型) 级限流，无独立表 |

**索引**：account.go:235-252（platform/type/status/priority/last_used_at/schedulable/rate_limited_at/rate_limit_reset_at/overload_until）；部分索引 migrations/062_*.sql:1-7（idx_accounts_schedulable_hot、idx_accounts_active_schedulable）。

### 7.2 users

| 列 | 默认 | 证据 |
|---|---|---|
| concurrency | 5 | migrations/001_init.sql:47；ent/schema/user.go:55 |
| rpm_limit | 0（不限） | ent/schema/user.go:121 |

### 7.3 groups / account_groups

| 表.列 | 证据 | 作用 |
|---|---|---|
| groups.rpm_limit | ent/schema/group.go:275 | 分组 RPM |
| account_groups.priority | ent/schema/account_group.go:33 | 分组内账号优先级 ⚠️ 未确认被调度打分消费 |
| account_groups 复合主键 | account_group.go:25 | (account_id, group_id) |
| 索引 | migrations/150_*.sql:1-5 | (group_id, priority, account_id)、(account_id, priority, group_id) |

### 7.4 scheduler_outbox

migrations/036_scheduler_outbox.sql:1-10：id BIGSERIAL、event_type TEXT、account_id BIGINT、group_id BIGINT、payload JSONB、created_at TIMESTAMPTZ。
去重键：migrations/152_scheduler_outbox_dedup_key.sql、153_*.sql（dedup_key 唯一索引）。

### 7.5 Redis 键总表（非 DB，但等价重要）

| 键 | 类型 | 常量位置 |
|---|---|---|
| concurrency:account:{id} | ZSET | concurrency_cache.go:28 |
| concurrency:user:{id} | ZSET | :30 |
| concurrency:api_key:{id} | ZSET | :32 |
| concurrency:live:account:{id} / live:user / live:api_key | ZSET | :33-35 |
| concurrency:openai_ws_ingress:api_key:{id} | ZSET | :38 |
| wait:account:{id} | STRING 计数 | :44 |
| concurrency:wait:{id} | STRING 计数 | :42 |
| concurrency:account:active_index / user:active_index | ZSET | :51-52 |

---

## 8. 配置项速查

deploy/config.example.yaml 与 backend/internal/config/config.go 的对应关系：

| 配置键 | 默认 | 代码默认位置 | 说明 |
|---|---|---|---|
| gateway.scheduling.sticky_session_max_waiting | 3 | config.go:2555 | 粘性等待队列长度 |
| gateway.scheduling.sticky_session_wait_timeout | 120s | :2556 | 粘性等待超时 |
| gateway.scheduling.fallback_wait_timeout | 30s | :2557 | 兜底等待超时 |
| gateway.scheduling.fallback_max_waiting | 100 | :2558 | 兜底队列长度 |
| gateway.scheduling.fallback_selection_mode | last_used | :2559 | 兜底排序（另有 random，gateway_scheduling.go:1843） |
| gateway.scheduling.prefer_soonest_reset | false | :2560 | use-it-or-lose-it |
| gateway.scheduling.load_batch_enabled | true | :2561 | 关闭后**无负载均衡** |
| gateway.scheduling.load_batch_cache_ttl_ms | 200 | :2562 | 进程内负载读缓存 |
| gateway.scheduling.slot_cleanup_interval | 30s | :2565 | 槽位清理周期 |
| gateway.scheduling.db_fallback_enabled | true | :2566 | 快照未命中是否回源 DB |
| gateway.scheduling.outbox_poll_interval_seconds | 1 | :2569 | outbox 轮询 |
| gateway.scheduling.full_rebuild_interval_seconds | 300 | :2574 | 全量重建周期 |
| gateway.concurrency_slot_ttl_minutes | 30 | :2548 | 槽位 TTL（泄漏兜底） |
| gateway.openai_ws.lb_top_k | 7 | :2487 | TopK |
| gateway.openai_ws.sticky_session_ttl_seconds | 3600 | :1351 | 粘性 TTL |
| gateway.openai_ws.max_conns_per_account | 128（示例） | :1311 | WS 连接池硬上限 |
| gateway.openai_ws.dynamic_max_conns_by_account_concurrency_enabled | true（示例） | :1317 | 按账号并发缩放连接池 |
| gateway.openai_ws.oauth_max_conns_factor / apikey_max_conns_factor | 5.0 | :1321/:1323 | 连接池系数 |
| gateway.openai_ws.max_ingress_connections_per_api_key | 64 | :1274/:2453 | 客户端 WS 入站租约上限 |
| gateway.openai_ws.scheduler_score_weights.* | 见 §2.2 | :2494-2503 | 打分权重 |
| gateway.openai_scheduler.sticky_escape_enabled | true | 示例 :429 | 粘性逃逸 |
| gateway.openai_scheduler.sticky_escape_ttft_ms | 15000 | 示例 :431 | 逃逸 TTFT 阈值 |
| gateway.openai_scheduler.sticky_escape_error_rate | 0.5 | 示例 :433 | 逃逸错误率阈值 |
| gateway.image_concurrency.enabled | false | 示例 :510 | 图片独立并发保护 |
| gateway.max_account_switches | 10 | gateway_handler.go:82 | 换账号上限 |
| settings.account_scheduling_thresholds | 全 100（关闭） | setting_update.go:565 | 按平台自动停调阈值 |
| settings.openai_advanced_scheduler_enabled | — | openai_account_scheduler.go:27 | 高级调度器开关 |
| settings.openai_advanced_scheduler_lb_top_k | — | domain_constants.go:649 | 后台覆盖 TopK |

---

## 9. 结论摘要

1. **账号并发上限字段是 accounts.concurrency（ent account.go:100，默认 3）**；<= 0 等于**完全不限制**（concurrency_service.go:343）。
2. **accounts.load_factor（account.go:103）只影响选号打分，不改变真实并发上限**；它与 concurrency 的不一致是「看起来不并发/并发过大」最常见的结构性来源（account.go:169 EffectiveLoadFactor vs 抢槽用 account.Concurrency）。
3. **抢槽是 Redis ZSET + Lua 原子操作**（concurrency_cache.go:71-107），ZCARD(普通槽) + ZCARD(Live 租约) < maxConcurrency；**WS 长连接会持续占用账号并发额度**。
4. **超限不拒绝**：先换账号 → 再指数退避等待（100ms→2s，±20% 抖动）→ 再排队（sticky 3 / fallback 100）→ 最后才 429（gateway_queue_full / gateway_concurrency_limit）。
5. **粘性会话（TTL 默认 3600s，权重 previous_response=5.0 / session_sticky=3.0）是把流量集中到少数账号的第一大结构性原因**，且是设计行为。
6. 排查顺序建议：ZCARD vs concurrency → 判断「真占满」还是「选不到」→ 再看 6 个时间列 + temp_unschedulable_reason 前缀 + model_rate_limits → 最后看 outbox/快照延迟。
