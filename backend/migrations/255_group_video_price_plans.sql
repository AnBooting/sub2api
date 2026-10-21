-- PRD-06 / K-4：视频计费计划（按模型族声明计量模式与单价）。
-- 现有 video_model_prices 表达的是「每秒」单价（Grok 口径），本列用于表达更通用的
-- 计量模式：per_request / per_second / per_token，并可带分辨率与「是否含输入视频」分档。
-- 幂等：ADD COLUMN IF NOT EXISTS，不改写任何历史列与数据。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_price_plans JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN groups.video_price_plans IS
    'Per-model-family video billing plans: family -> {mode: per_request|per_second|per_token, prices}. No bundled default; an unconfigured plan must fail closed';
