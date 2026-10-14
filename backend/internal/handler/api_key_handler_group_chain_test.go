package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyGroupChainUserHTTPPresence(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		expected bool
		invalid  bool
	}{
		{`{}`, false, false}, {`{"group_id":null}`, true, false},
		{`{"groups":null}`, false, true}, {`{"group_id":null,"groups":[],"expected_group_chain_revision":0}`, true, true},
		{`{"group_id":0}`, true, true}, {`{"group_id":-1}`, true, true},
		{`{"groups":[]}`, false, true}, {`{"groups":[],"expected_group_chain_revision":0}`, false, false},
	} {
		var req UpdateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(tc.raw), &req))
		require.Equal(t, tc.expected, req.GroupIDProvided)
		err := service.ValidateAPIKeyGroupChainInput(req.GroupID, req.GroupIDProvided, req.Groups, req.GroupsNull, false, false, req.ExpectedGroupChainRevision)
		require.Equal(t, tc.invalid, err != nil, tc.raw)
	}
	var create CreateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"key","groups":[]}`), &create))
	require.NoError(t, service.ValidateAPIKeyGroupChainInput(create.GroupID, create.GroupIDProvided, create.Groups, create.GroupsNull, false, true, nil))
}

func TestAPIKeyGroupChainCreateRejectsNullAndConflictBeforeSuccessfulReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prior := service.DefaultIdempotencyCoordinator()
	coordinator := service.NewIdempotencyCoordinator(newUserMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig())
	service.SetDefaultIdempotencyCoordinator(coordinator)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(prior) })
	// Seed a successful create in the real coordinator. The real handler has no
	// service: any extra callback dispatch would panic instead of replaying.
	for _, tc := range []struct {
		name    string
		seed    string
		invalid []string
	}{
		{"omission-null", `{"name":"key"}`, []string{`{"name":"key","groups":null}`}},
		{"empty-conflict", `{"name":"key","groups":[]}`, []string{`{"name":"key","group_id":null,"groups":[]}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload CreateAPIKeyRequest
			require.NoError(t, json.Unmarshal([]byte(tc.seed), &payload))
			dispatched := 0
			_, err := coordinator.Execute(context.Background(), service.IdempotencyExecuteOptions{Scope: "user.api_keys.create", ActorScope: "user:7", Method: http.MethodPost, Route: "/api/v1/api-keys", IdempotencyKey: tc.name, Payload: payload, RequireKey: true, TTL: service.DefaultWriteIdempotencyTTL()}, func(context.Context) (any, error) {
				dispatched++
				return map[string]any{"id": 1, "group_chain_revision": 0}, nil
			})
			require.NoError(t, err)
			require.Equal(t, 1, dispatched)
			router := gin.New()
			router.Use(withUserSubject(7))
			router.POST("/api/v1/api-keys", NewAPIKeyHandler(nil).Create)
			call := func(body string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", tc.name)
				result := httptest.NewRecorder()
				router.ServeHTTP(result, request)
				return result
			}
			replay := call(tc.seed)
			require.Equal(t, http.StatusOK, replay.Code)
			require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
			require.Equal(t, 1, dispatched)
			for _, body := range tc.invalid {
				result := call(body)
				require.Equal(t, http.StatusBadRequest, result.Code, body)
				require.Empty(t, result.Header().Get("X-Idempotency-Replayed"))
				require.Equal(t, 1, dispatched)
			}
		})
	}
}
