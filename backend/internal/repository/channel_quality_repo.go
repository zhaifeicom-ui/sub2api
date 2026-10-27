package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type channelQualityRepository struct{ db *sql.DB }

func NewChannelQualityRepository(db *sql.DB) service.ChannelQualityRepository {
	return &channelQualityRepository{db: db}
}

const channelQualityConfigSelect = `SELECT c.monitor_id,m.name,m.provider,c.enabled,c.models,c.customer_access,
 c.cooldown_seconds,c.auto_enabled,c.auto_interval_seconds,c.version
 FROM channel_quality_test_configs c JOIN channel_monitors m ON m.id=c.monitor_id`

type channelQualityScanner interface{ Scan(...any) error }

func scanChannelQualityConfig(row channelQualityScanner) (*service.ChannelQualityConfig, error) {
	c := &service.ChannelQualityConfig{AvailableModels: []string{}}
	err := row.Scan(&c.MonitorID, &c.MonitorName, &c.Provider, &c.Enabled, pq.Array(&c.Models), &c.CustomerAccess,
		&c.CooldownSeconds, &c.AutoEnabled, &c.AutoIntervalSeconds, &c.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrChannelQualityNotFound
	}
	return c, err
}

func (r *channelQualityRepository) ListConfigs(ctx context.Context) ([]service.ChannelQualityConfig, error) {
	rows, err := r.db.QueryContext(ctx, channelQualityConfigSelect+` ORDER BY m.provider,m.name,c.monitor_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.ChannelQualityConfig{}
	for rows.Next() {
		c, err := scanChannelQualityConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *channelQualityRepository) GetConfig(ctx context.Context, monitorID int64) (*service.ChannelQualityConfig, error) {
	return scanChannelQualityConfig(r.db.QueryRowContext(ctx, channelQualityConfigSelect+` WHERE c.monitor_id=$1`, monitorID))
}

func (r *channelQualityRepository) SaveConfig(ctx context.Context, c service.ChannelQualityConfig) (*service.ChannelQualityConfig, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO channel_quality_test_configs(monitor_id,enabled,models,customer_access,cooldown_seconds,auto_enabled,auto_interval_seconds)
 VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(monitor_id) DO UPDATE SET enabled=EXCLUDED.enabled,models=EXCLUDED.models,customer_access=EXCLUDED.customer_access,
 cooldown_seconds=EXCLUDED.cooldown_seconds,auto_enabled=EXCLUDED.auto_enabled,auto_interval_seconds=EXCLUDED.auto_interval_seconds,
 version=channel_quality_test_configs.version+1,updated_at=NOW()
 WHERE channel_quality_test_configs.version=$8 AND $8>0`, c.MonitorID, c.Enabled, pq.Array(c.Models), c.CustomerAccess,
		c.CooldownSeconds, c.AutoEnabled, c.AutoIntervalSeconds, c.Version)
	if err != nil {
		return nil, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, service.ErrChannelQualityConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_quality_test_schedule WHERE monitor_id=$1`, c.MonitorID); err != nil {
		return nil, err
	}
	if c.Enabled && c.AutoEnabled {
		if _, err = tx.ExecContext(ctx, `INSERT INTO channel_quality_test_schedule(monitor_id,model,next_run_at)
 SELECT $1,m,NOW()+($3 * INTERVAL '1 second') FROM unnest($2::TEXT[]) m`, c.MonitorID, pq.Array(c.Models), c.AutoIntervalSeconds); err != nil {
			return nil, err
		}
	}
	updated, err := scanChannelQualityConfig(tx.QueryRowContext(ctx, channelQualityConfigSelect+` WHERE c.monitor_id=$1`, c.MonitorID))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *channelQualityRepository) DeleteConfig(ctx context.Context, monitorID int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM channel_quality_test_configs WHERE monitor_id=$1`, monitorID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return service.ErrChannelQualityNotFound
	}
	return nil
}

