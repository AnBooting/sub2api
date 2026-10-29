package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// The guard and call keys are used by the standalone Redis client; the two-key
// scripts do not provide Redis Cluster cross-slot support. Guard retention and
// capacity control must be provided by the service integration before use.
func liveHandoffGuardKey(callHash string) string { return "live:v2:guard:" + callHash }

func liveHandoffOperationHash(operation string) string {
	digest := sha256.Sum256([]byte(operation))
	return hex.EncodeToString(digest[:])
}

func liveHandoffRecordArgs(record *service.LiveCallRecord) ([]any, error) {
	identity, _, err := service.LiveSettlementIdentityFromRecord(record)
	if err != nil {
		return nil, err
	}
	args := []any{
		"call_id", record.CallID,
		"user_id", strconv.FormatInt(record.UserID, 10),
		"account_id", strconv.FormatInt(record.AccountID, 10),
		"api_key_id", strconv.FormatInt(record.APIKeyID, 10),
		"group_id", strconv.FormatInt(record.GroupID, 10),
		"subscription_id", strconv.FormatInt(record.SubscriptionID, 10),
		"lease_id", record.LeaseID,
		"model", record.Model,
		"created_at", strconv.FormatInt(record.CreatedAt.UnixMilli(), 10),
		"expires_at", strconv.FormatInt(record.ExpiresAt.UnixMilli(), 10),
		"user_agent", record.UserAgent,
		"ip_address", record.IPAddress,
		"inbound_endpoint", record.InboundEndpoint,
	}
	if identity.RealtimePricePerMin != nil {
		args = append(args, "realtime_price_per_min", strconv.FormatFloat(*identity.RealtimePricePerMin, 'f', -1, 64))
	}
	if identity.BillingPolicy != nil {
		value, err := json.Marshal(identity.BillingPolicy)
		if err != nil {
			return nil, err
		}
		args = append(args, "billing_policy", string(value))
	}
	return args, nil
}

// liveHandoffPresence tails the ARGV list with presence markers for the
// optional price and policy fields. A nil field must be absent from the hash
// and a present field must exist there; empty and nil are distinct.
func liveHandoffPresence(record *service.LiveCallRecord) []any {
	price := "0"
	if record.RealtimePricePerMin != nil {
		price = "1"
	}
	policy := "0"
	if record.BillingPolicy != nil {
		policy = "1"
	}
	return []any{price, policy}
}

func liveHandoffControllerResult(result int, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	switch result {
	case -4:
		return false, service.ErrLiveHandoffProtocol
	case -3:
		return false, service.ErrLiveHandoffIncomplete
	case -2:
		return false, service.ErrLiveIdentityMismatch
	default:
		return result == 1, nil
	}
}

func (c *gatewayCache) ClaimLiveControllerV2(ctx context.Context, callHash, controller, owner string, expectedGeneration int64) (bool, error) {
	if callHash == "" || expectedGeneration <= 0 {
		return false, service.ErrLiveHandoffUnconfirmed
	}
	result, err := claimLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash), liveHandoffGuardKey(callHash)}, controller, owner, strconv.FormatInt(expectedGeneration, 10)).Int()
	return liveHandoffControllerResult(result, err)
}

func (c *gatewayCache) ReleaseLiveControllerV2(ctx context.Context, callHash, owner string, expectedGeneration int64) (bool, error) {
	if callHash == "" || expectedGeneration <= 0 {
		return false, service.ErrLiveHandoffUnconfirmed
	}
	result, err := releaseLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash), liveHandoffGuardKey(callHash)}, owner, strconv.FormatInt(expectedGeneration, 10)).Int()
	return liveHandoffControllerResult(result, err)
}

