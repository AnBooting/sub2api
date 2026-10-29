package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"
)

const (
	LiveSettlementJobPending    = "pending"
	LiveSettlementJobSettled    = "settled"
	LiveSettlementJobQuarantine = "quarantine"
)

const (
	// LiveUsageConflictReason 标记既有 usage 行与冻结快照不一致（归属/时间桶/金额等）。
	LiveUsageConflictReason = "usage_log_conflict"
	// LiveBillingConflictReason 标记幂等账本中同一 request 已绑定不同指纹。
	LiveBillingConflictReason = "usage_billing_conflict"
)

var (
	ErrLiveSettlementUnconfirmed      = errors.New("live settlement is not Redis-confirmed")
	ErrLiveSettlementConflict         = errors.New("live settlement frozen facts conflict")
	ErrLiveSettlementLostLease        = errors.New("live settlement claim has expired or changed")
	ErrLiveSettlementQuarantined      = errors.New("live settlement requires manual reconciliation")
	ErrLiveSettlementStoreUnavailable = errors.New("live settlement store is unavailable")
)

// LiveSettlementIdentity is the immutable billing identity at Redis precision.
type LiveSettlementIdentity struct {
	CallHash            string
	UserID              int64
	APIKeyID            int64
	AccountID           int64
	GroupID             int64
	SubscriptionID      int64
	LeaseHash           string
	Model               string
	CreatedAtMillis     int64
	ExpiresAtMillis     int64
	RealtimePricePerMin *float64
	BillingPolicy       *LiveBillingPolicy
	MetadataDigest      string
}

// LiveSettlementIdentityFromRecord derives the canonical identity used by Redis and PG.
func LiveSettlementIdentityFromRecord(record *LiveCallRecord) (LiveSettlementIdentity, string, error) {
	if !liveCallHasCompleteSnapshot(record) {
		return LiveSettlementIdentity{}, "", ErrLiveIdentityMismatch
	}
	identity, err := liveSettlementIdentityFields(record)
	if err != nil {
		return LiveSettlementIdentity{}, "", err
	}
	fingerprint, err := liveFrozenDigest(identity)
	return identity, fingerprint, err
}

func liveSettlementIdentityFields(record *LiveCallRecord) (LiveSettlementIdentity, error) {
	if record == nil || record.UserID <= 0 || record.APIKeyID <= 0 || record.AccountID <= 0 || record.LeaseID == "" || record.Model == "" {
		return LiveSettlementIdentity{}, ErrLiveIdentityMismatch
	}
	var price *float64
	if record.RealtimePricePerMin != nil {
		v := liveCanonicalZero(*record.RealtimePricePerMin)
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return LiveSettlementIdentity{}, ErrLiveSettlementInvalid
		}
		price = &v
	}
	var policy *LiveBillingPolicy
	if record.BillingPolicy != nil {
		copyPolicy := *record.BillingPolicy
		for _, value := range []*float64{&copyPolicy.APIKeyQuota, &copyPolicy.RateLimit5h, &copyPolicy.RateLimit1d, &copyPolicy.RateLimit7d} {
			if math.IsNaN(*value) || math.IsInf(*value, 0) {
				return LiveSettlementIdentity{}, ErrLiveSettlementInvalid
			}
			*value = liveCanonicalZero(*value)
		}
		policy = &copyPolicy
	}
	metadata, err := liveFrozenDigest(struct {
		UserAgent       string
		IPAddress       string
		InboundEndpoint string
	}{record.UserAgent, record.IPAddress, record.InboundEndpoint})
	if err != nil {
		return LiveSettlementIdentity{}, err
	}
	lease := sha256.Sum256([]byte(record.LeaseID))
	return LiveSettlementIdentity{
		CallHash: record.CallHash, UserID: record.UserID, APIKeyID: record.APIKeyID,
		AccountID: record.AccountID, GroupID: record.GroupID, SubscriptionID: record.SubscriptionID,
		LeaseHash: hex.EncodeToString(lease[:]), Model: record.Model,
		CreatedAtMillis: record.CreatedAt.UnixMilli(), ExpiresAtMillis: record.ExpiresAt.UnixMilli(),
		RealtimePricePerMin: price, BillingPolicy: policy, MetadataDigest: metadata,
	}, nil
}

