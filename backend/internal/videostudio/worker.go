package videostudio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	Store  *Store
	Client *http.Client
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func NewWorker(s *Store) *Worker {
	return &Worker{Store: s, Client: &http.Client{Timeout: 75 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (w *Worker) Start() {
	w.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		w.cancel = cancel
		w.done = make(chan struct{})
		go func() {
			defer close(w.done)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w.Tick(ctx)
				}
			}
		}()
	})
}
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
}

// Claim with SKIP LOCKED permits multiple app replicas without duplicate submits.
// A submitting task with an expired lease is NOT resubmitted: remote acceptance
// may have happened before the process died.
func (w *Worker) Tick(ctx context.Context) {
	_, e := w.Store.DB.ExecContext(ctx, `UPDATE video_studio_tasks SET status='needs_review',error_message='提交中断，无法确认上游是否已创建任务；请管理员核对',updated_at=now() WHERE NOT settled AND ((status='submitting' AND poll_after<now()) OR (status='pending' AND poll_deadline<now()))`)
	if e != nil {
		slog.Warn("video recovery scan failed", "error", e)
		return
	}
	for i := 0; i < 4; i++ {
		id, e := w.claim(ctx)
		if errors.Is(e, sql.ErrNoRows) {
			return
		}
		if e != nil {
			slog.Warn("video claim failed", "error", e)
			return
		}
		w.process(ctx, id)
	}
}
func (w *Worker) claim(ctx context.Context) (string, error) {
	var id string
	tx, err := w.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	// Short global claim lock makes per-account concurrency exact across replicas.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(75188211)`); err != nil {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `WITH candidate AS (
 SELECT t.id FROM video_studio_tasks t JOIN video_studio_channels ch ON ch.group_id=t.group_id
 WHERE NOT t.settled AND t.poll_after<=now() AND (t.status IN ('queued','in_progress') OR (t.status='pending' AND ch.enabled AND (SELECT count(*) FROM video_studio_tasks a WHERE a.account_id=t.account_id AND NOT a.settled AND a.status IN ('submitting','queued','in_progress','needs_review'))<COALESCE((SELECT min((vc.config->>'max_concurrent')::int) FROM video_studio_channels vc WHERE vc.account_id=t.account_id AND vc.enabled),1)))
 ORDER BY CASE WHEN t.status='pending' THEN 1 ELSE 0 END,t.poll_after FOR UPDATE OF t SKIP LOCKED LIMIT 1)
 UPDATE video_studio_tasks SET status=CASE WHEN status='pending' THEN 'submitting' ELSE status END,poll_after=now()+interval '3 minutes',updated_at=now() WHERE id=(SELECT id FROM candidate) RETURNING id`).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func laneOrigin(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || (u.Host != "laneai.dev" && u.Host != "www.laneai.dev") {
		return "", errors.New("LaneAI 视频账号 Base URL 必须为 https://laneai.dev 或其 /v1 地址")
	}
	return u.Scheme + "://" + u.Host, nil
}

type upstreamResponse struct {
	ID      string   `json:"id"`
	TaskID  string   `json:"task_id"`
	Status  string   `json:"status"`
	Outputs []string `json:"outputs"`
	Result  struct {
		URL string `json:"url"`
	} `json:"result"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (w *Worker) call(ctx context.Context, base, key, method, path string, payload any) (upstreamResponse, int, error) {
	var out upstreamResponse
	var b []byte
	var err error
	if payload != nil {
		b, err = json.Marshal(payload)
		if err != nil {
			return out, 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(b))
	if err != nil {
		return out, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return out, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		return out, resp.StatusCode, err
	}
	if len(body) > 2*1024*1024 {
		return out, resp.StatusCode, errors.New("上游响应过大")
	}
	if err = json.Unmarshal(body, &out); err != nil {
		return out, resp.StatusCode, errors.New("上游响应不是有效 JSON")
	}
	return out, resp.StatusCode, nil
}
func (w *Worker) process(ctx context.Context, id string) {
	t, e := w.Store.Task(ctx, 0, id)
	if e != nil {
		slog.Warn("video load failed", "id", id, "error", e)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 85*time.Second)
	defer cancel()
	if time.Now().After(t.PollDeadline) {
		w.review(id, "任务超过 24 小时未结算，请管理员核对上游")
		return
	}
	raw, key, e := w.Store.Account(ctx, t.AccountID)
	if e != nil {
		w.deferTask(ctx, t, "上游账号暂不可用")
		return
	}
	base, e := laneOrigin(raw)
	if e != nil {
		w.deferTask(ctx, t, e.Error())
		return
	}
	if t.Status == "submitting" {
		ready, err := w.Store.AccountReady(ctx, t.AccountID)
		if err != nil || !ready {
			w.deferTask(ctx, t, "上游账号暂停或不可调度，任务保留排队")
			return
		}
		c, e := w.Store.Channel(ctx, t.GroupID)
		if e != nil {
			w.deferTask(ctx, t, "视频分组配置不可用")
			return
		}
		input := t.Request.Input
		imgs, e := w.Store.AssetURLs(ctx, t.UserID, input.Images, c.PublicBaseURL)
		if e != nil {
			w.fail(ctx, t, e.Error())
			return
		}
		vids, e := w.Store.AssetURLs(ctx, t.UserID, input.Videos, c.PublicBaseURL)
		if e != nil {
			w.fail(ctx, t, e.Error())
			return
		}
		audio, e := w.Store.AssetURLs(ctx, t.UserID, input.Audios, c.PublicBaseURL)
		if e != nil {
			w.fail(ctx, t, e.Error())
			return
		}
		payload := map[string]any{"model": t.Request.Model, "input": map[string]any{"prompt": input.Prompt, "duration": input.Duration, "resolution": strings.ToLower(input.Resolution), "aspect_ratio": input.AspectRatio, "generate_audio": input.GenerateAudio, "images": imgs, "videos": vids, "audios": audio}}
		out, status, e := w.call(ctx, base, key, http.MethodPost, "/v1/task/submit", payload)
		// Only an explicit 4xx rejection (except timeout/conflict) proves no job was accepted.
		if status >= 400 && status < 500 && status != 408 && status != 409 {
			w.fail(ctx, t, "上游拒绝任务："+safeMessage(out.Error.Message, status))
			return
		}
		if e != nil || status < 200 || status >= 300 {
			w.review(id, "上游提交结果不确定，请核对后处理，系统不会自动重发")
			return
		}
		upstreamID := out.ID
		if upstreamID == "" {
			upstreamID = out.TaskID
		}
		if upstreamID == "" || len(upstreamID) > 200 || strings.ContainsAny(upstreamID, "/\\?#") {
			w.review(id, "上游未返回有效任务 ID，请核对")
			return
		}
		// Retry only this local persistence, never the network create.
		persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, e = w.Store.DB.ExecContext(persistCtx, `UPDATE video_studio_tasks SET upstream_id=$2,status='queued',poll_after=now()+interval '15 seconds',updated_at=now(),error_message='' WHERE id=$1 AND NOT settled`, id, upstreamID)
		if e != nil {
			slog.Error("video task accepted but persistence failed", "id", id, "upstream_id", upstreamID, "error", e)
		}
		return
	}
	if time.Now().After(t.PollDeadline) {
		w.review(id, "任务超过 24 小时仍未完成，请管理员核对上游；冻结金额暂不释放")
		return
	}
	out, status, e := w.call(ctx, base, key, http.MethodGet, "/v1/task/"+url.PathEscape(t.UpstreamID), nil)
	if e != nil || status != 200 {
		w.deferTask(ctx, t, "上游查询暂时失败，稍后继续")
		return
	}
	switch out.Status {
	case "completed":
		outputs := out.Outputs
		if len(outputs) == 0 && out.Result.URL != "" {
			outputs = []string{out.Result.URL}
		}
		for _, raw := range outputs {
			u, e := url.Parse(raw)
			if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
				w.review(id, "上游视频地址不合法，请管理员核对")
				return
			}
		}
		if len(outputs) == 0 {
			w.review(id, "上游显示成功但缺少视频地址，请管理员核对")
			return
		}
		if e = w.Store.Finish(ctx, id, "completed", "", outputs); e != nil {
			slog.Error("video settlement failed", "id", id, "error", e)
		}
	case "failed":
		w.fail(ctx, t, safeMessage(out.Error.Message, 0))
	case "queued", "in_progress":
		_, e = w.Store.DB.ExecContext(ctx, `UPDATE video_studio_tasks SET status=$2,error_message='',poll_after=now()+interval '20 seconds',updated_at=now() WHERE id=$1 AND NOT settled`, id, out.Status)
		if e != nil {
			slog.Warn("video status save failed", "id", id, "error", e)
		}
	default:
		w.review(id, "未知上游任务状态，需要核对："+out.Status)
	}
}
func safeMessage(msg string, status int) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Sprintf("任务失败（状态 %d）", status)
	}
	r := []rune(msg)
	if len(r) > 300 {
		msg = string(r[:300])
	}
	return msg
}
func (w *Worker) fail(ctx context.Context, t Task, message string) {
	if e := w.Store.Finish(ctx, t.ID, "failed", message, nil); e != nil {
		slog.Error("video refund failed", "id", t.ID, "error", e)
	}
}
func (w *Worker) review(id, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := w.Store.Review(ctx, id, message); e != nil {
		slog.Error("video review save failed", "id", id, "error", e)
	}
}
func (w *Worker) deferTask(ctx context.Context, t Task, message string) {
	// No HTTP request has been sent in the submitting branch at these call sites.
	status := t.Status
	if status == "submitting" {
		status = "pending"
	}
	_, e := w.Store.DB.ExecContext(ctx, `UPDATE video_studio_tasks SET status=$2,error_message=$3,poll_after=now()+interval '60 seconds',updated_at=now() WHERE id=$1 AND NOT settled`, t.ID, status, message)
	if e != nil {
		slog.Warn("video defer failed", "id", t.ID, "error", e)
	}
}
