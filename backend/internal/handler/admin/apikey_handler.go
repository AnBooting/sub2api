package admin

import (
	"encoding/json"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService service.AdminService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService: adminService,
	}
}

// AdminUpdateAPIKeyGroupRequest represents the request to update an API key.
type AdminUpdateAPIKeyGroupRequest struct {
	GroupID                    *int64                     `json:"group_id"` // omission=no change, null/0=clear normal
	GroupIDProvided            bool                       `json:"-"`
	Groups                     *[]service.APIKeyGroupLink `json:"groups"`
	GroupsNull                 bool                       `json:"-"`
	ExpectedGroupChainRevision *int64                     `json:"expected_group_chain_revision"`
	ResetRateLimitUsage        *bool                      `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

func (r *AdminUpdateAPIKeyGroupRequest) UnmarshalJSON(data []byte) error {
	type plain AdminUpdateAPIKeyGroupRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	var err error
	r.GroupIDProvided, r.GroupsNull, err = service.APIKeyGroupChainPresence(data)
	return err
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := service.ValidateAPIKeyGroupChainInput(req.GroupID, req.GroupIDProvided, req.Groups, req.GroupsNull, true, false, req.ExpectedGroupChainRevision); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var result *service.AdminUpdateAPIKeyGroupIDResult
	if req.Groups != nil {
		result, err = h.adminService.AdminUpdateAPIKeyGroups(c.Request.Context(), keyID, service.AdminUpdateAPIKeyGroupsInput{Groups: *req.Groups, ExpectedGroupChainRevision: req.ExpectedGroupChainRevision, ResetRateLimitUsage: req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage})
	} else {
		var resetKey *service.APIKey
		if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage {
			resetKey, err = h.adminService.AdminResetAPIKeyRateLimitUsage(c.Request.Context(), keyID)
			if err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
		groupID := req.GroupID
		if req.GroupIDProvided && groupID == nil {
			zero := int64(0)
			groupID = &zero
		}
		result, err = h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, groupID)
		if err == nil && resetKey != nil && !req.GroupIDProvided && req.GroupID == nil {
			result.APIKey = resetKey
		}
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}