func channelQualityPermission(c *service.ChannelQualityConfig, model string, admin bool) bool {
	if c == nil || !c.Enabled || (!admin && c.CustomerAccess != "run") {
		return false
	}
	for _, m := range c.Models {
		if m == model {
			return true
		}
	}
	return false
}

func (r *channelQualityRepository) Reserve(ctx context.Context, run *service.ChannelQualityRun, admin bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return err
	}
	c, err := scanChannelQualityConfig(tx.QueryRowContext(ctx, channelQualityConfigSelect+` WHERE c.monitor_id=$1 FOR UPDATE OF c`, run.MonitorID))
	if err != nil {
		return err
	}
	if !channelQualityPermission(c, run.Model, admin) {
		return service.ErrChannelQualityDenied
	}
	if run.Automatic && !c.AutoEnabled {
		return service.ErrChannelQualityDenied
	}
	run.MonitorName = c.MonitorName
	if _, err = tx.ExecContext(ctx, `UPDATE channel_quality_test_runs SET status='failed',finished_at=deadline_at,duration_ms=(EXTRACT(EPOCH FROM (deadline_at-started_at))*1000)::INTEGER,
 error_code='interrupted',error_message='检测超时或服务重启中断，请重新检测' WHERE status='running' AND deadline_at<NOW()`); err != nil {
		return err
	}
	var running bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM channel_quality_test_runs WHERE status='running')`).Scan(&running); err != nil {
		return err
	}
	if running {
		return service.ErrChannelQualityBusy
	}
	var cooling bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM channel_quality_test_runs WHERE monitor_id=$1 AND model=$2
 AND COALESCE(finished_at,started_at)>NOW()-($3 * INTERVAL '1 second'))`, run.MonitorID, run.Model, c.CooldownSeconds).Scan(&cooling); err != nil {
		return err
	}
	if cooling {
		return service.ErrChannelQualityCooldown
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO channel_quality_test_runs(monitor_id,monitor_name,model,api_mode,requested_by,automatic)
 VALUES($1,$2,$3,$4,$5,$6) RETURNING id,started_at,deadline_at`, run.MonitorID, run.MonitorName, run.Model,
		run.APIMode, run.RequestedBy, run.Automatic).Scan(&run.ID, &run.StartedAt, &run.DeadlineAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *channelQualityRepository) Finish(ctx context.Context, run *service.ChannelQualityRun) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE channel_quality_test_runs SET status=$2,finished_at=$3,duration_ms=$4,response_length=$5,
 input_tokens=$6,output_tokens=$7,source=$8,error_code=$9,error_message=$10 WHERE id=$1 AND status='running'`,
		run.ID, run.Status, run.FinishedAt, run.DurationMS, run.ResponseLength, run.InputTokens, run.OutputTokens, run.Source, run.ErrorCode, run.ErrorMessage); err != nil {
		return err
	}
	if run.Automatic {
		if _, err = tx.ExecContext(ctx, `UPDATE channel_quality_test_schedule q SET next_run_at=GREATEST(q.next_run_at,
 $3::TIMESTAMPTZ+(GREATEST(c.auto_interval_seconds,c.cooldown_seconds)*INTERVAL '1 second'))
 FROM channel_quality_test_configs c WHERE q.monitor_id=$1 AND q.model=$2 AND c.monitor_id=q.monitor_id AND c.enabled AND c.auto_enabled`,
			run.MonitorID, run.Model, run.FinishedAt); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_quality_test_runs WHERE monitor_id=$1 AND model=$2 AND status<>'running'
 AND id NOT IN (SELECT id FROM channel_quality_test_runs WHERE monitor_id=$1 AND model=$2 AND status<>'running' ORDER BY id DESC LIMIT 5)`, run.MonitorID, run.Model); err != nil {
		return err
	}
	return tx.Commit()
}

const channelQualityRunSelect = `SELECT id,monitor_id,monitor_name,model,prompt_version,api_mode,requested_by,status,automatic,
 started_at,deadline_at,finished_at,duration_ms,response_length,input_tokens,output_tokens,source,error_code,error_message
 FROM channel_quality_test_runs`

func scanChannelQualityRun(row channelQualityScanner) (*service.ChannelQualityRun, error) {
	r := &service.ChannelQualityRun{}
	err := row.Scan(&r.ID, &r.MonitorID, &r.MonitorName, &r.Model, &r.PromptVersion, &r.APIMode, &r.RequestedBy, &r.Status, &r.Automatic,
		&r.StartedAt, &r.DeadlineAt, &r.FinishedAt, &r.DurationMS, &r.ResponseLength, &r.InputTokens, &r.OutputTokens, &r.Source, &r.ErrorCode, &r.ErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrChannelQualityNotFound
	}
	if err == nil && r.Status == "running" && time.Now().After(r.DeadlineAt) {
		r.Status = "failed"
		r.ErrorCode = "interrupted"
		r.ErrorMessage = "检测超时或服务重启中断，请重新检测"
	}
	return r, err
}

func (r *channelQualityRepository) ListRuns(ctx context.Context, monitorID int64, model string) ([]service.ChannelQualityRun, error) {
	q := `SELECT id,monitor_id,monitor_name,model,prompt_version,api_mode,requested_by,status,automatic,started_at,deadline_at,
 finished_at,duration_ms,response_length,input_tokens,output_tokens,'' AS source,error_code,error_message
 FROM channel_quality_test_runs WHERE monitor_id=$1 AND model=$2 ORDER BY id DESC LIMIT 5`
	rows, err := r.db.QueryContext(ctx, q, monitorID, model)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.ChannelQualityRun{}
	for rows.Next() {
		run, err := scanChannelQualityRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (r *channelQualityRepository) GetRun(ctx context.Context, id int64) (*service.ChannelQualityRun, error) {
	return scanChannelQualityRun(r.db.QueryRowContext(ctx, channelQualityRunSelect+` WHERE id=$1`, id))
}

func (r *channelQualityRepository) WithChannelQualityScheduleLock(ctx context.Context, fn func(context.Context) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var acquired bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(733002333)`).Scan(&acquired); err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	if err = fn(ctx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *channelQualityRepository) ClaimChannelQualityDue(ctx context.Context) (*service.ChannelQualityScheduledCheck, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return nil, err
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM channel_quality_test_runs WHERE status='running' AND deadline_at>NOW())`).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, nil
	}
	job := &service.ChannelQualityScheduledCheck{}
	err = tx.QueryRowContext(ctx, `WITH due AS (
 SELECT q.monitor_id,q.model,c.auto_interval_seconds FROM channel_quality_test_schedule q
 JOIN channel_quality_test_configs c ON c.monitor_id=q.monitor_id
 JOIN channel_monitors m ON m.id=q.monitor_id
 WHERE q.next_run_at<=NOW() AND c.enabled AND c.auto_enabled AND q.model=ANY(c.models) AND m.enabled
 AND NOT EXISTS(SELECT 1 FROM channel_quality_test_runs h WHERE h.monitor_id=q.monitor_id AND h.model=q.model
 AND COALESCE(h.finished_at,h.deadline_at)>NOW()-(c.cooldown_seconds*INTERVAL '1 second'))
 ORDER BY q.next_run_at,q.monitor_id,q.model LIMIT 1 FOR UPDATE OF q SKIP LOCKED
 ) UPDATE channel_quality_test_schedule q SET next_run_at=NOW()+(due.auto_interval_seconds*INTERVAL '1 second'),
 last_attempt_at=NOW(),last_outcome='claimed' FROM due
 WHERE q.monitor_id=due.monitor_id AND q.model=due.model RETURNING q.monitor_id,q.model`).Scan(&job.MonitorID, &job.Model)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *channelQualityRepository) RecordChannelQualityScheduleOutcome(ctx context.Context, job *service.ChannelQualityScheduledCheck, outcome string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE channel_quality_test_schedule SET last_outcome=$3 WHERE monitor_id=$1 AND model=$2`, job.MonitorID, job.Model, outcome)
	return err
}
