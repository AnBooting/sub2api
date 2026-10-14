// Package handler provides HTTP request handlers for the application.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler handles API key-related requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
}

// NewAPIKeyHandler creates a new APIKeyHandler
func NewAPIKeyHandler(apiKeyService *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
	}
}

// CreateAPIKeyRequest represents the create API key request payload
type CreateAPIKeyRequest struct {
	Name            string                     `json:"name" binding:"required"`
	GroupID         *int64                     `json:"group_id"` // nullable
	GroupIDProvided bool                       `json:"-"`
	Groups          *[]service.APIKeyGroupLink `json:"groups"`
	GroupsNull      bool                       `json:"-"`
	CustomKey       *string                    `json:"custom_key"`      // 可选的自定义key
	IPWhitelist     []string                   `json:"ip_whitelist"`    // IP 白名单
	IPBlacklist     []string                   `json:"ip_blacklist"`    // IP 黑名单
	Quota           *float64                   `json:"quota"`           // 配额限制 (USD)
	ExpiresInDays   *int                       `json:"expires_in_days"` // 过期天数

	// Rate limit fields (0 = unlimited)
	RateLimit5h *float64 `json:"rate_limit_5h"`
	RateLimit1d *float64 `json:"rate_limit_1d"`
	RateLimit7d *float64 `json:"rate_limit_7d"`
}

// UpdateAPIKeyRequest represents the update API key request payload
type UpdateAPIKeyRequest struct {
	Name                       string                     `json:"name"`
	GroupID                    *int64                     `json:"group_id"`
	GroupIDProvided            bool                       `json:"-"`
	Groups                     *[]service.APIKeyGroupLink `json:"groups"`
	GroupsNull                 bool                       `json:"-"`
	ExpectedGroupChainRevision *int64                     `json:"expected_group_chain_revision"`
	Status                     string                     `json:"status" binding:"omitempty,oneof=active inactive"`
	IPWhitelist                *[]string                  `json:"ip_whitelist"` // IP 白名单（nil 不修改，空数组清空）
	IPBlacklist                *[]string                  `json:"ip_blacklist"` // IP 黑名单（nil 不修改，空数组清空）
	Quota                      *float64                   `json:"quota"`        // 配额限制 (USD), 0=无限制
	ExpiresAt                  *string                    `json:"expires_at"`   // 过期时间 (ISO 8601)
	ResetQuota                 *bool                      `json:"reset_quota"`  // 重置已用配额

	// Rate limit fields (nil = no change, 0 = unlimited)
	RateLimit5h         *float64 `json:"rate_limit_5h"`
	RateLimit1d         *float64 `json:"rate_limit_1d"`
	RateLimit7d         *float64 `json:"rate_limit_7d"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage"` // 重置限速用量
}

func (r *CreateAPIKeyRequest) UnmarshalJSON(data []byte) error {
	type plain CreateAPIKeyRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	var err error
	r.GroupIDProvided, r.GroupsNull, err = service.APIKeyGroupChainPresence(data)
	return err
}

func (r *UpdateAPIKeyRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateAPIKeyRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	var err error
	r.GroupIDProvided, r.GroupsNull, err = service.APIKeyGroupChainPresence(data)
	return err
}

func validAPIKeyLimit(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

func validateAPIKeyCreateRequest(req CreateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	if req.ExpiresInDays != nil && *req.ExpiresInDays <= 0 {
		return errors.New("invalid expires_in_days")
	}
	return nil
}

func validateAPIKeyUpdateRequest(req UpdateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	return nil
}

// List handles listing user's API keys with pagination
// GET /api/v1/api-keys
func (h *APIKeyHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	// Parse filter parameters
	var filters service.APIKeyListFilters
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		filters.Search = search
	}
	filters.Status = c.Query("status")
	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		gid, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err == nil {
			filters.GroupID = &gid
		}
	}

	keys, result, err := h.apiKeyService.List(c.Request.Context(), subject.UserID, params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.APIKey, 0, len(keys))
	for i := range keys {
		out = append(out, *dto.APIKeyFromService(&keys[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

// GetByID handles getting a single API key
// GET /api/v1/api-keys/:id
func (h *APIKeyHandler) GetByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证所有权
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Create handles creating a new API key
// POST /api/v1/api-keys
func (h *APIKeyHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyCreateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative, and expires_in_days must be greater than zero")
		return
	}

	// Validate presence before idempotency replay can return a cached success.
	if err := service.ValidateAPIKeyGroupChainInput(req.GroupID, req.GroupIDProvided, req.Groups, req.GroupsNull, false, true, nil); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	svcReq := service.CreateAPIKeyRequest{
		Name:            req.Name,
		GroupID:         req.GroupID,
		GroupIDProvided: req.GroupIDProvided,
		Groups:          req.Groups,
		GroupsNull:      req.GroupsNull,
		CustomKey:       req.CustomKey,
		IPWhitelist:     req.IPWhitelist,
		IPBlacklist:     req.IPBlacklist,
		ExpiresInDays:   req.ExpiresInDays,
	}
	if req.Quota != nil {
		svcReq.Quota = *req.Quota
	}
	if req.RateLimit5h != nil {
		svcReq.RateLimit5h = *req.RateLimit5h
	}
	if req.RateLimit1d != nil {
		svcReq.RateLimit1d = *req.RateLimit1d
	}
	if req.RateLimit7d != nil {
		svcReq.RateLimit7d = *req.RateLimit7d
	}

	executeUserIdempotentJSON(c, "user.api_keys.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		key, err := h.apiKeyService.Create(ctx, subject.UserID, svcReq)
		if err != nil {
			return nil, err
		}
		return dto.APIKeyFromService(key), nil
	})
}

// Update handles updating an API key
// PUT /api/v1/api-keys/:id
func (h *APIKeyHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyUpdateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative")
		return
	}

	svcReq := service.UpdateAPIKeyRequest{
		IPWhitelist:         req.IPWhitelist,
		IPBlacklist:         req.IPBlacklist,
		Quota:               req.Quota,
		ResetQuota:          req.ResetQuota,
		RateLimit5h:         req.RateLimit5h,
		RateLimit1d:         req.RateLimit1d,
		RateLimit7d:         req.RateLimit7d,
		ResetRateLimitUsage: req.ResetRateLimitUsage,
	}
	if req.Name != "" {
		svcReq.Name = &req.Name
	}
	svcReq.GroupID = req.GroupID
	svcReq.GroupIDProvided = req.GroupIDProvided
	svcReq.Groups = req.Groups
	svcReq.GroupsNull = req.GroupsNull
	svcReq.ExpectedGroupChainRevision = req.ExpectedGroupChainRevision
	if req.Status != "" {
		svcReq.Status = &req.Status
	}
	// Parse expires_at if provided
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			// Empty string means clear expiration
			svcReq.ExpiresAt = nil
			svcReq.ClearExpiration = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				response.BadRequest(c, "Invalid expires_at format: "+err.Error())
				return
			}
			svcReq.ExpiresAt = &t
		}
	}

	key, err := h.apiKeyService.Update(c.Request.Context(), keyID, subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Delete handles deleting an API key
// DELETE /api/v1/api-keys/:id
func (h *APIKeyHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	err = h.apiKeyService.Delete(c.Request.Context(), keyID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "API key deleted successfully"})
}

