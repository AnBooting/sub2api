package repository

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"
)

// 本测试静态校验 SQL 的占位符编号与实参个数是否严格一致。
//
// 背景：sqlmock 只按 SQL 字符串正则匹配、按 WithArgs 比对实参序列，它既不解析
// 占位符编号，也不做 PostgreSQL 端的 prepare/bind。因此「SQL 只用到 $6，却传了
// 8 个实参」这类缺陷在 mock 下全绿，真实 PG 却会报：
//   bind message supplies 8 parameters, but prepared statement requires 6
// 甚至 $4 同时被当成 int 与 text 的类型冲突。
// usage_stats_repair_queue.go 的 Fail 曾真实存在该缺陷（SET 用 $4/$5/$6，
// WHERE 又用 $4/$5 当 owner/fence，实参 8 个），而当时的 mock 测试是绿的。

var sqlPlaceholderPattern = regexp.MustCompile(`\$([0-9]+)`)

// maxSQLPlaceholder 返回 SQL 中出现的最大占位符编号，没有占位符时返回 0。
func maxSQLPlaceholder(query string) int {
	max := 0
	for _, m := range sqlPlaceholderPattern.FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}

// sqlArgMismatch 返回占位符与实参不一致时的描述；一致时返回空串。
// 抽成纯函数，便于用真实缺陷形态做负向自证（见 TestQueueSQLPlaceholdersDetectMismatch）。
func sqlArgMismatch(query string, argCount int) string {
	maxN := maxSQLPlaceholder(query)
	if maxN == 0 {
		return ""
	}
	if maxN != argCount {
		return fmt.Sprintf("SQL 占位符最大编号 $%d 与实参个数 %d 不一致", maxN, argCount)
	}
	seen := make(map[int]bool, maxN)
	for _, m := range sqlPlaceholderPattern.FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			seen[n] = true
		}
	}
	for i := 1; i <= maxN; i++ {
		if !seen[i] {
			return fmt.Sprintf("SQL 缺少占位符 $%d（占位符必须从 1 连续编号）", i)
		}
	}
	return ""
}

// TestQueueSQLPlaceholdersDetectMismatch 负向自证：用曾经真实存在的缺陷形态喂给
// 检测函数，必须报错。没有这条，扫描测试可能因为逻辑写错而永远为绿。
func TestQueueSQLPlaceholdersDetectMismatch(t *testing.T) {
	// 缺陷形态 1：SQL 只到 $6，实参 8 个（Fail 的真实历史 bug）。
	buggy := "UPDATE t SET state = CASE WHEN attempts >= $4 THEN 'quarantine' ELSE 'dirty' END, reason_code = $5, next_retry_at = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond') WHERE timezone_name = $1 AND day_start = $2 AND generation = $3 AND claim_owner = $4 AND claim_fence = $5"
	if got := sqlArgMismatch(buggy, 8); got == "" {
		t.Fatal("检测函数未能发现「$6 占位符配 8 个实参」的真实缺陷形态")
	}
	// 缺陷形态 2：占位符跳号（$1、$3），即使个数巧合对上也不允许。
	if got := sqlArgMismatch("SELECT 1 WHERE a = $1 AND b = $3", 2); got == "" {
		t.Fatal("检测函数未能发现占位符跳号")
	}
	// 正向：修复后的形态必须通过。
	fixed := "UPDATE t SET state = CASE WHEN attempts >= $6 THEN 'quarantine' ELSE 'dirty' END, reason_code = $7, next_retry_at = clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond') WHERE timezone_name = $1 AND day_start = $2 AND generation = $3 AND claim_owner = $4 AND claim_fence = $5"
	if got := sqlArgMismatch(fixed, 8); got != "" {
		t.Fatalf("修复后的形态被误判为缺陷: %s", got)
	}
}

// resolveStringExpr 把 SQL 实参解析成字符串：支持字面量，也支持同文件内的
// 字符串常量标识符（SQL 已抽成 const 时仍可校验）。
func resolveStringExpr(file *ast.File, expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.Ident:
		obj := file.Scope.Lookup(e.Name)
		if obj == nil || obj.Kind != ast.Con {
			return "", false
		}
		spec, ok := obj.Decl.(*ast.ValueSpec)
		if !ok || len(spec.Values) != 1 {
			return "", false
		}
		return resolveStringExpr(file, spec.Values[0])
	}
	return "", false
}

// TestQueueSQLPlaceholdersMatchArgs 校验 usage_stats_repair_queue.go 中每个
// *Context 调用的占位符数量与实参数量一致。
func TestQueueSQLPlaceholdersMatchArgs(t *testing.T) {
	const src = "usage_stats_repair_queue.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", src, err)
	}

	checked := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "QueryContext", "ExecContext", "QueryRowContext":
		default:
			return true
		}
		// 形如 x.QueryContext(ctx, SQL, args...) —— 实参从索引 2 开始。
		if len(call.Args) < 2 {
			return true
		}
		query, ok := resolveStringExpr(file, call.Args[1])
		if !ok || maxSQLPlaceholder(query) == 0 {
			return true
		}
		argCount := len(call.Args) - 2
		checked++
		pos := fset.Position(call.Pos())
		if msg := sqlArgMismatch(query, argCount); msg != "" {
			t.Errorf("%s:%d %s", pos.Filename, pos.Line, msg)
		}
		return true
	})

	if checked == 0 {
		t.Fatal("没有检查到任何带占位符的 SQL 调用，测试已失效")
	}
	t.Logf("已校验 %d 处带占位符的 SQL 调用", checked)
}
