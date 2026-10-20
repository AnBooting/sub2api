//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// stubVideoTaskRepo 记录收到的归属与 limit，用于断言查询参数。
type stubVideoTaskRepo struct {
	userID int64
	limit  int
	calls  int
	tasks  []*service.VideoTask
}

func (s *stubVideoTaskRepo) Create(context.Context, *service.VideoTask) (*service.VideoTask, error) {
	return nil, errors.New("unexpected call")
}

func (s *stubVideoTaskRepo) GetByTaskNo(context.Context, string) (*service.VideoTask, error) {
	return nil, service.ErrVideoTaskNotFound
}

func (s *stubVideoTaskRepo) ListDue(context.Context, time.Time, int) ([]*service.VideoTask, error) {
	return nil, nil
}

func (s *stubVideoTaskRepo) MarkChecked(context.Context, int64, int, time.Time, string) error {
	return errors.New("unexpected call")
}

func (s *stubVideoTaskRepo) AttachBilling(context.Context, int64, int64, string, service.VideoTaskBillingPatch) error {
	return errors.New("unexpected call")
}

func (s *stubVideoTaskRepo) MarkCompleted(context.Context, int64, time.Time) (bool, error) {
	return false, errors.New("unexpected call")
}

func (s *stubVideoTaskRepo) MarkFailed(context.Context, int64, time.Time, string) (bool, error) {
	return false, errors.New("unexpected call")
}

func (s *stubVideoTaskRepo) ListByUser(_ context.Context, userID int64, limit int) ([]*service.VideoTask, error) {
	s.calls++
	s.userID = userID
	s.limit = limit
	return s.tasks, nil
}

// callListVideoTasks 发起一次用户侧请求。userID <= 0 表示未登录。
func callListVideoTasks(t *testing.T, handler *VideoTaskHandler, userID int64, query string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/videos/tasks", func(c *gin.Context) {
		if userID > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		handler.ListTasks(c)
	})
	target := "/api/v1/videos/tasks"
	if query != "" {
		target += "?" + query
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder.Code, recorder.Body.String()
}

// decodeVideoTaskList 解析响应信封里的任务数组。
func decodeVideoTaskList(t *testing.T, body string) []map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（body=%s）", err, body)
	}
	raw, ok := envelope["data"].([]any)
	if !ok {
		t.Fatalf("data 必须是数组：%s", body)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("任务项必须是对象：%s", body)
		}
		items = append(items, item)
	}
	return items
}

// TestVideoTaskHandler_ListTasks_RequiresAuth 未登录必须 401，且不得查库。
func TestVideoTaskHandler_ListTasks_RequiresAuth(t *testing.T) {
	repo := &stubVideoTaskRepo{}
	handler := NewVideoTaskHandler(service.NewVideoTaskService(repo))

	status, body := callListVideoTasks(t, handler, 0, "")

	if status != http.StatusUnauthorized {
		t.Fatalf("未登录必须返回 401，实际 %d（body=%s）", status, body)
	}
	if repo.calls != 0 {
		t.Fatalf("未登录时不得查库，实际调用 %d 次", repo.calls)
	}
}

// TestVideoTaskHandler_ListTasks_UsesAuthenticatedUser 只查登录用户自己的任务。
func TestVideoTaskHandler_ListTasks_UsesAuthenticatedUser(t *testing.T) {
	accountID := int64(99)
	repo := &stubVideoTaskRepo{tasks: []*service.VideoTask{{
		TaskNo:         "task-1",
		Status:         service.VideoTaskStatusCompleted,
		Model:          "grok-video",
		Platform:       service.VideoTaskPlatformGrok,
		BillingMode:    "per_request",
		UpstreamTaskID: "upstream-secret-id",
		AccountID:      &accountID,
		CreatedAt:      time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}}}
	handler := NewVideoTaskHandler(service.NewVideoTaskService(repo))

	status, body := callListVideoTasks(t, handler, 42, "")

	if status != http.StatusOK {
		t.Fatalf("必须返回 200，实际 %d（body=%s）", status, body)
	}
	if repo.calls != 1 {
		t.Fatalf("应查库 1 次，实际 %d", repo.calls)
	}
	if repo.userID != 42 {
		t.Fatalf("必须按登录用户查询，实际 userID=%d", repo.userID)
	}
	items := decodeVideoTaskList(t, body)
	if len(items) != 1 {
		t.Fatalf("应返回 1 条任务，实际 %d（body=%s）", len(items), body)
	}
	if items[0]["task_no"] != "task-1" {
		t.Fatalf("task_no 不对：%v", items[0]["task_no"])
	}
	if items[0]["status"] != service.VideoTaskStatusCompleted {
		t.Fatalf("status 不对：%v", items[0]["status"])
	}
	// 最小字段：不得出现上游任务号与承载账号。
	for _, forbidden := range []string{"upstream-secret-id", "upstream_task_id", "account_id", "billing_snapshot"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("响应体泄露了 %q：%s", forbidden, body)
		}
	}
}

// TestVideoTaskHandler_ListTasks_Limit limit 默认 20、上限 50。
func TestVideoTaskHandler_ListTasks_Limit(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  int
	}{
		{name: "默认", query: "", want: 20},
		{name: "非法值回落默认", query: "limit=abc", want: 20},
		{name: "负数回落默认", query: "limit=-5", want: 20},
		{name: "合法值", query: "limit=5", want: 5},
		{name: "超上限截断", query: "limit=999", want: 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubVideoTaskRepo{}
			handler := NewVideoTaskHandler(service.NewVideoTaskService(repo))

			status, body := callListVideoTasks(t, handler, 42, tc.query)

			if status != http.StatusOK {
				t.Fatalf("必须返回 200，实际 %d（body=%s）", status, body)
			}
			if repo.limit != tc.want {
				t.Fatalf("limit 应为 %d，实际 %d", tc.want, repo.limit)
			}
		})
	}
}

// TestVideoTaskHandler_ListTasks_EmptyList 没有任务时返回空数组而不是 404。
func TestVideoTaskHandler_ListTasks_EmptyList(t *testing.T) {
	repo := &stubVideoTaskRepo{}
	handler := NewVideoTaskHandler(service.NewVideoTaskService(repo))

	status, body := callListVideoTasks(t, handler, 42, "")

	if status != http.StatusOK {
		t.Fatalf("必须返回 200，实际 %d（body=%s）", status, body)
	}
	if !strings.Contains(body, "\"data\":[]") {
		t.Fatalf("空列表必须返回空数组：%s", body)
	}
}
