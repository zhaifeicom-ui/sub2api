package admin

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetPromptInjections(c *gin.Context) {
	config, err := h.settingService.RefreshPromptInjectionConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}
func (h *SettingHandler) GetManagedPolicyEvents(c *gin.Context) {
	events, err := h.settingService.ListManagedPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, events)
}

func (h *SettingHandler) UpdatePromptInjections(c *gin.Context) {
	var config service.PromptInjectionConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	updated, err := h.settingService.SetPromptInjectionConfig(c.Request.Context(), config)
	if err != nil {
		if errors.Is(err, service.ErrPromptInjectionConflict) {
			response.ErrorFrom(c, err)
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, updated)
}
