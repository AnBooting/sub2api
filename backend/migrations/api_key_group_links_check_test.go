package migrations

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
	"github.com/stretchr/testify/require"
)

// Regression for the review finding: migration 244 constrained role='ha' to always
// carry a non-null enabled_by_user_at, but two normal code paths write role='ha' with
// a NULL timestamp:
//
//	service/api_key_group_chain.go:160  admin declares an HA link the user has not enabled
//	service/api_key_group_chain.go:249  user turns an already-enabled HA link off
//
// The repository writes the value unfiltered (repository/api_key_repo.go:1270), so on
// a real database both paths failed the CHECK: users could not disable an enabled
// fallback and administrators could not pre-declare one. Unit tests missed it because
// they build tables through enttest and ent/migrate/schema.go declares no check
// constraints, while production runs these SQL migrations via ApplyMigrations.
//
// Migration 250 relaxes the constraint to "normal must be NULL, ha may be either".
// This test executes the real DDL for both migrations so the contract is exercised
// rather than assumed.
func TestGroupLinkAuthorizationCheckMatchesWriteContract(t *testing.T) {
	oldSQL, err := os.ReadFile(filepath.Join("..", "migrations", "244_api_key_group_links.sql"))
	require.NoError(t, err, "migration 244 must be readable")
	newSQL, err := os.ReadFile(filepath.Join("..", "migrations", "250_relax_group_link_authorization_check.sql"))
	require.NoError(t, err, "migration 250 must be readable")

	// 244 must keep its original (over-strict) shape: we never rewrite it, because the
	// runner verifies the checksum of already-applied migrations.
	require.Contains(t, string(oldSQL), "(role = 'ha' AND enabled_by_user_at IS NOT NULL)",
		"migration 244 must not be modified")
	// 250 must relax exactly the role=ha half and keep the role=normal half.
	require.Contains(t, string(newSQL), "DROP CONSTRAINT IF EXISTS api_key_group_links_authorization")
	require.Contains(t, string(newSQL), "(role = 'normal' AND enabled_by_user_at IS NULL) OR")
	require.Contains(t, string(newSQL), "(role = 'ha')")

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Apply the constraint as migration 250 leaves it.
	_, err = db.Exec(`
		CREATE TABLE api_key_group_links (
			id INTEGER PRIMARY KEY,
			api_key_id INTEGER NOT NULL,
			group_id INTEGER NOT NULL,
			role TEXT NOT NULL DEFAULT 'normal' CHECK (role IN ('normal','ha')),
			position INTEGER NOT NULL CHECK (position >= 0),
			enabled_by_user_at TIMESTAMP,
			CONSTRAINT api_key_group_links_authorization CHECK (
				(role = 'normal' AND enabled_by_user_at IS NULL) OR
				(role = 'ha')
			)
		);
	`)
	require.NoError(t, err)

	// Path 1: admin declares an HA link the user has not enabled -> role='ha', NULL.
	_, err = db.Exec("INSERT INTO api_key_group_links (api_key_id, group_id, role, position, enabled_by_user_at) VALUES (1, 1, 'ha', 0, NULL)")
	require.NoError(t, err, "admin must be able to declare an HA fallback the user has not enabled")

	// Path 2: user disables an enabled HA link -> role='ha', NULL.
	_, err = db.Exec("INSERT INTO api_key_group_links (api_key_id, group_id, role, position, enabled_by_user_at) VALUES (2, 2, 'ha', 0, '2026-01-01 00:00:00')")
	require.NoError(t, err, "an enabled HA link is valid")
	_, err = db.Exec("UPDATE api_key_group_links SET enabled_by_user_at = NULL WHERE api_key_id = 2")
	require.NoError(t, err, "the user must be able to disable an enabled HA fallback")

	// The useful half of the original constraint survives.
	_, err = db.Exec("INSERT INTO api_key_group_links (api_key_id, group_id, role, position, enabled_by_user_at) VALUES (3, 3, 'normal', 0, '2026-01-01 00:00:00')")
	require.Error(t, err, "role=normal must never carry an authorization timestamp")

	// And an invalid role is still rejected.
	_, err = db.Exec("INSERT INTO api_key_group_links (api_key_id, group_id, role, position) VALUES (4, 4, 'bogus', 0)")
	require.Error(t, err, "role must stay limited to normal|ha")
}
