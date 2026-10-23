package videostudio

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Store struct {
	DB         *sql.DB
	Invalidate func(context.Context, int64, int64)
}

func (s *Store) Channel(ctx context.Context, groupID int64) (Channel, error) {
	var c Channel
	var b []byte
	err := s.DB.QueryRowContext(ctx, `SELECT group_id,account_id,enabled,config FROM video_studio_channels WHERE group_id=$1`, groupID).Scan(&c.GroupID, &c.AccountID, &c.Enabled, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	var config Channel
	if err = json.Unmarshal(b, &config); err != nil {
		return c, err
	}
	config.GroupID = c.GroupID
	config.AccountID = c.AccountID
	config.Enabled = c.Enabled
	return config, nil
}
func (s *Store) SaveChannel(ctx context.Context, c Channel) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a CROSS JOIN groups g WHERE a.id=$1 AND g.id=$2 AND a.deleted_at IS NULL AND g.deleted_at IS NULL AND a.platform='video' AND a.type='apikey' AND g.platform='video' AND g.subscription_type='standard')`, c.AccountID, c.GroupID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return errors.New("请选择视频 API Key 账号和视频余额分组")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2) ON CONFLICT(account_id,group_id) DO NOTHING`, c.AccountID, c.GroupID); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	_, err = tx.ExecContext(ctx, `INSERT INTO video_studio_channels(group_id,account_id,enabled,config) VALUES($1,$2,$3,$4) ON CONFLICT(group_id) DO UPDATE SET account_id=$2,enabled=$3,config=$4,updated_at=now()`, c.GroupID, c.AccountID, c.Enabled, b)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT group_id FROM video_studio_channels ORDER BY group_id`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	out := []Channel{}
	for _, id := range ids {
		c, e := s.Channel(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}
func (s *Store) KeyGroup(ctx context.Context, uid, kid int64) (int64, error) {
	var gid int64
	err := s.DB.QueryRowContext(ctx, `SELECT k.group_id FROM api_keys k JOIN users u ON u.id=k.user_id JOIN groups g ON g.id=k.group_id WHERE k.id=$1 AND k.user_id=$2 AND k.deleted_at IS NULL AND k.status='active' AND (k.expires_at IS NULL OR k.expires_at>now()) AND u.deleted_at IS NULL AND u.status='active' AND g.deleted_at IS NULL AND g.status='active' AND g.platform='video' AND g.subscription_type='standard'`, kid, uid).Scan(&gid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("请选择属于你的有效视频余额分组密钥")
	}
	return gid, err
}

type scanner interface{ Scan(...any) error }

const taskColumns = `id,user_id,api_key_id,group_id,account_id,COALESCE(project_id,''),shot_id,request,pricing,upstream_id,status,result,error_message,hold_amount,cost,settled,created_at,poll_deadline`

func scanTask(row scanner) (Task, error) {
	var t Task
	var req, price, result []byte
	err := row.Scan(&t.ID, &t.UserID, &t.KeyID, &t.GroupID, &t.AccountID, &t.ProjectID, &t.ShotID, &req, &price, &t.UpstreamID, &t.Status, &result, &t.Error, &t.Hold, &t.Cost, &t.Settled, &t.CreatedAt, &t.PollDeadline)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(req, &t.Request); err != nil {
		return t, err
	}
	if err = json.Unmarshal(price, &t.Pricing); err != nil {
		return t, err
	}
	var r struct {
		Outputs []string `json:"outputs"`
	}
	if err = json.Unmarshal(result, &r); err != nil {
		return t, err
	}
	t.Outputs = r.Outputs
	return t, nil
}
func (s *Store) Task(ctx context.Context, uid int64, id string) (Task, error) {
	return scanTask(s.DB.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE id=$1 AND ($2::bigint=0 OR user_id=$2)`, id, uid))
}
func (s *Store) Tasks(ctx context.Context, uid int64, before string, project string) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE user_id=$1 AND ($3='' OR project_id=$3) AND ($2='' OR (created_at,id)<(SELECT created_at,id FROM video_studio_tasks WHERE id=$2 AND user_id=$1)) ORDER BY created_at DESC,id DESC LIMIT 50`, uid, before, project)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Reserve atomically creates a durable job and freezes both wallet and key budget.
// The same idempotency key can never reserve twice, including concurrent callers.
type Reservation struct {
	Channel     Channel
	Request     Request
	Price       Price
	Idempotency string
}

func (s *Store) ReserveMany(ctx context.Context, uid int64, items []Reservation) ([]Task, error) {
	if len(items) < 1 || len(items) > 40 {
		return nil, errors.New("每批须为 1–40 个镜头")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	out := make([]Task, 0, len(items))
	for _, item := range items {
		t, e := s.reserveTx(ctx, tx, uid, item.Channel, item.Request, item.Price, item.Idempotency)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for _, t := range out {
		s.invalidate(ctx, uid, t.KeyID)
	}
	return out, nil
}
func (s *Store) Reserve(ctx context.Context, uid int64, c Channel, r Request, p Price, idem string) (Task, error) {
	out, err := s.ReserveMany(ctx, uid, []Reservation{{Channel: c, Request: r, Price: p, Idempotency: idem}})
	if err != nil {
		return Task{}, err
	}
	return out[0], nil
}
func (s *Store) reserveTx(ctx context.Context, tx *sql.Tx, uid int64, c Channel, r Request, p Price, idem string) (Task, error) {
	if len(idem) < 8 || len(idem) > 120 {
		return Task{}, errors.New("请提供 8–120 字符的 Idempotency-Key")
	}
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	pb, _ := json.Marshal(p)
	var err error
	// Serialize per user: balance, active jobs and quota reservations share this lock.
	var balance float64
	if err = tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&balance); err != nil {
		return Task{}, err
	}
	old, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE user_id=$1 AND idempotency_key=$2`, uid, idem))
	if err == nil {
		var prev string
		err = tx.QueryRowContext(ctx, `SELECT request_hash FROM video_studio_tasks WHERE id=$1`, old.ID).Scan(&prev)
		if err != nil {
			return Task{}, err
		}
		if hash != prev {
			return Task{}, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM video_studio_tasks WHERE user_id=$1 AND NOT settled`, uid).Scan(&n); err != nil {
		return Task{}, err
	}
	if n >= 40 {
		return Task{}, errors.New("已有 40 个未结算视频任务，请等待或联系管理员核对")
	}
	if r.ProjectID != "" {
		var doc []byte
		if err = tx.QueryRowContext(ctx, `SELECT document FROM video_studio_projects WHERE id=$1 AND user_id=$2 AND NOT archived FOR UPDATE`, r.ProjectID, uid).Scan(&doc); err != nil {
			return Task{}, ErrNotFound
		}
		var d Document
		if json.Unmarshal(doc, &d) != nil {
			return Task{}, errors.New("项目损坏")
		}
		found := false
		for _, shot := range d.Shots {
			if shot.ID == r.ShotID {
				found = true
			}
		}
		if !found {
			return Task{}, errors.New("镜头不属于此项目")
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE api_keys SET video_reserved=video_reserved+$1,quota_used=quota_used+$1 WHERE id=$2 AND user_id=$3 AND group_id=$4 AND status='active' AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>now()) AND (quota=0 OR quota_used+$1<=quota) AND (rate_limit_5h=0 OR (CASE WHEN window_5h_start>now()-interval '5 hours' THEN usage_5h ELSE 0 END)+video_reserved+$1<=rate_limit_5h) AND (rate_limit_1d=0 OR (CASE WHEN window_1d_start>now()-interval '1 day' THEN usage_1d ELSE 0 END)+video_reserved+$1<=rate_limit_1d) AND (rate_limit_7d=0 OR (CASE WHEN window_7d_start>now()-interval '7 days' THEN usage_7d ELSE 0 END)+video_reserved+$1<=rate_limit_7d)`, p.Amount, r.KeyID, uid, c.GroupID)
	if err != nil {
		return Task{}, err
	}
	n64, _ := res.RowsAffected()
	if n64 != 1 {
		return Task{}, ErrFunds
	}
	res, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance-$1,frozen_balance=frozen_balance+$1,updated_at=now() WHERE id=$2 AND balance>=$1`, p.Amount, uid)
	if err != nil {
		return Task{}, err
	}
	n64, _ = res.RowsAffected()
	if n64 != 1 {
		return Task{}, ErrFunds
	}
	id := newID("vid_")
	_, err = tx.ExecContext(ctx, `INSERT INTO video_studio_tasks(id,user_id,api_key_id,group_id,account_id,project_id,shot_id,idempotency_key,request_hash,request,pricing,hold_amount) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12)`, id, uid, r.KeyID, c.GroupID, c.AccountID, r.ProjectID, r.ShotID, idem, hash, b, pb, p.Amount)
	if err != nil {
		return Task{}, err
	}
	return scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE id=$1`, id))
}

