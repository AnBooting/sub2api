-- A conservative completeness boundary for rebuilding usage aggregates.
-- Before this migration the retention history cannot be inferred from MIN(created_at).
CREATE TABLE usage_stats_repair_control (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    raw_safe_from TIMESTAMPTZ NOT NULL,
    timezone_name TEXT NOT NULL CHECK (timezone_name <> ''),
    generation BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO usage_stats_repair_control (id, raw_safe_from, timezone_name)
VALUES (1, clock_timestamp(), current_setting('TimeZone'));

CREATE TABLE usage_stats_repair_days (
    timezone_name TEXT NOT NULL CHECK (timezone_name <> ''),
    day_start TIMESTAMPTZ NOT NULL,
    day_end TIMESTAMPTZ NOT NULL CHECK (day_end > day_start),
    generation BIGINT NOT NULL CHECK (generation > 0),
    applied_generation BIGINT NOT NULL DEFAULT 0 CHECK (applied_generation >= 0 AND applied_generation <= generation),
    state TEXT NOT NULL CHECK (state IN ('clean', 'dirty', 'quarantine')),
    reason_code TEXT NOT NULL DEFAULT '',
    attempts INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_until TIMESTAMPTZ,
    claim_owner TEXT NOT NULL DEFAULT '' CHECK (claim_owner <> '' OR claim_fence = 0),
    claim_fence BIGINT NOT NULL DEFAULT 0 CHECK (claim_fence >= 0),
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (timezone_name, day_start)
);
CREATE INDEX idx_usage_stats_repair_dirty ON usage_stats_repair_days (next_retry_at, day_start) WHERE state = 'dirty';
CREATE INDEX idx_usage_stats_repair_quarantine ON usage_stats_repair_days (updated_at, day_start) WHERE state = 'quarantine';

-- Existing group rollup invalidation is allowed only where the full day is known
-- to remain available. An incomplete old day must not rewind a complete rollup.
CREATE OR REPLACE FUNCTION invalidate_group_usage_rollup_state()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_date DATE;
    candidate_date DATE;
    published_before DATE;
    safe_from TIMESTAMPTZ;
    configured_timezone TEXT;
BEGIN
    SELECT closed_before INTO published_before FROM usage_group_rollup_state WHERE id = 1 FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing group rollup state'; END IF;
    SELECT raw_safe_from, timezone_name INTO safe_from, configured_timezone
      FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    IF TG_OP IN ('UPDATE', 'DELETE') AND OLD.group_id IS NOT NULL THEN
        candidate_date := (OLD.created_at AT TIME ZONE configured_timezone)::date;
        IF candidate_date::timestamp AT TIME ZONE configured_timezone >= safe_from THEN
            affected_date := candidate_date;
        END IF;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.group_id IS NOT NULL THEN
        candidate_date := (NEW.created_at AT TIME ZONE configured_timezone)::date;
        IF candidate_date::timestamp AT TIME ZONE configured_timezone >= safe_from THEN
            affected_date := LEAST(affected_date, candidate_date);
        END IF;
    END IF;
    IF affected_date IS NOT NULL AND published_before > affected_date THEN
        UPDATE usage_group_rollup_state SET closed_before = LEAST(closed_before, affected_date), updated_at = NOW() WHERE id = 1;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION invalidate_group_usage_rollup_state_after_insert()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    affected_date DATE;
    published_before DATE;
    safe_from TIMESTAMPTZ;
    configured_timezone TEXT;
BEGIN
    SELECT closed_before INTO published_before FROM usage_group_rollup_state WHERE id = 1 FOR KEY SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing group rollup state'; END IF;
    SELECT raw_safe_from, timezone_name INTO safe_from, configured_timezone
      FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    SELECT MIN((created_at AT TIME ZONE configured_timezone)::date) INTO affected_date
      FROM inserted_usage_logs
     WHERE group_id IS NOT NULL
       AND ((created_at AT TIME ZONE configured_timezone)::date::timestamp AT TIME ZONE configured_timezone) >= safe_from;
    IF affected_date IS NOT NULL AND published_before > affected_date THEN
        UPDATE usage_group_rollup_state SET closed_before = LEAST(closed_before, affected_date), updated_at = NOW() WHERE id = 1;
    END IF;
    RETURN NULL;
END;
$$;

-- A tracked day gets a new generation for every later write, including writes
-- unrelated to Live. Quarantine is sticky until an explicit reconciliation.
CREATE FUNCTION bump_usage_stats_repair_day(affected_at TIMESTAMPTZ)
RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE
    safe_from TIMESTAMPTZ;
    configured_timezone TEXT;
    local_date DATE;
    start_at TIMESTAMPTZ;
    end_at TIMESTAMPTZ;
    affected_rows INTEGER;
BEGIN
    SELECT raw_safe_from, timezone_name INTO safe_from, configured_timezone
      FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    local_date := (affected_at AT TIME ZONE configured_timezone)::date;
    start_at := local_date::timestamp AT TIME ZONE configured_timezone;
    end_at := (local_date + 1)::timestamp AT TIME ZONE configured_timezone;
    INSERT INTO usage_stats_repair_days AS day (
        timezone_name, day_start, day_end, generation, applied_generation, state, reason_code, updated_at
    ) VALUES (
        configured_timezone, start_at, end_at, 1, 0,
        CASE WHEN start_at < safe_from THEN 'quarantine' ELSE 'dirty' END,
        CASE WHEN start_at < safe_from THEN 'raw_history_incomplete' ELSE '' END,
        clock_timestamp()
    )
    ON CONFLICT (timezone_name, day_start) DO UPDATE SET
        generation = day.generation + 1,
        state = CASE WHEN day.state = 'quarantine' OR day.day_start < safe_from THEN 'quarantine' ELSE 'dirty' END,
        reason_code = CASE
            WHEN day.state = 'quarantine' THEN day.reason_code
            WHEN day.day_start < safe_from THEN 'raw_history_incomplete'
            ELSE ''
        END,
        attempts = CASE WHEN day.state = 'quarantine' THEN day.attempts ELSE 0 END,
        next_retry_at = CASE WHEN day.state = 'quarantine' THEN day.next_retry_at ELSE clock_timestamp() END,
        updated_at = clock_timestamp()
    WHERE day.day_end = EXCLUDED.day_end;
    -- ON CONFLICT 的 WHERE 在 day_end 不一致时会静默命中 0 行，脏标记随之丢失。
    -- 显式探测并按 sticky quarantine 处理，让查询报告该日不可用，而不是继续按旧状态发布。
    GET DIAGNOSTICS affected_rows = ROW_COUNT;
    IF affected_rows = 0 THEN
        UPDATE usage_stats_repair_days
           SET state = 'quarantine',
               reason_code = 'day_end_mismatch',
               generation = generation + 1,
               updated_at = clock_timestamp()
         WHERE timezone_name = configured_timezone AND day_start = start_at;
        GET DIAGNOSTICS affected_rows = ROW_COUNT;
        IF affected_rows = 0 THEN
            RAISE EXCEPTION 'usage statistics repair day missing for %', start_at;
        END IF;
    END IF;
END;
$$;

CREATE FUNCTION bump_usage_stats_repair_insert() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE affected_at TIMESTAMPTZ; configured_timezone TEXT;
BEGIN
    SELECT timezone_name INTO configured_timezone FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    FOR affected_at IN SELECT MIN(created_at) FROM inserted_stats_usage_logs GROUP BY (created_at AT TIME ZONE configured_timezone)::date ORDER BY 1 LOOP
        PERFORM bump_usage_stats_repair_day(affected_at);
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE FUNCTION bump_usage_stats_repair_delete() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE affected_at TIMESTAMPTZ; configured_timezone TEXT;
BEGIN
    SELECT timezone_name INTO configured_timezone FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    FOR affected_at IN SELECT MIN(created_at) FROM deleted_stats_usage_logs GROUP BY (created_at AT TIME ZONE configured_timezone)::date ORDER BY 1 LOOP
        PERFORM bump_usage_stats_repair_day(affected_at);
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE FUNCTION bump_usage_stats_repair_update() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE affected_at TIMESTAMPTZ; configured_timezone TEXT;
BEGIN
    SELECT timezone_name INTO configured_timezone FROM usage_stats_repair_control WHERE id = 1 FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'missing usage statistics repair control'; END IF;
    FOR affected_at IN
        SELECT MIN(created_at) FROM (
            SELECT created_at FROM old_stats_usage_logs UNION ALL SELECT created_at FROM new_stats_usage_logs
        ) changed GROUP BY (created_at AT TIME ZONE configured_timezone)::date ORDER BY 1
    LOOP
        PERFORM bump_usage_stats_repair_day(affected_at);
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE TRIGGER usage_logs_stats_repair_insert AFTER INSERT ON usage_logs
    REFERENCING NEW TABLE AS inserted_stats_usage_logs FOR EACH STATEMENT EXECUTE FUNCTION bump_usage_stats_repair_insert();
CREATE TRIGGER usage_logs_stats_repair_delete AFTER DELETE ON usage_logs
    REFERENCING OLD TABLE AS deleted_stats_usage_logs FOR EACH STATEMENT EXECUTE FUNCTION bump_usage_stats_repair_delete();
CREATE TRIGGER usage_logs_stats_repair_update AFTER UPDATE ON usage_logs
    REFERENCING OLD TABLE AS old_stats_usage_logs NEW TABLE AS new_stats_usage_logs FOR EACH STATEMENT EXECUTE FUNCTION bump_usage_stats_repair_update();
