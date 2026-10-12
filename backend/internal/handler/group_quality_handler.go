package handler

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type GroupQualityHandler struct {
	service *service.GroupQualityService
	groups  channelMonitorV2GroupAuthorizer
}

func NewGroupQualityHandler(s *service.GroupQualityService, g *service.APIKeyService) *GroupQualityHandler {
	return &GroupQualityHandler{s, g}
}

func qualityGroupIDs(ctx context.Context, g channelMonitorV2GroupAuthorizer, userID int64) (map[int64]bool, error) {
	groups, err := g.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, g := range groups {
		out[g.ID] = true
	}
	return out, nil
}
func (h *GroupQualityHandler) authorize(c *gin.Context, id int64, run bool) bool {
	if channelMonitorV2IsAdmin(c) {
		return true
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "authentication required")
		return false
	}
	ids, err := qualityGroupIDs(c.Request.Context(), h.groups, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if !ids[id] {
		response.ErrorFrom(c, service.ErrQualityDenied)
		return false
	}
	cfg, err := h.service.GetConfig(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, service.ErrQualityDenied)
		return false
	}
	if !cfg.Enabled || cfg.CustomerAccess == "hidden" || (run && cfg.CustomerAccess != "run") {
		response.ErrorFrom(c, service.ErrQualityDenied)
		return false
	}
	return true
}
func qualityID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid ID")
		return 0, false
	}
	return id, true
}
func redactQualityRun(r *service.GroupQualityRun) {
	r.AccountID = nil
	r.RequestedBy = nil
	r.UpstreamModel = ""
}

func (h *GroupQualityHandler) Configs(c *gin.Context) {
	admin := channelMonitorV2IsAdmin(c)
	var ids map[int64]bool
	if !admin {
		sub, ok := middleware.GetAuthSubjectFromContext(c)
		if !ok {
			response.Unauthorized(c, "authentication required")
			return
		}
		var err error
		ids, err = qualityGroupIDs(c.Request.Context(), h.groups, sub.UserID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	cs, err := h.service.ListConfigs(c.Request.Context(), ids, admin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := []service.GroupQualityConfig{}
	for _, cfg := range cs {
		if !admin {
			if !ids[cfg.GroupID] || !cfg.Enabled || cfg.CustomerAccess == "hidden" {
				continue
			}
			cfg.AccountID = nil
		}
		out = append(out, cfg)
	}
	response.Success(c, gin.H{"items": out, "prompt_version": service.QualityPromptVersion})
}
func (h *GroupQualityHandler) SaveConfig(c *gin.Context) {
	// Defense in depth even if accidentally registered under user routes.
	if !channelMonitorV2IsAdmin(c) {
		response.ErrorFrom(c, service.ErrQualityDenied)
		return
	}
	id, ok := qualityID(c)
	if !ok {
		return
	}
	var cfg service.GroupQualityConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.BadRequest(c, "invalid configuration")
		return
	}
	cfg.GroupID = id
	out, err := h.service.SaveConfig(c.Request.Context(), cfg)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}
func (h *GroupQualityHandler) History(c *gin.Context) {
	id, ok := qualityID(c)
	if !ok || !h.authorize(c, id, false) {
		return
	}
	runs, err := h.service.ListRuns(c.Request.Context(), id, c.Query("model"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !channelMonitorV2IsAdmin(c) {
		for i := range runs {
			redactQualityRun(&runs[i])
		}
	}
	response.Success(c, gin.H{"items": runs})
}
func (h *GroupQualityHandler) Detail(c *gin.Context) {
	id, ok := qualityID(c)
	if !ok {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, service.ErrQualityDenied)
		return
	}
	if !h.authorize(c, run.GroupID, false) {
		return
	}
	if !channelMonitorV2IsAdmin(c) {
		redactQualityRun(run)
	}
	response.Success(c, run)
}
func (h *GroupQualityHandler) Start(c *gin.Context) {
	id, ok := qualityID(c)
	if !ok || !h.authorize(c, id, true) {
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
	run, err := h.service.Start(c.Request.Context(), id, subject.UserID, req.Model, channelMonitorV2IsAdmin(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !channelMonitorV2IsAdmin(c) {
		redactQualityRun(run)
	}
	response.Success(c, run)
}
