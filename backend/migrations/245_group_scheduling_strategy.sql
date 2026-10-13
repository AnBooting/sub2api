-- TS-02 / PRD-02 C-4：分组级调度策略覆盖。
-- 值域 inherit（默认，随全局 scheduling_strategy）/ stable / balanced / latency。
-- 只新增列并给存量行默认 inherit，不改写任何历史列与数据。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS scheduling_strategy VARCHAR(20) NOT NULL DEFAULT 'inherit';

-- CHECK 约束风格与本仓库既有迁移一致（幂等：按 pg_constraint 名判定后再建）。
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'groups_scheduling_strategy_check'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_scheduling_strategy_check
            CHECK (scheduling_strategy IN ('inherit', 'stable', 'balanced', 'latency'));
    END IF;
END $$;

COMMENT ON COLUMN groups.scheduling_strategy IS
    'Per-group scheduling strategy override (C-4): inherit follows the global scheduling_strategy; otherwise stable/balanced/latency';
