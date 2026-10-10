-- Additive C3 configuration revision; no historical billing metadata is invented.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS group_chain_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_group_chain_revision_nonnegative CHECK (group_chain_revision >= 0);

CREATE TABLE api_key_group_links (
    id BIGSERIAL PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL DEFAULT 'normal' CHECK (role IN ('normal', 'ha')),
    position INTEGER NOT NULL CHECK (position >= 0),
    enabled_by_user_at TIMESTAMPTZ,
    CONSTRAINT api_key_group_links_group_unique UNIQUE (api_key_id, group_id),
    CONSTRAINT api_key_group_links_position_unique UNIQUE (api_key_id, role, position),
    CONSTRAINT api_key_group_links_authorization CHECK (
        (role = 'normal' AND enabled_by_user_at IS NULL) OR
        (role = 'ha' AND enabled_by_user_at IS NOT NULL)
    )
);
CREATE INDEX api_key_group_links_group_id_idx ON api_key_group_links(group_id);

-- Preserve the old single binding exactly. Never create HA authorizations.
INSERT INTO api_key_group_links (api_key_id, group_id, role, position)
SELECT id, group_id, 'normal', 0 FROM api_keys WHERE group_id IS NOT NULL;

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS requested_group_id BIGINT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS served_group_id BIGINT;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS failover VARCHAR(16);
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS attempt_count INTEGER;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_failover_values CHECK (failover IN ('none', 'account', 'group', 'ha'));
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_attempt_count_nonnegative CHECK (attempt_count >= 0);

-- Rollback before publishing multi-group edits: DROP TABLE api_key_group_links;
-- ALTER TABLE api_keys DROP COLUMN group_chain_revision;
-- ALTER TABLE usage_logs DROP COLUMN requested_group_id, DROP COLUMN served_group_id,
--     DROP COLUMN failover, DROP COLUMN attempt_count;
-- api_keys.group_id and historical billing values are intentionally retained.
