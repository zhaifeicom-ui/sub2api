package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type balancePurchaseSettings struct{ service.SettingRepository }

func (balancePurchaseSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	return map[string]string{service.SettingPaymentEnabled: "true"}, nil
}

type balancePurchaseGroups struct {
	service.GroupRepository
	group *service.Group
}

// SQLite serializes writes on this single connection; production uses the
// repository's SELECT FOR UPDATE implementation on PostgreSQL unchanged.
type sqliteBalanceSubscriptions struct {
	service.UserSubscriptionRepository
}

func (r sqliteBalanceSubscriptions) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	return r.GetByID(ctx, id)
}

func (r balancePurchaseGroups) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

func TestSubscriptionBalancePurchaseAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:balance_purchase_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	u, err := client.User.Create().SetEmail("balance-purchase@example.test").SetPasswordHash("test").SetBalance(10).Save(ctx)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName("Day card").SetSubscriptionType(service.SubscriptionTypeSubscription).SetSubscriptionConcurrency(2).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetGroupID(g.ID).SetName("Day card").SetPrice(2).SetValidityDays(1).Save(ctx)
	require.NoError(t, err)
	groups := balancePurchaseGroups{group: &service.Group{ID: g.ID, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}}
	subs := service.NewSubscriptionService(groups, sqliteBalanceSubscriptions{NewUserSubscriptionRepository(client)}, nil, client, &config.Config{})
	configSvc := service.NewPaymentConfigService(client, balancePurchaseSettings{}, nil)
	svc := service.NewPaymentService(client, nil, nil, nil, subs, configSvc, nil, groups, nil)
	const key = "test-purchase-idempotency-0001"
	first, err := svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 2, key)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	buyer, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, 8.0, buyer.Balance)
	sub, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(u.ID)).Only(ctx)
	require.NoError(t, err)
	expiry := sub.ExpiresAt
	replay, err := svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 2, key)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, first.OrderID, replay.OrderID)
	buyer, _ = client.User.Get(ctx, u.ID)
	require.Equal(t, 8.0, buyer.Balance)
	_, err = client.UserSubscription.UpdateOneID(sub.ID).SetConcurrencyLimit(3).SetDailyUsageUsd(0.25).Save(ctx)
	require.NoError(t, err)
	_, err = svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 2, "test-success-renewal-0001")
	require.NoError(t, err)
	renewed, err := client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, expiry.AddDate(0, 0, 1).Equal(renewed.ExpiresAt))
	require.Equal(t, 3, renewed.ConcurrencyLimit)
	require.Equal(t, 0.25, renewed.DailyUsageUsd)
	expiry = renewed.ExpiresAt
	_, err = svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 3, "test-price-changed-0001")
	require.Error(t, err)
	_, err = client.User.UpdateOneID(u.ID).SetFrozenBalance(7).Save(ctx)
	require.NoError(t, err)
	_, err = svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 2, "test-frozen-balance-0001")
	require.Error(t, err)
	buyer, _ = client.User.Get(ctx, u.ID)
	require.Equal(t, 6.0, buyer.Balance)
	orders, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(u.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, orders)
	// Inject a late failure after balance deduction/order creation: all must roll back.
	_, err = db.Exec("CREATE TRIGGER fail_balance_sub_audit BEFORE INSERT ON payment_audit_logs BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END;")
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(u.ID).SetFrozenBalance(0).Save(ctx)
	require.NoError(t, err)
	_, err = svc.PurchaseSubscriptionWithBalance(ctx, u.ID, plan.ID, 2, "test-late-rollback-0001")
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit unavailable")
	buyer, _ = client.User.Get(ctx, u.ID)
	require.Equal(t, 6.0, buyer.Balance)
	latest, err := client.UserSubscription.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, expiry.Equal(latest.ExpiresAt))
	_, err = db.Exec("DROP TRIGGER fail_balance_sub_audit")
	require.NoError(t, err)
	u2, err := client.User.Create().SetEmail("concurrent-buyer@example.test").SetPasswordHash("test").SetBalance(3).Save(ctx)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.PurchaseSubscriptionWithBalance(ctx, u2.ID, plan.ID, 2, fmt.Sprintf("concurrent-purchase-%04d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success, "concurrent purchases must not overspend")
	buyer, err = client.User.Get(ctx, u2.ID)
	require.NoError(t, err)
	require.Equal(t, 1.0, buyer.Balance)
	stats, err := svc.GetDashboardStats(ctx, 30)
	require.NoError(t, err)
	require.Zero(t, stats.TotalCount, "internal spending is not external revenue")
}
