-- PRD-03 / TS-03：白标充值中心的最小订单映射与商品快照。
-- 只保留「本站用户 ↔ 上游订单号 ↔ 商品快照 ↔ 展示状态 ↔ 卡密结果」所需字段，
-- 不建设第二套支付/财务账本（PRD-03 §2.2、BE-02）。
-- 幂等：CREATE TABLE IF NOT EXISTS，不改写任何历史列与数据。
--
-- 卡密列只存密文（SecretEncryptor，AES-256-GCM），明文永不落库、不进日志、不进接口响应；
-- 上游凭据不落库，只从环境变量读取（PRD-03 BE-01、共享契约 C-8）。
CREATE TABLE IF NOT EXISTS yunmao_orders (
    id                BIGSERIAL PRIMARY KEY,
    order_no          VARCHAR(64)  NOT NULL,          -- 本站可追踪订单号（对外唯一标识）
    user_id           BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key   VARCHAR(64)  NOT NULL,          -- 客户端请求 ID，防重复下单（BE-04）
    upstream_order_no VARCHAR(128),                   -- 上游订单号，白标：只在服务端内部使用
    product_id        VARCHAR(128) NOT NULL,
    product_snapshot  JSONB        NOT NULL DEFAULT '{}'::jsonb,
    amount_cny        NUMERIC(12, 2) NOT NULL DEFAULT 0,
    quantity          INTEGER      NOT NULL DEFAULT 1,
    display_status    VARCHAR(32)  NOT NULL DEFAULT 'pending',
    upstream_status   VARCHAR(64),
    card_secret       TEXT,                           -- 卡密密文；明文只在所属用户查询时解密返回
    card_hint         VARCHAR(64),                    -- 脱敏提示（如尾号），可安全展示
    delivery_attempts INTEGER      NOT NULL DEFAULT 0,
    last_error_code   VARCHAR(64),
    last_queried_at   TIMESTAMPTZ,
    paid_at           TIMESTAMPTZ,
    delivered_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT yunmao_orders_order_no_unique UNIQUE (order_no),
    CONSTRAINT yunmao_orders_user_idempotency_unique UNIQUE (user_id, idempotency_key),
    CONSTRAINT yunmao_orders_quantity_positive CHECK (quantity > 0),
    CONSTRAINT yunmao_orders_amount_nonnegative CHECK (amount_cny >= 0),
    -- 展示状态仅是上游支付/交付结果的映射，不扩展独立支付状态机（PRD-03 §2）。
    CONSTRAINT yunmao_orders_display_status_check
        CHECK (display_status IN ('pending', 'paid', 'delivery_pending', 'delivered'))
);

CREATE INDEX IF NOT EXISTS yunmao_orders_user_created_idx
    ON yunmao_orders (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS yunmao_orders_display_status_idx
    ON yunmao_orders (display_status, updated_at DESC);

COMMENT ON TABLE yunmao_orders IS
    'PRD-03 white-label shop order mapping: site order no, owner, upstream order no, product snapshot, display status, encrypted card secret. No independent payment/finance ledger.';
COMMENT ON COLUMN yunmao_orders.card_secret IS
    'Encrypted card secret (AES-256-GCM). Plaintext is never persisted, logged or exposed to non-owners.';
COMMENT ON COLUMN yunmao_orders.display_status IS
    'Mapping of the upstream payment/delivery result only: pending | paid | delivery_pending | delivered.';

-- 商品目录快照（单行）：同步失败时保留最近一次成功数据，购买入口暂停（PRD-03 §2.8、BE-08）。
CREATE TABLE IF NOT EXISTS yunmao_catalog_snapshots (
    id               SMALLINT     PRIMARY KEY,
    products         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    synced_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    sync_failed_at   TIMESTAMPTZ,
    sync_error_code  VARCHAR(64),
    CONSTRAINT yunmao_catalog_snapshots_singleton CHECK (id = 1)
);

COMMENT ON TABLE yunmao_catalog_snapshots IS
    'PRD-03 catalog snapshot (singleton row id=1): last successful product list plus sync failure marker so the buy entry can be paused without dropping the last good data.';

-- Rollback: DROP TABLE yunmao_orders; DROP TABLE yunmao_catalog_snapshots;
