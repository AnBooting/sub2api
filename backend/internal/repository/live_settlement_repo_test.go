//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newLiveSettlementTestRepository(t *testing.T, db *sql.DB) service.LiveSettlementStore {
	t.Helper()
	store, err := NewLiveSettlementRepository(db, "Asia/Shanghai")
	require.NoError(t, err)
	return store
}

func TestLiveSettlementConstructorRejectsInvalidTimezone(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	for _, zone := range []string{"", "Local", "Invalid/Not_A_Zone"} {
		_, err := NewLiveSettlementRepository(db, zone)
		require.Error(t, err)
	}
	for _, zone := range []string{"Asia/Shanghai", "America/New_York", "UTC"} {
		_, err := NewLiveSettlementRepository(db, zone)
		require.NoError(t, err)
	}
}

// testLiveHandoffStore models the v2 response contract; it is not a Redis Lua test.
type testLiveHandoffStore struct {
	base       service.LiveCallStore
	identity   string
	digest     string
	generation int64
}

func (s *testLiveHandoffStore) BeginLiveSettlementVerifiedV2(ctx context.Context, record *service.LiveCallRecord, candidate *service.LiveSettlement, identityFingerprint, candidateFrozenDigest string, expectedGeneration int64) (service.LiveHandoffBeginResult, error) {
	winner, token, _, err := s.base.BeginLiveSettlementVerified(ctx, record, candidate, time.Hour)
	if err != nil {
		return service.LiveHandoffBeginResult{}, err
	}
	if s.identity == "" {
		s.identity, s.digest, s.generation = identityFingerprint, candidateFrozenDigest, expectedGeneration
	}
	if s.identity != identityFingerprint {
		return service.LiveHandoffBeginResult{}, service.ErrLiveIdentityMismatch
	}
	return service.LiveHandoffBeginResult{Winner: winner, Token: token, FrozenDigest: s.digest, Generation: s.generation}, nil
}

func newLiveSettlementConfirmedFixture(t *testing.T, price *float64, candidate *service.LiveSettlement) (service.ConfirmedLiveFrozen, *service.LiveCallRecord, *testLiveHandoffStore) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.LiveCallStore)
	created := time.UnixMilli(1800000000123)
	record := &service.LiveCallRecord{
		CallID: "raw-upstream-call-id", CallHash: HashLiveCallID("raw-upstream-call-id"),
		UserID: 3, APIKeyID: 4, AccountID: 5, GroupID: 6, LeaseID: "lease-id",
		Model: "gpt-live", CreatedAt: created, ExpiresAt: created.Add(time.Hour),
		RealtimePricePerMin: price, BillingPolicy: &service.LiveBillingPolicy{RateLimit5h: 5},
		Controller: service.LiveControllerPending, UserAgent: "secret-user-agent",
		IPAddress: "192.0.2.7", AttestationCiphertext: "secret-attestation",
	}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	v2 := &testLiveHandoffStore{base: store}
	snapshot, err := (&service.OpenAIGatewayService{}).ConfirmLiveFrozen(context.Background(), v2, record, candidate, 7)
	require.NoError(t, err)
	return snapshot, record, v2
}

func TestLiveSettlementRegisterRequiresRedisConfirmation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), service.ConfirmedLiveFrozen{})
	require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementConfirmedSnapshotCannotBeAltered(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	snapshot.Frozen.ActualCost = 12
	reloaded, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, 0.20, reloaded.Frozen.ActualCost)
}

func TestLiveSettlementConfirmationUsesRedisWinnerAndSanitizesSnapshot(t *testing.T) {
	price := 0.20
	winner := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	first, record, store := newLiveSettlementConfirmedFixture(t, &price, winner)
	later := *winner
	later.DurationMs = 120000
	later.TotalCost, later.ActualCost = 0.40, 0.40
	later.FrozenAt = later.FrozenAt.Add(time.Second)
	second, err := (&service.OpenAIGatewayService{}).ConfirmLiveFrozen(context.Background(), store, record, &later, 7)
	require.NoError(t, err)
	firstSnapshot, err := first.ValidatedSnapshot()
	require.NoError(t, err)
	secondSnapshot, err := second.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, firstSnapshot.Frozen.DurationMs, secondSnapshot.Frozen.DurationMs)
	require.Equal(t, firstSnapshot.Frozen.TotalCost, secondSnapshot.Frozen.TotalCost)
	require.Equal(t, firstSnapshot.Frozen.ActualCost, secondSnapshot.Frozen.ActualCost)
	require.Equal(t, firstSnapshot.Frozen.FrozenAt, secondSnapshot.Frozen.FrozenAt)

	encoded, err := json.Marshal(firstSnapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), record.CallID)
	require.Contains(t, string(encoded), record.UserAgent)
	require.Contains(t, string(encoded), record.IPAddress)
	require.NotContains(t, string(encoded), record.AttestationCiphertext)
}

func TestLiveSettlementV2RejectsUnboundDigestAndGeneration(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	_, record, store := newLiveSettlementConfirmedFixture(t, &price, candidate)
	digest := store.digest
	store.digest = "wrong-digest"
	_, err := (&service.OpenAIGatewayService{}).ConfirmLiveFrozen(context.Background(), store, record, candidate, 7)
	require.ErrorIs(t, err, service.ErrLiveSettlementConflict)
	store.digest, store.generation = digest, 0
	_, err = (&service.OpenAIGatewayService{}).ConfirmLiveFrozen(context.Background(), store, record, candidate, 7)
	require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
}

func TestLiveSettlementUnpricedLegacyZeroIsQuarantined(t *testing.T) {
	candidate := &service.LiveSettlement{Status: service.LiveSettlementSettled, SettlementMode: "standard", DurationMs: 1000, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, nil, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, service.LiveSettlementJobQuarantine, snapshot.InitialStatus)
}

