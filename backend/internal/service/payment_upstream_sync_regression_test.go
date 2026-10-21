//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// purchaseRedeemRepoStub 复用共享的兑换码桩，只补齐核销后回读所需的 GetByID。
type purchaseRedeemRepoStub struct {
	*redeemCodeRepoStub
}

func (s *purchaseRedeemRepoStub) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range s.codesByCode {
		if code.ID == id {
			cloned := *code
			return &cloned, nil
		}
	}
	return nil, ErrRedeemCodeNotFound
}

// createPaymentBalanceOrder 建一张余额充值订单（上游 payment_orders 表），
// 供 /purchase 路径的回归用例复用。
func createPaymentBalanceOrder(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	status string,
) *dbent.PaymentOrder {
	t.Helper()
	user, err := client.User.Create().
		SetEmail("purchase-balance-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com").
		SetPasswordHash("hash").
		SetUsername("purchase-balance-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(80).
		SetPayAmount(80).
		SetFeeRate(0).
		SetRechargeCode("PAY-BAL-" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetOutTradeNo("purchase_balance_" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-purchase-balance-" + strconv.FormatInt(time.Now().UnixNano(), 10)).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(status).
		SetPaidAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetUpdatedAt(time.Now()).
		Save(ctx)
	require.NoError(t, err)
	return order
}

// TestPurchaseOrderTypeGateAcceptsOnlyUpstreamTypes 锁住上游 7c6f70680 的语义在
// 我们的 /purchase 入口仍然生效：订单类型只接受 balance / subscription，其余
// （含大小写变体、空白变体、白标其它类型）一律 INVALID_ORDER_TYPE。
func TestPurchaseOrderTypeGateAcceptsOnlyUpstreamTypes(t *testing.T) {
	svc := &PaymentService{}

	plan, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{OrderType: payment.OrderTypeBalance, Amount: 10}, &PaymentConfig{})
	require.NoError(t, err)
	require.Nil(t, plan)

	for _, orderType := range []string{"x", "Balance", " balance", "subscription ", "yunmao", ""} {
		_, err := svc.validateOrderInput(context.Background(), CreateOrderRequest{OrderType: orderType, Amount: 10}, &PaymentConfig{})
		require.Error(t, err, "order type %q must be rejected", orderType)
		require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err), "order type %q", orderType)
	}
}

// TestPurchaseRefundRejectsUnsupportedOrderType 锁住上游 83337a0c6 / 7c6f70680：
// 退款只支持余额充值订单，订阅等其它订单类型一律拒绝，不会进入扣减流程。
func TestPurchaseRefundRejectsUnsupportedOrderType(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusCompleted, time.Now())

	svc := &PaymentService{entClient: client}
	err := svc.RequestRefund(ctx, order.ID, order.UserID, "申请退款")
	require.Error(t, err)
	require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))

	// 订单状态不被改动：拒绝发生在扣减之前。
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
}

// TestPurchaseNotificationCreditsOnceAndIgnoresDuplicate 锁住 /purchase 的成功路径与
// 重复回调：回调成功后订单完成、额度只发放一次；同一回调再发一次不得重复发放。
func TestPurchaseNotificationCreditsOnceAndIgnoresDuplicate(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentBalanceOrder(t, ctx, client, OrderStatusPending)

	redeemRepo := &purchaseRedeemRepoStub{redeemCodeRepoStub: &redeemCodeRepoStub{codesByCode: map[string]*RedeemCode{
		order.RechargeCode: {
			ID:     201,
			Code:   order.RechargeCode,
			Type:   RedeemTypeBalance,
			Value:  order.Amount,
			Status: StatusUnused,
		},
	}}}
	credited := 0.0
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID}}
	userRepo.updateBalanceFn = func(_ context.Context, _ int64, amount float64) error {
		credited += amount
		return nil
	}
	svc := &PaymentService{
		entClient:     client,
		redeemService: &RedeemService{redeemRepo: redeemRepo, userRepo: userRepo, entClient: client},
	}

	notification := &payment.PaymentNotification{
		TradeNo: "alipay-purchase-trade",
		OrderID: order.OutTradeNo,
		Amount:  order.PayAmount,
		Status:  payment.NotificationStatusSuccess,
	}
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification, payment.TypeAlipay))

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Len(t, redeemRepo.useCalls, 1, "成功回调必须核销一次兑换码")
	require.Equal(t, 80.0, credited, "成功回调必须发放额度一次")

	// 重复回调：不得重复核销、不得重复发放额度、不得改写已完成订单。
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification, payment.TypeAlipay))
	reloaded, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Len(t, redeemRepo.useCalls, 1, "重复回调不得重复核销兑换码")
	require.Equal(t, 80.0, credited, "重复回调不得重复发放额度")
}

// TestPurchaseNotificationRejectsAmountMismatchWithoutFulfilling 锁住 /purchase 的失败
// 路径：金额不符的回调被拒绝，订单不完成、额度不发放。
func TestPurchaseNotificationRejectsAmountMismatchWithoutFulfilling(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentBalanceOrder(t, ctx, client, OrderStatusPending)

	redeemRepo := &purchaseRedeemRepoStub{redeemCodeRepoStub: &redeemCodeRepoStub{codesByCode: map[string]*RedeemCode{
		order.RechargeCode: {
			ID:     202,
			Code:   order.RechargeCode,
			Type:   RedeemTypeBalance,
			Value:  order.Amount,
			Status: StatusUnused,
		},
	}}}
	credited := 0.0
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID}}
	userRepo.updateBalanceFn = func(_ context.Context, _ int64, amount float64) error {
		credited += amount
		return nil
	}
	svc := &PaymentService{
		entClient:     client,
		redeemService: &RedeemService{redeemRepo: redeemRepo, userRepo: userRepo, entClient: client},
	}

	notification := &payment.PaymentNotification{
		TradeNo: "alipay-purchase-trade-mismatch",
		OrderID: order.OutTradeNo,
		Amount:  order.PayAmount + 1,
		Status:  payment.NotificationStatusSuccess,
	}
	err := svc.HandlePaymentNotification(ctx, notification, payment.TypeAlipay)
	require.Error(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status, "失败回调不得完成订单")
	require.Empty(t, redeemRepo.useCalls, "失败回调不得核销兑换码")
	require.Zero(t, credited, "失败回调不得发放额度")
}
