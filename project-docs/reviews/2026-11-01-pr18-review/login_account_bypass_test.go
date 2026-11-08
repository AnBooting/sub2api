//go:build unit

package routes

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"net/http"
	"strings"
	"testing"
)

type reviewPR18Repo struct {
	service.UserRepository
	attempts  int
	lastEmail string
}

func (r *reviewPR18Repo) GetByEmail(_ context.Context, email string) (*service.User, error) {
	r.attempts++
	r.lastEmail = email
	return nil, service.ErrUserNotFound
}
func TestReviewPR18LoginCannotSkipLockedAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[string]string{
		"uppercase":        `{"Email":"victim@example.com","password":"wrong"}`,
		"oversize":         `{"email":"victim@example.com","password":"wrong","padding":"` + strings.Repeat("x", 65536) + `"}`,
		"trailing_json":    `{"email":"victim@example.com","password":"wrong"} {}`,
		"mixed_case_decoy": `{"email":"decoy@example.com","Email":"victim@example.com","password":"wrong"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			repo := &reviewPR18Repo{}
			cfg := &config.Config{}
			auth := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			h := handler.NewAuthHandler(cfg, auth, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			RegisterAuthRoutes(router.Group("/api/v1"), &handler.Handlers{Auth: h, Setting: &handler.SettingHandler{}}, servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }), servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), rdb, nil, nil)
			for i := 0; i < 5; i++ {
				rec := postLoginBody(router, "203.0.113.50:5000", "", loginBody("victim@example.com"))
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("prime %d: %d %s", i, rec.Code, rec.Body.String())
				}
			}
			normal := postLoginBody(router, "203.0.113.50:5000", "", loginBody("victim@example.com"))
			if normal.Code != http.StatusTooManyRequests {
				t.Fatalf("control not locked: %d", normal.Code)
			}
			rec := postLoginBody(router, "203.0.113.50:5000", "", body)
			t.Logf("control=%d alternate=%d repository_attempts=%d last_email=%s response=%s", normal.Code, rec.Code, repo.attempts, repo.lastEmail, rec.Body.String())
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("locked account bypassed with %s: got %d", name, rec.Code)
			}
		})
	}
}
