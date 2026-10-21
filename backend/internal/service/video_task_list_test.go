//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// userListVideoTaskRepo 是用户侧列表端口的记录型替身。
type userListVideoTaskRepo struct {
	userID int64
	limit  int
	calls  int
	cursor *VideoTaskCursor
	tasks  []*VideoTask
}

func (r *userListVideoTaskRepo) Create(context.Context, *VideoTask) (*VideoTask, error) {
	return nil, errors.New("unexpected call")
}

func (r *userListVideoTaskRepo) GetByTaskNo(context.Context, string) (*VideoTask, error) {
	return nil, ErrVideoTaskNotFound
}

func (r *userListVideoTaskRepo) ListDue(context.Context, time.Time, int) ([]*VideoTask, error) {
	return nil, nil
}

func (r *userListVideoTaskRepo) MarkChecked(context.Context, int64, int, time.Time, string) error {
	return errors.New("unexpected call")
}

func (r *userListVideoTaskRepo) AttachBilling(context.Context, int64, int64, string, VideoTaskBillingPatch) error {
	return errors.New("unexpected call")
}

func (r *userListVideoTaskRepo) MarkCompleted(context.Context, int64, time.Time) (bool, error) {
	return false, errors.New("unexpected call")
}

func (r *userListVideoTaskRepo) MarkFailed(context.Context, int64, time.Time, string) (bool, error) {
	return false, errors.New("unexpected call")
}

func (r *userListVideoTaskRepo) ListByUser(_ context.Context, userID int64, cursor *VideoTaskCursor, limit int) ([]*VideoTask, error) {
	r.calls++
	r.userID = userID
	r.limit = limit
	r.cursor = cursor
	return r.tasks, nil
}

// userListTasks 返回 count 条任务（created_at 依次递减，模拟倒序结果集）。
func userListTasks(count int) []*VideoTask {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	out := make([]*VideoTask, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, &VideoTask{
			ID:        int64(100 - i),
			TaskNo:    "task-" + string(rune('a'+i)),
			Status:    VideoTaskStatusProcessing,
			CreatedAt: base.Add(-time.Duration(i) * time.Minute),
		})
	}
	return out
}

// TestVideoTaskService_ListUserTasks_Ownership 只按调用方给定的 user_id 查询。
func TestVideoTaskService_ListUserTasks_Ownership(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	if _, err := svc.ListUserTasks(context.Background(), 42, nil, 20); err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if repo.userID != 42 {
		t.Fatalf("必须按调用方 user_id 查询，实际 %d", repo.userID)
	}
}

// TestVideoTaskService_ListUserTasks_RejectsInvalidUser user_id 非法时不得查库。
func TestVideoTaskService_ListUserTasks_RejectsInvalidUser(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	if _, err := svc.ListUserTasks(context.Background(), 0, nil, 20); !errors.Is(err, ErrVideoTaskForbidden) {
		t.Fatalf("必须返回 %v，实际 %v", ErrVideoTaskForbidden, err)
	}
	if repo.calls != 0 {
		t.Fatalf("非法 user_id 时不得查库，实际调用 %d 次", repo.calls)
	}
}

// TestVideoTaskService_ListUserTasks_Limit 默认 20、上限 50。
// 服务层会多取一条用于判断是否还有下一页，因此仓储收到的是 limit+1。
func TestVideoTaskService_ListUserTasks_Limit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "默认", limit: 0, want: 21},
		{name: "合法值", limit: 5, want: 6},
		{name: "超上限截断", limit: 999, want: 51},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &userListVideoTaskRepo{}
			svc := NewVideoTaskService(repo)

			if _, err := svc.ListUserTasks(context.Background(), 7, nil, tc.limit); err != nil {
				t.Fatalf("ListUserTasks 出错：%v", err)
			}
			if repo.limit != tc.want {
				t.Fatalf("仓储应收到 limit+1=%d，实际 %d", tc.want, repo.limit)
			}
		})
	}
}

