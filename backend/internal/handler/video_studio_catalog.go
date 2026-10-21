package handler

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/videostudio"
	"github.com/gin-gonic/gin"
)

func (h *VideoStudioHandler) ChannelChoices(c *gin.Context) {
	if !videoAdmin(c) {
		return
	}
	type choice struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	load := func(query string) ([]choice, error) {
		rows, e := h.store.DB.QueryContext(c.Request.Context(), query)
		if e != nil {
			return nil, e
		}
		defer func() { _ = rows.Close() }()
		out := []choice{}
		for rows.Next() {
			var v choice
			if e = rows.Scan(&v.ID, &v.Name); e != nil {
				return nil, e
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}
	groups, e := load(`SELECT id,name FROM groups WHERE platform='video' AND deleted_at IS NULL AND status='active' AND subscription_type='standard' ORDER BY name`)
	if e != nil {
		videoReply(c, nil, e)
		return
	}
	accounts, e := load(`SELECT id,name FROM accounts WHERE platform='video' AND type='apikey' AND deleted_at IS NULL AND status='active' ORDER BY name`)
	videoReply(c, gin.H{"groups": groups, "accounts": accounts}, e)
}
func (h *VideoStudioHandler) ImportCatalog(c *gin.Context) {
	if !videoAdmin(c) {
		return
	}
	var req struct {
		AccountID int64 `json:"account_id"`
	}
	if e := c.ShouldBindJSON(&req); e != nil {
		videoReply(c, nil, errors.New("请选择视频账号"))
		return
	}
	raw, key, e := h.store.Account(c.Request.Context(), req.AccountID)
	if e != nil {
		videoReply(c, nil, e)
		return
	}
	models, e := videostudio.Discover(c.Request.Context(), raw, key)
	videoReply(c, models, e)
}
func (h *VideoStudioHandler) keyModels(c *gin.Context) ([]videostudio.Model, error) {
	k, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || k.Group == nil || k.Group.Platform != "video" {
		return nil, errors.New("请使用绑定视频分组的密钥")
	}
	ch, e := h.store.Channel(c.Request.Context(), k.Group.ID)
	if e != nil {
		return nil, e
	}
	if !ch.Enabled {
		return nil, errors.New("此视频渠道尚未开放")
	}
	out := []videostudio.Model{}
	for _, m := range ch.Models {
		if k.Group.ModelAllowlist.Enabled && !k.Group.ModelAllowlist.Allows(m.ID) {
			continue
		}
		out = append(out, videostudio.PublicModel(m))
	}
	return out, nil
}
func (h *VideoStudioHandler) Models(c *gin.Context) {
	models, e := h.keyModels(c)
	if e != nil {
		videoReply(c, nil, e)
		return
	}
	data := []gin.H{}
	for _, m := range models {
		data = append(data, gin.H{"id": m.ID, "object": "model", "created": 0, "owned_by": "video-channel"})
	}
	videoReply(c, gin.H{"object": "list", "data": data}, nil)
}
func (h *VideoStudioHandler) Skills(c *gin.Context) {
	models, e := h.keyModels(c)
	if e != nil {
		videoReply(c, nil, e)
		return
	}
	videoReply(c, gin.H{
		"object": "skills", "protocol": "sub2api.video.tasks.v1", "upstream_adapter": "laneai_task", "models": models,
		"auth":           "Authorization: Bearer YOUR_STATION_KEY",
		"endpoints":      gin.H{"models": "GET /v1/models", "quote": "POST /v1/videos/quote", "submit": "POST /v1/task/submit", "query": "GET /v1/task/{id}", "upload": "POST /v1/video/assets (multipart/form-data, field=file)", "assets": "GET /v1/video/assets"},
		"submit_headers": gin.H{"Idempotency-Key": "Required, 8–120 characters. Reuse only for retrying the exact same request."},
		"example":        gin.H{"model": "USE_MODEL_ID_FROM_MODELS", "input": gin.H{"prompt": "Describe the scene", "duration": 4, "resolution": "768p", "aspect_ratio": "16:9", "generate_audio": false}},
		"input_schema":   gin.H{"prompt": "1–7000 characters", "duration": "Integer seconds within the chosen model limits", "resolution": "One of the keys in the model rates map", "aspect_ratio": []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}, "generate_audio": "Boolean; requires model.audio", "images": "Array of owned asset IDs, not remote URLs", "videos": "Array of owned asset IDs", "audios": "Array of owned asset IDs"},
		"result":         "Submit returns a station id. Poll GET /v1/task/{id} every 5–15 seconds; completed outputs is an array of upstream HTTPS video URLs.",
		"statuses":       []string{"pending", "submitting", "queued", "in_progress", "completed", "failed", "needs_review"},
		"billing":        "Final wallet prices, no chat/group multiplier. Quote first; optional expected_amount protects against price changes. Failed tasks refund. needs_review holds funds pending reconciliation. Script planning uses a separate chat key.",
		"errors":         gin.H{"400": "Invalid input or no matching tariff", "402": "Insufficient balance/quota", "404": "Task not found or not owned by this key", "409": "Idempotency conflict", "500": "Service unavailable"},
		"compatibility":  "Not OpenAI Videos or MiniMax official API. LaneAI task request shape, station-specific IDs, asset IDs and idempotency semantics.",
		"retention":      "Video outputs are upstream links and may expire. Download promptly; no automatic clip stitching.",
	}, nil)
}
