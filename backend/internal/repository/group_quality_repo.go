package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type groupQualityRepository struct{ db *sql.DB }

func NewGroupQualityRepository(db *sql.DB) service.GroupQualityRepository {
	return &groupQualityRepository{db}
}

const qualityConfigSelect = `SELECT g.id,g.name,g.platform,COALESCE(c.enabled,FALSE),
 COALESCE(c.models,ARRAY['gpt-6-astra','gpt-6-sol']::TEXT[]),COALESCE(c.customer_access,'hidden'),
 COALESCE(c.api_mode,'responses'),c.account_id,COALESCE(c.cooldown_seconds,600),COALESCE(c.version,0),COALESCE(c.auto_enabled,FALSE),COALESCE(c.auto_interval_seconds,600)
 FROM groups g LEFT JOIN group_quality_test_configs c ON c.group_id=g.id WHERE g.deleted_at IS NULL`

type qualityScanner interface{ Scan(...any) error }

func scanQualityConfig(row qualityScanner) (*service.GroupQualityConfig, error) {
	c := &service.GroupQualityConfig{AvailableModels: []string{}}
	err := row.Scan(&c.GroupID, &c.GroupName, &c.Platform, &c.Enabled, pq.Array(&c.Models), &c.CustomerAccess, &c.APIMode, &c.AccountID, &c.CooldownSeconds, &c.Version, &c.AutoEnabled, &c.AutoIntervalSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGroupNotFound
	}
	return c, err
}
func (r *groupQualityRepository) ListConfigs(ctx context.Context) ([]service.GroupQualityConfig, error) {
	rows, err := r.db.QueryContext(ctx, qualityConfigSelect+` AND g.status='active' ORDER BY g.sort_order,g.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.GroupQualityConfig{}
	for rows.Next() {
		c, err := scanQualityConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
func (r *groupQualityRepository) GetConfig(ctx context.Context, id int64) (*service.GroupQualityConfig, error) {
	return scanQualityConfig(r.db.QueryRowContext(ctx, qualityConfigSelect+` AND g.id=$1`, id))
}
func (r *groupQualityRepository) SaveConfig(ctx context.Context, c service.GroupQualityConfig) (*service.GroupQualityConfig, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO group_quality_test_configs(group_id,enabled,models,customer_access,api_mode,account_id,cooldown_seconds,auto_enabled,auto_interval_seconds)
 SELECT $1,$2,$3,$4,$5,$6,$7,$9,$10 WHERE $8=0 OR EXISTS(SELECT 1 FROM group_quality_test_configs WHERE group_id=$1)
 ON CONFLICT(group_id) DO UPDATE SET enabled=EXCLUDED.enabled,models=EXCLUDED.models,customer_access=EXCLUDED.customer_access,
 api_mode=EXCLUDED.api_mode,account_id=EXCLUDED.account_id,cooldown_seconds=EXCLUDED.cooldown_seconds,auto_enabled=EXCLUDED.auto_enabled,auto_interval_seconds=EXCLUDED.auto_interval_seconds,version=group_quality_test_configs.version+1,updated_at=NOW()
 WHERE group_quality_test_configs.version=$8`, c.GroupID, c.Enabled, pq.Array(c.Models), c.CustomerAccess, c.APIMode, c.AccountID, c.CooldownSeconds, c.Version, c.AutoEnabled, c.AutoIntervalSeconds)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, service.ErrQualityConflict
	}
	// Saving postpones future automatic checks by the selected interval; never calls a model immediately.
	if _, err = tx.ExecContext(ctx, `DELETE FROM group_quality_test_schedule WHERE group_id=$1`, c.GroupID); err != nil {
		return nil, err
	}
	if c.Enabled && c.AutoEnabled {
		if _, err = tx.ExecContext(ctx, `INSERT INTO group_quality_test_schedule(group_id,model,next_run_at) SELECT $1,m,NOW()+($3 * INTERVAL '1 second') FROM unnest($2::TEXT[]) m`, c.GroupID, pq.Array(c.Models), c.AutoIntervalSeconds); err != nil {
			return nil, err
		}
	}
	updated, err := scanQualityConfig(tx.QueryRowContext(ctx, qualityConfigSelect+` AND g.id=$1`, c.GroupID))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func qualityPermission(c service.GroupQualityConfig, model string, admin bool) bool {
	if !c.Enabled || (!admin && c.CustomerAccess != "run") {
		return false
	}
	for _, m := range c.Models {
		if m == model {
			return true
		}
	}
	return false
}

