package middleware

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const clientRequestIDHeader = "X-Client-Request-ID"

// ClientRequestID ensures every request has a unique client_request_id in request.Context().
//
// This is used by the Ops monitoring module for end-to-end request correlation.
func ClientRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}

		// 复用上游已注入的值（合法则沿用），否则生成新 ID；两条分支必须产生
		// 相同的上下文与日志效果，否则“上游已注入”的请求会丢掉 client_request_id。
		rawID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		id, valid := normalizeCorrelationID(rawID)
		if !valid {
			id = uuid.New().String()
		}

		c.Header(clientRequestIDHeader, id)
		ctx := context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, id)
		// RequestLogger 已在链路前方写入 client_request_id（可能为空），此处只做
		// 覆盖而不是追加，避免同一日志行出现两个同名 key。
		requestLogger := logger.FromContext(ctx).With(zap.String("client_request_id", strings.TrimSpace(id)))
		ctx = logger.IntoContext(ctx, requestLogger)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
