package videostudio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Discover reads only catalog endpoints. No generation or paid requests occur.
// Ambiguous pricing fails closed instead of importing generic token/flat fields.
func Discover(ctx context.Context, raw, key string) ([]Model, error) {
	return discover(ctx, raw, key, &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }})
}
func discover(ctx context.Context, raw, key string, client *http.Client) ([]Model, error) {
	base, err := videoOrigin(raw)
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
		defer func() { _ = resp.Body.Close() }()
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
	models := movoModels(ids)
	if len(models) == 0 {
		return nil, errors.New("此密钥没有已适配的视频模型")
	}
	return models, nil
}