const (
	LiveReservationProvisioning = "provisioning"
	LiveReservationBound        = "bound"
	LiveReservationActive       = "active"
	LiveReservationFrozen       = "frozen"
	LiveReservationUnknown      = "unknown"
	LiveReservationQuarantine   = "quarantine"
)

// LiveReservationTerms contain only facts available before the upstream request.
type LiveReservationTerms struct {
	UserID              int64
	APIKeyID            int64
	AccountID           int64
	GroupID             int64
	SubscriptionID      int64
	LeaseHash           string
	Model               string
	RealtimePricePerMin *float64
	BillingPolicy       *LiveBillingPolicy
	MetadataDigest      string
	SettlementMode      string
}

func (identity LiveSettlementIdentity) ReservationTerms(mode string) LiveReservationTerms {
	var price *float64
	if identity.RealtimePricePerMin != nil {
		copyPrice := *identity.RealtimePricePerMin
		price = &copyPrice
	}
	var policy *LiveBillingPolicy
	if identity.BillingPolicy != nil {
		copyPolicy := *identity.BillingPolicy
		policy = &copyPolicy
	}
	return LiveReservationTerms{
		UserID: identity.UserID, APIKeyID: identity.APIKeyID, AccountID: identity.AccountID,
		GroupID: identity.GroupID, SubscriptionID: identity.SubscriptionID, LeaseHash: identity.LeaseHash,
		Model: identity.Model, RealtimePricePerMin: price, BillingPolicy: policy,
		MetadataDigest: identity.MetadataDigest, SettlementMode: mode,
	}
}

// LiveReservationTermsFromRecord does not require an upstream call ID or timestamps.
func LiveReservationTermsFromRecord(record *LiveCallRecord, mode string) (LiveReservationTerms, error) {
	identity, err := liveSettlementIdentityFields(record)
	if err != nil {
		return LiveReservationTerms{}, err
	}
	terms := identity.ReservationTerms(mode)
	if _, err := terms.Fingerprint(); err != nil {
		return LiveReservationTerms{}, err
	}
	return terms, nil
}

func (terms LiveReservationTerms) Fingerprint() (string, error) {
	if terms.UserID <= 0 || terms.APIKeyID <= 0 || terms.AccountID <= 0 || terms.Model == "" || terms.LeaseHash == "" || terms.MetadataDigest == "" ||
		terms.RealtimePricePerMin == nil || *terms.RealtimePricePerMin <= 0 || math.IsNaN(*terms.RealtimePricePerMin) || math.IsInf(*terms.RealtimePricePerMin, 0) ||
		(terms.SettlementMode != liveSettlementModeStandard && terms.SettlementMode != liveSettlementModeSimpleSkip && terms.SettlementMode != liveSettlementModeSimpleKeyOnly) {
		return "", ErrLiveSettlementInvalid
	}
	return liveFrozenDigest(terms)
}

type LiveSessionReservation struct {
	OperationID         string
	Generation          int64
	Terms               LiveReservationTerms
	Status              string
	Identity            *LiveSettlementIdentity
	IdentityFingerprint string
}

// LiveFrozenFact excludes mutable Redis status, attempt and log flags.
type LiveFrozenFact struct {
	TotalCost      float64
	ActualCost     float64
	BillingMode    string
	SettlementMode string
	DurationMs     int
	FrozenAt       time.Time
}

// LiveFrozenSnapshot is a detached copy of the confirmed facts and billing command.
type LiveFrozenSnapshot struct {
	Version             int
	Identity            LiveSettlementIdentity
	IdentityFingerprint string
	Frozen              LiveFrozenFact
	SourceStatus        string
	HandoffGeneration   int64
	Billing             *UsageBillingCommand
	Usage               UsageLog
	InitialStatus       string
	QuarantineReason    string
}

