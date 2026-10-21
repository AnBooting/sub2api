package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// blockingPricingNetwork 进入出站请求后先通知测试，再等待测试放行，
// 用来观察「后台任务还在跑」这个时间窗。
type blockingPricingNetwork struct {
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	attempts    int
}

func newBlockingPricingNetwork() *blockingPricingNetwork {
	return &blockingPricingNetwork{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingPricingNetwork) FetchPricingJSON(ctx context.Context, _ string) ([]byte, error) {
	b.mu.Lock()
	b.attempts++
	b.mu.Unlock()
	b.enterOnce.Do(func() { close(b.entered) })
	select {
	case <-b.release:
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}
	return nil, errors.New("pricing network must be disabled")
}

func (b *blockingPricingNetwork) FetchHashText(context.Context, string) (string, error) {
	return "", errors.New("pricing network must be disabled")
}

func (b *blockingPricingNetwork) releaseAll() {
	b.releaseOnce.Do(func() { close(b.release) })
}

func (b *blockingPricingNetwork) attemptCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempts
}

// 打开远端拉取与原生 CNY 两个开关时，管理接口会排一次后台刷新。刷新跑在自己的
// goroutine 里：没有调用方能 recover 它的 panic，未被 recover 的 panic 会终止整个
// 进程——单元测试里表现为整个 admin 包 FAIL（而不是单个用例失败）。
//
// 这里固定住这条契约：后台刷新的失败必须被服务收敛成一次「可见的同步失败」，
// 而不是把进程带崩；同时失败必须出现在定价状态里，不能被悄悄吞掉。
func TestPricingBackgroundRefreshPanicIsContained(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	net := &noPricingNetwork{}
	pricing := service.NewPricingService(cfg, net)
	t.Cleanup(pricing.Stop)
	h := NewChannelHandler(nil, nil, pricing, nil)
	router := gin.New()
	router.PUT("/pricing/settings", h.UpdatePricingControls)

	result := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/pricing/settings",
		strings.NewReader(`{"remote_enabled":true,"native_cny_enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(result, request)
	require.Equal(t, http.StatusOK, result.Code)

	// Join the background refresh the handler scheduled. Without the service
	// containing background panics the panic escapes the goroutine and kills the
	// whole test binary: the entire admin package fails, not just this case.
	pricing.WaitForBackgroundRefresh()

	require.Equal(t, 1, net.attemptCount(), "the scheduled background refresh must be observable")
	status := pricing.GetPricingControlStatus()
	cnyError, _ := status["cny_error"].(string)
	require.Contains(t, cnyError, "pricing network must be disabled",
		"a failed background refresh must be reported, not swallowed")
}

// 后台刷新的生命周期必须绑定在服务上：服务的持有方要能「等它做完」，
// 后台任务不得越过触发者（HTTP 请求 / 测试用例）的边界继续跑。
func TestPricingBackgroundRefreshIsJoinable(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	net := newBlockingPricingNetwork()
	pricing := service.NewPricingService(cfg, net)
	t.Cleanup(func() {
		net.releaseAll()
		pricing.Stop()
	})
	require.NoError(t, pricing.UpdateControls(context.Background(), service.PricingControls{
		RemoteEnabled: true, NativeCNYEnabled: true,
	}))
	pricing.RefreshNativeCNY()

	select {
	case <-net.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the background refresh never reached the pricing client")
	}

	joined := make(chan struct{})
	go func() {
		pricing.WaitForBackgroundRefresh()
		close(joined)
	}()
	select {
	case <-joined:
		t.Fatal("WaitForBackgroundRefresh returned while the background refresh was still running")
	case <-time.After(200 * time.Millisecond):
	}

	net.releaseAll()
	select {
	case <-joined:
	case <-time.After(10 * time.Second):
		t.Fatal("WaitForBackgroundRefresh did not return after the background refresh finished")
	}
	require.Equal(t, 1, net.attemptCount())
}

// Stop 必须幂等（关停钩子与调用方收尾都可能停止同一个服务），并且停止之后
// 不再接受新的后台任务。
func TestPricingServiceStopIsIdempotentAndStopsNewBackgroundWork(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	net := &noPricingNetwork{}
	pricing := service.NewPricingService(cfg, net)

	pricing.Stop()
	pricing.Stop() // must not panic

	pricing.RefreshNativeCNY() // must not schedule anything after the stop
	pricing.WaitForBackgroundRefresh()
	require.Zero(t, net.attemptCount(), "a stopped service must not start new background work")
}
