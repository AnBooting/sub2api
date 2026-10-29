package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type lockedLiveReservation struct {
	reservation         service.LiveSessionReservation
	termsFingerprint    string
	callHash            sql.NullString
	identityFingerprint sql.NullString
}

func scanLockedLiveReservation(row *sql.Row, operationID string) (lockedLiveReservation, error) {
	var result lockedLiveReservation
	var termsJSON, identityJSON []byte
	if err := row.Scan(&result.reservation.Generation, &termsJSON, &result.termsFingerprint,
		&result.reservation.Status, &result.callHash, &result.identityFingerprint, &identityJSON); err != nil {
		return result, err
	}
	result.reservation.OperationID = operationID
	if err := json.Unmarshal(termsJSON, &result.reservation.Terms); err != nil {
		return result, err
	}
	if len(identityJSON) > 0 {
		var identity service.LiveSettlementIdentity
		if err := json.Unmarshal(identityJSON, &identity); err != nil {
			return result, err
		}
		result.reservation.Identity = &identity
	}
	result.reservation.IdentityFingerprint = result.identityFingerprint.String
	return result, nil
}

func lockLiveHashes(ctx context.Context, tx *sql.Tx, hashes ...string) error {
	sort.Strings(hashes)
	previous := ""
	for _, hash := range hashes {
		if hash == "" || hash == previous {
			continue
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 824736119))`, hash); err != nil {
			return err
		}
		previous = hash
	}
	return nil
}

func peekLiveOperation(ctx context.Context, tx *sql.Tx, operationID string) (int64, string, error) {
	var generation int64
	var hash sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT generation, call_hash FROM live_session_reservations WHERE operation_id = $1`, operationID).Scan(&generation, &hash)
	return generation, hash.String, err
}

func lockLiveReservationPair(ctx context.Context, tx *sql.Tx, first, second int64) error {
	if first > second {
		first, second = second, first
	}
	rows, err := tx.QueryContext(ctx, `SELECT generation FROM live_session_reservations WHERE generation IN ($1,$2) ORDER BY generation FOR UPDATE`, first, second)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var generation int64
		if err := rows.Scan(&generation); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 2 {
		return service.ErrLiveSettlementConflict
	}
	return nil
}

func validLiveOperationID(operationID string) bool {
	return operationID != "" && len(operationID) <= 128 && operationID == strings.TrimSpace(operationID)
}

