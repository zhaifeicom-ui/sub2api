package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQualityScheduleBusyDoesNotClaimOrCallModels(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(true))
	m.ExpectRollback()
	job, err := r.ClaimDue(context.Background())
	require.NoError(t, err)
	require.Nil(t, job)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestQualityScheduleClaimAdvancesFromNowNotOldDueDate(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(false))
	m.ExpectQuery(`(?s)WITH due AS.*c.auto_enabled.*next_run_at=NOW\(\)`).WillReturnRows(sqlmock.NewRows([]string{"group_id", "model"}).AddRow(1, "gpt-6-sol"))
	m.ExpectCommit()
	job, err := r.ClaimDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, &service.QualityScheduledCheck{GroupID: 1, Model: "gpt-6-sol"}, job)
	require.NoError(t, m.ExpectationsWereMet())
}
func TestQualityScheduleLeaderLockSkipsOtherReplicas(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &groupQualityRepository{db: db}
	m.ExpectBegin()
	m.ExpectQuery("SELECT pg_try_advisory_xact_lock").WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(false))
	m.ExpectRollback()
	called := false
	require.NoError(t, r.WithScheduleLock(context.Background(), func(context.Context) error { called = true; return nil }))
	require.False(t, called)
	require.NoError(t, m.ExpectationsWereMet())
}
