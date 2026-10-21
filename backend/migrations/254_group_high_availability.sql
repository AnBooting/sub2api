-- PRD-04 / TS-04：分组可作高可用（HA）兜底。
-- 管理员标记该分组可被用户主动开启为兜底候选；支持模型复用 model_allowlist，
-- 倍率复用 rate_multiplier（默认 1x），因此本迁移只新增一个布尔列。
-- 幂等：ADD COLUMN IF NOT EXISTS，不改写任何历史列与数据。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS allow_high_availability BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.allow_high_availability IS
    'Group can serve as a user-authorized high-availability fallback (PRD-04): admin marks it, the user opts in per API key; models use model_allowlist, multiplier uses rate_multiplier';
