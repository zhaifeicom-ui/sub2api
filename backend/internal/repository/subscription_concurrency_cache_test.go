package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionConcurrencyRedisSlots(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewConcurrencyCache(client, 15, 900)
	svc := service.NewConcurrencyService(cache)
	ctx := service.WithSubscriptionConcurrency(context.Background(), &service.UserSubscription{ID: 101, UserID: 1, GroupID: 8}, &service.Group{ID: 8, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionConcurrency: 1})
	ctx, cancel := context.WithCancel(ctx)
	first, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.True(t, first.Acquired)
	blocked, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.False(t, blocked.Acquired)
	require.True(t, r.Exists("concurrency:subscription:101"))
	cancel()
	first.ReleaseFunc()
	first.ReleaseFunc()
	ctx = context.WithoutCancel(ctx)
	next, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.True(t, next.Acquired)
	next.ReleaseFunc()
	count, err := cache.GetUserConcurrency(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, count)
	// A crashed process must not leave a permanent subscription lock.
	last, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.True(t, last.Acquired)
	r.FastForward(16 * time.Minute)
	require.False(t, r.Exists("concurrency:subscription:101"))
	last.ReleaseFunc()
}