// ImmutableDigest is the versioned Redis/PG handoff identity for frozen facts.
func (snapshot LiveFrozenSnapshot) ImmutableDigest() (string, error) {
	if snapshot.Version != 1 || snapshot.IdentityFingerprint == "" {
		return "", ErrLiveSettlementInvalid
	}
	identityFingerprint, err := liveFrozenDigest(snapshot.Identity)
	if err != nil {
		return "", err
	}
	if identityFingerprint != snapshot.IdentityFingerprint {
		return "", ErrLiveIdentityMismatch
	}
	facts := struct {
		Version             int
		IdentityFingerprint string
		Frozen              LiveFrozenFact
	}{snapshot.Version, snapshot.IdentityFingerprint, snapshot.Frozen}
	return liveFrozenDigest(facts)
}

// LiveHandoffBeginResult is returned only by the verified v2 Redis protocol.
type LiveHandoffBeginResult struct {
	Winner       *LiveSettlement
	Token        string
	FrozenDigest string
	Generation   int64
}

type LiveHandoffStore interface {
	BeginLiveSettlementVerifiedV2(ctx context.Context, record *LiveCallRecord, candidate *LiveSettlement, identityFingerprint, candidateFrozenDigest string, expectedGeneration int64) (LiveHandoffBeginResult, error)
}

// ConfirmedLiveFrozen can only be constructed by confirming the Redis checkpoint.
type ConfirmedLiveFrozen struct{ payload string }

// ValidatedSnapshot returns a detached copy; the zero value is never confirmed.
func (c ConfirmedLiveFrozen) ValidatedSnapshot() (LiveFrozenSnapshot, error) {
	if c.payload == "" {
		return LiveFrozenSnapshot{}, ErrLiveSettlementUnconfirmed
	}
	var snapshot LiveFrozenSnapshot
	if err := json.Unmarshal([]byte(c.payload), &snapshot); err != nil {
		return LiveFrozenSnapshot{}, ErrLiveSettlementUnconfirmed
	}
	return snapshot, nil
}