func (s *Store) invalidate(ctx context.Context, uid, kid int64) {
	if s.Invalidate != nil {
		s.Invalidate(ctx, uid, kid)
	}
}

// Finish locks the task before applying funds. It is safe for polling, recovery,
// and admin reconciliation to race: terminal settlement occurs exactly once.
func (s *Store) CancelPending(ctx context.Context, uid int64, id string) error {
	return s.finish(ctx, id, "failed", "用户取消了尚未提交的任务", nil, "pending", uid)
}
func (s *Store) Finish(ctx context.Context, id, status, message string, outputs []string) error {
	return s.finish(ctx, id, status, message, outputs, "", 0)
}
func (s *Store) RefundReview(ctx context.Context, id, message string) error {
	return s.finish(ctx, id, "failed", message, nil, "needs_review", 0)
}
func (s *Store) finish(ctx context.Context, id, status, message string, outputs []string, requiredStatus string, uid int64) error {
	if status != "completed" && status != "failed" {
		return errors.New("无效结算状态")
	}
	if status == "completed" && len(outputs) == 0 {
		return errors.New("上游返回完成但没有视频")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	t, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if uid != 0 && t.UserID != uid {
		return ErrNotFound
	}
	if requiredStatus != "" && t.Status != requiredStatus {
		return ErrConflict
	}
	if t.Settled {
		return nil
	}
	cost := t.Hold
	if status == "failed" {
		cost = 0
	}
	refund := t.Hold - cost
	res, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$1,frozen_balance=frozen_balance-$2,updated_at=now() WHERE id=$3 AND frozen_balance>=$2`, refund, t.Hold, t.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("冻结余额异常，需管理员核对")
	}
	_, err = tx.ExecContext(ctx, `UPDATE api_keys SET video_reserved=GREATEST(0,video_reserved-$1),quota_used=GREATEST(0,quota_used+$2-$1),usage_5h=(CASE WHEN window_5h_start>now()-interval '5 hours' THEN usage_5h ELSE 0 END)+$2,usage_1d=(CASE WHEN window_1d_start>now()-interval '1 day' THEN usage_1d ELSE 0 END)+$2,usage_7d=(CASE WHEN window_7d_start>now()-interval '7 days' THEN usage_7d ELSE 0 END)+$2,window_5h_start=CASE WHEN window_5h_start>now()-interval '5 hours' THEN window_5h_start ELSE now() END,window_1d_start=CASE WHEN window_1d_start>now()-interval '1 day' THEN window_1d_start ELSE now() END,window_7d_start=CASE WHEN window_7d_start>now()-interval '7 days' THEN window_7d_start ELSE now() END WHERE id=$3`, t.Hold, cost, t.KeyID)
	if err != nil {
		return err
	}
	result, _ := json.Marshal(map[string]any{"outputs": outputs})
	_, err = tx.ExecContext(ctx, `UPDATE video_studio_tasks SET status=$2,result=$3,error_message=$4,cost=$5,settled=true,updated_at=now() WHERE id=$1`, id, status, result, message, cost)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.invalidate(ctx, t.UserID, t.KeyID)
	return nil
}
func (s *Store) Projects(ctx context.Context, uid int64) ([]Project, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,revision,document,updated_at FROM video_studio_projects WHERE user_id=$1 AND NOT archived ORDER BY updated_at DESC LIMIT 100`, uid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Project{}
	for rows.Next() {
		var p Project
		var b []byte
		if err = rows.Scan(&p.ID, &p.Revision, &b, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &p.Document); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) SaveProject(ctx context.Context, uid int64, p Project) (Project, error) {
	if err := validateDocument(p.Document); err != nil {
		return p, err
	}
	ids := append([]string{}, p.Document.CharacterImages...)
	for _, shot := range p.Document.Shots {
		ids = append(ids, shot.Images...)
		ids = append(ids, shot.Videos...)
		ids = append(ids, shot.Audios...)
		if shot.FirstFrame != "" {
			ids = append(ids, shot.FirstFrame)
		}
	}
	if _, err := s.AssetURLs(ctx, uid, ids, ""); err != nil {
		return p, err
	}
	b, _ := json.Marshal(p.Document)
	var err error
	if p.ID == "" {
		p.ID = newID("proj_")
		p.Revision = 1
		err = s.DB.QueryRowContext(ctx, `INSERT INTO video_studio_projects(id,user_id,document) VALUES($1,$2,$3) RETURNING updated_at`, p.ID, uid, b).Scan(&p.UpdatedAt)
	} else {
		err = s.DB.QueryRowContext(ctx, `UPDATE video_studio_projects SET document=$1,revision=revision+1,updated_at=now() WHERE id=$2 AND user_id=$3 AND NOT archived AND revision=$4 RETURNING revision,updated_at`, b, p.ID, uid, p.Revision).Scan(&p.Revision, &p.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrConflict
		}
	}
	return p, err
}
func (s *Store) DeleteProject(ctx context.Context, uid int64, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE video_studio_projects SET archived=true,updated_at=now() WHERE id=$1 AND user_id=$2`, id, uid)
	return err
}
func (s *Store) Assets(ctx context.Context, uid int64) ([]Asset, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT id,name,mime,octet_length(content) FROM video_studio_assets WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, uid)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []Asset{}
	for rows.Next() {
		var a Asset
		if e = rows.Scan(&a.ID, &a.Name, &a.MIME, &a.Bytes); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) PutAsset(ctx context.Context, uid int64, name, mime string, b []byte) (Asset, error) {
	return s.putAsset(ctx, uid, newID("asset_"), name, mime, b)
}
func (s *Store) putAsset(ctx context.Context, uid int64, id, name, mime string, b []byte) (Asset, error) {
	a := Asset{ID: id, Name: name, MIME: mime, Bytes: len(b)}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return a, e
	}
	defer func() { _ = tx.Rollback() }()
	var u int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&u); e != nil {
		return a, e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM video_studio_assets WHERE id=$1 AND user_id=$2)`, id, uid).Scan(&exists); e != nil {
		return a, e
	}
	if exists {
		return a, tx.Commit()
	}
	var total int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(octet_length(content)),0) FROM video_studio_assets WHERE user_id=$1`, uid).Scan(&total); e != nil {
		return a, e
	}
	if total+int64(len(b)) > 200*1024*1024 {
		return a, errors.New("素材库超过 200MB，请删除不用的素材")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO video_studio_assets(id,user_id,name,mime,content) VALUES($1,$2,$3,$4,$5)`, a.ID, uid, name, mime, b)
	if e != nil {
		return a, e
	}
	return a, tx.Commit()
}
func (s *Store) AssetURLs(ctx context.Context, uid int64, ids []string, base string) ([]string, error) {
	return s.AssetURLsForType(ctx, uid, ids, base, "")
}
func (s *Store) AssetURLsForType(ctx context.Context, uid int64, ids []string, base, kind string) ([]string, error) {
	out := []string{}
	for _, id := range ids {
		var owner int64
		var mime string
		e := s.DB.QueryRowContext(ctx, `SELECT user_id,mime FROM video_studio_assets WHERE id=$1`, id).Scan(&owner, &mime)
		if e != nil || owner != uid {
			return nil, errors.New("参考素材不存在或不属于你")
		}
		if kind != "" && !strings.HasPrefix(mime, kind+"/") {
			return nil, errors.New("参考素材类型不符，请将图片、视频、音频放入对应字段")
		}
		out = append(out, strings.TrimRight(base, "/")+"/api/v1/video-assets/"+id)
	}
	return out, nil
}
func (s *Store) AssetContent(ctx context.Context, id string) (string, []byte, error) {
	var mime string
	var b []byte
	e := s.DB.QueryRowContext(ctx, `SELECT mime,content FROM video_studio_assets WHERE id=$1`, id).Scan(&mime, &b)
	return mime, b, e
}
func (s *Store) DeleteAsset(ctx context.Context, uid int64, id string) error {
	var used bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM video_studio_tasks WHERE user_id=$1 AND NOT settled AND request::text LIKE '%'||$2||'%') OR EXISTS(SELECT 1 FROM video_studio_projects WHERE user_id=$1 AND NOT archived AND document::text LIKE '%'||$2||'%')`, uid, id).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return errors.New("素材仍被项目或进行中的任务使用，请先移除引用")
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM video_studio_assets WHERE id=$1 AND user_id=$2`, id, uid)
	return err
}
func (s *Store) Account(ctx context.Context, id int64) (string, string, error) {
	var creds []byte
	err := s.DB.QueryRowContext(ctx, `SELECT credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL AND type='apikey' AND platform='video'`, id).Scan(&creds)
	if err != nil {
		return "", "", err
	}
	var c struct {
		Base     string `json:"base_url"`
		Key      string `json:"api_key"`
		Protocol string `json:"api_protocol"`
	}
	if err = json.Unmarshal(creds, &c); err != nil {
		return "", "", err
	}
	if _, err := Origin(c.Base, c.Protocol); err != nil {
		return "", "", err
	}
	if c.Key == "" {
		return "", "", fmt.Errorf("上游账号没有 API Key")
	}
	return c.Base, c.Key, nil
}

