package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *groupQualityRepository) WithScheduleLock(ctx context.Context, fn func(context.Context) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var acquired bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(733002332)`).Scan(&acquired); err != nil {
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
func (r *groupQualityRepository) ClaimDue(ctx context.Context) (*service.QualityScheduledCheck, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(733002331)`); err != nil {
		return nil, err
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_quality_test_runs WHERE status='running' AND deadline_at>NOW())`).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, nil
	}
	job := &service.QualityScheduledCheck{}
	err = tx.QueryRowContext(ctx, `WITH due AS (
 SELECT q.group_id,q.model,c.auto_interval_seconds FROM group_quality_test_schedule q
 JOIN group_quality_test_configs c ON c.group_id=q.group_id JOIN groups g ON g.id=q.group_id
 WHERE q.next_run_at<=NOW() AND c.enabled AND c.auto_enabled AND q.model=ANY(c.models) AND g.deleted_at IS NULL AND g.status='active'
 AND NOT EXISTS(SELECT 1 FROM group_quality_test_runs h WHERE h.group_id=q.group_id AND h.model=q.model AND COALESCE(h.finished_at,h.deadline_at)>NOW()-(c.cooldown_seconds*INTERVAL '1 second'))
 ORDER BY q.next_run_at,q.group_id,q.model LIMIT 1 FOR UPDATE OF q SKIP LOCKED
 ) UPDATE group_quality_test_schedule q SET next_run_at=NOW()+(due.auto_interval_seconds * INTERVAL '1 second'),last_attempt_at=NOW(),last_outcome='claimed'
 FROM due WHERE q.group_id=due.group_id AND q.model=due.model RETURNING q.group_id,q.model`).Scan(&job.GroupID, &job.Model)
	if err == sql.ErrNoRows {
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
func (r *groupQualityRepository) RecordScheduleOutcome(ctx context.Context, job *service.QualityScheduledCheck, outcome string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE group_quality_test_schedule SET last_outcome=$3 WHERE group_id=$1 AND model=$2`, job.GroupID, job.Model, outcome)
	return err
}
