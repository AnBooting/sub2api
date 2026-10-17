-- PRD-06 / TS-06：异步视频的本站任务表。
--
-- 背景（PRD-06 §2.3 / §2.4 / §4）：
--   结算此前完全由客户端轮询驱动（客户端不查 → 永不结算），PRD-06 §4 明令
--   「不允许客户端是否查询决定是否扣费」。本表把「本站任务 ↔ 用户 ↔ 实际服务分组 ↔
--   上游任务号 ↔ 承载账号」的归属关系固化下来，供后台 worker 用**原承载账号**
--   主动查询上游并推进终态，而不必每次重新调度选号（§2.3）。
--
-- 边界：
--   - 本表只补该批协议要求的最小生命周期（processing/completed/failed），
--     不是第二套计费账本；金额结算仍复用既有 RecordUsage + usage_billing_dedup。
--   - billing_snapshot 保存创建时的计费快照（JSONB），与 Redis pending 快照同源，
--     供 worker 在 Redis 快照过期后仍能按原价结算。
--   - 幂等：CREATE TABLE IF NOT EXISTS，不改写任何历史列与数据。
--   - 归属可追溯（BE-03）：查询鉴权按 (user_id, api_key_id) 限定，绝不跨用户泄漏；
--     上游凭据不落库（account_id 只是外键引用）。
CREATE TABLE IF NOT EXISTS video_tasks (
    id                BIGSERIAL PRIMARY KEY,
    task_no           VARCHAR(64)  NOT NULL,          -- 本站任务号（对外唯一标识）
    user_id           BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id        BIGINT       NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id          BIGINT       NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    platform          VARCHAR(32)  NOT NULL DEFAULT '',   -- grok | openai(seedance)
    model             VARCHAR(128) NOT NULL DEFAULT '',
    billing_model     VARCHAR(128) NOT NULL DEFAULT '',
    billing_mode      VARCHAR(32)  NOT NULL DEFAULT '',   -- 计费模式快照（per_token/per_request/per_second）
    -- 上游任务号 = 客户端结算占位用的同一个 task key，必须**完整保存、不得截断**：
    -- 截断会让 worker 与客户端拿到不同的锁 → 同一任务重复扣费（见 VideoTask.BillingKey）。
    -- 故用 TEXT 而不是 VARCHAR(191)：191 仍可能短于真实 ResponseID，超长会让
    -- INSERT 失败、任务静默登记不进来，等于退回「只能靠客户端轮询」的旧缺口。
    upstream_task_id  TEXT         NOT NULL,
    account_id        BIGINT       REFERENCES accounts(id) ON DELETE SET NULL, -- 承载账号（不重新选号）
    billing_snapshot  JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status            VARCHAR(32)  NOT NULL DEFAULT 'processing',
    attempts          INTEGER      NOT NULL DEFAULT 0,
    last_error        TEXT,
    last_checked_at   TIMESTAMPTZ,
    next_check_at     TIMESTAMPTZ,
    settled_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT video_tasks_task_no_unique UNIQUE (task_no),
    -- 同一 Key 的同一上游任务只允许一行：创建路径重放不会产生重复任务。
    CONSTRAINT video_tasks_owner_upstream_unique UNIQUE (user_id, api_key_id, upstream_task_id),
    CONSTRAINT video_tasks_status_check
        CHECK (status IN ('processing', 'completed', 'failed')),
    CONSTRAINT video_tasks_attempts_nonnegative CHECK (attempts >= 0)
);

-- worker 扫描未终态任务：按 (status, next_check_at) 取到期批次。
CREATE INDEX IF NOT EXISTS video_tasks_due_idx
    ON video_tasks (status, next_check_at);

-- 归属查询：用户只能看到自己的任务。
CREATE INDEX IF NOT EXISTS video_tasks_owner_created_idx
    ON video_tasks (user_id, created_at DESC);

COMMENT ON TABLE video_tasks IS
    'PRD-06 async video task ledger: site task no, owner, actual serving group, upstream task id, carrying account, billing snapshot, and processing/completed/failed lifecycle. Drives server-side settlement so charging never depends on client polling.';
COMMENT ON COLUMN video_tasks.account_id IS
    'The account that actually served the create. Queries must reuse this account and never re-run account scheduling (PRD-06 2.3).';
COMMENT ON COLUMN video_tasks.billing_snapshot IS
    'Create-time billing snapshot (same source as the Redis pending snapshot) so a late worker settlement still prices the original task.';
COMMENT ON COLUMN video_tasks.upstream_task_id IS
    'Upstream task key, identical to the client-facing task id so worker and client share one settlement claim.';

-- Rollback: DROP TABLE video_tasks;