// ConfirmLiveFrozen accepts only a verified v2 winner bound to the PG creation generation.
// The caller must obtain expectedGeneration from committed provisioning/bind state.
func (s *OpenAIGatewayService) ConfirmLiveFrozen(ctx context.Context, store LiveHandoffStore, record *LiveCallRecord, candidate *LiveSettlement, expectedGeneration int64) (ConfirmedLiveFrozen, error) {
	if s == nil || store == nil || !liveCallHasCompleteSnapshot(record) || candidate == nil || expectedGeneration <= 0 {
		return ConfirmedLiveFrozen{}, ErrLiveSettlementUnconfirmed
	}
	recordCopy := *record
	if record.RealtimePricePerMin != nil {
		price := *record.RealtimePricePerMin
		if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return ConfirmedLiveFrozen{}, ErrLiveSettlementInvalid
		}
		recordCopy.RealtimePricePerMin = &price
	}
	if record.BillingPolicy != nil {
		policy := *record.BillingPolicy
		for _, value := range []*float64{&policy.APIKeyQuota, &policy.RateLimit5h, &policy.RateLimit1d, &policy.RateLimit7d} {
			if math.IsNaN(*value) || math.IsInf(*value, 0) {
				return ConfirmedLiveFrozen{}, ErrLiveSettlementInvalid
			}
			if *value == 0 {
				*value = 0
			}
		}
		recordCopy.BillingPolicy = &policy
	}
	record = &recordCopy
	candidateCopy := *candidate
	candidateCopy.TotalCost = liveCanonicalZero(candidateCopy.TotalCost)
	candidateCopy.ActualCost = liveCanonicalZero(candidateCopy.ActualCost)
	candidateCopy.FrozenAt = candidateCopy.FrozenAt.UTC()
	candidate = &candidateCopy
	if !validLiveFrozenCandidate(candidate) || (record.RealtimePricePerMin == nil && (candidate.TotalCost != 0 || candidate.ActualCost != 0)) {
		return ConfirmedLiveFrozen{}, ErrLiveSettlementInvalid
	}

	identity, identityFingerprint, err := LiveSettlementIdentityFromRecord(record)
	if err != nil {
		return ConfirmedLiveFrozen{}, err
	}
	proposed := LiveFrozenSnapshot{Version: 1, Identity: identity, IdentityFingerprint: identityFingerprint, Frozen: liveFrozenFact(candidate)}
	candidateDigest, err := proposed.ImmutableDigest()
	if err != nil {
		return ConfirmedLiveFrozen{}, err
	}
	result, err := store.BeginLiveSettlementVerifiedV2(ctx, record, candidate, identityFingerprint, candidateDigest, expectedGeneration)
	if err != nil {
		return ConfirmedLiveFrozen{}, err
	}
	var decoded LiveSettlement
	if result.Winner == nil || result.Token == "" || result.Generation != expectedGeneration ||
		json.Unmarshal([]byte(result.Token), &decoded) != nil || !reflect.DeepEqual(result.Winner, &decoded) {
		return ConfirmedLiveFrozen{}, ErrLiveSettlementUnconfirmed
	}
	stateCopy := *result.Winner
	stateCopy.TotalCost = liveCanonicalZero(stateCopy.TotalCost)
	stateCopy.ActualCost = liveCanonicalZero(stateCopy.ActualCost)
	stateCopy.FrozenAt = stateCopy.FrozenAt.UTC()
	state := &stateCopy
	snapshot := LiveFrozenSnapshot{
		Version: 1, Identity: identity, IdentityFingerprint: identityFingerprint,
		Frozen: liveFrozenFact(state), HandoffGeneration: result.Generation,
		SourceStatus: state.Status, InitialStatus: LiveSettlementJobPending,
	}
	actualDigest, err := snapshot.ImmutableDigest()
	if err != nil || actualDigest != result.FrozenDigest {
		return ConfirmedLiveFrozen{}, ErrLiveSettlementConflict
	}
	if record.RealtimePricePerMin == nil || *record.RealtimePricePerMin <= 0 ||
		math.IsNaN(*record.RealtimePricePerMin) || math.IsInf(*record.RealtimePricePerMin, 0) {
		snapshot.InitialStatus, snapshot.QuarantineReason = LiveSettlementJobQuarantine, "invalid_price"
	} else if !validLiveFrozenCandidate(state) || identity.ExpiresAtMillis <= identity.CreatedAtMillis {
		snapshot.InitialStatus, snapshot.QuarantineReason = LiveSettlementJobQuarantine, "invalid_frozen_state"
	}
	if snapshot.InitialStatus != LiveSettlementJobQuarantine {
		actual := state.ActualCost
		if state.SettlementMode == liveSettlementModeSimpleSkip {
			actual = 0
		}
		usage := liveUsageLog(record, state, actual)
		usage.CreatedAt = time.UnixMilli(identity.CreatedAtMillis).UTC()
		snapshot.Usage = *usage
		if state.SettlementMode != liveSettlementModeSimpleSkip && state.ActualCost > 0 {
			apiKey, _ := liveBillingAPIKey(record)
			keyOnly := state.SettlementMode == liveSettlementModeSimpleKeyOnly
			isSubscription := record.SubscriptionID > 0 && !keyOnly
			var subscription *UserSubscription
			if isSubscription {
				subscription = &UserSubscription{ID: record.SubscriptionID}
			}
			cost := &CostBreakdown{TotalCost: state.TotalCost, ActualCost: state.ActualCost, BillingMode: state.BillingMode}
			snapshot.Billing = buildUsageBillingCommand(record.CallHash, usage, &postUsageBillingParams{
				Cost: cost, User: &User{ID: record.UserID}, APIKey: apiKey,
				Account: &Account{ID: record.AccountID}, Subscription: subscription,
				IsSubscriptionBill: isSubscription, APIKeyService: s.apiKeyService,
				SimpleModeKeyRateLimitOnly: keyOnly,
			})
		}
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return ConfirmedLiveFrozen{}, err
	}
	return ConfirmedLiveFrozen{payload: string(payload)}, nil
}

