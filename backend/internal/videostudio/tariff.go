package videostudio

import (
	"errors"
	"math"
)

// Tariff replaces base resolution pricing when rules are configured. Exactly one
// rule must match: unsupported combinations never silently fall back to a price.
type Tariff struct {
	Resolution     string  `json:"resolution"`
	MinSeconds     int     `json:"min_seconds"`
	MaxSeconds     int     `json:"max_seconds"`
	Audio          string  `json:"audio"`     // any, on, off
	Reference      string  `json:"reference"` // any, with, without
	Mode           string  `json:"mode"`      // second, job
	Rate           float64 `json:"rate"`
	CostCNY        float64 `json:"cost_cny,omitempty"`
	MinimumSeconds int     `json:"minimum_seconds,omitempty"`
	StepSeconds    int     `json:"step_seconds,omitempty"`
	BaseFee        float64 `json:"base_fee,omitempty"`
	BaseCostCNY    float64 `json:"base_cost_cny,omitempty"`
	ImageFee       float64 `json:"image_fee,omitempty"`
	ImageCostCNY   float64 `json:"image_cost_cny,omitempty"`
	VideoFee       float64 `json:"video_fee,omitempty"`
	VideoCostCNY   float64 `json:"video_cost_cny,omitempty"`
	AudioFee       float64 `json:"audio_fee,omitempty"`
	AudioCostCNY   float64 `json:"audio_cost_cny,omitempty"`
}

func (t Tariff) validate(m Model) error {
	if _, ok := m.Rates[t.Resolution]; !ok {
		return errors.New("价格规则清晰度必须在模型支持范围内")
	}
	if t.MinSeconds < m.MinSeconds || t.MaxSeconds > m.MaxSeconds || t.MaxSeconds < t.MinSeconds || t.MinimumSeconds < 0 || t.MinimumSeconds > 600 || t.StepSeconds < 0 || t.StepSeconds > 600 {
		return errors.New("价格规则时长不合法")
	}
	if (t.Mode != "second" && t.Mode != "job") || (t.Audio != "any" && t.Audio != "on" && t.Audio != "off") || (t.Reference != "any" && t.Reference != "with" && t.Reference != "without") {
		return errors.New("价格规则的计价单位、声音或参考素材条件不合法")
	}
	for _, v := range []float64{t.Rate, t.CostCNY, t.BaseFee, t.BaseCostCNY, t.ImageFee, t.ImageCostCNY, t.VideoFee, t.VideoCostCNY, t.AudioFee, t.AudioCostCNY} {
		if v < 0 || v > 10000 || math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("价格规则金额不合法")
		}
	}
	if t.Rate <= 0 {
		return errors.New("价格规则单价必须大于零")
	}
	return nil
}
func (t Tariff) matches(i Input) bool {
	ref := len(i.Images)+len(i.Videos)+len(i.Audios) > 0
	return t.Resolution == i.Resolution && i.Duration >= t.MinSeconds && i.Duration <= t.MaxSeconds && (t.Audio == "any" || (t.Audio == "on") == i.GenerateAudio) && (t.Reference == "any" || (t.Reference == "with") == ref)
}
func quoteRules(m Model, i Input) (float64, float64, error) {
	var chosen *Tariff
	for j := range m.Rules {
		if m.Rules[j].matches(i) {
			if chosen != nil {
				return 0, 0, errors.New("多个价格规则同时命中，请管理员修正重叠规则")
			}
			chosen = &m.Rules[j]
		}
	}
	if chosen == nil {
		return 0, 0, errors.New("当前时长、声音或参考素材组合未配置售价，禁止生成")
	}
	t := *chosen
	units := 1.0
	if t.Mode == "second" {
		seconds := i.Duration
		if seconds < t.MinimumSeconds {
			seconds = t.MinimumSeconds
		}
		if t.StepSeconds > 0 {
			seconds = ((seconds + t.StepSeconds - 1) / t.StepSeconds) * t.StepSeconds
		}
		units = float64(seconds)
	}
	p := t.BaseFee + t.Rate*units + t.ImageFee*float64(len(i.Images)) + t.VideoFee*float64(len(i.Videos)) + t.AudioFee*float64(len(i.Audios))
	c := t.BaseCostCNY + t.CostCNY*units + t.ImageCostCNY*float64(len(i.Images)) + t.VideoCostCNY*float64(len(i.Videos)) + t.AudioCostCNY*float64(len(i.Audios))
	return p, c, nil
}
func PublicModel(m Model) Model {
	m.CostCNY = nil
	m.Rules = append([]Tariff(nil), m.Rules...)
	for i := range m.Rules {
		m.Rules[i].CostCNY = 0
		m.Rules[i].BaseCostCNY = 0
		m.Rules[i].ImageCostCNY = 0
		m.Rules[i].VideoCostCNY = 0
		m.Rules[i].AudioCostCNY = 0
	}
	return m
}
