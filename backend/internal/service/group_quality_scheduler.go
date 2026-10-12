package service

import (
	"context"
	"log/slog"
	"time"
)

type QualityScheduledCheck struct {
	GroupID int64
	Model   string
}
type GroupQualityScheduleRepository interface {
	WithScheduleLock(context.Context, func(context.Context) error) error
	ClaimDue(context.Context) (*QualityScheduledCheck, error)
	RecordScheduleOutcome(context.Context, *QualityScheduledCheck, string) error
}

// No model is called until an administrator explicitly enables a group schedule.
func (s *GroupQualityService) StartAutomatic() {
	s.autoOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.autoCancel = cancel
		s.autoWG.Add(1)
		go func() {
			defer s.autoWG.Done()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			s.automaticTick(ctx)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.automaticTick(ctx)
				}
			}
		}()
	})
}
func (s *GroupQualityService) StopAutomatic() {
	if s.autoCancel != nil {
		s.autoCancel()
	}
	s.autoWG.Wait()
}
func (s *GroupQualityService) automaticTick(parent context.Context) {
	repo, ok := s.repo.(GroupQualityScheduleRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	err := repo.WithScheduleLock(ctx, func(ctx context.Context) error {
		job, err := repo.ClaimDue(ctx)
		if err != nil || job == nil {
			return err
		}
		// One due model per tick; the global running-job index serializes inference.
		// Due dates advance before submission, so an error never causes rapid paid retries.
		_, err = s.Start(ctx, job.GroupID, 0, job.Model, true)
		outcome := "submitted"
		if err != nil {
			outcome = "skipped"
		}
		if writeErr := repo.RecordScheduleOutcome(ctx, job, outcome); writeErr != nil {
			return writeErr
		}
		if err != nil {
			slog.Warn("scheduled quality test skipped", "group_id", job.GroupID, "model", job.Model)
		}
		return nil
	})
	if err != nil && parent.Err() == nil {
		slog.Warn("quality scheduler check failed")
	}
}
