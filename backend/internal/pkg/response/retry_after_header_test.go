//go:build unit

package response

import (
	"net/http"
	"net/http/httptest"
	"testing"

	errors2 "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// R-01：429 错误携带 retry_after_seconds 元数据时，ErrorFrom 必须落 Retry-After 头。
func TestErrorFromWritesRetryAfterHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)

	err := errors2.TooManyRequests("REDEEM_RATE_LIMITED", "too many failed attempts").WithRetryAfter(600)
	require.True(t, ErrorFrom(c, err))
	require.Equal(t, "600", w.Header().Get("Retry-After"))
}

func TestErrorFromOmitsRetryAfterHeaderWithoutMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)

	require.True(t, ErrorFrom(c, errors2.BadRequest("TEST", "bad")))
	require.Empty(t, w.Header().Get("Retry-After"))
}
