package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint: reports the real install state (I-02)，不再硬编码。
	// 前端用它判断 setup 向导重启后主服务是否已接管。匿名端点只暴露布尔与阶段
	// 枚举，不泄露内部状态；安装类写端点只注册在 setup 模式服务器上，本端点
	// 返回 needs_setup=true 并不会在主服务上开启重装面。
	r.GET("/setup/status", func(c *gin.Context) {
		needsSetup := setup.NeedsSetup()
		step := "completed"
		if needsSetup {
			step = "welcome"
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": needsSetup,
				"step":        step,
			},
		})
	})
}
