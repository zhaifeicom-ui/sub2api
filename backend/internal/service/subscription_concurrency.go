package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type SubscriptionConcurrencyCache interface {
	AcquireSubscriptionSlot(context.Context, int64, int, string) (bool, error)
	ReleaseSubscriptionSlot(context.Context, int64, string) error
}

type SubscriptionConcurrencyRepository interface {
	UpdateConcurrencyLimit(context.Context, int64, int) error
}

type subscriptionConcurrencyContextKey struct{}
type subscriptionConcurrencyPolicy struct {
	userID, subscriptionID int64
	limit                  int
}

// HasSubscriptionConcurrencyLimit is used by persistent transports that cannot
// safely release a request-scoped reservation at their handshake boundary.
func HasSubscriptionConcurrencyLimit(ctx context.Context) bool {
	_, ok := ctx.Value(subscriptionConcurrencyContextKey{}).(subscriptionConcurrencyPolicy)
	return ok
}

// Authentication supplies the active subscription. Never accept scope/limits
// from client headers. A subscription row uniquely identifies user + group.
func WithSubscriptionConcurrency(ctx context.Context, sub *UserSubscription, group *Group) context.Context {
	if sub == nil || group == nil || !group.IsSubscriptionType() || sub.ID <= 0 || sub.GroupID != group.ID {
		return ctx
	}
	limit := sub.ConcurrencyLimit
	if limit == 0 {
		limit = group.SubscriptionConcurrency
	}
	if limit <= 0 {
		return ctx
	}
	return context.WithValue(ctx, subscriptionConcurrencyContextKey{}, subscriptionConcurrencyPolicy{sub.UserID, sub.ID, limit})
}

func (s *ConcurrencyService) acquireSubscriptionAwareUserSlot(ctx context.Context, userID int64, maxConcurrency int) (*AcquireResult, error) {
	policy, ok := ctx.Value(subscriptionConcurrencyContextKey{}).(subscriptionConcurrencyPolicy)
	if !ok {
		return s.acquireUnscopedUserSlot(ctx, userID, maxConcurrency)
	}
	if policy.userID != userID {
		return nil, fmt.Errorf("subscription concurrency user mismatch")
	}
	cache, ok := s.cache.(SubscriptionConcurrencyCache)
	if !ok {
		return nil, fmt.Errorf("subscription concurrency cache unavailable")
	}
	requestID := generateRequestID()
	acquired, err := cache.AcquireSubscriptionSlot(ctx, policy.subscriptionID, policy.limit, requestID)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return &AcquireResult{Acquired: false}, nil
	}
	releaseSubscription := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cache.ReleaseSubscriptionSlot(releaseCtx, policy.subscriptionID, requestID); err != nil {
			logger.LegacyPrintf("service.concurrency", "failed to release subscription slot %d: %v", policy.subscriptionID, err)
		}
	}
	userSlot, err := s.acquireUnscopedUserSlot(ctx, userID, maxConcurrency)
	if err != nil || !userSlot.Acquired {
		releaseSubscription()
		return userSlot, err
	}
	var once sync.Once
	return &AcquireResult{Acquired: true, ReleaseFunc: func() { once.Do(func() { userSlot.ReleaseFunc(); releaseSubscription() }) }}, nil
}