func liveCanonicalZero(value float64) float64 {
	if value == 0 {
		return 0
	}
	return value
}

func liveFrozenFact(state *LiveSettlement) LiveFrozenFact {
	return LiveFrozenFact{TotalCost: liveCanonicalZero(state.TotalCost), ActualCost: liveCanonicalZero(state.ActualCost),
		BillingMode: state.BillingMode, SettlementMode: state.SettlementMode,
		DurationMs: state.DurationMs, FrozenAt: state.FrozenAt.UTC()}
}

func validLiveFrozenCandidate(state *LiveSettlement) bool {
	if state == nil || (state.Status != LiveSettlementPending && state.Status != LiveSettlementSettled) ||
		(state.SettlementMode != liveSettlementModeStandard && state.SettlementMode != liveSettlementModeSimpleSkip && state.SettlementMode != liveSettlementModeSimpleKeyOnly) ||
		state.DurationMs < 0 || state.FrozenAt.IsZero() || state.TotalCost < 0 || state.ActualCost < 0 {
		return false
	}
	return !math.IsNaN(state.TotalCost) && !math.IsInf(state.TotalCost, 0) &&
		!math.IsNaN(state.ActualCost) && !math.IsInf(state.ActualCost, 0)
}

func liveFrozenDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type LiveSettlementJob struct {
	ID                int64
	Snapshot          LiveFrozenSnapshot
	Status            string
	FrozenDigest      string
	HandoffPending    bool
	HandoffGeneration int64
}

type RegisterResult struct {
	Job     LiveSettlementJob
	Created bool
}

type LiveSettlementClaim struct {
	Job        LiveSettlementJob
	Owner      string
	Generation int64
	LeaseUntil time.Time
}

type FinalizeResult struct {
	JobID       int64
	Applied     bool
	AlreadyDone bool
	Billing     *UsageBillingApplyResult
}

type LiveSettlementStore interface {
	ReserveProvisioning(ctx context.Context, operationID string, terms LiveReservationTerms) (LiveSessionReservation, error)
	Bind(ctx context.Context, operationID string, generation int64, identity LiveSettlementIdentity) (LiveSessionReservation, error)
	// MarkActive 用已提交的 bound reservation 的持久证据（ConfirmLiveHandoffCreation)
	// 把 reservation 从 bound 推进到 active。proof 必须与该 reservation 的
	// operation/call_hash/api_key/identity_fingerprint/generation/protocol 完全一致；
	// quarantine/unknown/错代一律拒绝。完成 Redis 创建后调用，使 RegisterFrozen 可注册。
	MarkActive(ctx context.Context, proof ConfirmedLiveHandoffCreation) (LiveSessionReservation, error)
	RegisterFrozen(ctx context.Context, confirmed ConfirmedLiveFrozen) (RegisterResult, error)
	ClaimDue(ctx context.Context, owner string, limit int, ttl time.Duration) ([]LiveSettlementClaim, error)
	FinalizeClaimed(ctx context.Context, claim LiveSettlementClaim) (FinalizeResult, error)
	Lookup(ctx context.Context, callHash string, apiKeyID int64) (LiveSettlementJob, error)
	ListHandoffs(ctx context.Context, afterID int64, limit int) ([]LiveSettlementJob, error)
	// AuthorizeHandoff 持久化「该 job 的 Redis ACK 已被主库确认为已授权」，写入
	// handoff_authorized/at 作为线性化点。幂等可重放：同一 job 只能授权一次，
	// 对已隔离（quarantine）或身份/摘要/代次不符的 job 必须拒绝。
	AuthorizeHandoff(ctx context.Context, callHash, identityFingerprint, frozenDigest string, generation int64) error
	// MarkHandoffDone 仅接受已授权（handoff_authorized=TRUE）的 job；未授权、
	// 身份/摘要/代次不符或已隔离一律 ErrLiveSettlementLostLease。
	MarkHandoffDone(ctx context.Context, callHash, identityFingerprint, frozenDigest string, generation int64) error
}
