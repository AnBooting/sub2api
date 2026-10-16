package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

func (h *ChannelHandler) GetPricingControls(c *gin.Context) {
	response.Success(c, h.pricingService.GetPricingControlStatus())
}

func (h *ChannelHandler) UpdateDeepseekHolidays(c *gin.Context) {
	var req struct {
		Dates *[]string `json:"dates" binding:"required"`
	}
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		response.BadRequest(c, "dates must be a list of YYYY-MM-DD holiday dates")
		return
	}
	before := h.pricingService.DeepseekHolidayDates()
	beforeCalendar := h.pricingService.GetControls().DeepSeekHolidays
	if err := h.pricingService.UpdateDeepseekHolidays(c.Request.Context(), *req.Dates); err != nil {
		if errors.Is(err, service.ErrInvalidDeepseekHolidays) {
			response.BadRequest(c, err.Error())
		} else {
			response.InternalError(c, "could not save holiday calendar")
		}
		return
	}
	middleware.SetAuditAction(c, "admin.pricing.holidays.update")
	middleware.SetAuditHolidayCalendar(c, before, h.pricingService.DeepseekHolidayDates())
	middleware.SetAuditHolidayCalendarYears(c, beforeCalendar, h.pricingService.GetControls().DeepSeekHolidays)
	response.Success(c, h.pricingService.GetPricingControlStatus())
}

func (h *ChannelHandler) UpdatePricingControls(c *gin.Context) {
	var req struct {
		RemoteEnabled    *bool           `json:"remote_enabled" binding:"required"`
		NativeCNYEnabled *bool           `json:"native_cny_enabled" binding:"required"`
		DeepSeekHolidays json.RawMessage `json:"deepseek_holidays"`
		// PRD-01/K-6: the admin page may change the pricing sync period. Omitted
		// (nil) preserves the stored override; 0 is rejected because "use the config
		// default" is expressed by omitting the field, and a real period is >= 1h.
		UpdateIntervalHours *int `json:"update_interval_hours"`
	}
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		response.BadRequest(c, "both pricing switches are required")
		return
	}
	controls := service.PricingControls{RemoteEnabled: *req.RemoteEnabled, NativeCNYEnabled: *req.NativeCNYEnabled}
	if req.UpdateIntervalHours != nil {
		if *req.UpdateIntervalHours == 0 {
			response.BadRequest(c, "update_interval_hours must be positive; omit it to keep the current value")
			return
		}
		controls.UpdateIntervalHours = *req.UpdateIntervalHours
	}
	if len(req.DeepSeekHolidays) > 0 {
		if bytes.Equal(bytes.TrimSpace(req.DeepSeekHolidays), []byte("null")) {
			response.BadRequest(c, "deepseek_holidays must be an object, not null")
			return
		}
		if err := json.Unmarshal(req.DeepSeekHolidays, &controls.DeepSeekHolidays); err != nil {
			response.BadRequest(c, "deepseek_holidays must be a year-to-date-array object")
			return
		}
	}
	// This snapshot only decides whether to schedule a refresh. Calendar omission
	// is resolved atomically by UpdateControls, not against this earlier read.
	before := h.pricingService.GetControls()
	beforeDates := h.pricingService.DeepseekHolidayDates()
	if err := h.pricingService.UpdateControls(c.Request.Context(), controls); err != nil {
		if errors.Is(err, service.ErrInvalidDeepSeekHolidays) {
			response.BadRequest(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrInvalidPricingSyncInterval) {
			response.BadRequest(c, "update_interval_hours must be between 1 and 720")
			return
		}
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if controls.DeepSeekHolidays != nil {
		middleware.SetAuditAction(c, "admin.pricing.holidays.update")
		middleware.SetAuditHolidayCalendar(c, beforeDates, h.pricingService.DeepseekHolidayDates())
		middleware.SetAuditHolidayCalendarYears(c, before.DeepSeekHolidays, h.pricingService.GetControls().DeepSeekHolidays)
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
