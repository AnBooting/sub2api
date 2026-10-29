//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type reviewLiveAuditUserRepo struct { liveRecordingOpsRepo }

// Model the production SQL storage mapping error_message -> OpsErrorLog.Message.
// User scoping and projection are executed by the real OpsService below.
func (r *reviewLiveAuditUserRepo) ListErrorLogs(_ context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	rows := []*OpsErrorLog{}
	for _, entry := range r.snapshot() {
		if filter.UserID == nil || entry.UserID == nil || *filter.UserID != *entry.UserID { continue }
		rows = append(rows, &OpsErrorLog{
			ID: int64(len(rows)+1), CreatedAt: entry.CreatedAt, UserID: entry.UserID,
			APIKeyID: entry.APIKeyID, AccountID: entry.AccountID, Model: entry.Model,
			Phase: entry.ErrorPhase, Type: entry.ErrorType, StatusCode: entry.StatusCode,
			Message: entry.ErrorMessage, Platform: entry.Platform,
		})
	}
	return &OpsErrorLogList{Errors: rows, Total: len(rows), Page: 1, PageSize: 20}, nil
}

func TestReviewLiveSettlementDiagnosticIsPrivateFromUserErrorList(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	audit := &reviewLiveAuditUserRepo{}
	svc.SetOpsRepository(audit)
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New(`ledger at 10.23.45.67:5432: relation "usage_billing_dedup" is unavailable`)
	svc.finalizeLiveCall(record)
	require.Len(t, audit.snapshot(), 1)
	userOps := &OpsService{opsRepo: audit}
	result, err := userOps.ListUserErrorRequests(context.Background(), record.UserID, nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "10.23.45.67", "internal ledger diagnostics must not become a public error message")
	require.NotContains(t, string(encoded), "usage_billing_dedup")
}
