//go:build unit

package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TS-03 / PRD-03：256 只新增白标充值中心的两张表（订单映射 + 商品快照），
// 不改写任何历史列与数据。
func TestYunmaoOrdersMigration(t *testing.T) {
	content, err := FS.ReadFile("256_yunmao_orders.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// 订单映射表：四个展示状态 + 幂等唯一约束 + 加密卡密列。
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS yunmao_orders")
	require.Contains(t, sql, "order_no VARCHAR(64) NOT NULL")
	require.Contains(t, sql, "user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE")
	require.Contains(t, sql, "idempotency_key VARCHAR(64) NOT NULL")
	require.Contains(t, sql, "upstream_order_no VARCHAR(128)")
	require.Contains(t, sql, "product_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "card_secret TEXT")
	require.Contains(t, sql, "CONSTRAINT yunmao_orders_user_idempotency_unique UNIQUE (user_id, idempotency_key)")
	require.Contains(t, sql, "CONSTRAINT yunmao_orders_order_no_unique UNIQUE (order_no)")
	require.Contains(t, sql,
		"CONSTRAINT yunmao_orders_display_status_check CHECK (display_status IN ('pending', 'paid', 'delivery_pending', 'delivered'))")

	// 商品快照表：单行 + 同步失败标记（保留最近成功数据）。
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS yunmao_catalog_snapshots")
	require.Contains(t, sql, "CONSTRAINT yunmao_catalog_snapshots_singleton CHECK (id = 1)")

	// 白标：迁移注释里不得出现供应商品牌词（避免把上游身份写进 schema 注释）。
	require.NotContains(t, strings.ToLower(string(content)), "yunmao.com")

	// 不得改写历史：没有 DROP COLUMN / DROP TABLE / DELETE FROM / UPDATE 之类的破坏性语句。
	comments := regexp.MustCompile(`(?ms)/\*.*?\*/|--[^\n]*`)
	body := strings.ToUpper(string(comments.ReplaceAll(content, nil)))
	require.NotContains(t, body, "DROP COLUMN")
	require.NotContains(t, body, "DROP TABLE")
	require.NotContains(t, body, "DELETE FROM")
	require.NotContains(t, body, "TRUNCATE")
}

// 卡密列必须是密文存储语义：迁移里要有明确的 COMMENT 说明，防止后来者当明文用。
func TestYunmaoOrdersCardColumnIsDocumentedAsEncrypted(t *testing.T) {
	content, err := FS.ReadFile("256_yunmao_orders.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "COMMENT ON COLUMN yunmao_orders.card_secret IS")
	require.Contains(t, strings.ToLower(sql), "encrypted card secret")
}
