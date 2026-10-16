package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/shopspring/decimal"
)

type BalanceSubscriptionPurchase struct {
	OrderID  int64   `json:"order_id"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Status   string  `json:"status"`
	Replayed bool    `json:"replayed"`
}

func balanceSubscriptionReference(userID int64, key string) string {
	hash := sha256.Sum256([]byte(strconv.FormatInt(userID, 10) + ":" + key))
	return fmt.Sprintf("bs_%x", hash[:28])
}

// PurchaseSubscriptionWithBalance never calls a payment provider. The user row
// is locked before replay lookup, and funds/order/fulfillment commit together.
func (s *PaymentService) PurchaseSubscriptionWithBalance(ctx context.Context, userID, planID int64, expectedPrice float64, idempotencyKey string) (*BalanceSubscriptionPurchase, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if userID <= 0 || planID <= 0 || len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || math.IsNaN(expectedPrice) || math.IsInf(expectedPrice, 0) || expectedPrice <= 0 {
		return nil, infraerrors.BadRequest("INVALID_BALANCE_PURCHASE", "invalid purchase parameters")
	}
	if s.entClient == nil || s.subscriptionSvc == nil || s.configService == nil {
		return nil, fmt.Errorf("subscription purchase unavailable")
	}
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, infraerrors.Forbidden("PAYMENT_DISABLED", "payment system is disabled")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	// An UPDATE obtains a real row lock on PostgreSQL and is also testable on
	// SQLite. Concurrent billing and purchases serialize on the same user row.
	changed, err := tx.User.Update().Where(user.IDEQ(userID), user.StatusEQ(StatusActive)).SetUpdatedAt(time.Now()).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user is unavailable")
	}
	buyer, err := tx.User.Get(txCtx, userID)
	if err != nil {
		return nil, err
	}
	reference := balanceSubscriptionReference(userID, idempotencyKey)
	existing, err := tx.PaymentOrder.Query().Where(paymentorder.OutTradeNoEQ(reference), paymentorder.UserIDEQ(userID)).Only(txCtx)
	if err == nil {
		if existing.PlanID == nil || existing.SubscriptionGroupID == nil || *existing.PlanID != planID || existing.Status != OrderStatusCompleted || existing.PaymentType != "balance" {
			return nil, infraerrors.Conflict("PURCHASE_REPLAY_CONFLICT", "purchase key has already been used")
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		s.invalidateBalancePurchaseCaches(userID, *existing.SubscriptionGroupID)
		return &BalanceSubscriptionPurchase{existing.ID, existing.Amount, "USD", existing.Status, true}, nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	plan, err := tx.SubscriptionPlan.Get(txCtx, planID)
	if err != nil || !plan.ForSale {
		return nil, infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan is not for sale")
	}
	group, err := tx.Group.Get(txCtx, plan.GroupID)
	if err != nil || group.Status != StatusActive || group.SubscriptionType != SubscriptionTypeSubscription {
		return nil, infraerrors.BadRequest("GROUP_NOT_AVAILABLE", "subscription group is unavailable")
	}
	if math.IsNaN(plan.Price) || math.IsInf(plan.Price, 0) || plan.Price <= 0 || math.Abs(plan.Price-expectedPrice) > 0.00000001 {
		return nil, infraerrors.Conflict("PLAN_PRICE_CHANGED", "plan price changed; refresh and confirm again")
	}
	days := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	if days <= 0 || days > MaxValidityDays {
		return nil, infraerrors.BadRequest("INVALID_PLAN_VALIDITY", "invalid subscription duration")
	}
	if decimal.NewFromFloat(buyer.Balance).Sub(decimal.NewFromFloat(math.Max(buyer.FrozenBalance, 0))).LessThan(decimal.NewFromFloat(plan.Price)) {
		return nil, infraerrors.BadRequest("INSUFFICIENT_BALANCE", "insufficient available account balance")
	}
	changed, err = tx.User.Update().Where(user.IDEQ(userID), user.BalanceGTE(plan.Price)).AddBalance(-plan.Price).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, infraerrors.BadRequest("INSUFFICIENT_BALANCE", "insufficient account balance")
	}
	now := time.Now()
	order, err := tx.PaymentOrder.Create().SetUserID(userID).SetUserEmail(buyer.Email).SetUserName(buyer.Username).
		SetAmount(plan.Price).SetPayAmount(plan.Price).SetFeeRate(0).SetRechargeCode(reference).
		SetOutTradeNo(reference).SetPaymentType("balance").SetPaymentTradeNo(reference).
		SetProviderKey("internal_balance").SetProviderSnapshot(map[string]any{"schema_version": 2, "currency": "USD", "provider_key": "internal_balance"}).
		SetOrderType(payment.OrderTypeSubscription).SetPlanID(planID).SetSubscriptionGroupID(plan.GroupID).SetSubscriptionDays(days).
		SetStatus(OrderStatusCompleted).SetPaidAt(now).SetCompletedAt(now).SetExpiresAt(now).
		SetClientIP("").SetSrcHost("internal").Save(txCtx)
	if err != nil {
		return nil, err
	}
	_, _, err = s.subscriptionSvc.assignOrExtendSubscription(txCtx, &AssignSubscriptionInput{UserID: userID, GroupID: plan.GroupID, ValidityDays: days, Notes: paymentSubscriptionOrderNote(order.ID)}, true)
	if err != nil {
		return nil, err
	}
	_, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("SUBSCRIPTION_ASSIGNED").SetDetail("Purchased with account balance in USD").SetOperator(strconv.FormatInt(userID, 10)).Save(txCtx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateBalancePurchaseCaches(userID, plan.GroupID)
	return &BalanceSubscriptionPurchase{order.ID, plan.Price, "USD", OrderStatusCompleted, false}, nil
}

func (s *PaymentService) invalidateBalancePurchaseCaches(userID, groupID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.redeemService != nil && s.redeemService.authCacheInvalidator != nil {
		s.redeemService.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.subscriptionSvc != nil {
		if err := s.subscriptionSvc.invalidateSubscriptionCaches(userID, groupID); err != nil {
			logger.LegacyPrintf("service.payment", "balance purchase subscription cache invalidation: %v", err)
		}
		if s.subscriptionSvc.billingCacheService != nil {
			if err := s.subscriptionSvc.billingCacheService.InvalidateUserBalance(ctx, userID); err != nil {
				logger.LegacyPrintf("service.payment", "balance purchase balance cache invalidation: %v", err)
			}
		}
	}
}
