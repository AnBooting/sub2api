package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// VideoTaskHandler 提供用户侧的视频任务只读接口。
//
// 越权边界：任务归属**只**来自登录态（ctx 里的 userID），
// 任何按他人 user_id 查询的参数都不提供。
type VideoTaskHandler struct {
	tasks *service.VideoTaskService
}

// NewVideoTaskHandler 构造 handler。tasks 为 nil 时所有接口 fail closed（503）。
func NewVideoTaskHandler(tasks *service.VideoTaskService) *VideoTaskHandler {
	return &VideoTaskHandler{tasks: tasks}
}

// ListTasks 列出当前用户的视频任务（创建时间倒序，支持游标翻页）。
//
// GET /api/v1/videos/tasks?limit=20&cursor=<opaque>
//
// limit 默认 20、上限 50（超出直接截断）；cursor 非法时回落到第一页；
// 没有任务时返回空数组而不是 404。
func (h *VideoTaskHandler) ListTasks(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h == nil || h.tasks == nil {
		response.ErrorFrom(c, service.ErrVideoTaskUnavailable)
		return
	}
	limit := service.NormalizeVideoTaskListLimit(parseVideoTaskListLimit(c.Query("limit")))
	cursor := parseVideoTaskCursor(c.Query("cursor"))
	page, err := h.tasks.ListUserTasks(c.Request.Context(), subject.UserID, cursor, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]dto.VideoTaskSummaryDTO, 0, len(page.Items))
	for _, task := range page.Items {
		if task == nil {
			continue
		}
		items = append(items, dto.ToVideoTaskSummaryDTO(task))
	}
	result := dto.VideoTaskPageDTO{Items: items}
	if page.NextCursor != "" {
		next := page.NextCursor
		result.NextCursor = &next
	}
	response.Success(c, result)
}

// parseVideoTaskListLimit 解析 limit 查询参数。
//
// 非法值一律按 0 处理（交给服务层收敛成默认值 20），不做 400：
// 前端只要传了数字就能拿到结果，不必区分「参数非法」与「没传」。
func parseVideoTaskListLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return parsed
}

// parseVideoTaskCursor 解析 cursor 查询参数。
//
// 非法游标（解码失败 / 格式错）时回落到第一页并记日志：
// 一个坏参数不该把整页变成 500，日志让运维能观察到客户端在传坏游标。
func parseVideoTaskCursor(raw string) *service.VideoTaskCursor {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	cursor, err := service.DecodeVideoTaskCursor(trimmed)
	if err != nil {
		logger.L().Warn("video_task.invalid_cursor", zap.String("reason", err.Error()))
		return nil
	}
	return cursor
}