func (s *Store) AccountReady(ctx context.Context, id int64) (bool, error) {
	var ready bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND deleted_at IS NULL AND status='active' AND schedulable=true AND (expires_at IS NULL OR expires_at>now()))`, id).Scan(&ready)
	return ready, err
}

func (s *Store) Review(ctx context.Context, id, message string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE video_studio_tasks SET status='needs_review',error_message=$2,updated_at=now(),poll_after=now()+interval '5 minutes' WHERE id=$1 AND NOT settled`, id, message)
	return e
}
func (s *Store) Attach(ctx context.Context, id, upstreamID string) error {
	if upstreamID == "" || len(upstreamID) > 200 || strings.ContainsAny(upstreamID, "/\\?#") {
		return errors.New("无效上游任务 ID")
	}
	result, e := s.DB.ExecContext(ctx, `UPDATE video_studio_tasks SET upstream_id=$2,status='queued',error_message='',poll_after=now(),poll_deadline=now()+interval '24 hours',updated_at=now() WHERE id=$1 AND NOT settled AND status='needs_review'`, id, upstreamID)
	if e != nil {
		return e
	}
	affected, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Store) ReviewTasks(ctx context.Context) ([]Task, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT `+taskColumns+` FROM video_studio_tasks WHERE status='needs_review' ORDER BY created_at LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	out := []Task{}
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
