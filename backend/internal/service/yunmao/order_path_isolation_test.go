//go:build unit

package yunmao

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestYunmaoOrderPathIsIndependentFromUpstreamPaymentOrders 锁住 D7 的隔离语义：
// 云猫白标下单走 yunmao_orders（迁移 256）这条独立链路，与上游 payment_orders 的
// 支付/退款七连修复互不影响——云猫侧既不引用上游订单类型常量，也不碰上游订单表。
func TestYunmaoOrderPathIsIndependentFromUpstreamPaymentOrders(t *testing.T) {
	// 云猫订单没有上游的订单类型维度（上游按 balance / subscription 分流并据此校验）。
	_, recordHasOrderType := reflect.TypeOf(OrderRecord{}).FieldByName("OrderType")
	require.False(t, recordHasOrderType, "云猫订单记录不得引入上游订单类型维度")
	_, createHasOrderType := reflect.TypeOf(CreateOrderRecord{}).FieldByName("OrderType")
	require.False(t, createHasOrderType, "云猫下单入参不得引入上游订单类型维度")

	// 云猫链路的生产代码不引用上游订单表与上游订单类型常量。
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		for i, line := range strings.Split(string(source), "\n") {
			require.NotContainsf(t, line, "payment_orders", "%s:%d 云猫链路不得引用上游订单表", file, i+1)
			require.NotContainsf(t, line, "OrderTypeBalance", "%s:%d 云猫链路不得引用上游订单类型", file, i+1)
			require.NotContainsf(t, line, "OrderTypeSubscription", "%s:%d 云猫链路不得引用上游订单类型", file, i+1)
		}
	}
}

// TestYunmaoOrderCreationStaysOnItsOwnPath 用生产代码下单一次，证明云猫链路自成一路：
// 订单落在云猫侧，幂等键重复提交不会多出一条映射，也不经过上游的订单类型校验。
func TestYunmaoOrderCreationStaysOnItsOwnPath(t *testing.T) {
	svc, store, _ := newOrderService(t)

	view, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{
		ProductID:      "sku-1",
		Quantity:       1,
		IdempotencyKey: "d7-idem-1",
	})
	require.NoError(t, err)
	require.NotEmpty(t, view.OrderNo)
	require.Len(t, store.orders, 1)

	_, err = svc.CreateOrder(context.Background(), 42, CreateOrderRequest{
		ProductID:      "sku-1",
		Quantity:       1,
		IdempotencyKey: "d7-idem-1",
	})
	require.NoError(t, err)
	require.Len(t, store.orders, 1, "重复幂等键不得多出一条云猫订单")
}