func TestLiveSettlementRegisterPersistsConfirmedSnapshot(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	termsFingerprint, err := snapshot.Identity.ReservationTerms(snapshot.Frozen.SettlementMode).Fingerprint()
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE call_hash = .*FOR UPDATE").
		WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).
			AddRow("operation-one", 7, snapshot.IdentityFingerprint, service.LiveReservationActive, termsFingerprint))
	mock.ExpectQuery("INSERT INTO live_settlements").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	mock.ExpectExec("UPDATE live_session_reservations SET status = 'frozen'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), confirmed)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, int64(42), result.Job.ID)
	require.Equal(t, snapshot.Frozen, result.Job.Snapshot.Frozen)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementRegisterQuarantinesFrozenConflict(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	termsFingerprint, err := snapshot.Identity.ReservationTerms(snapshot.Frozen.SettlementMode).Fingerprint()
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE call_hash = .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).
			AddRow("operation-one", 7, snapshot.IdentityFingerprint, service.LiveReservationFrozen, termsFingerprint))
	mock.ExpectQuery("SELECT .* FROM live_settlements.*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "api_key_id", "identity_fingerprint", "frozen_digest", "billing_digest", "snapshot", "status", "handoff_generation", "handoff_pending"}).
			AddRow(42, snapshot.Identity.APIKeyID, snapshot.IdentityFingerprint, "different-frozen-digest", "other-billing-digest", encoded, service.LiveSettlementJobPending, 7, true))
	mock.ExpectExec("INSERT INTO live_settlement_conflicts").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE live_session_reservations SET status = 'quarantine'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM live_settlements .*ORDER BY id FOR UPDATE").WithArgs(int64(7), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	mock.ExpectExec("UPDATE live_settlements SET quarantined_from_status").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), confirmed)
	require.ErrorIs(t, err, service.ErrLiveSettlementConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFreezeDigestExcludesMutableAndBillingFields(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	before, err := liveFrozenSnapshotDigest(snapshot)
	require.NoError(t, err)
	snapshot.SourceStatus = service.LiveSettlementSettled
	snapshot.Usage.Model = "changed-usage"
	if snapshot.Billing != nil {
		snapshot.Billing.BalanceCost = 99
	}
	after, err := liveFrozenSnapshotDigest(snapshot)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestLiveSettlementClaimUsesDatabaseClockAndFencing(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	leaseUntil := time.Unix(1800000100, 0).UTC()
	claimDigest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	mock.ExpectQuery("(?s)WITH due AS .*clock_timestamp.*FOR UPDATE SKIP LOCKED.*UPDATE live_settlements.*clock_timestamp").
		WithArgs(1, "worker-a", int64(120000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "snapshot", "status", "claim_owner", "claim_generation", "lease_until", "frozen_digest", "handoff_generation"}).
			AddRow(42, encoded, service.LiveSettlementJobPending, "worker-a", 7, leaseUntil, claimDigest, snapshot.HandoffGeneration))
	claims, err := newLiveSettlementTestRepository(t, db).ClaimDue(context.Background(), "worker-a", 1, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, int64(42), claims[0].Job.ID)
	require.Equal(t, int64(7), claims[0].Generation)
	require.Equal(t, leaseUntil, claims[0].LeaseUntil)
	require.Equal(t, snapshot.Frozen, claims[0].Job.Snapshot.Frozen)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementClaimRejectsInvalidArguments(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := newLiveSettlementTestRepository(t, db)
	_, err = store.ClaimDue(context.Background(), "", 1, time.Minute)
	require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
	_, err = store.ClaimDue(context.Background(), "worker-a", 0, time.Minute)
	require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
	_, err = store.ClaimDue(context.Background(), "worker-a", 1, 0)
	require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementHandoffLookupAndList(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	digest, err := liveFrozenSnapshotDigest(snapshot)
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	columns := []string{"id", "snapshot", "status", "frozen_digest", "handoff_pending", "handoff_generation"}
	mock.ExpectQuery("SELECT .* FROM live_settlements WHERE call_hash = .*api_key_id").
		WithArgs(snapshot.Identity.CallHash, snapshot.Identity.APIKeyID).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(42, encoded, service.LiveSettlementJobSettled, digest, true, 3))
	mock.ExpectQuery("SELECT .* FROM live_settlements WHERE .*handoff_pending.*ORDER BY id.*LIMIT").
		WithArgs(int64(0), 10).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(42, encoded, service.LiveSettlementJobSettled, digest, true, 3))
	store := newLiveSettlementTestRepository(t, db)
	job, err := store.Lookup(context.Background(), snapshot.Identity.CallHash, snapshot.Identity.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.LiveSettlementJobSettled, job.Status)
	require.True(t, job.HandoffPending)
	require.Equal(t, digest, job.FrozenDigest)
	require.Equal(t, int64(3), job.HandoffGeneration)
	jobs, err := store.ListHandoffs(context.Background(), 0, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, job.ID, jobs[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementMarkHandoffDoneRequiresAuthorization(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// 未授权: SELECT authorized=false → 拒绝
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
		WithArgs("hash", "identity", "frozen", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobPending, false))
	mock.ExpectRollback()
	store := newLiveSettlementTestRepository(t, db)
	require.ErrorIs(t, store.MarkHandoffDone(context.Background(), "hash", "identity", "frozen", 3), service.ErrLiveSettlementLostLease)
	require.NoError(t, mock.ExpectationsWereMet())

	// 已隔离: status=quarantine → 拒绝
	db2, mock2, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db2.Close() })
	mock2.ExpectBegin()
	mock2.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
		WithArgs("hash", "identity", "frozen", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobQuarantine, true))
	mock2.ExpectRollback()
	require.ErrorIs(t, newLiveSettlementTestRepository(t, db2).MarkHandoffDone(context.Background(), "hash", "identity", "frozen", 3), service.ErrLiveSettlementLostLease)
	require.NoError(t, mock2.ExpectationsWereMet())

	// 已授权且匹配 → 成功 + 幂等
	db3, mock3, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db3.Close() })
	for i := 0; i < 2; i++ {
		mock3.ExpectBegin()
		mock3.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
			WithArgs("hash", "identity", "frozen", int64(3)).
			WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobPending, true))
		mock3.ExpectExec("UPDATE live_settlements SET handoff_pending").
			WithArgs("hash", "identity", "frozen", int64(3)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock3.ExpectCommit()
	}
	require.NoError(t, newLiveSettlementTestRepository(t, db3).MarkHandoffDone(context.Background(), "hash", "identity", "frozen", 3))
	require.NoError(t, newLiveSettlementTestRepository(t, db3).MarkHandoffDone(context.Background(), "hash", "identity", "frozen", 3))
	require.NoError(t, mock3.ExpectationsWereMet())
}