// A guard is written first so a partially failed creation cannot later be
// recreated by a legacy Save. A failed script is not a transactional rollback.
var createLiveCallV2Script = redis.NewScript(`
    local key, guard = KEYS[1], KEYS[2]
    local op, identity, generation, ttl = ARGV[1], ARGV[2], ARGV[3], ARGV[4]
    local guard_value = op .. ':' .. identity .. ':' .. generation
    local old_guard = redis.call('GET', guard)
    if old_guard then
        local price_present = redis.call('HGET', key, 'realtime_price_per_min') ~= false
        local policy_present = redis.call('HGET', key, 'billing_policy') ~= false
        if old_guard ~= guard_value then return -2 end
        if redis.call('PTTL', guard) ~= -1 then return -3 end
        if redis.call('EXISTS', key) == 0 then return -1 end
        if redis.call('HGET', key, 'handoff_v2_protocol') ~= '2' or
           redis.call('HGET', key, 'handoff_v2_phase') ~= 'active' or
           redis.call('HGET', key, 'handoff_v2_operation') ~= op or
           redis.call('HGET', key, 'handoff_v2_identity') ~= identity or
           redis.call('HGET', key, 'handoff_v2_generation') ~= generation then return -2 end
        if redis.call('PTTL', key) <= 0 then return -3 end
        for i = 5, #ARGV - 2, 2 do
            if redis.call('HGET', key, ARGV[i]) ~= ARGV[i + 1] then return -2 end
        end
        if (ARGV[#ARGV - 1] == '1') ~= price_present then return -2 end
        if (ARGV[#ARGV] == '1') ~= policy_present then return -2 end
        return 1
    end
    if redis.call('EXISTS', key) ~= 0 then return -2 end
    redis.call('SET', guard, guard_value)
    redis.call('HSET', key, 'handoff_v2_protocol', '2', 'handoff_v2_phase', 'creating',
        'handoff_v2_operation', op, 'handoff_v2_identity', identity,
        'handoff_v2_generation', generation, 'controller', 'pending', 'controller_owner', '')
    for i = 5, #ARGV - 2, 2 do redis.call('HSET', key, ARGV[i], ARGV[i + 1]) end
    if redis.call('PEXPIRE', key, ttl) ~= 1 then return -3 end
    redis.call('HSET', key, 'handoff_v2_phase', 'active')
    return 1
`)

func (c *gatewayCache) CreateLiveCallV2(ctx context.Context, record *service.LiveCallRecord, bound service.LiveHandoffBoundReservation, ttl time.Duration) (service.LiveHandoffCreationResult, error) {
	if record == nil || bound.Status != "bound" || strings.TrimSpace(bound.OperationID) == "" || bound.Generation <= 0 || ttl < time.Millisecond || record.Settlement != nil || record.Controller != service.LiveControllerPending || record.ControllerOwner != "" || record.RealtimePricePerMin == nil {
		return service.LiveHandoffCreationResult{}, service.ErrLiveHandoffUnconfirmed
	}
	_, fingerprint, err := service.LiveSettlementIdentityFromRecord(record)
	if err != nil {
		return service.LiveHandoffCreationResult{}, err
	}
	if bound.CallHash != record.CallHash || bound.APIKeyID != record.APIKeyID || bound.IdentityFingerprint != fingerprint {
		return service.LiveHandoffCreationResult{}, service.ErrLiveIdentityMismatch
	}
	fields, err := liveHandoffRecordArgs(record)
	if err != nil {
		return service.LiveHandoffCreationResult{}, err
	}
	fields = append(fields, "attestation", record.AttestationCiphertext)
	args := []any{liveHandoffOperationHash(bound.OperationID), fingerprint, strconv.FormatInt(bound.Generation, 10), strconv.FormatInt(ttl.Milliseconds(), 10)}
	args = append(args, fields...)
	args = append(args, liveHandoffPresence(record)...)
	result, err := createLiveCallV2Script.Run(ctx, c.rdb, []string{liveCallKey(record.CallHash), liveHandoffGuardKey(record.CallHash)}, args...).Int()
	if err != nil {
		return service.LiveHandoffCreationResult{}, err
	}
	switch result {
	case -1:
		return service.LiveHandoffCreationResult{}, service.ErrLiveCallNotFound
	case -2:
		return service.LiveHandoffCreationResult{}, service.ErrLiveIdentityMismatch
	case -3:
		return service.LiveHandoffCreationResult{}, service.ErrLiveHandoffIncomplete
	case 1:
		return service.LiveHandoffCreationResult{OperationID: bound.OperationID, CallHash: bound.CallHash, APIKeyID: bound.APIKeyID, IdentityFingerprint: fingerprint, Generation: bound.Generation, ProtocolVersion: service.LiveHandoffProtocolV2}, nil
	default:
		return service.LiveHandoffCreationResult{}, service.ErrLiveHandoffIncomplete
	}
}

