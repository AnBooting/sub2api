package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type liveSettlementRepository struct {
	db           *sql.DB
	timezoneName string
	location     *time.Location
}

func NewLiveSettlementRepository(db *sql.DB, timezoneName string) (service.LiveSettlementStore, error) {
	if timezoneName == "" || timezoneName == "Local" || strings.TrimSpace(timezoneName) != timezoneName {
		return nil, service.ErrLiveSettlementInvalid
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return nil, err
	}
	return &liveSettlementRepository{db: db, timezoneName: timezoneName, location: location}, nil
}

func (r *liveSettlementRepository) RegisterFrozen(ctx context.Context, confirmed service.ConfirmedLiveFrozen) (service.RegisterResult, error) {
	snapshot, err := confirmed.ValidatedSnapshot()
	if err != nil {
		return service.RegisterResult{}, err
	}
	if snapshot.HandoffGeneration <= 0 {
		return service.RegisterResult{}, service.ErrLiveSettlementUnconfirmed
	}
	if r == nil || r.db == nil {
		return service.RegisterResult{}, service.ErrLiveSettlementStoreUnavailable
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return service.RegisterResult{}, err
	}
	digest, err := liveFrozenSnapshotDigest(snapshot)
	if err != nil {
		return service.RegisterResult{}, err
	}
	billingDigest, err := liveBillingSnapshotDigest(snapshot)
	if err != nil {
		return service.RegisterResult{}, err
	}
	termsFingerprint, err := snapshot.Identity.ReservationTerms(snapshot.Frozen.SettlementMode).Fingerprint()
	if err != nil {
		return service.RegisterResult{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.RegisterResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockLiveHashes(ctx, tx, snapshot.Identity.CallHash); err != nil {
		return service.RegisterResult{}, err
	}

	var operationID, identityFingerprint, status, storedTermsFingerprint string
	var generation int64
	err = tx.QueryRowContext(ctx, `SELECT operation_id, generation, identity_fingerprint, status, terms_fingerprint FROM live_session_reservations WHERE call_hash = $1 FOR UPDATE`, snapshot.Identity.CallHash).
		Scan(&operationID, &generation, &identityFingerprint, &status, &storedTermsFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return service.RegisterResult{}, service.ErrLiveSettlementUnconfirmed
	}
	if err != nil {
		return service.RegisterResult{}, err
	}
	if status == service.LiveReservationQuarantine || status == service.LiveReservationUnknown {
		return service.RegisterResult{}, service.ErrLiveSettlementQuarantined
	}
	if status != service.LiveReservationActive && status != service.LiveReservationFrozen {
		return service.RegisterResult{}, service.ErrLiveSettlementUnconfirmed
	}
	if generation != snapshot.HandoffGeneration || identityFingerprint != snapshot.IdentityFingerprint || storedTermsFingerprint != termsFingerprint {
		existing := struct {
			OperationID, IdentityFingerprint, TermsFingerprint string
			Generation                                         int64
		}{operationID, identityFingerprint, storedTermsFingerprint, generation}
		incoming := liveFrozenEvidence(snapshot, digest, billingDigest)
		if err := quarantineLiveReservation(ctx, tx, generation, "reservation_conflict", existing, incoming); err != nil {
			return service.RegisterResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.RegisterResult{}, err
		}
		return service.RegisterResult{}, service.ErrLiveSettlementConflict
	}

	// frozen 表示该 reservation 已经有（或曾经有）对应 job：跳过 INSERT，直接锁查现有 job。
	if status == service.LiveReservationActive {
		var id int64
		err = tx.QueryRowContext(ctx, `INSERT INTO live_settlements (call_hash, user_id, api_key_id, account_id, group_id, model, created_at, settlement_mode, frozen_total_cost, frozen_actual_cost, identity_fingerprint, frozen_digest, billing_digest, snapshot, status, quarantine_reason, reservation_generation, handoff_generation) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17) ON CONFLICT (call_hash) DO NOTHING RETURNING id`, snapshot.Identity.CallHash, snapshot.Identity.UserID, snapshot.Identity.APIKeyID, snapshot.Identity.AccountID,
			snapshot.Identity.GroupID, snapshot.Identity.Model, time.UnixMilli(snapshot.Identity.CreatedAtMillis).UTC(),
			snapshot.Frozen.SettlementMode, snapshot.Frozen.TotalCost, snapshot.Frozen.ActualCost,
			snapshot.IdentityFingerprint, digest, billingDigest, string(encoded), snapshot.InitialStatus, snapshot.QuarantineReason, generation).Scan(&id)
		if err == nil {
			if snapshot.InitialStatus == service.LiveSettlementJobQuarantine {
				_, err = tx.ExecContext(ctx, `UPDATE live_session_reservations SET status = 'quarantine', quarantine_reason = $2, updated_at = clock_timestamp() WHERE generation = $1`, generation, snapshot.QuarantineReason)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE live_session_reservations SET status = 'frozen', updated_at = clock_timestamp() WHERE generation = $1 AND status = 'active'`, generation)
			}
			if err != nil {
				return service.RegisterResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return service.RegisterResult{}, err
			}
			return service.RegisterResult{Created: true, Job: service.LiveSettlementJob{ID: id, Snapshot: snapshot, Status: snapshot.InitialStatus, FrozenDigest: digest, HandoffPending: true, HandoffGeneration: generation}}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return service.RegisterResult{}, err
		}
	}

	var id int64
	var apiKeyID, existingGeneration int64
	var existingIdentity, existingDigest, existingBillingDigest, existingStatus string
	var existingJSON []byte
	var handoffPending bool
	err = tx.QueryRowContext(ctx, `SELECT id, api_key_id, identity_fingerprint, frozen_digest, billing_digest, snapshot, status, handoff_generation, handoff_pending FROM live_settlements WHERE call_hash = $1 FOR UPDATE`, snapshot.Identity.CallHash).
		Scan(&id, &apiKeyID, &existingIdentity, &existingDigest, &existingBillingDigest, &existingJSON, &existingStatus, &existingGeneration, &handoffPending)
	if errors.Is(err, sql.ErrNoRows) {
		if status != service.LiveReservationFrozen {
			return service.RegisterResult{}, err
		}
		// frozen reservation 却没有对应 job：不一致状态，必须隔离而不是补建。
		if err := quarantineLiveReservation(ctx, tx, generation, "frozen_reservation_without_job", nil, liveFrozenEvidence(snapshot, digest, billingDigest)); err != nil {
			return service.RegisterResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.RegisterResult{}, err
		}
		return service.RegisterResult{}, service.ErrLiveSettlementConflict
	}
	if err != nil {
		return service.RegisterResult{}, err
	}
	var existing service.LiveFrozenSnapshot
	decodeErr := json.Unmarshal(existingJSON, &existing)
	storedDigest, digestErr := existing.ImmutableDigest()
	storedBilling, billingErr := liveBillingSnapshotDigest(existing)
	if apiKeyID != snapshot.Identity.APIKeyID || existingIdentity != snapshot.IdentityFingerprint || existingDigest != digest ||
		existingBillingDigest != billingDigest || existingGeneration != generation || decodeErr != nil || digestErr != nil ||
		billingErr != nil || storedDigest != existingDigest || storedBilling != existingBillingDigest || existing.HandoffGeneration != generation {
		existingEvidence := struct {
			IdentityFingerprint, FrozenDigest, BillingDigest string
			Generation                                       int64
		}{existingIdentity, existingDigest, existingBillingDigest, existingGeneration}
		incoming := liveFrozenEvidence(snapshot, digest, billingDigest)
		if err := quarantineLiveReservation(ctx, tx, generation, "frozen_conflict", existingEvidence, incoming); err != nil {
			return service.RegisterResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return service.RegisterResult{}, err
		}
		return service.RegisterResult{}, service.ErrLiveSettlementConflict
	}
	if existingStatus == service.LiveSettlementJobQuarantine {
		return service.RegisterResult{}, service.ErrLiveSettlementQuarantined
	}
	if err := tx.Commit(); err != nil {
		return service.RegisterResult{}, err
	}
	return service.RegisterResult{Job: service.LiveSettlementJob{ID: id, Snapshot: existing, Status: existingStatus, FrozenDigest: existingDigest, HandoffPending: handoffPending, HandoffGeneration: existingGeneration}}, nil
}
func liveFrozenEvidence(snapshot service.LiveFrozenSnapshot, digest, billingDigest string) any {
	return struct {
		Identity      service.LiveSettlementIdentity
		Frozen        service.LiveFrozenFact
		FrozenDigest  string
		BillingDigest string
		Generation    int64
	}{snapshot.Identity, snapshot.Frozen, digest, billingDigest, snapshot.HandoffGeneration}
}

func liveFrozenSnapshotDigest(snapshot service.LiveFrozenSnapshot) (string, error) {
	return snapshot.ImmutableDigest()
}

// liveBillingSnapshotDigest 计算结算命令与用量行的版本化白名单 digest。
// Version 1 只覆盖参与扣费/写行的实际字段（含原始 RequestFingerprint 与
// UA/IP/endpoint 等元数据），新增结算字段必须提升版本；Version 0 视为 legacy
// 全字段布局，保证升级前已入库行的重算 digest 不变；其余版本拒绝。
func liveBillingSnapshotDigest(snapshot service.LiveFrozenSnapshot) (string, error) {
	switch snapshot.Version {
	case 0:
		facts := struct {
			Billing *service.UsageBillingCommand
			Usage   service.UsageLog
		}{snapshot.Billing, snapshot.Usage}
		return liveRepositoryDigest(facts)
	case 1:
		facts := liveBillingWhitelistV1{
			Version: 1,
			Billing: liveBillingCommandWhitelistFrom(snapshot.Billing),
			Usage:   liveUsageWhitelistFrom(&snapshot.Usage),
		}
		return liveRepositoryDigest(facts)
	default:
		return "", service.ErrLiveSettlementInvalid
	}
}

// liveBillingWhitelistV1 是 billing digest 的 V1 字段白名单。字段顺序即序列化
// 顺序，任何字段的增删都必须通过版本升级；从该白名单可以重建扣费命令与用量行。
type liveBillingWhitelistV1 struct {
	Version int
	Billing *liveBillingCommandWhitelist
	Usage   *liveUsageWhitelist
}

type liveBillingCommandWhitelist struct {
	RequestID           string
	APIKeyID            int64
	RequestFingerprint  string
	RequestPayloadHash  string
	UserID              int64
	AccountID           int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string
	BalanceCost         float64
	SubscriptionCost    float64
	APIKeyQuotaCost     float64
	APIKeyRateLimitCost float64
	AccountQuotaCost    float64
}

type liveUsageWhitelist struct {
	UserID           int64
	APIKeyID         int64
	AccountID        int64
	RequestID        string
	Model            string
	RequestedModel   string
	GroupID          *int64
	SubscriptionID   *int64
	RateMultiplier   float64
	BillingType      int8
	RequestType      int16
	TotalCost        float64
	ActualCost       float64
	DurationMs       *int
	UserAgent        *string
	IPAddress        *string
	InboundEndpoint  *string
	UpstreamEndpoint *string
	BillingMode      *string
	CreatedAt        time.Time
}

func liveBillingCommandWhitelistFrom(cmd *service.UsageBillingCommand) *liveBillingCommandWhitelist {
	if cmd == nil {
		return nil
	}
	return &liveBillingCommandWhitelist{
		RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, RequestFingerprint: cmd.RequestFingerprint,
		RequestPayloadHash: cmd.RequestPayloadHash, UserID: cmd.UserID, AccountID: cmd.AccountID,
		SubscriptionID: cmd.SubscriptionID, AccountType: cmd.AccountType, Model: cmd.Model,
		ServiceTier: cmd.ServiceTier, ReasoningEffort: cmd.ReasoningEffort, BillingType: cmd.BillingType,
		InputTokens: cmd.InputTokens, OutputTokens: cmd.OutputTokens, CacheCreationTokens: cmd.CacheCreationTokens,
		CacheReadTokens: cmd.CacheReadTokens, ImageCount: cmd.ImageCount, MediaType: cmd.MediaType,
		BalanceCost: cmd.BalanceCost, SubscriptionCost: cmd.SubscriptionCost, APIKeyQuotaCost: cmd.APIKeyQuotaCost,
		APIKeyRateLimitCost: cmd.APIKeyRateLimitCost, AccountQuotaCost: cmd.AccountQuotaCost,
	}
}

func liveUsageWhitelistFrom(log *service.UsageLog) *liveUsageWhitelist {
	if log == nil {
		return nil
	}
	return &liveUsageWhitelist{
		UserID: log.UserID, APIKeyID: log.APIKeyID, AccountID: log.AccountID, RequestID: log.RequestID,
		Model: log.Model, RequestedModel: log.RequestedModel, GroupID: log.GroupID, SubscriptionID: log.SubscriptionID,
		RateMultiplier: log.RateMultiplier, BillingType: log.BillingType, RequestType: int16(log.RequestType),
		TotalCost: log.TotalCost, ActualCost: log.ActualCost, DurationMs: log.DurationMs,
		UserAgent: log.UserAgent, IPAddress: log.IPAddress, InboundEndpoint: log.InboundEndpoint,
		UpstreamEndpoint: log.UpstreamEndpoint, BillingMode: log.BillingMode, CreatedAt: log.CreatedAt,
	}
}

// liveBillingCommandFromWhitelist 从 snapshot 的 V1 白名单字段重建扣费命令，
// FinalizeClaimed 用它保证「从同一白名单重建命令、不读取未列入白名单的字段」。
func liveBillingCommandFromWhitelist(snapshot *service.LiveFrozenSnapshot) (*service.UsageBillingCommand, error) {
	if snapshot == nil || snapshot.Billing == nil || snapshot.Version != 1 {
		return nil, service.ErrLiveSettlementInvalid
	}
	cmd := &service.UsageBillingCommand{
		RequestID: snapshot.Billing.RequestID, APIKeyID: snapshot.Billing.APIKeyID,
		RequestFingerprint: snapshot.Billing.RequestFingerprint, RequestPayloadHash: snapshot.Billing.RequestPayloadHash,
		UserID: snapshot.Billing.UserID, AccountID: snapshot.Billing.AccountID,
		SubscriptionID: snapshot.Billing.SubscriptionID, AccountType: snapshot.Billing.AccountType,
		Model: snapshot.Billing.Model, ServiceTier: snapshot.Billing.ServiceTier,
		ReasoningEffort: snapshot.Billing.ReasoningEffort, BillingType: snapshot.Billing.BillingType,
		InputTokens: snapshot.Billing.InputTokens, OutputTokens: snapshot.Billing.OutputTokens,
		CacheCreationTokens: snapshot.Billing.CacheCreationTokens, CacheReadTokens: snapshot.Billing.CacheReadTokens,
		ImageCount: snapshot.Billing.ImageCount, MediaType: snapshot.Billing.MediaType,
		BalanceCost: snapshot.Billing.BalanceCost, SubscriptionCost: snapshot.Billing.SubscriptionCost,
		APIKeyQuotaCost: snapshot.Billing.APIKeyQuotaCost, APIKeyRateLimitCost: snapshot.Billing.APIKeyRateLimitCost,
		AccountQuotaCost: snapshot.Billing.AccountQuotaCost,
	}
	if snapshot.Usage.RequestedModel != "" && cmd.Model == "" {
		cmd.Model = snapshot.Usage.Model
	}
	return cmd, nil
}

func liveRepositoryDigest(facts any) (string, error) {
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (r *liveSettlementRepository) ClaimDue(ctx context.Context, owner string, limit int, ttl time.Duration) ([]service.LiveSettlementClaim, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || limit <= 0 || limit > 1000 || ttl < time.Second || ttl > 24*time.Hour {
		return nil, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return nil, service.ErrLiveSettlementStoreUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `WITH due AS (
			SELECT id FROM live_settlements
			WHERE status = 'pending' AND next_retry_at <= clock_timestamp()
			  AND (lease_until IS NULL OR lease_until <= clock_timestamp())
			ORDER BY next_retry_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
		)
		UPDATE live_settlements AS job SET claim_owner = $2,
			claim_generation = job.claim_generation + 1,
			lease_until = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
			attempts = job.attempts + 1, updated_at = clock_timestamp()
		FROM due WHERE job.id = due.id
		RETURNING job.id, job.snapshot, job.status, job.claim_owner, job.claim_generation, job.lease_until, job.frozen_digest, job.handoff_generation`, limit, owner, ttl.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := make([]service.LiveSettlementClaim, 0)
	for rows.Next() {
		var claim service.LiveSettlementClaim
		var encoded []byte
		if err := rows.Scan(&claim.Job.ID, &encoded, &claim.Job.Status, &claim.Owner, &claim.Generation, &claim.LeaseUntil, &claim.Job.FrozenDigest, &claim.Job.HandoffGeneration); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &claim.Job.Snapshot); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return claims, nil
}

func (r *liveSettlementRepository) FinalizeClaimed(ctx context.Context, claim service.LiveSettlementClaim) (service.FinalizeResult, error) {
	if r == nil || r.db == nil {
		return service.FinalizeResult{}, service.ErrLiveSettlementStoreUnavailable
	}
	if strings.TrimSpace(claim.Owner) == "" || claim.Generation <= 0 || claim.Job.ID <= 0 || claim.Job.Snapshot.Version != 1 {
		return service.FinalizeResult{}, service.ErrLiveSettlementInvalid
	}
	// 调用者提供的快照仅作参考，权威事实在事务内从已锁定行读取（见下方 fencing）。
	snapshot := claim.Job.Snapshot
	var cmd *service.UsageBillingCommand

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.FinalizeResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	// 1) claim fencing：锁定 PG 行并读取权威事实。绝不信任调用者传入的 snapshot /
	// digest / generation —— 服务若误用或篡改 Snapshot，可以用 job X 的 lease 写出
	// job Y 的账本并标 X settled。以数据库返回的版本为准。
	var authoritativeJSON []byte
	var authoritativeStatus, authoritativeDigest string
	var authoritativeGeneration int64
	err = tx.QueryRowContext(ctx, `SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements
		WHERE id = $1 AND claim_owner = $2 AND claim_generation = $3 AND status = 'pending' AND lease_until > clock_timestamp()
		FOR UPDATE`, claim.Job.ID, claim.Owner, claim.Generation).
		Scan(&authoritativeJSON, &authoritativeStatus, &authoritativeDigest, &authoritativeGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		var currentStatus string
		if readErr := tx.QueryRowContext(ctx, `SELECT status FROM live_settlements WHERE id = $1`, claim.Job.ID).Scan(&currentStatus); readErr != nil {
			if errors.Is(readErr, sql.ErrNoRows) {
				return service.FinalizeResult{}, service.ErrLiveSettlementLostLease
			}
			return service.FinalizeResult{}, readErr
		}
		if currentStatus == service.LiveSettlementJobSettled {
			if err := tx.Commit(); err != nil {
				return service.FinalizeResult{}, err
			}
			tx = nil
			return service.FinalizeResult{JobID: claim.Job.ID, AlreadyDone: true}, nil
		}
		return service.FinalizeResult{}, service.ErrLiveSettlementLostLease
	}
	if err != nil {
		return service.FinalizeResult{}, err
	}
	if err := json.Unmarshal(authoritativeJSON, &snapshot); err != nil {
		return service.FinalizeResult{}, service.ErrLiveSettlementConflict
	}
	if authoritativeStatus != service.LiveSettlementJobPending || authoritativeGeneration <= 0 ||
		authoritativeDigest == "" || snapshot.HandoffGeneration != authoritativeGeneration {
		return service.FinalizeResult{}, service.ErrLiveSettlementConflict
	}
	storedDigest, err := snapshot.ImmutableDigest()
	if err != nil || storedDigest != authoritativeDigest {
		return service.FinalizeResult{}, service.ErrLiveSettlementConflict
	}
	if snapshot.IdentityFingerprint == "" || snapshot.Usage.RequestID == "" || snapshot.Usage.APIKeyID <= 0 {
		return service.FinalizeResult{}, service.ErrLiveSettlementInvalid
	}
	if snapshot.Billing != nil {
		cmd, err = liveBillingCommandFromWhitelist(&snapshot)
		if err != nil {
			return service.FinalizeResult{}, err
		}
		if cmd.RequestID != snapshot.Usage.RequestID || cmd.APIKeyID != snapshot.Usage.APIKeyID {
			return service.FinalizeResult{}, service.ErrLiveSettlementInvalid
		}
	} else {
		cmd = nil
	}

	// quarantine 只提交隔离事实：调用方必须先回滚到 savepoint，确保扣费不随隔离一起提交。
	quarantine := func(reason string) (service.FinalizeResult, error) {
		if _, qerr := tx.ExecContext(ctx, `UPDATE live_settlements SET status = 'quarantine', quarantine_reason = $2, claim_owner = '', lease_until = NULL, claim_generation = claim_generation + 1, updated_at = clock_timestamp() WHERE id = $1 AND status = 'pending'`, claim.Job.ID, reason); qerr != nil {
			return service.FinalizeResult{}, qerr
		}
		if cerr := tx.Commit(); cerr != nil {
			return service.FinalizeResult{}, cerr
		}
		tx = nil
		return service.FinalizeResult{}, service.ErrLiveSettlementQuarantined
	}

	// 2) 先锁住并校验既有 usage 行，再动账本：任何不一致都在扣费之前发现（P1-2/P1-8）。
	existing, err := lockLiveUsageLogRow(ctx, tx, snapshot.Usage.RequestID, snapshot.Usage.APIKeyID)
	if err != nil {
		return service.FinalizeResult{}, err
	}
	if existing != nil && !existing.matches(&snapshot.Usage) {
		return quarantine(service.LiveUsageConflictReason)
	}

	applied := cmd == nil // 零额 / simple_skip 无账本命令，但也算本次完成结算
	result := &service.UsageBillingApplyResult{}
	if cmd != nil {
		// savepoint 让「扣费已生效但 usage 冲突」可回滚，避免隔离与扣费一起提交。
		if _, err := tx.ExecContext(ctx, `SAVEPOINT live_finalize_billing`); err != nil {
			return service.FinalizeResult{}, err
		}
		claimed, claimErr := claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
		if errors.Is(claimErr, service.ErrUsageBillingRequestConflict) {
			if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT live_finalize_billing`); err != nil {
				return service.FinalizeResult{}, err
			}
			return quarantine(service.LiveBillingConflictReason)
		}
		if claimErr != nil {
			return service.FinalizeResult{}, claimErr
		}
		if claimed {
			result.Applied = true
			if err := applyUsageBillingEffectsTx(ctx, tx, cmd, result); err != nil {
				return service.FinalizeResult{}, err
			}
		}
		applied = claimed
	}

	// 3) 落 usage 行：已存在且校验通过时只补幂等读回，不覆盖历史行。
	if existing == nil {
		written, werr := writeLiveUsageLogTx(ctx, tx, &snapshot)
		if werr != nil {
			if errors.Is(werr, service.ErrLiveSettlementConflict) && cmd != nil && applied {
				if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT live_finalize_billing`); err != nil {
					return service.FinalizeResult{}, err
				}
			}
			if errors.Is(werr, service.ErrLiveSettlementConflict) {
				return quarantine(service.LiveUsageConflictReason)
			}
			return service.FinalizeResult{}, werr
		}
		_ = written
	}

	if _, err := tx.ExecContext(ctx, `UPDATE live_settlements SET status = 'settled', updated_at = clock_timestamp() WHERE id = $1 AND status = 'pending'`, claim.Job.ID); err != nil {
		return service.FinalizeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.FinalizeResult{}, err
	}
	tx = nil
	if applied {
		// 零额 / simple_skip 无账本命令：Billing 保持 nil，表示本次未产生扣费。
		if cmd == nil {
			return service.FinalizeResult{JobID: claim.Job.ID, Applied: true}, nil
		}
		return service.FinalizeResult{JobID: claim.Job.ID, Applied: true, Billing: result}, nil
	}
	return service.FinalizeResult{JobID: claim.Job.ID, AlreadyDone: true}, nil
}

// liveLockedUsageLog 是已加锁的既有 usage 行中参与一致性判定的字段。
// 只有全部有意义字段等值才允许复用该行；未知/部分 legacy 行由调用方隔离人审。
type liveLockedUsageLog struct {
	UserID           int64
	APIKeyID         int64
	AccountID        int64
	RequestID        string
	Model            string
	RequestedModel   string
	GroupID          *int64
	SubscriptionID   *int64
	RateMultiplier   float64
	BillingType      int8
	RequestType      int16
	TotalCost        float64
	ActualCost       float64
	DurationMs       *int64
	UserAgent        *string
	IPAddress        *string
	InboundEndpoint  *string
	UpstreamEndpoint *string
	BillingMode      *string
	CreatedAt        time.Time
}

func sameOptionalInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// matches 要求所有有意义字段等值：同金额但归属/时间桶/统计维度不同必须判为冲突，
// 不能当作成功并标 settled（否则 usage 汇总会落到错误的用户/账号/时间桶）。
func (e *liveLockedUsageLog) matches(want *service.UsageLog) bool {
	if e == nil || want == nil {
		return false
	}
	if e.UserID != want.UserID || e.APIKeyID != want.APIKeyID || e.AccountID != want.AccountID ||
		e.RequestID != strings.TrimSpace(want.RequestID) || e.Model != want.Model || e.BillingType != want.BillingType ||
		e.RequestType != int16(want.RequestType) || e.RateMultiplier != want.RateMultiplier {
		return false
	}
	requestedModel := strings.TrimSpace(want.RequestedModel)
	if requestedModel == "" {
		requestedModel = strings.TrimSpace(want.Model)
	}
	if e.RequestedModel != requestedModel || !sameOptionalInt64(e.GroupID, want.GroupID) ||
		!sameOptionalInt64(e.SubscriptionID, want.SubscriptionID) || !sameOptionalInt64(e.DurationMs, int64Optional(want.DurationMs)) ||
		!sameOptionalString(e.UserAgent, want.UserAgent) || !sameOptionalString(e.IPAddress, want.IPAddress) ||
		!sameOptionalString(e.InboundEndpoint, want.InboundEndpoint) || !sameOptionalString(e.UpstreamEndpoint, want.UpstreamEndpoint) ||
		!sameOptionalString(e.BillingMode, want.BillingMode) {
		return false
	}
	return e.TotalCost == want.TotalCost && e.ActualCost == want.ActualCost && e.CreatedAt.UTC().Equal(want.CreatedAt.UTC())
}

func int64Optional(v *int) *int64 {
	if v == nil {
		return nil
	}
	value := int64(*v)
	return &value
}

// lockLiveUsageLogRow 读取并锁住既有 usage 行（存在时），供 Finalize 在扣费前预检。
// 返回 nil 表示该请求尚未落过 usage 行。锁保证比对与后续读回期间行不会被并发改写。
func lockLiveUsageLogRow(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64) (*liveLockedUsageLog, error) {
	var row liveLockedUsageLog
	var duration sql.NullInt64
	var groupID, subscriptionID sql.NullInt64
	var userAgent, ipAddress, inbound, upstream, billingMode sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT user_id, api_key_id, account_id, request_id, model, requested_model, group_id,
			subscription_id, rate_multiplier, billing_type, request_type, total_cost, actual_cost, duration_ms,
			user_agent, ip_address, inbound_endpoint, upstream_endpoint, billing_mode, created_at
		FROM usage_logs WHERE request_id = $1 AND api_key_id = $2 FOR UPDATE`, requestID, apiKeyID).
		Scan(&row.UserID, &row.APIKeyID, &row.AccountID, &row.RequestID, &row.Model, &row.RequestedModel, &groupID,
			&subscriptionID, &row.RateMultiplier, &row.BillingType, &row.RequestType, &row.TotalCost, &row.ActualCost, &duration,
			&userAgent, &ipAddress, &inbound, &upstream, &billingMode, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if groupID.Valid {
		value := groupID.Int64
		row.GroupID = &value
	}
	if subscriptionID.Valid {
		value := subscriptionID.Int64
		row.SubscriptionID = &value
	}
	if duration.Valid {
		value := duration.Int64
		row.DurationMs = &value
	}
	for _, pair := range []struct {
		src sql.NullString
		dst **string
	}{{userAgent, &row.UserAgent}, {ipAddress, &row.IPAddress}, {inbound, &row.InboundEndpoint}, {upstream, &row.UpstreamEndpoint}, {billingMode, &row.BillingMode}} {
		if pair.src.Valid {
			value := pair.src.String
			*pair.dst = &value
		}
	}
	return &row, nil
}

// writeLiveUsageLogTx 在同一事务内写入 Live 会话的 usage 行，返回是否新建。
// 唯一键冲突返回 ErrLiveSettlementConflict 交由调用方隔离，绝不覆盖历史行。
func writeLiveUsageLogTx(ctx context.Context, tx *sql.Tx, snapshot *service.LiveFrozenSnapshot) (bool, error) {
	if snapshot == nil {
		return false, service.ErrLiveSettlementInvalid
	}
	log := snapshot.Usage
	prepared := prepareUsageLogInsert(&log)
	query := `
		INSERT INTO usage_logs (
			user_id,
			api_key_id,
			account_id,
			request_id,
			model,
			requested_model,
			upstream_model,
			upstream_response_model,
			upstream_model_mismatch,
			group_id,
			subscription_id,
			input_tokens,
			output_tokens,
			cache_creation_tokens,
			cache_read_tokens,
			cache_creation_5m_tokens,
			cache_creation_1h_tokens,
			image_output_tokens,
			image_output_cost,
			image_input_tokens,
			image_input_cost,
			input_cost,
			output_cost,
			cache_creation_cost,
			cache_read_cost,
			total_cost,
			actual_cost,
			rate_multiplier,
			account_rate_multiplier,
			billing_type,
			request_type,
			stream,
			openai_ws_mode,
			duration_ms,
			first_token_ms,
			user_agent,
			ip_address,
			image_count,
			image_size,
			image_input_size,
			image_output_size,
			image_size_source,
			image_size_breakdown,
			video_count,
			video_resolution,
			video_duration_seconds,
			service_tier,
			reasoning_effort,
			requested_reasoning_effort,
			inbound_endpoint,
			upstream_endpoint,
			cache_ttl_overridden,
			long_context_billing_applied,
			channel_id,
			model_mapping_chain,
			billing_tier,
			billing_mode,
			account_stats_cost,
			upstream_request_id,
			session_id,
			native_compaction_v2,
			created_at,
			pricing_snapshot,
			requested_group_id,
			served_group_id,
			failover,
			attempt_count
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11,
			$12, $13, $14, $15,
			$16, $17, $18, $19,
			$20, $21, $22, $23, $24, $25,
			$26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62, $63, $64, $65, $66, $67
		)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`
	var id int64
	if err := tx.QueryRowContext(ctx, query, prepared.args...).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 唯一键冲突：不覆盖既有行，交由调用方隔离人审。
			return false, service.ErrLiveSettlementConflict
		}
		return false, err
	}
	return true, nil
}

func (r *liveSettlementRepository) Lookup(ctx context.Context, callHash string, apiKeyID int64) (service.LiveSettlementJob, error) {
	if callHash == "" || apiKeyID <= 0 {
		return service.LiveSettlementJob{}, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.LiveSettlementJob{}, service.ErrLiveSettlementStoreUnavailable
	}
	job, err := scanLiveSettlementHandoff(r.db.QueryRowContext(ctx, `SELECT id, snapshot, status, frozen_digest, handoff_pending, handoff_generation FROM live_settlements WHERE call_hash = $1 AND api_key_id = $2`, callHash, apiKeyID))
	if errors.Is(err, sql.ErrNoRows) {
		return service.LiveSettlementJob{}, service.ErrLiveCallNotFound
	}
	if err != nil {
		return service.LiveSettlementJob{}, err
	}
	if job.Snapshot.Identity.CallHash != callHash || job.Snapshot.Identity.APIKeyID != apiKeyID {
		return service.LiveSettlementJob{}, service.ErrLiveSettlementConflict
	}
	return job, nil
}

func (r *liveSettlementRepository) ListHandoffs(ctx context.Context, afterID int64, limit int) ([]service.LiveSettlementJob, error) {
	if afterID < 0 || limit <= 0 || limit > 1000 {
		return nil, service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return nil, service.ErrLiveSettlementStoreUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, snapshot, status, frozen_digest, handoff_pending, handoff_generation FROM live_settlements WHERE id > $1 AND handoff_pending AND status IN ('pending','settled') ORDER BY id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]service.LiveSettlementJob, 0)
	for rows.Next() {
		job, err := scanLiveSettlementHandoff(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type liveHandoffScanner interface{ Scan(dest ...any) error }

func scanLiveSettlementHandoff(row liveHandoffScanner) (service.LiveSettlementJob, error) {
	var job service.LiveSettlementJob
	var encoded []byte
	if err := row.Scan(&job.ID, &encoded, &job.Status, &job.FrozenDigest, &job.HandoffPending, &job.HandoffGeneration); err != nil {
		return service.LiveSettlementJob{}, err
	}
	if err := json.Unmarshal(encoded, &job.Snapshot); err != nil {
		return service.LiveSettlementJob{}, err
	}
	digest, err := job.Snapshot.ImmutableDigest()
	if err != nil {
		return service.LiveSettlementJob{}, err
	}
	if digest != job.FrozenDigest {
		return service.LiveSettlementJob{}, service.ErrLiveSettlementConflict
	}
	return job, nil
}

// AuthorizeHandoff 持久化 ACK 授权线性化点：同一行 FOR UPDATE 校验身份/摘要/代次，
// 已隔离拒绝；只允许从未授权 → 已授权，重复调用幂等。
func (r *liveSettlementRepository) AuthorizeHandoff(ctx context.Context, callHash, identityFingerprint, frozenDigest string, generation int64) error {
	if callHash == "" || identityFingerprint == "" || frozenDigest == "" || generation <= 0 {
		return service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.ErrLiveSettlementStoreUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var authorized bool
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status, handoff_authorized FROM live_settlements WHERE call_hash = $1 AND identity_fingerprint = $2 AND frozen_digest = $3 AND handoff_generation = $4 FOR UPDATE`,
		callHash, identityFingerprint, frozenDigest, generation).Scan(&status, &authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrLiveSettlementLostLease
	}
	if err != nil {
		return err
	}
	if status == service.LiveSettlementJobQuarantine {
		return service.ErrLiveSettlementQuarantined
	}
	if authorized {
		// 幂等：已授权直接提交（不可撤销）。
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_settlements SET handoff_authorized = TRUE, handoff_authorized_at = clock_timestamp(), updated_at = clock_timestamp() WHERE call_hash = $1 AND identity_fingerprint = $2 AND frozen_digest = $3 AND handoff_generation = $4 AND status <> 'quarantine'`,
		callHash, identityFingerprint, frozenDigest, generation); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// MarkHandoffDone 只接受已授权的任务：事务内先 SELECT handoff_authorized FOR UPDATE，
// 未授权 / 身份 / 摘要 / 代次不符 / 已隔离一律 ErrLiveSettlementLostLease。
func (r *liveSettlementRepository) MarkHandoffDone(ctx context.Context, callHash, identityFingerprint, frozenDigest string, generation int64) error {
	if callHash == "" || identityFingerprint == "" || frozenDigest == "" || generation <= 0 {
		return service.ErrLiveSettlementInvalid
	}
	if r == nil || r.db == nil {
		return service.ErrLiveSettlementStoreUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var authorized bool
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status, handoff_authorized FROM live_settlements WHERE call_hash = $1 AND identity_fingerprint = $2 AND frozen_digest = $3 AND handoff_generation = $4 FOR UPDATE`,
		callHash, identityFingerprint, frozenDigest, generation).Scan(&status, &authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrLiveSettlementLostLease
	}
	if err != nil {
		return err
	}
	if status == service.LiveSettlementJobQuarantine || !authorized {
		return service.ErrLiveSettlementLostLease
	}
	result, err := tx.ExecContext(ctx, `UPDATE live_settlements SET handoff_pending = FALSE, updated_at = clock_timestamp() WHERE call_hash = $1 AND identity_fingerprint = $2 AND frozen_digest = $3 AND handoff_generation = $4 AND status IN ('pending','settled')`, callHash, identityFingerprint, frozenDigest, generation)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrLiveSettlementLostLease
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
