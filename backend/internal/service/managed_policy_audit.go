package service

import (
	"context"
	"log/slog"
	"time"
)

type ManagedPolicyEvent struct {
	CreatedAt       time.Time `json:"created_at"`
	RequestID       string    `json:"request_id"`
	GroupID         int64     `json:"group_id"`
	Version         int64     `json:"version"`
	Status          string    `json:"status"`
	TransportStatus string    `json:"transport_status"`
	Protocol        string    `json:"protocol"`
	RuleIDs         []string  `json:"rule_ids"`
	Before          int64     `json:"input_bytes_before"`
	After           int64     `json:"input_bytes_after"`
}

func (s *SettingService) RecordManagedPolicy(ctx context.Context, e ManagedPolicyEvent) {
	if s == nil {
		return
	}
	if repo, ok := s.settingRepo.(interface {
		RecordManagedPolicy(context.Context, ManagedPolicyEvent) error
	}); ok {
		write, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
		defer cancel()
		if err := repo.RecordManagedPolicy(write, e); err != nil {
			slog.Warn("managed policy audit unavailable", "request_id", e.RequestID)
		}
	}
}
func (s *SettingService) ListManagedPolicy(ctx context.Context) ([]ManagedPolicyEvent, error) {
	if repo, ok := s.settingRepo.(interface {
		ListManagedPolicy(context.Context) ([]ManagedPolicyEvent, error)
	}); ok {
		return repo.ListManagedPolicy(ctx)
	}
	return []ManagedPolicyEvent{}, nil
}
