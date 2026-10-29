-- Durable handoff for an already Redis-confirmed Live settlement.
-- Only existing usage metadata is retained; no credentials, upstream call ID, attestation,
-- or complete request-header collections are stored.
-- A reservation is committed before the upstream create request.
CREATE TABLE live_session_reservations (
    generation BIGSERIAL PRIMARY KEY,
    operation_id VARCHAR(128) NOT NULL UNIQUE,
    terms JSONB NOT NULL,
    terms_fingerprint VARCHAR(64) NOT NULL,
    status TEXT NOT NULL DEFAULT 'provisioning',
    call_hash VARCHAR(64) UNIQUE,
    bound_identity JSONB,
    identity_fingerprint VARCHAR(64),
    quarantine_reason VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (operation_id, generation),
    CONSTRAINT live_reservations_status_check CHECK (status IN ('provisioning','bound','active','frozen','unknown','quarantine')),
    CONSTRAINT live_reservations_bound_check CHECK (
        status NOT IN ('bound','active','frozen') OR
        (call_hash IS NOT NULL AND bound_identity IS NOT NULL AND identity_fingerprint IS NOT NULL)
    )
);
CREATE INDEX idx_live_reservations_recovery ON live_session_reservations (status, generation);

-- A separate binding row lets uniqueness conflicts be recorded without aborting the transaction.
CREATE TABLE live_session_bindings (
    call_hash VARCHAR(64) PRIMARY KEY,
    operation_id VARCHAR(128) NOT NULL UNIQUE,
    generation BIGINT NOT NULL UNIQUE,
    identity_fingerprint VARCHAR(64) NOT NULL,
    UNIQUE (call_hash, generation),
    FOREIGN KEY (operation_id, generation) REFERENCES live_session_reservations (operation_id, generation) ON DELETE RESTRICT
);

-- Bounded, deduplicated evidence contains only identity/frozen fields and their digests.
CREATE TABLE live_settlement_conflicts (
    id BIGSERIAL PRIMARY KEY,
    reservation_generation BIGINT NOT NULL REFERENCES live_session_reservations(generation) ON DELETE RESTRICT,
    domain VARCHAR(32) NOT NULL,
    existing_digest VARCHAR(64) NOT NULL,
    incoming_digest VARCHAR(64) NOT NULL,
    existing_evidence JSONB NOT NULL,
    incoming_evidence JSONB NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (reservation_generation, domain, existing_digest, incoming_digest)
);

CREATE TABLE live_settlements (
    id BIGSERIAL PRIMARY KEY,
    call_hash VARCHAR(64) NOT NULL UNIQUE,
    reservation_generation BIGINT NOT NULL UNIQUE REFERENCES live_session_reservations(generation) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    settlement_mode TEXT NOT NULL,
    frozen_total_cost NUMERIC NOT NULL,
    frozen_actual_cost NUMERIC NOT NULL,
    identity_fingerprint VARCHAR(64) NOT NULL,
    frozen_digest VARCHAR(64) NOT NULL,
    billing_digest VARCHAR(64) NOT NULL,
    snapshot JSONB NOT NULL,
    status TEXT NOT NULL,
    quarantined_from_status TEXT,
    quarantine_reason TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    claim_owner TEXT NOT NULL DEFAULT '',
    claim_generation BIGINT NOT NULL DEFAULT 0,
    lease_until TIMESTAMPTZ,
    handoff_pending BOOLEAN NOT NULL DEFAULT TRUE,
    handoff_generation BIGINT NOT NULL,
    handoff_authorized BOOLEAN NOT NULL DEFAULT FALSE,
    handoff_authorized_at TIMESTAMPTZ,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (call_hash, reservation_generation) REFERENCES live_session_bindings(call_hash, generation) ON DELETE RESTRICT,
    CONSTRAINT live_settlements_handoff_generation_check CHECK (handoff_generation = reservation_generation),
    CONSTRAINT live_settlements_authorized_check CHECK (handoff_authorized = (handoff_authorized_at IS NOT NULL)),
    CONSTRAINT live_settlements_status_check CHECK (status IN ('pending', 'settled', 'quarantine')),
    CONSTRAINT live_settlements_previous_status_check CHECK (quarantined_from_status IS NULL OR quarantined_from_status IN ('pending', 'settled')),
    CONSTRAINT live_settlements_attempts_check CHECK (attempts >= 0 AND claim_generation >= 0 AND handoff_generation > 0),
    CONSTRAINT live_settlements_frozen_cost_check CHECK (status = 'quarantine' OR (frozen_total_cost >= 0 AND frozen_actual_cost >= 0))
);

CREATE INDEX idx_live_settlements_due ON live_settlements (next_retry_at, id)
    WHERE status = 'pending';
CREATE INDEX idx_live_settlements_user_created ON live_settlements (user_id, created_at, id);
CREATE INDEX idx_live_settlements_group_created ON live_settlements (group_id, created_at, id);
CREATE INDEX idx_live_settlements_handoff ON live_settlements (id)
    WHERE handoff_pending;
