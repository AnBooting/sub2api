package service

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAPIKeyGroupChainPresenceAndValidation(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		present, null bool
	}{
		{`{}`, false, false}, {`{"group_id":null}`, true, false}, {`{"groups":null}`, false, true}, {`{"groups":[]}`, false, false},
	} {
		p, n, e := APIKeyGroupChainPresence([]byte(tc.raw))
		require.NoError(t, e)
		require.Equal(t, tc.present, p)
		require.Equal(t, tc.null, n)
	}
	empty := []APIKeyGroupLink{}
	zero := int64(0)
	require.Error(t, ValidateAPIKeyGroupChainInput(nil, true, &empty, false, false, false, &zero))
	require.Error(t, ValidateAPIKeyGroupChainInput(nil, false, nil, true, false, false, nil))
	require.ErrorIs(t, ValidateAPIKeyGroupChainInput(nil, false, &empty, false, false, false, nil), ErrGroupChainConflict)
	require.NoError(t, ValidateAPIKeyGroupChainInput(nil, false, &empty, false, false, true, nil))
	require.Error(t, ValidateAPIKeyGroupChainInput(&zero, true, nil, false, false, false, nil))
	require.NoError(t, ValidateAPIKeyGroupChainInput(&zero, true, nil, false, true, false, nil))
}

func TestAPIKeyGroupChainPositionsAndRoles(t *testing.T) {
	for _, bad := range [][]APIKeyGroupLink{
		{{GroupID: 1, Role: "normal", Position: 1}},
		{{GroupID: 1, Role: "normal", Position: 0}, {GroupID: 1, Role: "ha", Position: 0}},
		{{GroupID: 1, Role: "normal", Position: 0}, {GroupID: 2, Role: "normal", Position: 0}},
		{{GroupID: 1, Role: "invalid", Position: 0}},
	} {
		_, err := NormalizeAPIKeyGroupLinks(bad, false)
		require.Error(t, err)
	}
	_, err := NormalizeAPIKeyGroupLinks([]APIKeyGroupLink{{GroupID: 1, Role: "ha"}}, true)
	require.Error(t, err)
	links, err := NormalizeAPIKeyGroupLinks([]APIKeyGroupLink{{GroupID: 2, Role: "normal", Position: 1}, {GroupID: 1, Role: "normal", Position: 0}}, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), links[0].GroupID)
}

func TestAPIKeyGroupChainRetainsOnlyDatabaseHAAuthorization(t *testing.T) {
	original := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	forged := original.Add(time.Hour)
	current := []APIKeyGroupLink{{GroupID: 1, Role: "normal"}, {GroupID: 9, Role: "ha", EnabledByUserAt: &original}}
	got, err := ResolveAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 2, Role: "normal"}}, true)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.True(t, got[1].EnabledByUserAt.Equal(original))
	got, err = ResolveAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 9, Role: "ha", EnabledByUserAt: &forged}}, false)
	require.NoError(t, err)
	require.True(t, got[0].EnabledByUserAt.Equal(original))
	got, err = ResolveAPIKeyGroupChain(current, []APIKeyGroupLink{}, false)
	require.NoError(t, err)
	require.Empty(t, got)
	_, err = ResolveAPIKeyGroupChain(got, []APIKeyGroupLink{{GroupID: 9, Role: "ha", EnabledByUserAt: &original}}, false)
	require.Error(t, err)
	_, err = ResolveAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 9, Role: "normal"}}, true)
	require.Error(t, err)
}
