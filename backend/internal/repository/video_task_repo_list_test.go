//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// videoTaskListColumns 是用户侧列表查询返回的列（顺序与实现一致）。
var videoTaskListColumns = []string{
	"id", "task_no", "status", "model", "platform", "billing_mode", "created_at", "settled_at", "last_error",
}

// TestVideoTaskRepository_ListByUser 校验按用户查询的 SQL 形态、参数与最小字段返回。
func TestVideoTaskRepository_ListByUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 sqlmock 失败：%v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewVideoTaskRepository(db)
	createdAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	settledAt := createdAt.Add(time.Minute)

	rows := sqlmock.NewRows(videoTaskListColumns).
		AddRow(int64(11), "task-1", service.VideoTaskStatusCompleted, "grok-video", service.VideoTaskPlatformGrok, "per_request", createdAt, settledAt, "").
		AddRow(int64(10), "task-2", service.VideoTaskStatusProcessing, "seedance", service.VideoTaskPlatformSeedance, "per_second", createdAt.Add(-time.Hour), nil, "upstream timeout")

	// 归属过滤 + 倒序必须落在 SQL 里，而不是在应用层过滤。
	mock.ExpectQuery("FROM video_tasks WHERE user_id = \\$1 ORDER BY created_at DESC").
		WithArgs(int64(7), 20).
		WillReturnRows(rows)

	tasks, err := repo.ListByUser(context.Background(), 7, nil, 20)
	if err != nil {
		t.Fatalf("ListByUser 出错：%v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("应返回 2 条任务，实际 %d", len(tasks))
	}
	if tasks[0].TaskNo != "task-1" || tasks[0].Status != service.VideoTaskStatusCompleted {
		t.Fatalf("第一条任务内容不对：%+v", tasks[0])
	}
	// id 必须回填：翻页游标要靠它做并列兜底。
	if tasks[0].ID != 11 || tasks[1].ID != 10 {
		t.Fatalf("id 必须回填：%d / %d", tasks[0].ID, tasks[1].ID)
	}
	if tasks[0].SettledAt == nil || !tasks[0].SettledAt.Equal(settledAt) {
		t.Fatalf("settled_at 应为 %s，实际 %v", settledAt, tasks[0].SettledAt)
	}
	if tasks[1].SettledAt != nil {
		t.Fatalf("未结算任务的 settled_at 应为空，实际 %v", tasks[1].SettledAt)
	}
	if tasks[1].LastError != "upstream timeout" {
		t.Fatalf("last_error 应为 %q，实际 %q", "upstream timeout", tasks[1].LastError)
	}

	// 最小字段：不得带回上游任务号、承载账号与计费快照。
	if tasks[0].UpstreamTaskID != "" {
		t.Fatalf("不得返回 upstream_task_id，实际 %q", tasks[0].UpstreamTaskID)
	}
	if tasks[0].AccountID != nil {
		t.Fatalf("不得返回 account_id，实际 %v", *tasks[0].AccountID)
	}
	if len(tasks[0].BillingSnapshot) != 0 {
		t.Fatalf("不得返回 billing_snapshot，实际 %q", string(tasks[0].BillingSnapshot))
	}
	if tasks[0].UserID != 0 || tasks[0].APIKeyID != 0 || tasks[0].GroupID != 0 {
		t.Fatalf("用户侧摘要不应回填归属外键：%+v", tasks[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 断言未满足：%v", err)
	}
}

// TestVideoTaskRepository_ListByUser_WithCursor 游标条件必须是 (created_at, id) 严格小于。
func TestVideoTaskRepository_ListByUser_WithCursor(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 sqlmock 失败：%v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewVideoTaskRepository(db)
	cursorAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(videoTaskListColumns)

	mock.ExpectQuery("FROM video_tasks WHERE user_id = \\$1 AND \\(created_at, id\\) < \\(\\$2, \\$3\\) ORDER BY created_at DESC").
		WithArgs(int64(7), cursorAt, int64(42), 20).
		WillReturnRows(rows)

	tasks, err := repo.ListByUser(context.Background(), 7, &service.VideoTaskCursor{CreatedAt: cursorAt, ID: 42}, 20)
	if err != nil {
		t.Fatalf("ListByUser 出错：%v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("应返回 0 条任务，实际 %d", len(tasks))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("游标 SQL 断言未满足：%v", err)
	}
}

// TestVideoTaskRepository_ListByUser_RejectsMissingUser user_id 非法时不得查库。
func TestVideoTaskRepository_ListByUser_RejectsMissingUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 sqlmock 失败：%v", err)
	}
	defer func() { _ = db.Close() }()

	repo := NewVideoTaskRepository(db)
	if _, err := repo.ListByUser(context.Background(), 0, nil, 20); err == nil {
		t.Fatal("user_id 非法时必须报错")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("不应发起任何查询：%v", err)
	}
}
