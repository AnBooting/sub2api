package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/lib/pq"
)

// renamedMigrationReplayRule 登记「因迁移重编号而在旧库上被重放」的迁移。
//
// 背景：0.2.17 上游同步把本 fork 的 243–250 重命名为 251–258（避免与上游
// 243/244/245 撞号）。迁移执行器按**完整文件名**判断迁移是否已应用，旧库里
// `243_native_currency_pricing.sql` 的记录无法识别新的 `251_native_currency_pricing.sql`，
// 于是重放时撞上同名约束（SQLSTATE 42710），导致服务启动直接失败。
//
// 容错边界（三条必须同时满足，缺一不可）：
//  1. 迁移必须在本表登记过——未登记的迁移一律照旧失败；
//  2. 失败错误码必须属于该迁移登记的「重复对象」码（按各迁移 DDL 语义逐个判定）；
//  3. 当前文件内容的 SHA256 必须与登记的旧校验和一致——说明迁移内容没被改过。
//
// 任何一条不满足都照旧报错退出，不做任何猜测。
type renamedMigrationReplayRule struct {
	// newName 是重编号后的文件名，同时也是 renamedMigrationReplayRules 的键。
	newName string
	// oldName 是重编号前的文件名，只用于告警与运维核对。
	oldName string
	// oldChecksum 是旧文件内容（trim 后）的 SHA256；容错时用它与当前文件校验和比对。
	oldChecksum string
	// currentChecksum 是当前文件内容（trim 后）的 SHA256，用于自检登记是否与文件一致。
	currentChecksum string
	// duplicateErrorCodes 是允许容错的 PostgreSQL SQLSTATE；为空表示该迁移重跑本身不报错。
	duplicateErrorCodes []string
}

// renamedMigrationReplayRules 登记 0.2.17 上游同步时重命名的 8 个迁移。
//
// 允许容错的错误码按每条迁移的 DDL 逐个推导：
//
//   - 251：第 1、3 条是 ADD COLUMN IF NOT EXISTS（幂等）；第 2 条 ADD CONSTRAINT 没有
//     IF NOT EXISTS，重跑必撞 42710（duplicate_object）。
//   - 252：第 2 条 ADD CONSTRAINT → 42710；第 4 条 CREATE TABLE / 第 5 条 CREATE INDEX
//     没有 IF NOT EXISTS → 42P07（duplicate_table）。
//     唯一键冲突 23505 **不**登记：它只会出现在「库被部分应用」的状态，必须显式失败。
//   - 253–258：DDL 本身可重跑（ADD COLUMN IF NOT EXISTS / CREATE TABLE IF NOT EXISTS /
//     CREATE INDEX IF NOT EXISTS / pg_constraint 守卫的 DO 块 / DROP CONSTRAINT IF EXISTS
//     后重建），重跑不产生重复对象错误，因此不登记任何容错码。
//     其中 258 尤其要注意：它先 DROP CONSTRAINT IF EXISTS 再 ADD CONSTRAINT，
//     所以重跑是「先删后建」，不会撞 42710。
var renamedMigrationReplayRules = map[string]renamedMigrationReplayRule{
	"251_native_currency_pricing.sql": {
		newName:             "251_native_currency_pricing.sql",
		oldName:             "243_native_currency_pricing.sql",
		oldChecksum:         "007f996aff9b1bb9c6645253087c89d695e09c0356e5f3dd7aa67e870b21ec6a",
		currentChecksum:     "007f996aff9b1bb9c6645253087c89d695e09c0356e5f3dd7aa67e870b21ec6a",
		duplicateErrorCodes: []string{"42710"},
	},
	"252_api_key_group_links.sql": {
		newName:             "252_api_key_group_links.sql",
		oldName:             "244_api_key_group_links.sql",
		oldChecksum:         "905b0cb091bce9f60bbf890e3a864f96061172d050ddbc1aa3054e8f261ef82e",
		currentChecksum:     "905b0cb091bce9f60bbf890e3a864f96061172d050ddbc1aa3054e8f261ef82e",
		duplicateErrorCodes: []string{"42710", "42P07"},
	},
	"253_group_scheduling_strategy.sql": {
		newName:         "253_group_scheduling_strategy.sql",
		oldName:         "245_group_scheduling_strategy.sql",
		oldChecksum:     "60d2aebafbd1464cc86b91ac324b81a499fa3820ce420102d8b6b1db27d90785",
		currentChecksum: "60d2aebafbd1464cc86b91ac324b81a499fa3820ce420102d8b6b1db27d90785",
	},
	"254_group_high_availability.sql": {
		newName:         "254_group_high_availability.sql",
		oldName:         "246_group_high_availability.sql",
		oldChecksum:     "8d61900674fdb09a35aef1516995b27752f2d861c2c9add559d5f71e6bd9f910",
		currentChecksum: "8d61900674fdb09a35aef1516995b27752f2d861c2c9add559d5f71e6bd9f910",
	},
	"255_group_video_price_plans.sql": {
		newName:         "255_group_video_price_plans.sql",
		oldName:         "247_group_video_price_plans.sql",
		oldChecksum:     "5f4781866246d2b890ca916011e4659d97a7ccf99e546aa4854f073106996c33",
		currentChecksum: "5f4781866246d2b890ca916011e4659d97a7ccf99e546aa4854f073106996c33",
	},
	"256_yunmao_orders.sql": {
		newName:         "256_yunmao_orders.sql",
		oldName:         "248_yunmao_orders.sql",
		oldChecksum:     "0dfdecb43a3481217bec53423173486743037debc579f9a65dc0625e4d0d7106",
		currentChecksum: "0dfdecb43a3481217bec53423173486743037debc579f9a65dc0625e4d0d7106",
	},
	"257_video_tasks.sql": {
		newName:         "257_video_tasks.sql",
		oldName:         "249_video_tasks.sql",
		oldChecksum:     "4a15fc2d014428c4439e5e2035ff9ec8845a6a37dafb1602368bca7cc9a25046",
		currentChecksum: "4a15fc2d014428c4439e5e2035ff9ec8845a6a37dafb1602368bca7cc9a25046",
	},
	"258_relax_group_link_authorization_check.sql": {
		newName:         "258_relax_group_link_authorization_check.sql",
		oldName:         "250_relax_group_link_authorization_check.sql",
		oldChecksum:     "12cf51273ec7c4008bc119ee66b4800b975a61c9a73944126e52155bd78a8a11",
		currentChecksum: "9da912a1700757c935176693f691dec1d715ef8685902f29ff32c86adf2bb049",
		// 258 的当前内容与旧 250 的校验和不同（只改了注释里的编号引用，DDL 逐字未改），
		// 因此即使它真的报出重复对象错误也不会被容错——迁移内容在改名后发生变化时
		// 一律不容错，这是刻意收紧的护栏。
	},
}

