package admin

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type upstreamModelFetcher interface {
	FetchUpstreamSupportedModels(context.Context, *service.Account) ([]string, error)
}

// SyncUpstreamModels reads live upstream lists across selected groups. No account
// mappings or capability snapshots are modified, and no global catalog is added.
func (h *GroupHandler) SyncUpstreamModels(c *gin.Context) {
	var req struct {
		GroupIDs []int64 `json:"group_ids" binding:"required,min=1,max=100,dive,gt=0"`
		Platform string  `json:"platform" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Select existing groups and a concrete platform")
		return
	}
	if _, ok := domain.LookupPlatform(req.Platform); !ok || req.Platform == service.PlatformComposite {
		response.BadRequest(c, "Select a concrete platform")
		return
	}
	if h.upstreamModelFetcher == nil {
		response.InternalError(c, "Upstream model service is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	accounts := make(map[int64]service.Account)
	groups := make(map[int64]bool)
	for _, id := range req.GroupIDs {
		if groups[id] {
			continue
		}
		groups[id] = true
		group, err := h.adminService.GetGroup(ctx, id)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if group.Platform != req.Platform && group.Platform != service.PlatformComposite {
			response.BadRequest(c, "Group platform does not match the selected platform")
			return
		}
		items, err := h.adminService.ListAccountsForSchedulerScoreFilter(ctx, req.Platform, "", service.StatusActive, "", id, "")
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		for _, account := range items {
			if account.Platform == req.Platform && account.Status == service.StatusActive && account.Schedulable {
				accounts[account.ID] = account
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	models := make(map[string]string)
	failed := 0
	for _, item := range accounts {
		wg.Add(1)
		go func(account service.Account) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			live, err := h.upstreamModelFetcher.FetchUpstreamSupportedModels(ctx, &account)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return
			}
			for _, model := range clientModelsFromUpstream(&account, live) {
				key := strings.ToLower(model)
				if prev, ok := models[key]; !ok || model < prev {
					models[key] = model
				}
			}
		}(item)
	}
	wg.Wait()
	if len(accounts) > 0 && failed == len(accounts) {
		response.Error(c, http.StatusBadGateway, "Could not read model lists from any selected upstream")
		return
	}
	result := make([]string, 0, len(models))
	for _, model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	response.Success(c, gin.H{"models": result, "failed_accounts": failed, "total_accounts": len(accounts)})
}

func clientModelsFromUpstream(account *service.Account, live []string) []string {
	available := make(map[string]bool)
	for _, model := range live {
		model = strings.TrimSpace(model)
		if model != "" && !strings.Contains(model, "*") {
			available[model] = true
		}
	}
	candidates := make(map[string]bool)
	for model := range available {
		candidates[model] = true
	}
	for model := range account.GetModelMapping() {
		if model != "" && !strings.Contains(model, "*") {
			candidates[model] = true
		}
	}
	result := make([]string, 0)
	for model := range candidates {
		if account.IsModelSupported(model) && available[account.GetMappedModel(model)] {
			result = append(result, model)
		}
	}
	return result
}
