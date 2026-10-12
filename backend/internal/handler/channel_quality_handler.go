package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ChannelQualityHandler struct {
	service *service.ChannelQualityService
}

func NewChannelQualityHandler(s *service.ChannelQualityService) *ChannelQualityHandler {
	return &ChannelQualityHandler{service: s}
}

func channelQualityIsAdmin(c *gin.Context) bool { return channelMonitorV2IsAdmin(c) }

func channelQualityID(c *gin.Context, field string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(field), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid ID")
		return 0, false
	}
	return id, true
}

func (h *ChannelQualityHandler) Configs(c *gin.Context) {
	items, err := h.service.ListConfigs(c.Request.Context(), channelQualityIsAdmin(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "prompt_version": service.QualityPromptVersion})
}

func (h *ChannelQualityHandler) SaveConfig(c *gin.Context) {
	if !channelQualityIsAdmin(c) {
		response.ErrorFrom(c, service.ErrChannelQualityDenied)
		return
	}
	var cfg service.ChannelQualityConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.BadRequest(c, "invalid configuration")
		return
	}
	if raw := c.Param("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "invalid monitor ID")
			return
		}
		cfg.MonitorID = id
	}
	updated, err := h.service.SaveConfig(c.Request.Context(), cfg)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}

func (h *ChannelQualityHandler) DeleteConfig(c *gin.Context) {
	if !channelQualityIsAdmin(c) {
		response.ErrorFrom(c, service.ErrChannelQualityDenied)
		return
	}
	id, ok := channelQualityID(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteConfig(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *ChannelQualityHandler) authorize(c *gin.Context, monitorID int64, run bool) bool {
	if channelQualityIsAdmin(c) {
		return true
	}
	cfg, err := h.service.GetConfig(c.Request.Context(), monitorID)
	if err != nil || !cfg.Enabled || cfg.CustomerAccess == "hidden" || (run && cfg.CustomerAccess != "run") {
		response.ErrorFrom(c, service.ErrChannelQualityDenied)
		return false
	}
	return true
}

func (h *ChannelQualityHandler) History(c *gin.Context) {
	monitorID, ok := channelQualityID(c, "id")
	if !ok || !h.authorize(c, monitorID, false) {
		return
	}
	items, err := h.service.ListRuns(c.Request.Context(), monitorID, c.Query("model"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !channelQualityIsAdmin(c) {
		for i := range items {
			items[i].RequestedBy = nil
		}
	}
	response.Success(c, gin.H{"items": items})
}

func (h *ChannelQualityHandler) Detail(c *gin.Context) {
	id, ok := channelQualityID(c, "id")
	if !ok {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, service.ErrChannelQualityDenied)
		return
	}
	if !h.authorize(c, run.MonitorID, false) {
		return
	}
	if !channelQualityIsAdmin(c) {
		run.RequestedBy = nil
	}
	response.Success(c, run)
}

func (h *ChannelQualityHandler) Start(c *gin.Context) {
	monitorID, ok := channelQualityID(c, "id")
	if !ok || !h.authorize(c, monitorID, true) {
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "authentication required")
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	run, err := h.service.Start(c.Request.Context(), monitorID, subject.UserID, req.Model, channelQualityIsAdmin(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !channelQualityIsAdmin(c) {
		run.RequestedBy = nil
	}
	response.Success(c, run)
}
