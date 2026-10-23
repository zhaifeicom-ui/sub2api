package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAuthProjectionEnforcesInheritedSubscriptionConcurrency(t *testing.T) {
	repo, db := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, db, "subscription-concurrency@test.com")
	group, err := db.Group.Create().SetName("day-card").
		SetPlatform(service.PlatformDeepseek).SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetSubscriptionConcurrency(4).SetRateMultiplier(1).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, GroupID: &group.ID, Key: "sk-subscription-concurrency-test", Name: "day-card", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	auth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, auth.Group)
	require.Equal(t, 4, auth.Group.SubscriptionConcurrency, "auth projection must preserve the database limit")

	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	svc := service.NewConcurrencyService(NewConcurrencyCache(client, 15, 900))
	ctx = service.WithSubscriptionConcurrency(ctx, &service.UserSubscription{ID: 101, UserID: user.ID, GroupID: group.ID}, auth.Group)
	var releases []func()
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})
	for i := 0; i < 4; i++ {
		slot, err := svc.AcquireUserSlot(ctx, user.ID, 100)
		require.NoError(t, err)
		require.True(t, slot.Acquired)
		releases = append(releases, slot.ReleaseFunc)
	}
	fifth, err := svc.AcquireUserSlot(ctx, user.ID, 100)
	require.NoError(t, err)
	require.False(t, fifth.Acquired, "the fifth request must not reach the upstream")
	releases[0]()
	next, err := svc.AcquireUserSlot(ctx, user.ID, 100)
	require.NoError(t, err)
	require.True(t, next.Acquired, "a completed request must free its slot")
	releases = append(releases, next.ReleaseFunc)
}
