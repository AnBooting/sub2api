-- Migration 196 may already be recorded on long-running candidates. Reapply
-- the domestic-provider constraint under the same stable name so upgraded
-- databases also receive the provider allow-list, not only fresh installs.
DO $$
DECLARE
    invalid_scopes TEXT;
BEGIN
    SELECT string_agg(scope, ', ' ORDER BY scope)
      INTO invalid_scopes
      FROM (
          SELECT DISTINCT quote_literal(domestic_provider) || '@' || quote_literal(platform) AS scope
            FROM groups
           WHERE domestic_provider NOT IN (
                     '', 'deepseek', 'qwen', 'doubao', 'kimi', 'zhipu',
                     'minimax', 'baichuan', 'yi', 'stepfun', 'custom'
                 )
              OR (domestic_provider <> '' AND platform <> 'openai')
      ) AS invalid;

    IF invalid_scopes IS NOT NULL THEN
        RAISE EXCEPTION
            'groups contain unsupported domestic_provider/platform scopes: %',
            invalid_scopes
            USING ERRCODE = 'check_violation';
    END IF;
END
$$;

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_domestic_provider_platform_check;
ALTER TABLE groups
    ADD CONSTRAINT groups_domestic_provider_platform_check
    CHECK (
        domestic_provider IN (
            '', 'deepseek', 'qwen', 'doubao', 'kimi', 'zhipu',
            'minimax', 'baichuan', 'yi', 'stepfun', 'custom'
        )
        AND (domestic_provider = '' OR platform = 'openai')
    );
