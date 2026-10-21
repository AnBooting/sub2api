package middleware

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// accessLogStartKey 记录 Logger 进入时刻。panic 时由 Recovery 在写完最终状态后
// 用它补记访问日志，保证日志里的 status_code 与真实返回一致。
const accessLogStartKey = "middleware.access_log_start"

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		c.Set(accessLogStartKey, startTime)

		// 正常路径：c.Next() 返回后记录。panic 时本函数会被展开跳过，改由
		// Recovery（外层）在写入 500 之后调用 logPanickedRequestAccess，
		// 因此 panic 请求只会产生一条访问日志，且状态是最终响应码。
		c.Next()

		writeAccessLog(c, startTime)
	}
}

// logPanickedRequestAccess 供 Recovery 在 panic 响应写入之后调用。
func logPanickedRequestAccess(c *gin.Context) {
	value, ok := c.Get(accessLogStartKey)
	if !ok {
		return
	}
	delete(c.Keys, accessLogStartKey)
	startTime, ok := value.(time.Time)
	if !ok {
		return
	}
	writeAccessLog(c, startTime)
}

func writeAccessLog(c *gin.Context, startTime time.Time) {
	path := c.Request.URL.Path
	// 跳过健康检查等高频探针路径的日志
	if path == "/health" || path == "/setup/status" {
		return
	}

	endTime := time.Now()
	latency := endTime.Sub(startTime)

	method := c.Request.Method
	statusCode := c.Writer.Status()
	clientIP := ip.GetClientIP(c)
	protocol := c.Request.Proto
	accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
	platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
	model, _ := c.Request.Context().Value(ctxkey.Model).(string)
	reason, rejected := GetIngressRejectReason(c)
	if rejected {
		recordIngressReject(c, reason)
		allowed, droppedSummary := globalIngressRejectAccessSampler.allow(endTime)
		if droppedSummary > 0 {
			logger.FromContext(c.Request.Context()).Info("ingress rejection access logs dropped",
				zap.String("component", "http.access"),
				zap.Uint64("dropped_count", droppedSummary),
				zap.Bool(logger.OpsSystemLogSkipField, true),
			)
		}
		if !allowed {
			return
		}
	}

	fields := []zap.Field{
		zap.String("component", "http.access"),
		zap.Int("status_code", statusCode),
		zap.Int64("latency_ms", latency.Milliseconds()),
		zap.String("client_ip", clientIP),
		zap.String("protocol", protocol),
		zap.String("method", method),
		zap.String("path", path),
	}
	if rejected {
		fields = append(fields,
			zap.String("ingress_reject_reason", string(reason)),
			zap.Bool(logger.OpsSystemLogSkipField, true),
		)
	}
	if hasAccountID && accountID > 0 {
		fields = append(fields, zap.Int64("account_id", accountID))
	}
	if platform != "" {
		fields = append(fields, zap.String("platform", platform))
	}
	if model != "" {
		fields = append(fields, zap.String("model", model))
	}

	l := logger.FromContext(c.Request.Context()).With(fields...)
	l.Info("http request completed", zap.Time("completed_at", endTime))

	if len(c.Errors) > 0 {
		l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
	}
}