// SetAPIKeyHAGroupRequest 是用户对某个 HA 兜底组的主动开启/关闭请求。
//
// RateMultiplier 是显式声明的「忽略项」：倍率一律读服务端分组配置，
// 客户端传入的任何倍率字段都不参与计算，也不回写。
type SetAPIKeyHAGroupRequest struct {
	Enabled        bool     `json:"enabled"`
	RateMultiplier *float64 `json:"rate_multiplier"`
}

// SetAPIKeyHAGroup 让用户在自己的某个 API Key 上主动开启/关闭某个高可用兜底组。
// PUT /api/v1/keys/:id/ha-groups/:groupID
//
// 服务端校验顺序即安全边界：Key 归属 → 目标组 allow_high_availability → 组模型白名单
// 与 Key 适用范围匹配 → 倍率取服务端配置。任何一步失败都不落库。
func (h *APIKeyHandler) SetAPIKeyHAGroup(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || keyID <= 0 {
		response.BadRequest(c, "Invalid key ID")
		return
	}
	groupID, err := strconv.ParseInt(c.Param("groupID"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "Invalid group ID")
		return
	}

	if c.Request.Method == http.MethodDelete {
		// DELETE 是无条件关闭：忽略 body 的 enabled，允许空 body、任意 Content-Type，
		// 不得因 EOF 等请求体问题返回 400。
		result, err := h.apiKeyService.SetAPIKeyHAGroupEnabled(c.Request.Context(), subject.UserID, keyID, groupID, false)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		h.writeHAEnablementResponse(c, result)
		return
	}

	// PUT 必须显式带 enabled；缺失即 400，避免漏传字段被静默当成开启或关闭。
	req := SetAPIKeyHAGroupRequest{}
	if err := decodeHAEnablementJSON(c, &req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.apiKeyService.SetAPIKeyHAGroupEnabled(c.Request.Context(), subject.UserID, keyID, groupID, req.Enabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.writeHAEnablementResponse(c, result)
}

// decodeHAEnablementJSON 解析 PUT 请求体：enabled 必须显式出现，缺失即报错。
// 未知字段（含 rate_multiplier）一律忽略，不参与判定。
func decodeHAEnablementJSON(c *gin.Context, req *SetAPIKeyHAGroupRequest) error {
	raw, err := c.GetRawData()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("enabled is required")
	}
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if payload.Enabled == nil {
		return errors.New("enabled is required")
	}
	req.Enabled = *payload.Enabled
	return nil
}

// writeHAEnablementResponse 统一 PUT/DELETE 的成功响应，倍率恒为服务端配置值。
func (h *APIKeyHandler) writeHAEnablementResponse(c *gin.Context, result *service.APIKeyHAGroupEnablementResult) {
	response.Success(c, gin.H{
		"api_key":              dto.APIKeyFromService(result.APIKey),
		"group":                dto.GroupFromService(result.Group),
		"enabled":              result.Enabled,
		"rate_multiplier":      result.RateMultiplier,
		"group_chain_revision": result.GroupChainRevision,
	})
}

// GetAvailableGroups 获取用户可以绑定的分组列表
// GET /api/v1/groups/available
func (h *APIKeyHandler) GetAvailableGroups(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.Group, 0, len(groups))
	for i := range groups {
		out = append(out, *dto.GroupFromService(&groups[i]))
	}
	response.Success(c, out)
}

// GetUserGroupRates 获取当前用户的专属分组倍率配置
// GET /api/v1/groups/rates
func (h *APIKeyHandler) GetUserGroupRates(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	rates, err := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, rates)
}
