package admin

import (
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DomesticPricingHandler struct {
	service *service.DomesticPricingService
}

func NewDomesticPricingHandler(pricingService *service.DomesticPricingService) *DomesticPricingHandler {
	return &DomesticPricingHandler{service: pricingService}
}

type domesticPricingOverrideRequest struct {
	Provider                string   `json:"provider"`
	Model                   string   `json:"model"`
	Source                  string   `json:"source"`
	InputPricePerToken      *float64 `json:"input_price_per_token"`
	OutputPricePerToken     *float64 `json:"output_price_per_token"`
	CacheReadPricePerToken  *float64 `json:"cache_read_price_per_token"`
	CacheWritePricePerToken *float64 `json:"cache_write_price_per_token"`
}

func (h *DomesticPricingHandler) GetCatalog(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Domestic pricing service is unavailable")
		return
	}
	entries, err := h.service.ListCatalog(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to load domestic pricing catalog")
		return
	}
	response.Success(c, gin.H{
		"items":        entries,
		"currency":     "USD",
		"unit":         "token",
		"display_unit": "million_tokens",
	})
}

func (h *DomesticPricingHandler) PutOverride(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Domestic pricing service is unavailable")
		return
	}
	var req domesticPricingOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	if pathProvider := strings.TrimSpace(c.Param("provider")); pathProvider != "" {
		req.Provider = pathProvider
	}
	if pathModel := strings.TrimSpace(c.Param("model")); pathModel != "" {
		req.Model = pathModel
	}
	if req.InputPricePerToken == nil || req.OutputPricePerToken == nil {
		response.BadRequest(c, "input_price_per_token and output_price_per_token are required")
		return
	}
	entry, err := h.service.UpsertOverride(c.Request.Context(), service.DomesticPricingOverrideInput{
		Provider:                req.Provider,
		Model:                   req.Model,
		Source:                  req.Source,
		InputPricePerToken:      *req.InputPricePerToken,
		OutputPricePerToken:     *req.OutputPricePerToken,
		CacheReadPricePerToken:  req.CacheReadPricePerToken,
		CacheWritePricePerToken: req.CacheWritePricePerToken,
	})
	if err != nil {
		if errors.Is(err, service.ErrDomesticPricingInvalid) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, "Failed to save domestic pricing override")
		return
	}
	response.Success(c, entry)
}

func (h *DomesticPricingHandler) DeleteOverride(c *gin.Context) {
	if h == nil || h.service == nil {
		response.InternalError(c, "Domestic pricing service is unavailable")
		return
	}
	deleted, err := h.service.DeleteOverride(c.Request.Context(), c.Param("provider"), c.Param("model"))
	if err != nil {
		if errors.Is(err, service.ErrDomesticPricingInvalid) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, "Failed to delete domestic pricing override")
		return
	}
	if !deleted {
		response.NotFound(c, "Domestic pricing override not found")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
