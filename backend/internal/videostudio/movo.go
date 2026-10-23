package videostudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const MovoProtocol = "movo_video"

// Origin validates both the protocol and its destination before using credentials.
func Origin(raw, protocol string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/" && u.Path != "/v1" && u.Path != "/v1/") {
		return "", errors.New("视频上游必须使用已适配站点的 HTTPS 根地址或 /v1 地址")
	}
	if protocol == MovoProtocol && (u.Host == "movoapi.top" || u.Host == "www.movoapi.top") {
		return u.Scheme + "://" + u.Host, nil
	}
	return "", errors.New("视频协议与上游地址不匹配")
}

func VideoOrigin(raw string) (string, error) { return videoOrigin(raw) }

func videoOrigin(raw string) (string, error) {
	return Origin(raw, MovoProtocol)
}

func isMovo(base string) bool {
	u, err := url.Parse(base)
	return err == nil && (u.Host == "movoapi.top" || u.Host == "www.movoapi.top")
}

// Only the two verified Movo model contracts are offered.
func movoModels(ids map[string]bool) []Model {
	var out []Model
	for _, m := range []Model{
		{ID: "MiniMax-H3", Label: "MiniMax H3", Mode: "second", Rates: map[string]float64{"768p": 0}, CostCNY: map[string]float64{"768p": 0}, MinSeconds: 1, MaxSeconds: 15, MaxImages: 9, MaxVideos: 3, MaxAudios: 3, MaxReferences: 12, MaxPromptChars: 7000, Adaptive: true, Audio: true, Notes: "Movo：按秒；实际分组成本与售价须手动核对。参考视频和音频各自裁剪后合计最多 15 秒。"},
		{ID: "seedance2.5-yx-stable", Label: "Seedance 2.5", Mode: "job", Rates: map[string]float64{"480p": 0, "720p": 0, "1080p": 0}, CostCNY: map[string]float64{"480p": 0, "720p": 0, "1080p": 0}, MinSeconds: 4, MaxSeconds: 30, MaxImages: 30, MaxVideos: 10, MaxAudios: 10, MaxPromptChars: 16000, Adaptive: true, Audio: true, Notes: "Movo：4–30 秒同清晰度按条计费；声音、参考素材与 FACS 不另加费。请核对实际分组成本后填写价格。"},
	} {
		if ids[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

func ValidateMovoModel(m Model) error {
	if m.Disabled {
		return nil
	}
	for r := range m.Rates {
		if m.CostCNY[r] <= 0 {
			return errors.New("视频上游 Movo 实际分组成本尚未确认，不能以零成本开放")
		}
	}
	switch m.ID {
	case "MiniMax-H3":
		if m.Mode != "second" || m.MinSeconds < 1 || m.MaxSeconds > 15 || m.MaxImages > 9 || m.MaxVideos > 3 || m.MaxAudios > 3 || m.MaxReferences != 12 || m.MaxPromptChars > 7000 || len(m.Rates) != 1 || m.Rates["768p"] <= 0 {
			return errors.New("视频上游 Movo H3 仅支持 1–15 秒、768P、按秒，最多 9 图/3 视频/3 音频、总共 12 个素材")
		}
	case "seedance2.5-yx-stable":
		if m.Mode != "job" || m.MinSeconds < 4 || m.MaxSeconds > 30 || m.MaxImages > 30 || m.MaxVideos > 10 || m.MaxAudios > 10 || m.MaxPromptChars > 16000 {
			return errors.New("视频上游 Movo SD 2.5 仅支持 4–30 秒按条计费、30 图/10 视频/10 音频")
		}
		for r := range m.Rates {
			if r != "480p" && r != "720p" && r != "1080p" {
				return errors.New("视频模型 SD 2.5 仅支持 480p、720p、1080p")
			}
		}
	default:
		return errors.New("此 Movo 视频模型尚未适配")
	}
	return nil
}

func movoPayload(model string, in Input, images, videos, audios []string) map[string]any {
	p := map[string]any{"model": model, "prompt": in.Prompt, "seconds": in.Duration, "resolution": in.Resolution, "ratio": in.AspectRatio, "enable_audio": in.GenerateAudio}
	if model == "MiniMax-H3" {
		p = map[string]any{"model": model, "prompt": in.Prompt, "seconds": strconv.Itoa(in.Duration)}
		sizes := map[string]string{"16:9": "1344x768", "9:16": "768x1344", "1:1": "768x768", "4:3": "1024x768", "3:4": "768x1024", "21:9": "1536x672", "adaptive": "auto"}
		p["size"] = sizes[in.AspectRatio]
		media := []map[string]any{}
		for _, item := range []struct {
			kind string
			urls []string
		}{{"image", images}, {"video", videos}, {"audio", audios}} {
			for _, u := range item.urls {
				media = append(media, map[string]any{"type": item.kind + "_url", item.kind + "_url": map[string]string{"url": u}, "role": "reference_" + item.kind})
			}
		}
		metadata := map[string]any{"ratio": in.AspectRatio}
		if len(media) > 0 {
			metadata["content"] = media
			p["mode"] = "reference"
		}
		p["metadata"] = metadata
		return p
	}
	if len(images) > 0 {
		p["reference_images"] = images
	}
	if len(videos) > 0 {
		p["videos"] = videos
	}
	if len(audios) > 0 {
		p["audios"] = audios
	}
	if model == "seedance2.5-yx-stable" {
		p["facs"] = in.FACS == nil || *in.FACS
	}
	return p
}

// Never expose the upstream credential or follow a credential-bearing redirect.
func (w *Worker) movoOutput(ctx context.Context, base, key, id string, out upstreamResponse, task *Task) ([]string, error) {
	for _, raw := range []string{out.Metadata.URL, out.Result.URL, out.URL, out.ContentURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if !u.IsAbs() {
			b, _ := url.Parse(base)
			u = b.ResolveReference(u)
		}
		if isMovo(u.String()) && strings.Contains(u.Path, "/content") {
			continue
		}
		safe, err := secureOutputURL(u.String())
		if err != nil {
			return nil, err
		}
		return []string{safe}, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/videos/"+url.PathEscape(id)+"/content", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := *w.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode >= 300 && r.StatusCode < 400 {
		u, err := r.Location()
		if err != nil {
			return nil, err
		}
		safe, err := secureOutputURL(u.String())
		if err != nil {
			return nil, err
		}
		if isMovo(safe) {
			return nil, errors.New("上游下载地址仍需鉴权，请管理员核对")
		}
		return []string{safe}, nil
	}
	if r.StatusCode == http.StatusOK && task != nil && w.Store != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, 100*1024*1024+1))
		if err != nil {
			return nil, errors.New("上游视频下载未完成，需要核对")
		}
		if len(b) > 100*1024*1024 || http.DetectContentType(b) != "video/mp4" {
			return nil, errors.New("上游视频超过100MB或格式不是MP4，需要核对")
		}
		ch, err := w.Store.Channel(ctx, task.GroupID)
		if err != nil {
			return nil, err
		}
		a, err := w.Store.putAsset(ctx, task.UserID, "asset_result_"+strings.TrimPrefix(task.ID, "vid_"), task.ID+".mp4", "video/mp4", b)
		if err != nil {
			return nil, errors.New("保存上游视频失败，请核对素材库容量后继续查询")
		}
		return []string{strings.TrimRight(ch.PublicBaseURL, "/") + "/api/v1/video-assets/" + a.ID}, nil
	}
	return nil, fmt.Errorf("上游未返回可公开播放的视频地址（下载状态 %d），需要核对", r.StatusCode)
}

// Movo reports validation errors in either error.message or top-level message.
func (o *upstreamResponse) UnmarshalJSON(b []byte) error {
	type plain upstreamResponse
	var v struct {
		*plain
		Message string `json:"message"`
	}
	v.plain = (*plain)(o)
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if o.Error.Message == "" {
		o.Error.Message = v.Message
	}
	return nil
}
