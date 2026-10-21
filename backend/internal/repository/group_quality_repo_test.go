package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func qualityConfigMockRows(access string, enabled bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "platform", "enabled", "models", "customer_access", "api_mode", "account_id", "cooldown_seconds", "version", "auto_enabled", "auto_interval_seconds"}).AddRow(1, "group", "openai", enabled, `{gpt-6-astra,gpt-6-sol}`, access, "responses", nil, 600, 1, false, 600)
}
func TestQualityReservationRejectsRevokedCustomerAccess(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT g.id,g.name,g.platform").WithArgs(int64(1)).WillReturnRows(qualityConfigMockRows("view", true))
	m.ExpectRollback()
	run := &service.GroupQualityRun{GroupID: 1, Model: "gpt-6-sol", APIMode: "responses"}
	require.ErrorIs(t, r.Reserve(context.Background(), run, false), service.ErrQualityDenied)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestQualityReservationBusyAndCooldown(t *testing.T) {
	for _, busy := range []bool{true, false} {
		db, m, err := sqlmock.New()
		require.NoError(t, err)
		r := &groupQualityRepository{db: db}
		m.ExpectBegin()
		m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
		m.ExpectQuery("SELECT g.id,g.name,g.platform").WithArgs(int64(1)).WillReturnRows(qualityConfigMockRows("run", true))
		m.ExpectExec("UPDATE group_quality_test_runs SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 0))
		m.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(busy))
		want := service.ErrQualityBusy
		if !busy {
			m.ExpectQuery("SELECT EXISTS").WithArgs(int64(1), "gpt-6-sol", 600).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
			want = service.ErrQualityCooldown
		}
		m.ExpectRollback()
		require.ErrorIs(t, r.Reserve(context.Background(), &service.GroupQualityRun{GroupID: 1, Model: "gpt-6-sol", APIMode: "responses"}, true), want)
		require.NoError(t, m.ExpectationsWereMet())
		_ = db.Close()
	}
}
func TestQualityPermissionModelsAndCustomerModes(t *testing.T) {
	c := service.GroupQualityConfig{Enabled: true, Models: []string{"gpt-6-sol"}, CustomerAccess: "view"}
	require.False(t, qualityPermission(c, "gpt-6-sol", false))
	require.True(t, qualityPermission(c, "gpt-6-sol", true))
	require.False(t, qualityPermission(c, "gpt-6-astra", true))
	c.Enabled = false
	require.False(t, qualityPermission(c, "gpt-6-sol", true))
}

func TestQualityAutomaticReservationRechecksDisabledSchedule(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT g.id,g.name,g.platform").WithArgs(int64(1)).WillReturnRows(qualityConfigMockRows("hidden", true))
	m.ExpectRollback()
	err = r.Reserve(context.Background(), &service.GroupQualityRun{GroupID: 1, Model: "gpt-6-sol", APIMode: "responses", Automatic: true}, true)
	require.ErrorIs(t, err, service.ErrQualityDenied)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestQualityReservationSuccess(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	r := &groupQualityRepository{db: db}
	now := time.Now()
	account, user := int64(19), int64(42)
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT g.id,g.name,g.platform").WithArgs(int64(1)).WillReturnRows(qualityConfigMockRows("run", true))
	m.ExpectExec("finished_at=deadline_at").WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	m.ExpectQuery("SELECT EXISTS").WithArgs(int64(1), "gpt-6-sol", 600).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	m.ExpectQuery("INSERT INTO group_quality_test_runs").WithArgs(int64(1), "gpt-6-sol", account, "mapped-sol", "responses", user, false).WillReturnRows(sqlmock.NewRows([]string{"id", "started_at", "deadline_at"}).AddRow(8, now, now.Add(6*time.Minute)))
	m.ExpectCommit()
	run := &service.GroupQualityRun{GroupID: 1, Model: "gpt-6-sol", AccountID: &account, RequestedBy: &user, UpstreamModel: "mapped-sol", APIMode: "responses"}
	require.NoError(t, r.Reserve(context.Background(), run, true))
	require.EqualValues(t, 8, run.ID)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestQualityFinishBoundsRetention(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectExec("UPDATE group_quality_test_runs SET status").WithArgs(int64(8), "completed", now, 1000, 11, int64(12), int64(15), "<svg></svg>", "", "").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM group_quality_test_runs").WithArgs(int64(1), "gpt-6-sol").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	require.NoError(t, r.Finish(context.Background(), &service.GroupQualityRun{ID: 8, GroupID: 1, Model: "gpt-6-sol", Status: "completed", FinishedAt: &now, DurationMS: 1000, ResponseLength: 11, InputTokens: 12, OutputTokens: 15, Source: "<svg></svg>"}))
	require.NoError(t, m.ExpectationsWereMet())
}

func TestQualityExpiredHistoryDecodesWithoutSource(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now()
	r := &groupQualityRepository{db: db}
	cols := []string{"id", "group_id", "model", "account_id", "upstream_model", "prompt_version", "api_mode", "requested_by", "status", "started_at", "deadline_at", "finished_at", "duration_ms", "response_length", "input_tokens", "output_tokens", "source", "error_code", "error_message", "automatic"}
	m.ExpectQuery("SELECT id,group_id,model").WithArgs(int64(1), "gpt-6-sol").WillReturnRows(sqlmock.NewRows(cols).AddRow(8, 1, "gpt-6-sol", 19, "alias", "pelican-animation-v1", "responses", 42, "running", now.Add(-time.Hour), now.Add(-time.Minute), nil, 0, 0, 0, 0, "", "", "", false))
	runs, err := r.ListRuns(context.Background(), 1, "gpt-6-sol")
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "failed", runs[0].Status)
	require.Equal(t, "interrupted", runs[0].ErrorCode)
	require.Empty(t, runs[0].Source)
	require.NoError(t, m.ExpectationsWereMet())
}
