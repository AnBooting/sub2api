package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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

// ListTasks 列出当前用户的视频任务（创建时间倒序）。
//
// GET /api/v1/videos/tasks?limit=20
//
// limit 默认 20、上限 50（超出直接截断）；没有任务时返回空数组而不是 404。
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
	tasks, err := h.tasks.ListUserTasks(c.Request.Context(), subject.UserID, limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]dto.VideoTaskSummaryDTO, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		items = append(items, dto.ToVideoTaskSummaryDTO(task))
	}
	response.Success(c, items)
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