func (r *groupQualityRepository) Reserve(ctx context.Context, run *service.GroupQualityRun, admin bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serializes reservations only (milliseconds), NOT the upstream inference.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return err
	}
	c, err := scanQualityConfig(tx.QueryRowContext(ctx, qualityConfigSelect+` AND g.id=$1 FOR UPDATE OF g`, run.GroupID))
	if err != nil {
		return err
	}
	// Recheck under the reservation transaction to close config-revocation races.
	if !qualityPermission(*c, run.Model, admin) {
		return service.ErrQualityDenied
	}
	if run.Automatic && !c.AutoEnabled {
		return service.ErrQualityDenied
	}
	if c.APIMode != run.APIMode || (c.AccountID != nil && (run.AccountID == nil || *c.AccountID != *run.AccountID)) {
		return service.ErrQualityConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE group_quality_test_runs SET status='failed',finished_at=deadline_at,duration_ms=360000,error_code='interrupted',error_message='检测超时或服务重启中断，请重新检测' WHERE status='running' AND deadline_at<NOW()`); err != nil {
		return err
	}
	var running bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_quality_test_runs WHERE status='running')`).Scan(&running); err != nil {
		return err
	}
	if running {
		return service.ErrQualityBusy
	}
	var cooling bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_quality_test_runs WHERE group_id=$1 AND model=$2 AND COALESCE(finished_at,started_at)>NOW()-($3 * INTERVAL '1 second'))`, run.GroupID, run.Model, c.CooldownSeconds).Scan(&cooling); err != nil {
		return err
	}
	if cooling {
		return service.ErrQualityCooldown
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO group_quality_test_runs(group_id,model,account_id,upstream_model,api_mode,requested_by,automatic) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,started_at,deadline_at`, run.GroupID, run.Model, run.AccountID, run.UpstreamModel, run.APIMode, run.RequestedBy, run.Automatic).Scan(&run.ID, &run.StartedAt, &run.DeadlineAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *groupQualityRepository) Finish(ctx context.Context, run *service.GroupQualityRun) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE group_quality_test_runs SET status=$2,finished_at=$3,duration_ms=$4,response_length=$5,input_tokens=$6,output_tokens=$7,source=$8,error_code=$9,error_message=$10 WHERE id=$1 AND status='running'`, run.ID, run.Status, run.FinishedAt, run.DurationMS, run.ResponseLength, run.InputTokens, run.OutputTokens, run.Source, run.ErrorCode, run.ErrorMessage); err != nil {
		return err
	}
	if run.Automatic {
		if _, err = tx.ExecContext(ctx, `UPDATE group_quality_test_schedule q SET next_run_at=GREATEST(q.next_run_at,$3::TIMESTAMPTZ+(GREATEST(c.auto_interval_seconds,c.cooldown_seconds)*INTERVAL '1 second')) FROM group_quality_test_configs c WHERE q.group_id=$1 AND q.model=$2 AND c.group_id=q.group_id AND c.enabled AND c.auto_enabled`, run.GroupID, run.Model, run.FinishedAt); err != nil {
			return err
		}
	}
	// Bound storage to five completed/failed artifacts per group/model.
	if _, err = tx.ExecContext(ctx, `DELETE FROM group_quality_test_runs WHERE group_id=$1 AND model=$2 AND status<>'running' AND id NOT IN (SELECT id FROM group_quality_test_runs WHERE group_id=$1 AND model=$2 AND status<>'running' ORDER BY id DESC LIMIT 5)`, run.GroupID, run.Model); err != nil {
		return err
	}
	return tx.Commit()
}

const qualityRunSelect = `SELECT id,group_id,model,account_id,upstream_model,prompt_version,api_mode,requested_by,status,started_at,deadline_at,finished_at,duration_ms,response_length,input_tokens,output_tokens,source,error_code,error_message,automatic FROM group_quality_test_runs`

func scanQualityRun(row qualityScanner) (*service.GroupQualityRun, error) {
	r := &service.GroupQualityRun{}
	err := row.Scan(&r.ID, &r.GroupID, &r.Model, &r.AccountID, &r.UpstreamModel, &r.PromptVersion, &r.APIMode, &r.RequestedBy, &r.Status, &r.StartedAt, &r.DeadlineAt, &r.FinishedAt, &r.DurationMS, &r.ResponseLength, &r.InputTokens, &r.OutputTokens, &r.Source, &r.ErrorCode, &r.ErrorMessage, &r.Automatic)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrGroupNotFound
	}
	// Read-time expiry means a restart never leaves a perpetual spinner.
	if err == nil && r.Status == "running" && time.Now().After(r.DeadlineAt) {
		r.Status = "failed"
		r.ErrorCode = "interrupted"
		r.ErrorMessage = "检测超时或服务重启中断，请重新检测"
	}
	return r, err
}
func (r *groupQualityRepository) ListRuns(ctx context.Context, id int64, model string) ([]service.GroupQualityRun, error) {
	// Do not retrieve large source blobs for history polling.
	q := `SELECT id,group_id,model,account_id,upstream_model,prompt_version,api_mode,requested_by,status,started_at,deadline_at,finished_at,duration_ms,response_length,input_tokens,output_tokens,'' AS source,error_code,error_message,automatic FROM group_quality_test_runs WHERE group_id=$1 AND model=$2 ORDER BY id DESC LIMIT 5`
	rows, err := r.db.QueryContext(ctx, q, id, model)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.GroupQualityRun{}
	for rows.Next() {
		run, err := scanQualityRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}
func (r *groupQualityRepository) GetRun(ctx context.Context, id int64) (*service.GroupQualityRun, error) {
	return scanQualityRun(r.db.QueryRowContext(ctx, qualityRunSelect+` WHERE id=$1`, id))
}
