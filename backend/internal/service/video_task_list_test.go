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

func (r *userListVideoTaskRepo) ListByUser(_ context.Context, userID int64, limit int) ([]*VideoTask, error) {
	r.calls++
	r.userID = userID
	r.limit = limit
	return r.tasks, nil
}

// TestVideoTaskService_ListUserTasks_Ownership 只按调用方给定的 user_id 查询。
func TestVideoTaskService_ListUserTasks_Ownership(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	if _, err := svc.ListUserTasks(context.Background(), 42, 20); err != nil {
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

	if _, err := svc.ListUserTasks(context.Background(), 0, 20); !errors.Is(err, ErrVideoTaskForbidden) {
		t.Fatalf("必须返回 %v，实际 %v", ErrVideoTaskForbidden, err)
	}
	if repo.calls != 0 {
		t.Fatalf("非法 user_id 时不得查库，实际调用 %d 次", repo.calls)
	}
}

// TestVideoTaskService_ListUserTasks_Limit 默认 20、上限 50。
func TestVideoTaskService_ListUserTasks_Limit(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	if _, err := svc.ListUserTasks(context.Background(), 7, 0); err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if repo.limit != 20 {
		t.Fatalf("默认 limit 应为 20，实际 %d", repo.limit)
	}

	if _, err := svc.ListUserTasks(context.Background(), 7, 999); err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if repo.limit != 50 {
		t.Fatalf("超上限 limit 应截断为 50，实际 %d", repo.limit)
	}
}

// TestVideoTaskService_ListUserTasks_EmptyList 空结果返回空切片而不是 nil。
func TestVideoTaskService_ListUserTasks_EmptyList(t *testing.T) {
	repo := &userListVideoTaskRepo{}
	svc := NewVideoTaskService(repo)

	tasks, err := svc.ListUserTasks(context.Background(), 7, 20)
	if err != nil {
		t.Fatalf("ListUserTasks 出错：%v", err)
	}
	if tasks == nil {
		t.Fatal("空结果必须是空切片而不是 nil")
	}
	if len(tasks) != 0 {
		t.Fatalf("空结果长度应为 0，实际 %d", len(tasks))
	}
}