var beginLiveHandoffV2Script = redis.NewScript(`
    local key, guard = KEYS[1], KEYS[2]
    if redis.call('EXISTS', key) == 0 then return {-1, '', '', ''} end
    if redis.call('HGET', key, 'handoff_v2_protocol') ~= '2' then return {-4, '', '', ''} end
    local phase = redis.call('HGET', key, 'handoff_v2_phase')
    local identity, digest, generation = ARGV[1], ARGV[2], ARGV[3]
    local op = redis.call('HGET', key, 'handoff_v2_operation')
    if not op or redis.call('GET', guard) ~= op .. ':' .. identity .. ':' .. generation or
       redis.call('HGET', key, 'handoff_v2_identity') ~= identity or
       redis.call('HGET', key, 'handoff_v2_generation') ~= generation then return {-2, '', '', ''} end
    if redis.call('PTTL', guard) ~= -1 then return {-3, '', '', ''} end
    for i = 5, #ARGV - 2, 2 do
        if redis.call('HGET', key, ARGV[i]) ~= ARGV[i + 1] then return {-2, '', '', ''} end
    end
    if (redis.call('HGET', key, 'realtime_price_per_min') ~= false) ~= (ARGV[#ARGV - 1] == '1') then return {-2, '', '', ''} end
    if (redis.call('HGET', key, 'billing_policy') ~= false) ~= (ARGV[#ARGV] == '1') then return {-2, '', '', ''} end
    if phase == 'frozen' or phase == 'acked' then
        local winner = redis.call('HGET', key, 'settlement')
        local stored_digest = redis.call('HGET', key, 'handoff_v2_frozen_digest')
        if not winner or not stored_digest then return {-3, '', '', ''} end
        if phase == 'frozen' and redis.call('PTTL', key) ~= -1 then return {-3, '', '', ''} end
        return {1, winner, stored_digest, generation}
    end
    if phase ~= 'active' or redis.call('PTTL', key) <= 0 or redis.call('HGET', key, 'settlement') then return {-3, '', '', ''} end
    redis.call('PERSIST', key)
    redis.call('HSET', key, 'settlement', ARGV[4], 'handoff_v2_frozen_digest', digest,
        'handoff_v2_phase', 'frozen', 'controller', 'closed', 'controller_owner', '')
    return {1, ARGV[4], digest, generation}
`)

func liveHandoffFrozenDigest(record *service.LiveCallRecord, state *service.LiveSettlement) (string, string, error) {
	if state == nil || (state.Status != service.LiveSettlementPending && state.Status != service.LiveSettlementSettled) ||
		(state.SettlementMode != "standard" && state.SettlementMode != "simple_skip" && state.SettlementMode != "simple_key_only") ||
		state.DurationMs < 0 || state.FrozenAt.IsZero() || state.TotalCost < 0 || state.ActualCost < 0 ||
		math.IsNaN(state.TotalCost) || math.IsInf(state.TotalCost, 0) || math.IsNaN(state.ActualCost) || math.IsInf(state.ActualCost, 0) {
		return "", "", service.ErrLiveSettlementInvalid
	}
	identity, fingerprint, err := service.LiveSettlementIdentityFromRecord(record)
	if err != nil {
		return "", "", err
	}
	if record.RealtimePricePerMin == nil && (state.TotalCost != 0 || state.ActualCost != 0) {
		return "", "", service.ErrLiveIdentityMismatch
	}
	snapshot := service.LiveFrozenSnapshot{Version: 1, Identity: identity, IdentityFingerprint: fingerprint,
		Frozen: service.LiveFrozenFact{TotalCost: state.TotalCost, ActualCost: state.ActualCost, BillingMode: state.BillingMode,
			SettlementMode: state.SettlementMode, DurationMs: state.DurationMs, FrozenAt: state.FrozenAt.UTC()}}
	digest, err := snapshot.ImmutableDigest()
	return fingerprint, digest, err
}

