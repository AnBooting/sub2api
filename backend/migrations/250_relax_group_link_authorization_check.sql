-- PRD-04 / TS-04 fix: relax the api_key_group_links authorization CHECK.
--
-- Migration 244 constrained role='ha' to always carry a non-null
-- enabled_by_user_at:
--
--     (role = 'normal' AND enabled_by_user_at IS NULL) OR
--     (role = 'ha'     AND enabled_by_user_at IS NOT NULL)
--
-- That contradicts two normal code paths which both write role='ha' with a NULL
-- timestamp:
--   * service/api_key_group_chain.go:160 — an administrator declares an HA fallback
--     link that the user has not enabled yet (enabled_by_user_at is set only when the
--     user opts in), and
--   * service/api_key_group_chain.go:249 — the user turns an already-enabled HA link
--     off, which clears the timestamp while the role stays 'ha'.
--
-- The repository writes the value unfiltered (repository/api_key_repo.go:1270), so on
-- a real database both paths failed with a CHECK violation: users could not disable an
-- enabled fallback group and administrators could not pre-declare one.
--
-- The intent of the constraint is that a NORMAL link never carries an authorization
-- timestamp, and that only the 'ha' role may carry one. Both are preserved below.
--
-- This migration is additive and does not rewrite 244, whose checksum the runner
-- verifies for already-applied migrations.
ALTER TABLE api_key_group_links
    DROP CONSTRAINT IF EXISTS api_key_group_links_authorization;

ALTER TABLE api_key_group_links
    ADD CONSTRAINT api_key_group_links_authorization CHECK (
        (role = 'normal' AND enabled_by_user_at IS NULL) OR
        (role = 'ha')
    );
