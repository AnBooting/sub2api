-- Classify OpenAI-compatible domestic provider groups without introducing a
-- new gateway platform. Existing groups remain ordinary platform groups.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS domestic_provider VARCHAR(50) NOT NULL DEFAULT '';

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_domestic_provider_platform_check;
ALTER TABLE groups
    ADD CONSTRAINT groups_domestic_provider_platform_check
    CHECK (domestic_provider = '' OR platform = 'openai');

CREATE INDEX IF NOT EXISTS idx_groups_domestic_provider_active
    ON groups (domestic_provider)
    WHERE deleted_at IS NULL AND domestic_provider <> '';