func (r *liveSettlementRepository) ReserveProvisioning(ctx context.Context, operationID string, terms service.LiveReservationTerms) (service.LiveSessionReservation, error) {
	if !validLiveOperationID(operationID) {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementInvalid
	}
	fingerprint, err := terms.Fingerprint()
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	encoded, err := json.Marshal(terms)
	if err != nil || len(encoded) > 16384 {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementStoreUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	observedGeneration, observedHash, peekErr := peekLiveOperation(ctx, tx, operationID)
	if peekErr != nil && !errors.Is(peekErr, sql.ErrNoRows) {
		return service.LiveSessionReservation{}, peekErr
	}
	if err := lockLiveHashes(ctx, tx, observedHash); err != nil {
		return service.LiveSessionReservation{}, err
	}
	var generation int64
	err = tx.QueryRowContext(ctx, `INSERT INTO live_session_reservations (operation_id, terms, terms_fingerprint) VALUES ($1,$2,$3) ON CONFLICT (operation_id) DO NOTHING RETURNING generation`, operationID, string(encoded), fingerprint).Scan(&generation)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return service.LiveSessionReservation{OperationID: operationID, Generation: generation, Terms: terms, Status: service.LiveReservationProvisioning}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return service.LiveSessionReservation{}, err
	}
	locked, err := scanLockedLiveReservation(tx.QueryRowContext(ctx, `SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id = $1 FOR UPDATE`, operationID), operationID)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if (observedGeneration != 0 && observedGeneration != locked.reservation.Generation) || locked.callHash.String != observedHash {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementLostLease
	}
	if locked.termsFingerprint != fingerprint {
		if err := quarantineLiveReservation(ctx, tx, locked.reservation.Generation, "provisioning_conflict", locked.reservation.Terms, terms); err != nil {
			return service.LiveSessionReservation{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
	}
	if err := tx.Commit(); err != nil {
		return service.LiveSessionReservation{}, err
	}
	return locked.reservation, nil
}

func (r *liveSettlementRepository) Bind(ctx context.Context, operationID string, generation int64, identity service.LiveSettlementIdentity) (service.LiveSessionReservation, error) {
	if !validLiveOperationID(operationID) || generation <= 0 || len(identity.CallHash) != 64 || identity.CreatedAtMillis <= 0 || identity.ExpiresAtMillis <= identity.CreatedAtMillis {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementStoreUnavailable
	}
	identityFingerprint, err := liveRepositoryDigest(identity)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	identityJSON, err := json.Marshal(identity)
	if err != nil || len(identityJSON) > 16384 {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	observedGeneration, observedHash, err := peekLiveOperation(ctx, tx, operationID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementUnconfirmed
	}
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if err := lockLiveHashes(ctx, tx, observedHash, identity.CallHash); err != nil {
		return service.LiveSessionReservation{}, err
	}

	var otherOperation, otherFingerprint string
	var otherGeneration int64
	bindingErr := tx.QueryRowContext(ctx, `SELECT operation_id, generation, identity_fingerprint FROM live_session_bindings WHERE call_hash = $1`, identity.CallHash).Scan(&otherOperation, &otherGeneration, &otherFingerprint)
	if bindingErr != nil && !errors.Is(bindingErr, sql.ErrNoRows) {
		return service.LiveSessionReservation{}, bindingErr
	}
	collision := bindingErr == nil && otherOperation != operationID
	if collision {
		if otherGeneration == observedGeneration {
			return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
		}
		if err := lockLiveReservationPair(ctx, tx, observedGeneration, otherGeneration); err != nil {
			return service.LiveSessionReservation{}, err
		}
	}
	locked, err := scanLockedLiveReservation(tx.QueryRowContext(ctx, `SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id = $1 FOR UPDATE`, operationID), operationID)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if locked.reservation.Generation != observedGeneration || locked.callHash.String != observedHash {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementLostLease
	}
	if collision {
		other, err := scanLockedLiveReservation(tx.QueryRowContext(ctx, `SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id = $1 FOR UPDATE`, otherOperation), otherOperation)
		if err != nil {
			return service.LiveSessionReservation{}, err
		}
		incoming := struct {
			Reservation service.LiveSessionReservation
			Identity    service.LiveSettlementIdentity
		}{locked.reservation, identity}
		existing := struct {
			Reservation        service.LiveSessionReservation
			BindingFingerprint string
		}{other.reservation, otherFingerprint}
		if err := quarantineLiveReservations(ctx, tx, "binding_collision", []liveQuarantineTarget{
			{generation: observedGeneration, existing: incoming, incoming: existing},
			{generation: otherGeneration, existing: existing, incoming: incoming},
		}); err != nil {
			return service.LiveSessionReservation{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
	}
	if locked.reservation.Status == service.LiveReservationQuarantine || locked.reservation.Status == service.LiveReservationUnknown {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementQuarantined
	}
	incomingTerms := identity.ReservationTerms(locked.reservation.Terms.SettlementMode)
	incomingFingerprint, err := incomingTerms.Fingerprint()
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if locked.reservation.Generation != generation || locked.termsFingerprint != incomingFingerprint ||
		(locked.callHash.Valid && (locked.callHash.String != identity.CallHash || locked.identityFingerprint.String != identityFingerprint)) {
		if err := quarantineLiveReservation(ctx, tx, locked.reservation.Generation, "binding_conflict", locked.reservation, identity); err != nil {
			return service.LiveSessionReservation{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
	}
	if locked.callHash.Valid {
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return locked.reservation, nil
	}
	if locked.reservation.Status != service.LiveReservationProvisioning {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementUnconfirmed
	}
	var storedHash string
	err = tx.QueryRowContext(ctx, `INSERT INTO live_session_bindings (call_hash, operation_id, generation, identity_fingerprint) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING call_hash`, identity.CallHash, operationID, generation, identityFingerprint).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		// A conflicting operation/hash not observed before locking must be retried.
		return service.LiveSessionReservation{}, service.ErrLiveSettlementLostLease
	}
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE live_session_reservations SET status = 'bound', call_hash = $1, bound_identity = $2, identity_fingerprint = $3, updated_at = clock_timestamp() WHERE generation = $4 AND status = 'provisioning'`, identity.CallHash, string(identityJSON), identityFingerprint, generation)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
	}
	if err := tx.Commit(); err != nil {
		return service.LiveSessionReservation{}, err
	}
	locked.reservation.Status = service.LiveReservationBound
	locked.reservation.Identity = &identity
	locked.reservation.IdentityFingerprint = identityFingerprint
	return locked.reservation, nil
}

type liveQuarantineTarget struct {
	generation int64
	existing   any
	incoming   any
}

func quarantineLiveReservation(ctx context.Context, tx *sql.Tx, generation int64, domain string, existing, incoming any) error {
	return quarantineLiveReservations(ctx, tx, domain, []liveQuarantineTarget{{generation: generation, existing: existing, incoming: incoming}})
}

// Caller has locked every reservation in generation order before calling this helper.
func quarantineLiveReservations(ctx context.Context, tx *sql.Tx, domain string, targets []liveQuarantineTarget) error {
	if len(targets) == 0 || len(targets) > 2 {
		return service.ErrLiveSettlementInvalid
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].generation < targets[j].generation })
	for _, target := range targets {
		oldJSON, err := json.Marshal(target.existing)
		if err != nil || len(oldJSON) > 16384 {
			return service.ErrLiveSettlementInvalid
		}
		newJSON, err := json.Marshal(target.incoming)
		if err != nil || len(newJSON) > 16384 {
			return service.ErrLiveSettlementInvalid
		}
		oldDigest, err := liveRepositoryDigest(target.existing)
		if err != nil {
			return err
		}
		newDigest, err := liveRepositoryDigest(target.incoming)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO live_settlement_conflicts (reservation_generation, domain, existing_digest, incoming_digest, existing_evidence, incoming_evidence) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, target.generation, domain, oldDigest, newDigest, string(oldJSON), string(newJSON)); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE live_session_reservations SET status = 'quarantine', quarantine_reason = $2, updated_at = clock_timestamp() WHERE generation = $1`, target.generation, domain)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return service.ErrLiveSettlementConflict
		}
	}
	first, last := targets[0].generation, targets[len(targets)-1].generation
	rows, err := tx.QueryContext(ctx, `SELECT id FROM live_settlements WHERE reservation_generation IN ($1,$2) ORDER BY id FOR UPDATE`, first, last)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := tx.ExecContext(ctx, `UPDATE live_settlements SET quarantined_from_status = CASE WHEN status = 'quarantine' THEN quarantined_from_status ELSE status END, status = 'quarantine', quarantine_reason = $2, claim_owner = '', lease_until = NULL, claim_generation = claim_generation + 1, updated_at = clock_timestamp() WHERE reservation_generation = $1`, target.generation, domain); err != nil {
			return err
		}
	}
	return nil
}

// MarkActive 实现 service.LiveSettlementStore。proof 必须来自已提交的 bound 状态
// （ConfirmLiveHandoffCreation 的持久证据），逐字段与库中 reservation 核对后仅允许
// bound→active；quarantine/unknown/错代/未绑定一律拒绝。RegisterFrozen 只接受
// active/frozen，此转换是 Reserve→Bind→Confirm→RegisterFrozen 链条的必经步骤。
func (r *liveSettlementRepository) MarkActive(ctx context.Context, proof service.ConfirmedLiveHandoffCreation) (service.LiveSessionReservation, error) {
	result, err := proof.Validate()
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if r == nil || r.db == nil {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementStoreUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockLiveHashes(ctx, tx, result.CallHash); err != nil {
		return service.LiveSessionReservation{}, err
	}
	locked, err := scanLockedLiveReservation(tx.QueryRowContext(ctx, `SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id = $1 FOR UPDATE`, result.OperationID), result.OperationID)
	if err != nil {
		return service.LiveSessionReservation{}, err
	}
	if locked.reservation.Generation != result.Generation || !locked.callHash.Valid || locked.callHash.String != result.CallHash ||
		locked.identityFingerprint.String != result.IdentityFingerprint || locked.reservation.IdentityFingerprint != result.IdentityFingerprint {
		return service.LiveSessionReservation{}, service.ErrLiveSettlementConflict
	}
	switch locked.reservation.Status {
	case service.LiveReservationActive:
		if err := tx.Commit(); err != nil {
			return service.LiveSessionReservation{}, err
		}
		return locked.reservation, nil
	case service.LiveReservationBound:
	default:
		return service.LiveSessionReservation{}, service.ErrLiveSettlementUnconfirmed
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_session_reservations SET status = 'active', updated_at = clock_timestamp() WHERE generation = $1 AND status = 'bound'`, result.Generation); err != nil {
		return service.LiveSessionReservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.LiveSessionReservation{}, err
	}
	locked.reservation.Status = service.LiveReservationActive
	return locked.reservation, nil
}

// LookupBoundLiveHandoff 实现 service.LiveHandoffBoundLookup：返回已提交的 bound
// reservation 供 ConfirmLiveHandoffCreation 核验。只允许 status='bound'，否则视为
// 未确认；调用方（Redis 创建协议）用该结果与冻结身份逐字段核对。
func (r *liveSettlementRepository) LookupBoundLiveHandoff(ctx context.Context, callHash string, apiKeyID int64) (service.LiveHandoffBoundReservation, error) {
	if strings.TrimSpace(callHash) == "" || apiKeyID <= 0 {
		return service.LiveHandoffBoundReservation{}, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.LiveHandoffBoundReservation{}, service.ErrLiveSettlementStoreUnavailable
	}
	var bound service.LiveHandoffBoundReservation
	err := r.db.QueryRowContext(ctx, `SELECT b.operation_id, b.call_hash, b.generation, r.status, r.identity_fingerprint
		FROM live_session_bindings b
		JOIN live_session_reservations r ON r.operation_id = b.operation_id AND r.generation = b.generation
		WHERE b.call_hash = $1`, callHash).Scan(&bound.OperationID, &bound.CallHash, &bound.Generation, &bound.Status, &bound.IdentityFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return service.LiveHandoffBoundReservation{}, service.ErrLiveSettlementUnconfirmed
	}
	if err != nil {
		return service.LiveHandoffBoundReservation{}, err
	}
	bound.APIKeyID = apiKeyID
	if bound.Status != "bound" || bound.OperationID == "" {
		return service.LiveHandoffBoundReservation{}, service.ErrLiveSettlementUnconfirmed
	}
	return bound, nil
}
