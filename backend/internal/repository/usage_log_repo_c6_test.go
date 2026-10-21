package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

// Bind arguments to the real named SELECT contract, not a tail-relative offset.
func usageLogNamedInsertArgIndex(t *testing.T, name string) int {
	t.Helper()
	columns := strings.Split(usageLogSelectColumns, ", ")
	require.Equal(t, "id", columns[0])
	require.Len(t, columns, len(usageLogInsertArgTypes)+1)
	for i, column := range columns[1:] {
		if column == name {
			return i
		}
	}
	t.Fatalf("missing insert column %q", name)
	return -1
}

func TestUsageLogC6NullableInsertScanRoundTrip(t *testing.T) {
	requested, served := int64(10), int64(20)
	failover := "group"
	attempts := 3
	for _, tc := range []struct {
		name string
		log  service.UsageLog
	}{
		{"historical-null", service.UsageLog{}},
		{"routed", service.UsageLog{RequestedGroupID: &requested, ServedGroupID: &served, Failover: &failover, AttemptCount: &attempts}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.log.UserID = 1
			tc.log.APIKeyID = 2
			tc.log.AccountID = 3
			tc.log.RequestID = "c6"
			tc.log.Model = "model"
			tc.log.CreatedAt = time.Now().UTC()
			tc.log.GroupID = &served
			prepared := prepareUsageLogInsert(&tc.log)
			require.Len(t, prepared.args, len(usageLogInsertArgTypes))
			offset := len(prepared.args) - 4
			require.Equal(t, []string{"bigint", "bigint", "text", "integer"}, usageLogInsertArgTypes[offset:])
			requestedGroupID, ok := prepared.args[offset].(sql.NullInt64)
			require.True(t, ok, "requested group id argument must be sql.NullInt64")
			require.Equal(t, tc.log.RequestedGroupID != nil, requestedGroupID.Valid)
			db, mock := newSQLMock(t)
			values := []driver.Value{int64(42)}
			for _, arg := range prepared.args {
				v, err := driver.DefaultParameterConverter.ConvertValue(arg)
				require.NoError(t, err)
				values = append(values, v)
			}
			mock.ExpectQuery("SELECT c6_fixture").WillReturnRows(sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")).AddRow(values...))
			rows, err := db.QueryContext(context.Background(), "SELECT c6_fixture")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			require.True(t, rows.Next())
			got, err := scanUsageLog(rows)
			require.NoError(t, err)
			require.Equal(t, tc.log.RequestedGroupID, got.RequestedGroupID)
			require.Equal(t, tc.log.ServedGroupID, got.ServedGroupID)
			require.Equal(t, tc.log.Failover, got.Failover)
			require.Equal(t, tc.log.AttemptCount, got.AttemptCount)
			require.Equal(t, tc.log.GroupID, got.GroupID, "legacy consumer remains untouched")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
