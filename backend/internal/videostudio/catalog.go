package videostudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Discover reads only catalog endpoints. No generation or paid requests occur.
// Ambiguous pricing fails closed instead of importing generic token/flat fields.
func Discover(ctx context.Context, raw, key string) ([]Model, error) {
	return discover(ctx, raw, key, &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }})
}
func discover(ctx context.Context, raw, key string, client *http.Client) ([]Model, error) {
	base, err := laneOrigin(raw)
	if err != nil {
		return nil, err
	}
	read := func(path string, out any) error {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, e := client.Do(req)
		if e != nil {
			return errors.New("读取上游模型失败，请稍后重试")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("上游模型目录返回 %d", resp.StatusCode)
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
		if e != nil || len(b) > 4*1024*1024 {
			return errors.New("上游模型目录响应过大")
		}
		return json.Unmarshal(b, out)
	}
	var allowed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = read("/v1/models", &allowed); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, m := range allowed.Data {
		ids[m.ID] = true
	}
	var catalog struct {
		Data []struct {
			ID          string             `json:"model_name"`
			Label       string             `json:"display_name"`
			Type        string             `json:"model_type"`
			Endpoint    string             `json:"endpoint"`
			Rates       map[string]float64 `json:"video_rates"`
			Resolutions []string           `json:"video_resolution_allow"`
			Min         int                `json:"video_duration_min"`
			Max         int                `json:"video_duration_max"`
			Media       struct {
				Images int  `json:"max_images"`
				Videos int  `json:"max_videos"`
				Audios int  `json:"max_audios"`
				Audio  bool `json:"gen_audio"`
			} `json:"video_media"`
		} `json:"data"`
	}
	if err = read("/api/models", &catalog); err != nil {
		return nil, err
	}
	out := []Model{}
	seen := map[string]bool{}
	for _, u := range catalog.Data {
		if u.Type != "video" || !ids[u.ID] {
			continue
		}
		if seen[u.ID] {
			return nil, fmt.Errorf("模型 %s 存在重复渠道价格，请向上游确认", u.ID)
		}
		seen[u.ID] = true
		if u.Endpoint != "/v1/task/submit" || len(u.Rates) == 0 {
			return nil, fmt.Errorf("模型 %s 的协议或价格结构尚未适配，禁止自动导入", u.ID)
		}
		m := Model{ID: u.ID, Label: u.Label, Mode: "second", Rates: map[string]float64{}, CostCNY: map[string]float64{}, MinSeconds: u.Min, MaxSeconds: u.Max, MaxImages: u.Media.Images, MaxVideos: u.Media.Videos, MaxAudios: u.Media.Audios, Audio: u.Media.Audio}
		if job, ok := u.Rates["job"]; ok {
			if len(u.Rates) != 1 || len(u.Resolutions) == 0 {
				return nil, fmt.Errorf("模型 %s 按条价格缺少清晰度范围", u.ID)
			}
			m.Mode = "job"
			for _, res := range u.Resolutions {
				res = strings.ToLower(res)
				m.Rates[res] = job
				m.CostCNY[res] = job
			}
		} else {
			for res, p := range u.Rates {
				res = strings.ToLower(res)
				m.Rates[res] = p
				m.CostCNY[res] = p
			}
		}
		if m.Label == "" {
			m.Label = m.ID
		}
		ch := Channel{GroupID: 1, AccountID: 1, MaxConcurrent: 1, PublicBaseURL: "https://example.com", Models: []Model{m}}
		if e := ch.Validate(); e != nil {
			return nil, fmt.Errorf("模型 %s 配置不能自动导入：%w", m.ID, e)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) == 0 {
		return nil, errors.New("此上游密钥没有可用的视频模型")
	}
	return out, nil
}