// TestVideoTaskService_ListUserTasks_NextCursor 还有更多时给出下一页游标。
func TestVideoTaskService_ListUserTasks_NextCursor(t *testing.T) {
	repo := &userListVideoTaskRepo{tasks: userListTasks(21)}
	svc := NewVideoTaskService(repo)

	page, err := svc.ListUserTasks(context.Background(), 7, nil, 20)
	if err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if len(page.Items) != 20 {
		t.Fatalf("当前页应有 20 条，实际 %d", len(page.Items))
	}
	if page.NextCursor == "" {
		t.Fatal("还有更多时必须给出 next_cursor")
	}
	last := page.Items[len(page.Items)-1]
	cursor, err := DecodeVideoTaskCursor(page.NextCursor)
	if err != nil {
		t.Fatalf("next_cursor 必须可解码：%v", err)
	}
	if cursor.ID != last.ID || !cursor.CreatedAt.Equal(last.CreatedAt) {
		t.Fatalf("next_cursor 必须指向本页最后一条：%+v vs %+v", cursor, last)
	}
}

// TestVideoTaskService_ListUserTasks_NoMore 没有更多时 next_cursor 为空。
func TestVideoTaskService_ListUserTasks_NoMore(t *testing.T) {
	repo := &userListVideoTaskRepo{tasks: userListTasks(20)}
	svc := NewVideoTaskService(repo)

	page, err := svc.ListUserTasks(context.Background(), 7, nil, 20)
	if err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if len(page.Items) != 20 {
		t.Fatalf("当前页应有 20 条，实际 %d", len(page.Items))
	}
	if page.NextCursor != "" {
		t.Fatalf("没有更多时 next_cursor 必须为空，实际 %q", page.NextCursor)
	}
}

// TestVideoTaskService_ListUserTasks_EmptyList 空结果返回空页。
func TestVideoTaskService_ListUserTasks_EmptyList(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	page, err := svc.ListUserTasks(context.Background(), 7, nil, 20)
	if err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if page.Items == nil {
		t.Fatal("空结果必须是空切片而不是 nil")
	}
	if len(page.Items) != 0 {
		t.Fatalf("空结果长度应为 0，实际 %d", len(page.Items))
	}
	if page.NextCursor != "" {
		t.Fatalf("空结果 next_cursor 必须为空，实际 %q", page.NextCursor)
	}
}

// TestVideoTaskService_ListUserTasks_PassesCursor 游标必须原样传给仓储。
func TestVideoTaskService_ListUserTasks_PassesCursor(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)
	cursor := &VideoTaskCursor{CreatedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), ID: 42}

	if _, err := svc.ListUserTasks(context.Background(), 7, cursor, 20); err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if repo.cursor == nil {
		t.Fatal("游标必须传给仓储")
	}
	if repo.cursor.ID != cursor.ID || !repo.cursor.CreatedAt.Equal(cursor.CreatedAt) {
		t.Fatalf("游标必须原样传递：%+v", repo.cursor)
	}
}

// TestVideoTaskCursor_RoundTrip 游标编解码必须可逆。
func TestVideoTaskCursor_RoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 10, 1, 12, 34, 56, 789, time.UTC)
	encoded := EncodeVideoTaskCursor(createdAt, 4242)
	decoded, err := DecodeVideoTaskCursor(encoded)
	if err != nil {
		t.Fatalf("解码失败：%v", err)
	}
	if decoded.ID != 4242 {
		t.Fatalf("id 不对：%d", decoded.ID)
	}
	if !decoded.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at 不对：%s", decoded.CreatedAt)
	}
}

// TestVideoTaskCursor_Invalid 非法游标必须报错（调用方据此回落第一页）。
func TestVideoTaskCursor_Invalid(t *testing.T) {
	for _, raw := range []string{"", "not-base64!!", "YWJj", "MTIz", "MTIzOg"} {
		if _, err := DecodeVideoTaskCursor(raw); err == nil {
			t.Fatalf("非法游标 %q 必须报错", raw)
		}
	}
}
