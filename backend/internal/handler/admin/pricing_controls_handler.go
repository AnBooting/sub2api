package admin

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ChannelHandler) GetPricingControls(c *gin.Context) {
	response.Success(c, h.pricingService.GetPricingControlStatus())
}

func (h *ChannelHandler) UpdatePricingControls(c *gin.Context) {
	var req struct {
		RemoteEnabled    *bool `json:"remote_enabled" binding:"required"`
		NativeCNYEnabled *bool `json:"native_cny_enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "both pricing switches are required")
		return
	}
	before := h.pricingService.GetControls()
	controls := service.PricingControls{RemoteEnabled: *req.RemoteEnabled, NativeCNYEnabled: *req.NativeCNYEnabled}
	if err := h.pricingService.UpdateControls(c.Request.Context(), controls); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if controls.RemoteEnabled && controls.NativeCNYEnabled && (!before.NativeCNYEnabled || !before.RemoteEnabled) {
		h.pricingService.RefreshNativeCNY()
	}
	if h.settingService != nil {
		h.settingService.NotifyPublicSettingsUpdate()
	}
	response.Success(c, h.pricingService.GetPricingControlStatus())
}

func (h *ChannelHandler) RefreshPricing(c *gin.Context) {
	if err := h.pricingService.ForceUpdate(); err != nil {
		if errors.Is(err, service.ErrPricingPullDisabled) {
			response.Error(c, http.StatusConflict, err.Error())
		} else {
			response.Error(c, http.StatusBadGateway, err.Error())
		}
		return
	}
	response.Success(c, h.pricingService.GetPricingControlStatus())
}
