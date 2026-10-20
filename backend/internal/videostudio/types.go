// Package videostudio implements LaneAI video tasks and the durable workbench.
package videostudio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

var ErrNotFound = errors.New("记录不存在")
var ErrConflict = errors.New("内容已更新或幂等键冲突，请刷新后重试")
var ErrFunds = errors.New("余额或密钥额度不足")

type Model struct {
	ID         string             `json:"id"`
	Label      string             `json:"label"`
	Mode       string             `json:"mode"` // second or job; prices in station balance units
	Rates      map[string]float64 `json:"rates"`
	CostCNY    map[string]float64 `json:"cost_cny"`
	MinSeconds int                `json:"min_seconds"`
	MaxSeconds int                `json:"max_seconds"`
	MaxImages  int                `json:"max_images"`
	MaxVideos  int                `json:"max_videos"`
	MaxAudios  int                `json:"max_audios"`
	Audio      bool               `json:"audio"`
}
type Channel struct {
	GroupID       int64   `json:"group_id"`
	AccountID     int64   `json:"account_id"`
	Enabled       bool    `json:"enabled"`
	PublicBaseURL string  `json:"public_base_url"`
	Models        []Model `json:"models"`
	MaxConcurrent int     `json:"max_concurrent"`
	// Prices are final station-wallet prices. No hidden group or FX multiplier.
}
type Input struct {
	Prompt        string   `json:"prompt"`
	Duration      int      `json:"duration"`
	Resolution    string   `json:"resolution"`
	AspectRatio   string   `json:"aspect_ratio"`
	Images        []string `json:"images,omitempty"` // owned asset IDs, never arbitrary URLs
	Videos        []string `json:"videos,omitempty"`
	Audios        []string `json:"audios,omitempty"`
	GenerateAudio bool     `json:"generate_audio"`
}
type Request struct {
	ExpectedAmount *float64 `json:"expected_amount,omitempty"`
	KeyID          int64    `json:"key_id"`
	Model          string   `json:"model"`
	Input          Input    `json:"input"`
	ProjectID      string   `json:"project_id,omitempty"`
	ShotID         string   `json:"shot_id,omitempty"`
}
type Price struct {
	Amount          float64 `json:"amount"`
	UpstreamCostCNY float64 `json:"upstream_cost_cny,omitempty"`
	Model           Model   `json:"model"`
}
type Task struct {
	ID           string    `json:"id"`
	UserID       int64     `json:"-"`
	KeyID        int64     `json:"key_id"`
	GroupID      int64     `json:"group_id"`
	AccountID    int64     `json:"-"`
	ProjectID    string    `json:"project_id"`
	ShotID       string    `json:"shot_id"`
	Request      Request   `json:"request"`
	Pricing      Price     `json:"pricing"`
	UpstreamID   string    `json:"-"`
	Status       string    `json:"status"`
	Outputs      []string  `json:"outputs"`
	Error        string    `json:"error"`
	Hold         float64   `json:"hold"`
	Cost         float64   `json:"cost"`
	Settled      bool      `json:"settled"`
	CreatedAt    time.Time `json:"created_at"`
	PollDeadline time.Time `json:"-"`
}
type Shot struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Prompt     string   `json:"prompt"`
	Duration   int      `json:"duration"`
	Camera     string   `json:"camera"`
	Dialogue   string   `json:"dialogue"`
	Sound      string   `json:"sound"`
	FirstFrame string   `json:"first_frame"`
	Images     []string `json:"images"`
	TaskID     string   `json:"task_id"`
}
type Document struct {
	Title           string   `json:"title"`
	Outline         string   `json:"outline"`
	CharacterImages []string `json:"character_images"`
	Shots           []Shot   `json:"shots"`
}
type Project struct {
	ID        string    `json:"id"`
	Revision  int       `json:"revision"`
	Document  Document  `json:"document"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Asset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	MIME  string `json:"mime"`
	Bytes int    `json:"bytes"`
}

func newID(prefix string) string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}

func (c Channel) Validate() error {
	u, e := url.Parse(c.PublicBaseURL)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("素材公网地址必须是本站 HTTPS 地址")
	}
	if c.GroupID <= 0 || c.AccountID <= 0 || c.MaxConcurrent < 1 || c.MaxConcurrent > 8 || len(c.Models) < 1 || len(c.Models) > 20 {
		return errors.New("请配置分组、账号、模型和 1–8 个并发")
	}
	seen := map[string]bool{}
	for _, m := range c.Models {
		if strings.TrimSpace(m.ID) == "" || seen[m.ID] || (m.Mode != "second" && m.Mode != "job") || m.MinSeconds < 1 || m.MaxSeconds < m.MinSeconds || m.MaxSeconds > 30 || m.MaxImages < 0 || m.MaxImages > 9 || m.MaxVideos < 0 || m.MaxVideos > 3 || m.MaxAudios < 0 || m.MaxAudios > 3 || len(m.Rates) == 0 {
			return errors.New("模型配置不合法")
		}
		seen[m.ID] = true
		for r, p := range m.Rates {
			if (r != "768p" && r != "2k") || p <= 0 || p > 10000 || math.IsNaN(p) || math.IsInf(p, 0) {
				return errors.New("请填写有效的 768p/2k 售价")
			}
			cost, ok := m.CostCNY[r]
			if !ok || cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
				return errors.New("请填写人民币上游成本")
			}
		}
	}
	return nil
}
func Quote(c Channel, r Request) (Price, error) {
	for _, m := range c.Models {
		if m.ID == r.Model {
			p, ok := m.Rates[strings.ToLower(r.Input.Resolution)]
			if !ok || r.Input.Duration < m.MinSeconds || r.Input.Duration > m.MaxSeconds {
				return Price{}, errors.New("此模型不支持所选时长或清晰度")
			}
			if n := len([]rune(strings.TrimSpace(r.Input.Prompt))); n == 0 || n > 7000 {
				return Price{}, errors.New("提示词须为 1–7000 字")
			}
			if len(r.Input.Images) > m.MaxImages || len(r.Input.Videos) > m.MaxVideos || len(r.Input.Audios) > m.MaxAudios || len(r.Input.Images)+len(r.Input.Videos)+len(r.Input.Audios) > 12 {
				return Price{}, errors.New("参考素材数量超过模型上限")
			}
			if r.Input.GenerateAudio && !m.Audio {
				return Price{}, errors.New("此模型不支持声音")
			}
			switch r.Input.AspectRatio {
			case "16:9", "9:16", "1:1", "4:3", "3:4", "21:9":
			default:
				return Price{}, errors.New("请选择有效画面比例")
			}
			cost := m.CostCNY[strings.ToLower(r.Input.Resolution)]
			if m.Mode == "second" {
				p *= float64(r.Input.Duration)
				cost *= float64(r.Input.Duration)
			}
			return Price{Amount: math.Round(p*1e8) / 1e8, UpstreamCostCNY: cost, Model: m}, nil
		}
	}
	return Price{}, fmt.Errorf("模型 %s 未在此视频分组开放", r.Model)
}
func validateDocument(d Document) error {
	if len([]rune(d.Title)) > 120 || len([]rune(d.Outline)) > 20000 || len(d.Shots) > 40 || len(d.CharacterImages) > 5 {
		return errors.New("项目超出限制：40 个镜头、5 张角色图、2 万字大纲")
	}
	seen := map[string]bool{}
	for _, s := range d.Shots {
		if s.ID == "" || seen[s.ID] || s.Duration < 1 || s.Duration > 30 || len([]rune(s.Prompt)) > 7000 || len(s.Images) > 9 {
			return errors.New("镜头 ID、时长或提示词不合法")
		}
		seen[s.ID] = true
	}
	return nil
}