// sqlStatePattern 用于从错误文本里解析 SQLSTATE（驱动被包装、或测试替身只保留文本时使用）。
var sqlStatePattern = regexp.MustCompile(`(?i)SQLSTATE\s+([0-9A-Za-z]{5})`)

// migrationContentChecksum 计算迁移内容（trim 后）的 SHA256，与执行器落库的口径一致。
func migrationContentChecksum(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}

// postgresErrorCode 提取 PostgreSQL 错误码（SQLSTATE）；无法识别时返回空串。
func postgresErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	if m := sqlStatePattern.FindStringSubmatch(err.Error()); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// tolerateRenamedMigrationReplay 判断一次迁移执行失败能否**进入**「旧库重放已应用过的
// 改名迁移」的容错判定。
//
// 注意这只是第一道闸：它比对的是「源码里的迁移文件」与「登记表」，完全没有访问数据库。
// 返回 true 之后，调用方必须再调用 verifyRenamedMigrationLegacyHistory 向数据库取得
// 「这台库确实执行过旧迁移」的证据，两者都成立才允许放行。
func tolerateRenamedMigrationReplay(name string, checksum string, err error) (renamedMigrationReplayRule, bool) {
	rule, ok := renamedMigrationReplayRules[name]
	if !ok {
		// 未登记的迁移：照旧失败。
		return renamedMigrationReplayRule{}, false
	}
	if len(rule.duplicateErrorCodes) == 0 {
		// 该迁移重跑不会产生重复对象错误，无需容错。
		return renamedMigrationReplayRule{}, false
	}
	code := postgresErrorCode(err)
	if code == "" {
		return renamedMigrationReplayRule{}, false
	}
	if !slices.Contains(rule.duplicateErrorCodes, code) {
		// 错误码不在该迁移的允许集合里：照旧失败。
		return renamedMigrationReplayRule{}, false
	}
	if checksum != rule.oldChecksum {
		// 迁移内容在改名之后被改动过：照旧失败，不容错。
		return renamedMigrationReplayRule{}, false
	}
	return rule, true
}

// verifyRenamedMigrationLegacyHistory 向数据库取证：这台库是否真的完整执行过改名前的迁移。
//
// 只看「新文件名没记录 + 撞了重复对象错误」是不够的：迁移事务是原子的，重复对象错误会
// 让整份迁移回滚，后续语句从未执行；若此时按「已生效」放行，库里会静默缺列/缺表。
// 因此必须在持有 advisory lock 的连接上核对 schema_migrations 里的旧文件名记录及其
// checksum：只有与登记的旧校验和逐字一致，才说明这份迁移曾经完整执行过。
//
// 返回 error 时调用方必须照旧失败退出——缺证据就不放行，不做任何猜测或降级。
func verifyRenamedMigrationLegacyHistory(ctx context.Context, conn migrationConnection, rule renamedMigrationReplayRule) error {
	var recorded string
	err := conn.QueryRowContext(ctx,
		"SELECT checksum FROM schema_migrations WHERE filename = $1", rule.oldName,
	).Scan(&recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"duplicate object error is not proof that %s ran completely: no record for the previous filename %s; refusing to skip the migration",
			rule.newName, rule.oldName,
		)
	}
	if err != nil {
		return fmt.Errorf("verify legacy history for %s: %w", rule.newName, err)
	}
	if recorded != rule.oldChecksum {
		return fmt.Errorf(
			"legacy history drift for %s: schema_migrations records %s with checksum %s, registry expects %s; refusing to skip the migration",
			rule.newName, rule.oldName, recorded, rule.oldChecksum,
		)
	}
	return nil
}
