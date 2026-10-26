//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// A duplicate currency constraint does not prove the rest of the old migration
// ran: 251 adds usage_logs.pricing_snapshot after the failing statement. The
// transaction is rolled back on the duplicate, so that required DDL is not run.
// The runner must establish the recorded old filename/checksum before treating
// the entire migration as already installed.
func TestReviewMigrationDuplicateWithoutVerifiedLegacyHistoryMustFail(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	db, mock := renamedMigrationReplayFixture(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" already exists (SQLSTATE 42710)`))
	err := applyMigrationsFS(context.Background(), db,
		fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.NoError(t, mock.ExpectationsWereMet())
	require.Error(t, err, "duplicate constraint alone must not certify the complete old migration or bypass legacy checksum verification")
}