func (c *gatewayCache) BeginLiveSettlementVerifiedV2(ctx context.Context, record *service.LiveCallRecord, candidate *service.LiveSettlement, identityFingerprint, candidateFrozenDigest string, expectedGeneration int64) (service.LiveHandoffBeginResult, error) {
	if record == nil || candidate == nil || expectedGeneration <= 0 {
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffUnconfirmed
	}
	fingerprint, digest, err := liveHandoffFrozenDigest(record, candidate)
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	if fingerprint != identityFingerprint || digest != candidateFrozenDigest {
		return service.LiveHandoffBeginResult{}, service.ErrLiveIdentityMismatch
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	fields, err := liveHandoffRecordArgs(record)
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	generation := strconv.FormatInt(expectedGeneration, 10)
	args := []any{fingerprint, digest, generation, string(encoded)}
	args = append(args, fields...)
	args = append(args, liveHandoffPresence(record)...)
	raw, err := beginLiveHandoffV2Script.Run(ctx, c.rdb, []string{liveCallKey(record.CallHash), liveHandoffGuardKey(record.CallHash)}, args...).Slice()
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	if len(raw) != 4 {
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffIncomplete
	}
	code, ok := raw[0].(int64)
	if !ok {
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffIncomplete
	}
	switch code {
	case -1:
		return service.LiveHandoffBeginResult{}, service.ErrLiveCallNotFound
	case -2:
		return service.LiveHandoffBeginResult{}, service.ErrLiveIdentityMismatch
	case -3:
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffIncomplete
	case -4:
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffProtocol
	case 1:
	default:
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffIncomplete
	}
	token, ok1 := raw[1].(string)
	returnedDigest, ok2 := raw[2].(string)
	returnedGeneration, ok3 := raw[3].(string)
	if !ok1 || !ok2 || !ok3 || returnedGeneration != generation {
		return service.LiveHandoffBeginResult{}, service.ErrLiveHandoffIncomplete
	}
	var winner service.LiveSettlement
	if err := json.Unmarshal([]byte(token), &winner); err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	returnedIdentity, computedDigest, err := liveHandoffFrozenDigest(record, &winner)
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	if returnedIdentity != fingerprint || computedDigest != returnedDigest {
		return service.LiveHandoffBeginResult{}, service.ErrLiveIdentityMismatch
	}
	return service.LiveHandoffBeginResult{Winner: &winner, Token: token, FrozenDigest: returnedDigest, Generation: expectedGeneration}, nil
}

var acknowledgeLiveHandoffV2Script = redis.NewScript(`
    local key, guard = KEYS[1], KEYS[2]
    if redis.call('EXISTS', key) == 0 then return -1 end
    if redis.call('HGET', key, 'handoff_v2_protocol') ~= '2' then return -4 end
    local identity, digest, generation, ttl = ARGV[1], ARGV[2], ARGV[3], ARGV[4]
    local op = redis.call('HGET', key, 'handoff_v2_operation')
    if not op or redis.call('GET', guard) ~= op .. ':' .. identity .. ':' .. generation or
       redis.call('HGET', key, 'handoff_v2_identity') ~= identity or
       redis.call('HGET', key, 'handoff_v2_generation') ~= generation or
       redis.call('HGET', key, 'handoff_v2_frozen_digest') ~= digest then return -2 end
    if redis.call('PTTL', guard) ~= -1 then return -3 end
    local phase = redis.call('HGET', key, 'handoff_v2_phase')
    if not redis.call('HGET', key, 'settlement') or (phase ~= 'frozen' and phase ~= 'acked') then return -3 end
    if phase == 'frozen' then
        if redis.call('PTTL', key) ~= -1 then return -3 end
        redis.call('HSET', key, 'handoff_v2_phase', 'acked')
        if redis.call('PEXPIRE', key, ttl) ~= 1 then return -3 end
    elseif redis.call('PTTL', key) == -1 then
        if redis.call('PEXPIRE', key, ttl) ~= 1 then return -3 end
    end
    return 1
`)

func (c *gatewayCache) AcknowledgeLiveHandoffV2(ctx context.Context, ref service.LiveHandoffAckReference, ttl time.Duration) error {
	if ref.CallHash == "" || ref.IdentityFingerprint == "" || ref.FrozenDigest == "" || ref.Generation <= 0 || ttl < time.Millisecond {
		return service.ErrLiveHandoffUnconfirmed
	}
	result, err := acknowledgeLiveHandoffV2Script.Run(ctx, c.rdb, []string{liveCallKey(ref.CallHash), liveHandoffGuardKey(ref.CallHash)}, ref.IdentityFingerprint, ref.FrozenDigest, strconv.FormatInt(ref.Generation, 10), strconv.FormatInt(ttl.Milliseconds(), 10)).Int()
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case -1:
		return service.ErrLiveCallNotFound
	case -2:
		return service.ErrLiveIdentityMismatch
	case -3:
		return service.ErrLiveHandoffIncomplete
	case -4:
		return service.ErrLiveHandoffProtocol
	default:
		return service.ErrLiveHandoffIncomplete
	}
}

