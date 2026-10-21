package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequestBodyLimit 使用 MaxBytesReader 限制请求体大小。
//
// 注意：本中间件只包装 Body，不主动拒绝超大请求；413 由下游读取方
// 通过 *http.MaxBytesError 触发。不读 Body 的路由（GET/WebSocket）不受此限制。
func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// nil Body 时 MaxBytesReader 会在首次 Read 解引用空接口而 panic，
		// 且包装后下游的 req.Body == nil 守卫会失效，故此处直接跳过。
		if c.Request != nil && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