func TestLiveSettlementAuthorizeHandoffPersistsLinearizationPoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := newLiveSettlementTestRepository(t, db)
	// 首次授权: FOR UPDATE 读 + UPDATE 置 authorized
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
		WithArgs("hash", "identity", "frozen", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobPending, false))
	mock.ExpectExec("UPDATE live_settlements SET handoff_authorized = TRUE").
		WithArgs("hash", "identity", "frozen", int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, store.AuthorizeHandoff(context.Background(), "hash", "identity", "frozen", 3))
	// 幂等重放: 已 authorized → 仅提交
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
		WithArgs("hash", "identity", "frozen", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobPending, true))
	mock.ExpectCommit()
	require.NoError(t, store.AuthorizeHandoff(context.Background(), "hash", "identity", "frozen", 3))
	// 隔离拒绝
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, handoff_authorized FROM live_settlements WHERE call_hash").
		WithArgs("hash", "identity", "frozen", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "handoff_authorized"}).AddRow(service.LiveSettlementJobQuarantine, false))
	mock.ExpectRollback()
	require.ErrorIs(t, store.AuthorizeHandoff(context.Background(), "hash", "identity", "frozen", 3), service.ErrLiveSettlementQuarantined)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveReservationHashCollisionQuarantinesBothSides(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	terms := snapshot.Identity.ReservationTerms("standard")
	termsJSON, err := json.Marshal(terms)
	require.NoError(t, err)
	termsFingerprint, err := terms.Fingerprint()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT generation, call_hash FROM live_session_reservations WHERE operation_id").WithArgs("operation-one").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "call_hash"}).AddRow(7, nil))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT operation_id, generation, identity_fingerprint FROM live_session_bindings WHERE call_hash").WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint"}).AddRow("operation-other", 9, "other-identity"))
	mock.ExpectQuery("SELECT generation FROM live_session_reservations .*ORDER BY generation FOR UPDATE").WithArgs(int64(7), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"generation"}).AddRow(7).AddRow(9))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE operation_id = .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "terms", "terms_fingerprint", "status", "call_hash", "identity_fingerprint", "bound_identity"}).
			AddRow(7, termsJSON, termsFingerprint, service.LiveReservationProvisioning, nil, nil, nil))
	identityJSON, err := json.Marshal(snapshot.Identity)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE operation_id = .*FOR UPDATE").WithArgs("operation-other").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "terms", "terms_fingerprint", "status", "call_hash", "identity_fingerprint", "bound_identity"}).
			AddRow(9, termsJSON, termsFingerprint, service.LiveReservationActive, snapshot.Identity.CallHash, "other-identity", identityJSON))
	for _, generation := range []int64{7, 9} {
		mock.ExpectExec("INSERT INTO live_settlement_conflicts").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE live_session_reservations SET status = 'quarantine'").WithArgs(generation, "binding_collision").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery("SELECT id FROM live_settlements .*ORDER BY id FOR UPDATE").WithArgs(int64(7), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101).AddRow(102))
	for _, generation := range []int64{7, 9} {
		mock.ExpectExec("UPDATE live_settlements SET quarantined_from_status").WithArgs(generation, "binding_collision").WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).Bind(context.Background(), "operation-one", 7, snapshot.Identity)
	require.ErrorIs(t, err, service.ErrLiveSettlementConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementRegisterRequiresCommittedActiveReservation(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	termsFingerprint, err := snapshot.Identity.ReservationTerms(snapshot.Frozen.SettlementMode).Fingerprint()
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		rows     *sqlmock.Rows
		conflict bool
	}{
		{"missing", sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}), false},
		{"wrong-generation", sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).AddRow("operation-one", 8, snapshot.IdentityFingerprint, service.LiveReservationActive, termsFingerprint), true},
		{"wrong-identity", sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).AddRow("operation-one", 7, "wrong", service.LiveReservationActive, termsFingerprint), true},
		{"wrong-terms", sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).AddRow("operation-one", 7, snapshot.IdentityFingerprint, service.LiveReservationActive, "wrong"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE call_hash = .*FOR UPDATE").WithArgs(snapshot.Identity.CallHash).WillReturnRows(tc.rows)
			if tc.conflict {
				mock.ExpectExec("INSERT INTO live_settlement_conflicts").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE live_session_reservations SET status = 'quarantine'").WillReturnResult(sqlmock.NewResult(0, 1))
				gen := int64(7)
				if tc.name == "wrong-generation" {
					gen = 8
				}
				mock.ExpectQuery("SELECT id FROM live_settlements .*ORDER BY id FOR UPDATE").WithArgs(gen, gen).WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectExec("UPDATE live_settlements SET quarantined_from_status").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			_, err = newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), confirmed)
			if tc.conflict {
				require.ErrorIs(t, err, service.ErrLiveSettlementConflict)
			} else {
				require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLiveSettlementRegisterQuarantinesFrozenReservationWithoutJob(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	termsFingerprint, err := snapshot.Identity.ReservationTerms(snapshot.Frozen.SettlementMode).Fingerprint()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE call_hash = .*FOR UPDATE").WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).
			AddRow("operation-one", 7, snapshot.IdentityFingerprint, service.LiveReservationFrozen, termsFingerprint))
	mock.ExpectQuery("SELECT .* FROM live_settlements.*FOR UPDATE").WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "api_key_id", "identity_fingerprint", "frozen_digest", "billing_digest", "snapshot", "status", "handoff_generation", "handoff_pending"}))
	mock.ExpectExec("INSERT INTO live_settlement_conflicts").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE live_session_reservations SET status = 'quarantine'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id FROM live_settlements .*ORDER BY id FOR UPDATE").WithArgs(int64(7), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("UPDATE live_settlements SET quarantined_from_status").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), confirmed)
	require.ErrorIs(t, err, service.ErrLiveSettlementConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveReservationIdentityFactoryMatchesConfirmed(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, record, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	identity, fingerprint, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	require.Equal(t, snapshot.Identity, identity)
	require.Equal(t, snapshot.IdentityFingerprint, fingerprint)
}

func TestLiveReservationProvisionReturnsDurableGeneration(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	terms := snapshot.Identity.ReservationTerms("standard")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT generation, call_hash FROM live_session_reservations WHERE operation_id").WithArgs("operation-one").WillReturnRows(sqlmock.NewRows([]string{"generation", "call_hash"}))
	mock.ExpectQuery("INSERT INTO live_session_reservations").WillReturnRows(sqlmock.NewRows([]string{"generation"}).AddRow(7))
	mock.ExpectCommit()
	reservation, err := newLiveSettlementTestRepository(t, db).ReserveProvisioning(context.Background(), "operation-one", terms)
	require.NoError(t, err)
	require.Equal(t, "operation-one", reservation.OperationID)
	require.Equal(t, int64(7), reservation.Generation)
	require.Equal(t, service.LiveReservationProvisioning, reservation.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveReservationBindDoesNotMarkActive(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	terms := snapshot.Identity.ReservationTerms("standard")
	encoded, err := json.Marshal(terms)
	require.NoError(t, err)
	termsFingerprint, err := terms.Fingerprint()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT generation, call_hash FROM live_session_reservations WHERE operation_id").WithArgs("operation-one").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "call_hash"}).AddRow(7, nil))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT operation_id, generation, identity_fingerprint FROM live_session_bindings WHERE call_hash").WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint"}))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE operation_id = .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"generation", "terms", "terms_fingerprint", "status", "call_hash", "identity_fingerprint", "bound_identity"}).
			AddRow(7, encoded, termsFingerprint, service.LiveReservationProvisioning, nil, nil, nil))
	mock.ExpectQuery("INSERT INTO live_session_bindings").WillReturnRows(sqlmock.NewRows([]string{"call_hash"}).AddRow(snapshot.Identity.CallHash))
	mock.ExpectExec("UPDATE live_session_reservations SET status = 'bound'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	reservation, err := newLiveSettlementTestRepository(t, db).Bind(context.Background(), "operation-one", 7, snapshot.Identity)
	require.NoError(t, err)
	require.Equal(t, service.LiveReservationBound, reservation.Status)
	require.Equal(t, snapshot.IdentityFingerprint, reservation.IdentityFingerprint)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementRegisterRejectsBoundReservation(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(snapshot.Identity.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .* FROM live_session_reservations WHERE call_hash = .*FOR UPDATE").
		WithArgs(snapshot.Identity.CallHash).
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "generation", "identity_fingerprint", "status", "terms_fingerprint"}).
			AddRow("operation-one", 7, snapshot.IdentityFingerprint, service.LiveReservationBound, "unused"))
	mock.ExpectRollback()
	_, err = newLiveSettlementTestRepository(t, db).RegisterFrozen(context.Background(), confirmed)
	require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementPublicDigestMatchesStoredAndBindsIdentity(t *testing.T) {
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.Unix(1800000001, 123456789)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	publicDigest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	storedDigest, err := liveFrozenSnapshotDigest(snapshot)
	require.NoError(t, err)
	require.Equal(t, storedDigest, publicDigest)
	snapshot.Identity.APIKeyID++
	_, err = snapshot.ImmutableDigest()
	require.ErrorIs(t, err, service.ErrLiveIdentityMismatch)
}

// helper: build a confirmed snapshot with billing command for Finalize tests
func finalizedSnapshotFixture(t *testing.T) (service.LiveFrozenSnapshot, *service.UsageBillingCommand) {
	t.Helper()
	price := 0.20
	candidate := &service.LiveSettlement{Status: service.LiveSettlementPending, SettlementMode: "standard", DurationMs: 60000, TotalCost: 0.20, ActualCost: 0.20, FrozenAt: time.UnixMilli(1800000001123)}
	confirmed, _, _ := newLiveSettlementConfirmedFixture(t, &price, candidate)
	snapshot, err := confirmed.ValidatedSnapshot()
	require.NoError(t, err)
	require.NotNil(t, snapshot.Billing)
	cmd, err := liveBillingCommandFromWhitelist(&snapshot)
	require.NoError(t, err)
	return snapshot, cmd
}

func finalizedClaim(t *testing.T, snapshot service.LiveFrozenSnapshot, owner string, gen int64) service.LiveSettlementClaim {
	t.Helper()
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	return service.LiveSettlementClaim{
		Owner:      owner,
		Generation: gen,
		Job: service.LiveSettlementJob{
			ID:                42,
			Snapshot:          snapshot,
			Status:            service.LiveSettlementJobPending,
			FrozenDigest:      digest,
			HandoffGeneration: snapshot.HandoffGeneration,
		},
	}
}

func TestLiveSettlementBillingDigestV1WhitelistRoundTrip(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	digest, err := liveBillingSnapshotDigest(snapshot)
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var reloaded service.LiveFrozenSnapshot
	require.NoError(t, json.Unmarshal(encoded, &reloaded))
	reloadedDigest, err := liveBillingSnapshotDigest(reloaded)
	require.NoError(t, err)
	require.Equal(t, digest, reloadedDigest, "old JSON deserialization must recompute the same digest")
	// 篡改白名单内字段必须改变 digest
	mutated := snapshot
	if mutated.Billing != nil {
		clone := *mutated.Billing
		clone.BalanceCost++
		mutated.Billing = &clone
	}
	mutatedDigest, err := liveBillingSnapshotDigest(mutated)
	require.NoError(t, err)
	require.NotEqual(t, digest, mutatedDigest)
	// 未知版本拒绝
	unknown := snapshot
	unknown.Version = 2
	_, err = liveBillingSnapshotDigest(unknown)
	require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
}

func TestLiveSettlementBillingDigestV1RejectsUnknownVersion(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	for _, version := range []int{2, 3, -1} {
		unknown := snapshot
		unknown.Version = version
		_, err := liveBillingSnapshotDigest(unknown)
		require.ErrorIs(t, err, service.ErrLiveSettlementInvalid)
	}
	// legacy version 0 走旧全字段布局，仍可计算
	legacy := snapshot
	legacy.Version = 0
	_, err := liveBillingSnapshotDigest(legacy)
	require.NoError(t, err)
}

func TestLiveSettlementFinalizeClaimsAndBillsOnce(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	leaseUntil := time.Now().Add(5 * time.Minute)
	owner := "worker-a"
	gen := int64(3)
	claim := service.LiveSettlementClaim{Owner: owner, Generation: gen, LeaseUntil: leaseUntil, Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending, FrozenDigest: func() string {
		d, _ := snapshot.ImmutableDigest()
		return d
	}(), HandoffGeneration: snapshot.HandoffGeneration}}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), owner, gen).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, mustFrozenDigest(t, &snapshot), snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
	// usage_billing_dedup_archive 无行
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup_archive").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}))
	// effects: balance deduction
	mock.ExpectQuery("UPDATE users SET balance = balance").WithArgs(cmd.BalanceCost, cmd.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(100.0))
	// effects: api key rate-limit window
	mock.ExpectExec("UPDATE api_keys SET").WithArgs(cmd.APIKeyRateLimitCost, cmd.APIKeyID).WillReturnResult(sqlmock.NewResult(0, 1))
	// usage log insert
	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectExec("UPDATE live_settlements SET status = 'settled'").
		WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.NoError(t, err)
	require.True(t, res.Applied)
	require.False(t, res.AlreadyDone)
	require.NotNil(t, res.Billing)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFinalizeAlreadySettledIsIdempotent(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := finalizedClaim(t, snapshot, "worker-a", 3)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}))
	mock.ExpectQuery("SELECT status FROM live_settlements WHERE id").WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(service.LiveSettlementJobSettled))
	mock.ExpectCommit()
	res, err := newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.NoError(t, err)
	require.True(t, res.AlreadyDone)
	require.False(t, res.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFinalizeLostLeaseFails(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := finalizedClaim(t, snapshot, "worker-a", 3)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}))
	mock.ExpectQuery("SELECT status FROM live_settlements WHERE id").WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(service.LiveSettlementJobPending))
	mock.ExpectRollback()
	_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.ErrorIs(t, err, service.ErrLiveSettlementLostLease)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFinalizeDoesNotDoubleBillWhenLedgerExists(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := finalizedClaim(t, snapshot, "worker-a", 3)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, mustFrozenDigest(t, &snapshot), snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}).
			AddRow(snapshot.Usage.UserID, cmd.APIKeyID, snapshot.Usage.AccountID, cmd.RequestID, snapshot.Usage.Model, snapshot.Usage.Model,
				snapshot.Usage.GroupID, snapshot.Usage.SubscriptionID, snapshot.Usage.RateMultiplier, snapshot.Usage.BillingType, int16(snapshot.Usage.RequestType),
				snapshot.Usage.TotalCost, snapshot.Usage.ActualCost, int64(*snapshot.Usage.DurationMs), snapshot.Usage.UserAgent,
				snapshot.Usage.IPAddress, snapshot.Usage.InboundEndpoint, snapshot.Usage.UpstreamEndpoint, snapshot.Usage.BillingMode,
				snapshot.Usage.CreatedAt))
	// 既有 usage 行与快照一致：已有结算记录，不重复扣费、不重复写行。
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup").
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectExec("UPDATE live_settlements SET status = 'settled'").
		WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	res, err := newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.NoError(t, err)
	require.False(t, res.Applied)
	require.True(t, res.AlreadyDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFinalizeFingerprintConflictQuarantines(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := finalizedClaim(t, snapshot, "worker-a", 3)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, mustFrozenDigest(t, &snapshot), snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup").
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow("different-fingerprint"))
	mock.ExpectExec("ROLLBACK TO SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE live_settlements SET status = 'quarantine'").
		WithArgs(int64(42), service.LiveBillingConflictReason).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.ErrorIs(t, err, service.ErrLiveSettlementQuarantined)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementLookupBoundLiveHandoff(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := newLiveSettlementTestRepository(t, db)
	mock.ExpectQuery("SELECT b.operation_id, b.call_hash, b.generation, r.status, r.identity_fingerprint").
		WithArgs("hash").
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "call_hash", "generation", "status", "identity_fingerprint"}).
			AddRow("operation-one", "hash", int64(7), "bound", "identity-fp"))
	bound, err := store.(service.LiveHandoffBoundLookup).LookupBoundLiveHandoff(context.Background(), "hash", 3)
	require.NoError(t, err)
	require.Equal(t, "operation-one", bound.OperationID)
	require.Equal(t, "hash", bound.CallHash)
	require.Equal(t, int64(7), bound.Generation)
	require.Equal(t, "bound", bound.Status)
	require.Equal(t, "identity-fp", bound.IdentityFingerprint)
	require.Equal(t, int64(3), bound.APIKeyID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementLookupBoundLiveHandoffRejectsNonBound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	store := newLiveSettlementTestRepository(t, db)
	for _, status := range []string{"provisioning", "active", "frozen", "quarantine", "unknown"} {
		mock.ExpectQuery("SELECT b.operation_id, b.call_hash, b.generation, r.status, r.identity_fingerprint").
			WithArgs("hash").
			WillReturnRows(sqlmock.NewRows([]string{"operation_id", "call_hash", "generation", "status", "identity_fingerprint"}).
				AddRow("operation-one", "hash", int64(7), status, "identity-fp"))
		_, err := store.(service.LiveHandoffBoundLookup).LookupBoundLiveHandoff(context.Background(), "hash", 3)
		require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
	}
	mock.ExpectQuery("SELECT b.operation_id, b.call_hash, b.generation, r.status, r.identity_fingerprint").
		WithArgs("hash").
		WillReturnRows(sqlmock.NewRows([]string{"operation_id", "call_hash", "generation", "status", "identity_fingerprint"}))
	_, err = store.(service.LiveHandoffBoundLookup).LookupBoundLiveHandoff(context.Background(), "hash", 3)
	require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-1: ClaimDue 必须填充 FrozenDigest / HandoffGeneration，否则 FinalizeClaimed
// 会拿零值和真实 digest 比较，正常 job 一律被判为冲突。
func TestLiveSettlementClaimPopulatesFrozenDigestAndGeneration(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	leaseUntil := time.Unix(1800000100, 0).UTC()
	mock.ExpectQuery("WITH due AS").WithArgs(1, "worker-a", int64(120000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "snapshot", "status", "claim_owner", "claim_generation", "lease_until", "frozen_digest", "handoff_generation"}).
			AddRow(42, encoded, service.LiveSettlementJobPending, "worker-a", 7, leaseUntil, digest, snapshot.HandoffGeneration))
	claims, err := newLiveSettlementTestRepository(t, db).ClaimDue(context.Background(), "worker-a", 1, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, digest, claims[0].Job.FrozenDigest, "claim must carry the stored frozen digest")
	require.Equal(t, snapshot.HandoffGeneration, claims[0].Job.HandoffGeneration, "claim must carry the stored handoff generation")
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-1: 生产链路 ClaimDue -> FinalizeClaimed 必须能正常结算（不得因 claim 零值被误判冲突）。
func TestLiveSettlementFinalizeSucceedsFromClaimDueChain(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	leaseUntil := time.Now().Add(2 * time.Minute)
	mock.ExpectQuery("WITH due AS").WithArgs(1, "worker-a", int64(120000)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "snapshot", "status", "claim_owner", "claim_generation", "lease_until", "frozen_digest", "handoff_generation"}).
			AddRow(42, encoded, service.LiveSettlementJobPending, "worker-a", 7, leaseUntil, digest, snapshot.HandoffGeneration))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, mustFrozenDigest(t, &snapshot), snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup_archive").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}))
	mock.ExpectQuery("UPDATE users SET balance = balance").WithArgs(cmd.BalanceCost, cmd.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(9.8))
	mock.ExpectExec("UPDATE api_keys SET").WithArgs(cmd.APIKeyRateLimitCost, cmd.APIKeyID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectExec("UPDATE live_settlements SET status = 'settled'").WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	store := newLiveSettlementTestRepository(t, db)
	claims, err := store.ClaimDue(context.Background(), "worker-a", 1, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	res, err := store.FinalizeClaimed(context.Background(), claims[0])
	require.NoError(t, err, "a claimed job must finalize without a spurious conflict")
	require.True(t, res.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-1: simple_skip / 零额快照没有 billing 命令，仍必须能落账（不得直接拒绝）。
func TestLiveSettlementFinalizeZeroAmountWithoutBillingSettles(t *testing.T) {
	snapshot, _ := finalizedSnapshotFixture(t)
	snapshot.Billing = nil
	snapshot.Frozen.SettlementMode = "simple_skip"
	snapshot.Frozen.ActualCost = 0
	snapshot.Usage.ActualCost = 0
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := service.LiveSettlementClaim{
		Owner: "worker-a", Generation: 7,
		Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending,
			FrozenDigest: digest, HandoffGeneration: snapshot.HandoffGeneration},
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, mustFrozenDigest(t, &snapshot), snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(snapshot.Usage.RequestID, snapshot.Usage.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectExec("UPDATE live_settlements SET status = 'settled'").WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	res, err := newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.NoError(t, err, "a zero-amount simple_skip snapshot must settle without a billing command")
	require.True(t, res.Applied)
	require.Nil(t, res.Billing)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementFinalizeUsageConflictRollsBackBilling(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := service.LiveSettlementClaim{Owner: "worker-a", Generation: 7,
		Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending,
			FrozenDigest: digest, HandoffGeneration: snapshot.HandoffGeneration}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, digest, snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}).
			AddRow(int64(999), cmd.APIKeyID, snapshot.Usage.AccountID, cmd.RequestID, snapshot.Usage.Model, snapshot.Usage.Model,
				nil, nil, snapshot.Usage.RateMultiplier, snapshot.Usage.BillingType, int16(snapshot.Usage.RequestType),
				snapshot.Usage.TotalCost, snapshot.Usage.ActualCost, nil, nil, nil, nil, nil, nil, snapshot.Usage.CreatedAt))
	mock.ExpectExec("UPDATE live_settlements SET status = 'quarantine'").WithArgs(int64(42), service.LiveUsageConflictReason).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.ErrorIs(t, err, service.ErrLiveSettlementQuarantined)
	require.NoError(t, mock.ExpectationsWereMet(), "no billing SQL may run before the usage precheck rejects the row")
}

func TestLiveSettlementFinalizeRejectsSameAmountDifferentAttribution(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*service.LiveFrozenSnapshot, *[]driver.Value)
	}{
		{name: "account_id", mutate: func(_ *service.LiveFrozenSnapshot, row *[]driver.Value) { (*row)[2] = int64(777) }},
		{name: "created_at", mutate: func(s *service.LiveFrozenSnapshot, row *[]driver.Value) {
			(*row)[19] = s.Usage.CreatedAt.Add(time.Hour)
		}},
		{name: "model", mutate: func(_ *service.LiveFrozenSnapshot, row *[]driver.Value) { (*row)[4] = "other-model" }},
		{name: "user_agent", mutate: func(_ *service.LiveFrozenSnapshot, row *[]driver.Value) { v := "other-agent"; (*row)[14] = &v }},
		{name: "inbound_endpoint", mutate: func(_ *service.LiveFrozenSnapshot, row *[]driver.Value) { v := "/other"; (*row)[16] = &v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, cmd := finalizedSnapshotFixture(t)
			digest, err := snapshot.ImmutableDigest()
			require.NoError(t, err)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			claim := service.LiveSettlementClaim{Owner: "worker-a", Generation: 7,
				Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending,
					FrozenDigest: digest, HandoffGeneration: snapshot.HandoffGeneration}}
			row := []driver.Value{snapshot.Usage.UserID, cmd.APIKeyID, snapshot.Usage.AccountID, cmd.RequestID, snapshot.Usage.Model,
				snapshot.Usage.Model, nil, nil, snapshot.Usage.RateMultiplier, snapshot.Usage.BillingType,
				int16(snapshot.Usage.RequestType), snapshot.Usage.TotalCost, snapshot.Usage.ActualCost,
				int64(*snapshot.Usage.DurationMs), snapshot.Usage.UserAgent, snapshot.Usage.IPAddress,
				snapshot.Usage.InboundEndpoint, snapshot.Usage.UpstreamEndpoint, snapshot.Usage.BillingMode, snapshot.Usage.CreatedAt}
			tc.mutate(&snapshot, &row)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, digest, snapshot.HandoffGeneration))
			mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
				WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
					"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
					"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}).AddRow(row...))
			mock.ExpectExec("UPDATE live_settlements SET status = 'quarantine'").WithArgs(int64(42), service.LiveUsageConflictReason).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
			require.ErrorIs(t, err, service.ErrLiveSettlementQuarantined, "same amount but different attribution must not settle")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLiveSettlementFinalizeArchiveConflictLeavesNoHotKey(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := service.LiveSettlementClaim{Owner: "worker-a", Generation: 7,
		Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending,
			FrozenDigest: digest, HandoffGeneration: snapshot.HandoffGeneration}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, digest, snapshot.HandoffGeneration))
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(31))
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup_archive").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow("archived-different-fingerprint"))
	mock.ExpectExec("ROLLBACK TO SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE live_settlements SET status = 'quarantine'").WithArgs(int64(42), service.LiveBillingConflictReason).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.ErrorIs(t, err, service.ErrLiveSettlementQuarantined)
	require.NoError(t, mock.ExpectationsWereMet(), "the hot dedup insert must be rolled back before committing the quarantine")
}