var inspectLiveHandoffV2Script = redis.NewScript(`
    local key, guard = KEYS[1], KEYS[2]
    if redis.call('EXISTS', key) == 0 then
        local value = redis.call('GET', guard)
        if not value then return {-1, '', '', '', ''} end
        local op, identity, generation = string.match(value, '^([0-9a-f]+):([0-9a-f]+):([0-9]+)$')
        if not op or #op ~= 64 or #identity ~= 64 then return {-3, '', '', '', ''} end
        if redis.call('PTTL', guard) ~= -1 then return {-3, '', '', '', ''} end
        return {1, 'guard_only', identity, '', generation}
    end
    if redis.call('HGET', key, 'handoff_v2_protocol') ~= '2' then return {-2, '', '', '', ''} end
    local identity = redis.call('HGET', key, 'handoff_v2_identity') or ''
    local generation = redis.call('HGET', key, 'handoff_v2_generation') or ''
    local op = redis.call('HGET', key, 'handoff_v2_operation') or ''
    if redis.call('GET', guard) ~= op .. ':' .. identity .. ':' .. generation then return {-3, '', '', '', ''} end
    if redis.call('PTTL', guard) ~= -1 then return {-3, '', '', '', ''} end
    local phase = redis.call('HGET', key, 'handoff_v2_phase') or ''
    if phase == 'creating' then phase = 'creation_incomplete'
    elseif phase == 'active' and redis.call('PTTL', key) == -1 and not redis.call('HGET', key, 'settlement') then phase = 'freeze_incomplete' end
    local digest = redis.call('HGET', key, 'handoff_v2_frozen_digest') or ''
    return {1, phase, identity, digest, generation}
`)

func (c *gatewayCache) InspectLiveHandoffV2(ctx context.Context, callHash string) (service.LiveHandoffStatus, error) {
	if callHash == "" {
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffUnconfirmed
	}
	raw, err := inspectLiveHandoffV2Script.Run(ctx, c.rdb, []string{liveCallKey(callHash), liveHandoffGuardKey(callHash)}).Slice()
	if err != nil {
		return service.LiveHandoffStatus{}, err
	}
	if len(raw) != 5 {
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffIncomplete
	}
	code, ok := raw[0].(int64)
	if !ok {
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffIncomplete
	}
	switch code {
	case -1:
		return service.LiveHandoffStatus{}, service.ErrLiveCallNotFound
	case -2:
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffProtocol
	case -3:
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffIncomplete
	case 1:
	default:
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffIncomplete
	}
	phase, a := raw[1].(string)
	identity, b := raw[2].(string)
	digest, d := raw[3].(string)
	generation, e := raw[4].(string)
	if !a || !b || !d || !e {
		return service.LiveHandoffStatus{}, service.ErrLiveHandoffIncomplete
	}
	parsed, err := strconv.ParseInt(generation, 10, 64)
	if err != nil || parsed <= 0 {
		return service.LiveHandoffStatus{}, fmt.Errorf("invalid live handoff generation")
	}
	return service.LiveHandoffStatus{Phase: phase, IdentityFingerprint: identity, FrozenDigest: digest, Generation: parsed}, nil
}
