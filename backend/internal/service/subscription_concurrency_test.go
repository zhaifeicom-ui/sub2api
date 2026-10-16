package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

type subscriptionSlotsTestCache struct {
	ConcurrencyCache
	mu       sync.Mutex
	slots    map[int64]map[string]bool
	failUser int64
}

func (c *subscriptionSlotsTestCache) AcquireSubscriptionSlot(ctx context.Context, id int64, limit int, req string) (bool, error) {
	return c.AcquireUserSlot(ctx, -id, limit, req)
}
func (c *subscriptionSlotsTestCache) ReleaseSubscriptionSlot(ctx context.Context, id int64, req string) error {
	return c.ReleaseUserSlot(ctx, -id, req)
}

func (c *subscriptionSlotsTestCache) AcquireUserSlot(_ context.Context, id int64, limit int, req string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == c.failUser {
		return false, errors.New("redis error")
	}
	if c.slots[id] == nil {
		c.slots[id] = map[string]bool{}
	}
	if len(c.slots[id]) >= limit {
		return false, nil
	}
	c.slots[id][req] = true
	return true, nil
}
func (c *subscriptionSlotsTestCache) ReleaseUserSlot(_ context.Context, id int64, req string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.slots[id], req)
	return nil
}
func subscriptionTestContext(userID, subID int64, groupLimit, override int) context.Context {
	return WithSubscriptionConcurrency(context.Background(), &UserSubscription{ID: subID, UserID: userID, GroupID: 8, ConcurrencyLimit: override}, &Group{ID: 8, SubscriptionType: SubscriptionTypeSubscription, SubscriptionConcurrency: groupLimit})
}
func TestSubscriptionConcurrencyIsolationAndRelease(t *testing.T) {
	cache := &subscriptionSlotsTestCache{slots: map[int64]map[string]bool{}}
	svc := NewConcurrencyService(cache)
	ctx := subscriptionTestContext(1, 101, 1, 0)
	first, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.True(t, first.Acquired)
	blocked, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "multiple keys share subscription slots")
	for _, other := range []struct {
		ctx  context.Context
		user int64
	}{
		{subscriptionTestContext(2, 102, 1, 0), 2},
		{subscriptionTestContext(1, 103, 1, 0), 1},
		{context.Background(), 1},
	} {
		r, err := svc.AcquireUserSlot(other.ctx, other.user, 999)
		require.NoError(t, err)
		require.True(t, r.Acquired)
		r.ReleaseFunc()
	}
	first.ReleaseFunc()
	first.ReleaseFunc()
	r, err := svc.AcquireUserSlot(ctx, 1, 999)
	require.NoError(t, err)
	require.True(t, r.Acquired)
	r.ReleaseFunc()
	require.Empty(t, cache.slots[-101])
	require.Empty(t, cache.slots[1])
}
func TestSubscriptionConcurrencyGlobalFailureRollsBack(t *testing.T) {
	cache := &subscriptionSlotsTestCache{slots: map[int64]map[string]bool{}, failUser: 1}
	svc := NewConcurrencyService(cache)
	_, err := svc.AcquireUserSlot(subscriptionTestContext(1, 101, 2, 0), 1, 1)
	require.Error(t, err)
	require.Empty(t, cache.slots[-101])
	cache.failUser = 0
	r, err := svc.AcquireUserSlot(context.Background(), 1, 1)
	require.NoError(t, err)
	blocked, err := svc.AcquireUserSlot(subscriptionTestContext(1, 101, 2, 0), 1, 1)
	require.NoError(t, err)
	require.False(t, blocked.Acquired)
	require.Empty(t, cache.slots[-101])
	r.ReleaseFunc()
}
func TestSubscriptionConcurrencyParallelOverride(t *testing.T) {
	cache := &subscriptionSlotsTestCache{slots: map[int64]map[string]bool{}}
	svc := NewConcurrencyService(cache)
	ctx := subscriptionTestContext(1, 101, 5, 2)
	var success atomic.Int32
	var wg sync.WaitGroup
	releases := make(chan func(), 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := svc.AcquireUserSlot(ctx, 1, 999)
			if err == nil && r.Acquired {
				success.Add(1)
				releases <- r.ReleaseFunc
			}
		}()
	}
	wg.Wait()
	close(releases)
	require.EqualValues(t, 2, success.Load())
	for release := range releases {
		release()
	}
	require.Empty(t, cache.slots[-101])
	require.Empty(t, cache.slots[1])
}

type subscriptionConcurrencyRepoStub struct {
	UserSubscriptionRepository
	sub     UserSubscription
	updates int
}

func (r *subscriptionConcurrencyRepoStub) GetByID(context.Context, int64) (*UserSubscription, error) {
	sub := r.sub
	return &sub, nil
}
func (r *subscriptionConcurrencyRepoStub) UpdateConcurrencyLimit(_ context.Context, _ int64, limit int) error {
	r.updates++
	r.sub.ConcurrencyLimit = limit
	return nil
}
func TestSubscriptionConcurrencyUpdateOnlyLimit(t *testing.T) {
	repo := &subscriptionConcurrencyRepoStub{sub: UserSubscription{ID: 101, UserID: 1, GroupID: 8, DailyUsageUSD: 12, Notes: "keep"}}
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 100, MaxCost: 100, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(cache.Close)
	svc := &SubscriptionService{userSubRepo: repo, subCacheL1: cache}
	for _, limit := range []int{3, 0} {
		cache.Set(subCacheKey(1, 8), &UserSubscription{ConcurrencyLimit: 99}, 1)
		cache.Wait()
		sub, err := svc.UpdateConcurrencyLimit(context.Background(), 101, limit)
		require.NoError(t, err)
		require.Equal(t, limit, sub.ConcurrencyLimit)
		require.Equal(t, 12.0, sub.DailyUsageUSD)
		require.Equal(t, "keep", sub.Notes)
		_, cached := cache.Get(subCacheKey(1, 8))
		require.False(t, cached, "settings must invalidate subscription snapshots")
	}
	_, err = svc.UpdateConcurrencyLimit(context.Background(), 101, -1)
	require.Error(t, err)
	_, err = svc.UpdateConcurrencyLimit(context.Background(), 101, 100001)
	require.Error(t, err)
	require.Equal(t, 2, repo.updates)
}

func TestSubscriptionConcurrencyDisabledAndScopeValidation(t *testing.T) {
	ctx := subscriptionTestContext(1, 101, 0, 0)
	require.False(t, HasSubscriptionConcurrencyLimit(ctx))
	svc := NewConcurrencyService(&subscriptionSlotsTestCache{slots: map[int64]map[string]bool{}})
	r, err := svc.AcquireUserSlot(ctx, 1, 0)
	require.NoError(t, err)
	require.True(t, r.Acquired)
	r.ReleaseFunc()
	ctx = subscriptionTestContext(1, 101, 0, 2)
	require.True(t, HasSubscriptionConcurrencyLimit(ctx))
	_, err = svc.AcquireUserSlot(ctx, 2, 999)
	require.Error(t, err)
	standard := WithSubscriptionConcurrency(context.Background(), &UserSubscription{ID: 101, UserID: 1, GroupID: 8, ConcurrencyLimit: 2}, &Group{ID: 8, SubscriptionType: SubscriptionTypeStandard, SubscriptionConcurrency: 2})
	require.False(t, HasSubscriptionConcurrencyLimit(standard))
}