// P1-2 TOCTOU: 预检无行（未锁）→ 扣款 → 写入 usage 时才与并发已提交行冲突，
// 必须先 ROLLBACK TO SAVEPOINT 撤销扣费，再以隔离提交 quarantine。
func TestLiveSettlementFinalizeUsageWriteConflictRollsBackBilling(t *testing.T) {
	snapshot, cmd := finalizedSnapshotFixture(t)
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	claim := service.LiveSettlementClaim{Owner: "worker-a", Generation: 7,
		Job: service.LiveSettlementJob{ID: 42, Snapshot: snapshot, Status: service.LiveSettlementJobPending,
			FrozenDigest: digest, HandoffGeneration: snapshot.HandoffGeneration}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT snapshot, status, frozen_digest, handoff_generation FROM live_settlements").WithArgs(int64(42), "worker-a", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"snapshot", "status", "frozen_digest", "handoff_generation"}).AddRow(mustSnapshotJSON(t, &snapshot), service.LiveSettlementJobPending, digest, snapshot.HandoffGeneration))
	// 预检无既有 usage 行
	mock.ExpectQuery("FROM usage_logs WHERE request_id = .*FOR UPDATE").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
			"group_id", "subscription_id", "rate_multiplier", "billing_type", "request_type", "total_cost", "actual_cost",
			"duration_ms", "user_agent", "ip_address", "inbound_endpoint", "upstream_endpoint", "billing_mode", "created_at"}))
	mock.ExpectExec("SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(31))
	mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup_archive").WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}))
	mock.ExpectQuery("UPDATE users SET balance = balance").WithArgs(cmd.BalanceCost, cmd.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(9.8))
	mock.ExpectExec("UPDATE api_keys SET").WithArgs(cmd.APIKeyRateLimitCost, cmd.APIKeyID).WillReturnResult(sqlmock.NewResult(0, 1))
	// 写入 usage 行与并发已提交行冲突：INSERT 无行 → 唯一键冲突
	mock.ExpectQuery("INSERT INTO usage_logs").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	// 必须回滚已生效扣费
	mock.ExpectExec("ROLLBACK TO SAVEPOINT live_finalize_billing").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE live_settlements SET status = 'quarantine'").WithArgs(int64(42), service.LiveUsageConflictReason).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	_, err = newLiveSettlementTestRepository(t, db).FinalizeClaimed(context.Background(), claim)
	require.ErrorIs(t, err, service.ErrLiveSettlementQuarantined)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementMarkActiveAdvancesBoundReservation(t *testing.T) {
	cache, record, lookup, _ := newMarkActiveFixture(t)
	proof, err := service.ConfirmLiveHandoffCreation(context.Background(), lookup, cache, record, 7, time.Hour)
	require.NoError(t, err)
	result, err := proof.Validate()
	require.NoError(t, err)
	require.Equal(t, "operation-one", result.OperationID)
	identity, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	terms := identity.ReservationTerms("standard")
	termsJSON, err := json.Marshal(terms)
	require.NoError(t, err)
	termsFP, err := terms.Fingerprint()
	require.NoError(t, err)
	identityJSON, err := json.Marshal(identity)
	require.NoError(t, err)

	lockRows := func(mock sqlmock.Sqlmock, status string) {
		mock.ExpectBegin()
		mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(result.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id").
			WithArgs(result.OperationID).
			WillReturnRows(sqlmock.NewRows([]string{"generation", "terms", "terms_fingerprint", "status", "call_hash", "identity_fingerprint", "bound_identity"}).
				AddRow(7, termsJSON, termsFP, status, result.CallHash, fp, identityJSON))
	}
	db, mock, dbErr := sqlmock.New()
	require.NoError(t, dbErr)
	t.Cleanup(func() { _ = db.Close() })
	lockRows(mock, service.LiveReservationBound)
	mock.ExpectExec("UPDATE live_session_reservations SET status = 'active'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	res, err := newLiveSettlementTestRepository(t, db).MarkActive(context.Background(), proof)
	require.NoError(t, err)
	require.Equal(t, service.LiveReservationActive, res.Status)
	require.Equal(t, int64(7), res.Generation)
	lockRows(mock, service.LiveReservationActive)
	mock.ExpectCommit()
	res, err = newLiveSettlementTestRepository(t, db).MarkActive(context.Background(), proof)
	require.NoError(t, err)
	require.Equal(t, service.LiveReservationActive, res.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementMarkActiveRejectsQuarantineReservation(t *testing.T) {
	cache, record, lookup, _ := newMarkActiveFixture(t)
	proof, err := service.ConfirmLiveHandoffCreation(context.Background(), lookup, cache, record, 7, time.Hour)
	require.NoError(t, err)
	result, err := proof.Validate()
	require.NoError(t, err)
	identity, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	terms := identity.ReservationTerms("standard")
	termsJSON, err := json.Marshal(terms)
	require.NoError(t, err)
	termsFP, err := terms.Fingerprint()
	require.NoError(t, err)
	identityJSON, err := json.Marshal(identity)
	require.NoError(t, err)

	db, mock, dbErr := sqlmock.New()
	require.NoError(t, dbErr)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(result.CallHash).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT generation, terms, terms_fingerprint, status, call_hash, identity_fingerprint, bound_identity FROM live_session_reservations WHERE operation_id").
		WithArgs(result.OperationID).
		WillReturnRows(sqlmock.NewRows([]string{"generation", "terms", "terms_fingerprint", "status", "call_hash", "identity_fingerprint", "bound_identity"}).
			AddRow(7, termsJSON, termsFP, service.LiveReservationQuarantine, result.CallHash, fp, identityJSON))
	mock.ExpectRollback()
	_, err = newLiveSettlementTestRepository(t, db).MarkActive(context.Background(), proof)
	require.ErrorIs(t, err, service.ErrLiveSettlementUnconfirmed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLiveSettlementMarkActiveRejectsMismatchedProofIdentity(t *testing.T) {
	cache, record, lookup, _ := newMarkActiveFixture(t)
	other := *record
	other.UserID++
	proof, err := service.ConfirmLiveHandoffCreation(context.Background(), lookup, cache, &other, 7, time.Hour)
	require.Error(t, err, "identity mismatch must prevent proof creation")
	_ = proof
}

// newMarkActiveFixture 用生产路径构建 bound reservation + Redis creation proof。
func newMarkActiveFixture(t *testing.T) (*gatewayCache, *service.LiveCallRecord, handoffBoundLookup, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client).(*gatewayCache)
	price := 0.1
	created := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	record := &service.LiveCallRecord{
		CallID: "mark-active", CallHash: HashLiveCallID("mark-active"), UserID: 1, APIKeyID: 2,
		AccountID: 3, GroupID: 4, SubscriptionID: 5, LeaseID: "lease-mark",
		Model: "gpt-live", CreatedAt: created, ExpiresAt: created.Add(time.Hour),
		Controller: service.LiveControllerPending, RealtimePricePerMin: &price,
		BillingPolicy: &service.LiveBillingPolicy{RateLimit5h: 100},
		UserAgent:     "ua", IPAddress: "127.0.0.1", InboundEndpoint: "/live",
		AttestationCiphertext: "encrypted",
	}
	_, fp, err := service.LiveSettlementIdentityFromRecord(record)
	require.NoError(t, err)
	lookup := handoffBoundLookup{reservation: service.LiveHandoffBoundReservation{
		OperationID: "operation-one", CallHash: record.CallHash, APIKeyID: record.APIKeyID,
		IdentityFingerprint: fp, Generation: 7, Status: "bound",
	}}
	return cache, record, lookup, server
}

func mustSnapshotJSON(t *testing.T, snapshot *service.LiveFrozenSnapshot) []byte {
	t.Helper()
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	return encoded
}

func mustFrozenDigest(t *testing.T, snapshot *service.LiveFrozenSnapshot) string {
	t.Helper()
	digest, err := snapshot.ImmutableDigest()
	require.NoError(t, err)
	return digest
}
