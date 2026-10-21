//go:build unit

package service

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- 发送预算护栏的判定引擎（AST 级） ---
//
// 旧判定只按「同一个函数里有没有闸门字串」放行，于是两类绕过抓不住：
//  1. 函数里已有一处闸门，末尾或另一个分支再新增一次直发 —— 旧判定认为
//     「这个函数有闸门」而放行，新增的绕过点不会让护栏变红；
//  2. 注释里出现闸门字串 —— 旧判定把注释当成闸门。
//
// 新判定按控制流配对：一次直发上游调用只有在被一条**失败即中止**的
// allowUpstreamSend 闸门检查支配时，才算受请求发送预算约束；否则一律报出
// file:function:line。注释不参与判定（只看 AST 里的真实调用）。
//
// 判定的两种违规形态：
//   - 函数内没有任何闸门检查却有直发（漏接线）；
//   - 函数内有闸门检查，但某条直发不受该闸门支配（同函数内混用闸门与直发）。

// budgetGateCallNames 是「闸门检查」：调用失败即中止本次请求的预算校验。
var budgetGateCallNames = map[string]bool{
	"allowUpstreamSend": true,
}

// budgetGatedSendCallNames 是「受闸门约束的发送」：调用本身就是发送点（内部已扣预算）。
var budgetGatedSendCallNames = map[string]bool{
	"sendUpstreamWithBudget": true,
}

// budgetDirectSendReceivers 是直发上游调用的接收者名（HTTP 客户端）。
var budgetDirectSendReceivers = map[string]bool{
	"httpUpstream": true,
}

// budgetDirectSendMethods 是直发上游调用的方法名。
var budgetDirectSendMethods = map[string]bool{
	"Do":        true,
	"DoWithTLS": true,
}

// budgetSendViolation 是一条未受请求发送预算约束的上游发送点。
type budgetSendViolation struct {
	File   string
	Func   string
	Line   int
	Reason string
}

func (v budgetSendViolation) String() string {
	return fmt.Sprintf("%s:%s:%d（%s）", v.File, v.Func, v.Line, v.Reason)
}

// budgetGuard 是一条生效中的闸门检查。
type budgetGuard struct {
	name string
	line int
}

// budgetSendScanner 扫描单个函数体里的上游发送点。
type budgetSendScanner struct {
	file        string
	fset        *token.FileSet
	funcName    string
	hasGuard    bool
	hasGatedSend bool
	violations  []budgetSendViolation
}

// budgetCallChain 把调用目标拆成选择器链：
// s.httpUpstream.Do → ["s", "httpUpstream", "Do"]。
func budgetCallChain(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		return append(budgetCallChain(e.X), e.Sel.Name)
	case *ast.ParenExpr:
		return budgetCallChain(e.X)
	case *ast.IndexExpr:
		return budgetCallChain(e.X)
	case *ast.IndexListExpr:
		return budgetCallChain(e.X)
	}
	return nil
}

// budgetFuncHasGuardCall 判断函数体里是否存在闸门检查调用：用于区分
// 「整个函数没有闸门」与「同函数内混用闸门与直发」。
func budgetFuncHasGuardCall(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		chain := budgetCallChain(call.Fun)
		if len(chain) == 0 {
			return true
		}
		if budgetGateCallNames[chain[len(chain)-1]] {
			found = true
			return false
		}
		return true
	})
	return found
}

// budgetFuncHasGatedSend 判断函数体里是否存在「受闸门约束的发送」
// （sendUpstreamWithBudget）。同一函数里既有封装闸门发送、又有直发，就是混用：
// 直发那条绕开了封装闸门，必须报出来。
func budgetFuncHasGatedSend(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		chain := budgetCallChain(call.Fun)
		if len(chain) == 0 {
			return true
		}
		if budgetGatedSendCallNames[chain[len(chain)-1]] {
			found = true
			return false
		}
		return true
	})
	return found
}

// budgetBlockAborts 判断语句块是否以 return 中止（闸门检查失败即中止）。
// 只认块顶层的 return：嵌套在 if 里的 return 未必真的中止本次请求。
func budgetBlockAborts(block *ast.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, stmt := range block.List {
		if _, ok := stmt.(*ast.ReturnStmt); ok {
			return true
		}
	}
	return false
}

// findGateCall 在节点里找闸门检查调用。真实写法是
// `if err := allowUpstreamSend(ctx); err != nil {` —— 闸门调用在 if 的
// Init 赋值语句里，不在 Cond 里，因此两处都要看。
func (s *budgetSendScanner) findGateCall(node ast.Node) (string, int, bool) {
	if node == nil {
		return "", 0, false
	}
	var name string
	var line int
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		chain := budgetCallChain(call.Fun)
		if len(chain) == 0 {
			return true
		}
		if budgetGateCallNames[chain[len(chain)-1]] {
			name = chain[len(chain)-1]
			line = s.fset.Position(call.Pos()).Line
			found = true
			return false
		}
		return true
	})
	return name, line, found
}

// scanStmts 扫描语句块；guards 是当前生效的闸门栈。
func (s *budgetSendScanner) scanStmts(stmts []ast.Stmt, guards []budgetGuard) {
	for _, stmt := range stmts {
		switch st := stmt.(type) {
		case *ast.IfStmt:
			guards = s.scanIf(st, guards)
		case *ast.BlockStmt:
			s.scanStmts(st.List, guards)
		case *ast.ForStmt:
			s.scanStmts(st.Body.List, guards)
		case *ast.RangeStmt:
			s.scanStmts(st.Body.List, guards)
		case *ast.SwitchStmt:
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					s.scanStmts(cc.Body, guards)
				}
			}
		case *ast.TypeSwitchStmt:
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CaseClause); ok {
					s.scanStmts(cc.Body, guards)
				}
			}
		case *ast.SelectStmt:
			for _, c := range st.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					s.scanStmts(cc.Body, guards)
				}
			}
		case *ast.LabeledStmt:
			s.scanStmts([]ast.Stmt{st.Stmt}, guards)
		default:
			s.scanExprs(stmt, guards)
		}
	}
}

// scanIf 处理 if 语句：闸门检查失败即中止时，它支配同一块里后续语句与 else 分支；
// if 主体是「闸门拒绝」分支，自身不受理闸门保护。返回后续语句生效的闸门栈。
func (s *budgetSendScanner) scanIf(st *ast.IfStmt, guards []budgetGuard) []budgetGuard {
	// Init 与 Cond 在 if 判定之前求值：里面的直发不受该 if 自己的闸门保护。
	if st.Init != nil {
		s.scanExprs(st.Init, guards)
	}
	s.scanExprs(st.Cond, guards)
	inner := guards
	name, line, ok := s.findGateCall(st.Init)
	if !ok {
		name, line, ok = s.findGateCall(st.Cond)
	}
	if ok && budgetBlockAborts(st.Body) {
		inner = append(append([]budgetGuard{}, guards...), budgetGuard{name: name, line: line})
	}
	s.scanStmts(st.Body.List, guards)
	if st.Else != nil {
		switch e := st.Else.(type) {
		case *ast.BlockStmt:
			s.scanStmts(e.List, inner)
		case *ast.IfStmt:
			s.scanStmts([]ast.Stmt{e}, inner)
		}
	}
	return inner
}

// scanExprs 扫描语句里的调用表达式。
func (s *budgetSendScanner) scanExprs(node ast.Node, guards []budgetGuard) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			// 闭包继承外层闸门：闸门检查在闭包创建之前已经通过。
			s.scanStmts(x.Body.List, guards)
			return false
		case *ast.CallExpr:
			s.checkCall(x, guards)
			return true
		}
		return true
	})
}

// checkCall 判定一次调用是否构成未受约束的直发上游调用。
func (s *budgetSendScanner) checkCall(call *ast.CallExpr, guards []budgetGuard) {
	chain := budgetCallChain(call.Fun)
	if len(chain) == 0 {
		return
	}
	last := chain[len(chain)-1]
	if budgetGatedSendCallNames[last] {
		return // 受闸门约束的发送本身
	}
	if budgetGateCallNames[last] {
		return // 闸门检查本身
	}
	if len(chain) < 2 {
		return
	}
	if !budgetDirectSendReceivers[chain[len(chain)-2]] || !budgetDirectSendMethods[last] {
		return
	}
	if s.hasGatedSend {
		// 同一函数里既有封装闸门发送、又有直发：直发这条绕开了封装闸门。
		s.violations = append(s.violations, budgetSendViolation{
			File:   s.file,
			Func:   s.funcName,
			Line:   s.fset.Position(call.Pos()).Line,
			Reason: "同一函数内混用封装闸门发送与直发：这条直发绕开了 sendUpstreamWithBudget",
		})
		return
	}
	if len(guards) > 0 {
		// 被闸门支配：受请求发送预算约束。
		return
	}
	reason := "直发上游调用没有经过请求发送预算闸门"
	if s.hasGuard {
		reason = "同一函数内的闸门检查没有支配这条直发"
	}
	s.violations = append(s.violations, budgetSendViolation{
		File:   s.file,
		Func:   s.funcName,
		Line:   s.fset.Position(call.Pos()).Line,
		Reason: reason,
	})
}

// budgetUngatedSendViolations 返回文件内未受请求发送预算约束的上游发送点
// （含函数名与行号）。源码解析失败时保守报错：读不懂源码不等于源码合规。
func budgetUngatedSendViolations(file string, source string) []budgetSendViolation {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, source, parser.SkipObjectResolution)
	if err != nil {
		return []budgetSendViolation{{
			File:   file,
			Func:   "<parse>",
			Line:   0,
			Reason: "源码解析失败，无法判定发送预算约束：" + err.Error(),
		}}
	}
	var out []budgetSendViolation
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		scanner := &budgetSendScanner{
			file:         file,
			fset:         fset,
			funcName:     fn.Name.Name,
			hasGuard:     budgetFuncHasGuardCall(fn),
			hasGatedSend: budgetFuncHasGatedSend(fn),
		}
		scanner.scanStmts(fn.Body.List, nil)
		out = append(out, scanner.violations...)
	}
	return out
}

// budgetFuncBodySource 返回指定函数的源码（从声明行到函数结束）。
func budgetFuncBodySource(t *testing.T, file string, source string, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, source, parser.SkipObjectResolution)
	require.NoErrorf(t, err, "解析 %s 失败", file)
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		return source[fset.Position(fn.Pos()).Offset:fset.Position(fn.End()).Offset]
	}
	require.FailNowf(t, "豁免函数不存在", "%s 里找不到豁免函数 %s", file, name)
	return ""
}

// --- 反例用例：护栏必须抓得住的绕过形态 ---
//
// 这些用例直接调用护栏自己的判定函数（budgetUngatedSendViolations），
// 不是另写一套判定逻辑。

// TestBudgetScanCatchesMixedGateAndDirectSend 反例一：同一个函数里一处经闸门、
// 一处直发 —— 必须被报出来（函数名 + 行号）。旧判定只数「函数里有没有闸门字串」，
// 这种混用会漏过。
func TestBudgetScanCatchesMixedGateAndDirectSend(t *testing.T) {
	source := `package service

import "context"

func (s *GatewayService) sampleMixed(ctx context.Context, account *Account) error {
	if err := allowUpstreamSend(ctx); err != nil {
		return err
	}
	resp, err := s.sendUpstreamWithBudget(ctx, nil, "", account)
	if err != nil {
		return err
	}
	_ = resp
	return s.httpUpstream.Do(nil, "", account.ID, account.Concurrency)
}
`

	violations := budgetUngatedSendViolations("sample_mixed.go", source)
	require.Len(t, violations, 1, "同函数内混用闸门与直发必须被报出来")
	require.Equal(t, "sampleMixed", violations[0].Func)
	require.Equal(t, 14, violations[0].Line)
	require.Contains(t, violations[0].Reason, "混用")
}

// TestBudgetScanCatchesDirectSendBeforeGate 反例二：闸门检查在直发**之后**
// （或只覆盖了另一个分支）时，那条直发并没有被闸门保护，必须被报出来。
func TestBudgetScanCatchesDirectSendBeforeGate(t *testing.T) {
	source := `package service

import "context"

func (s *GatewayService) sampleDirectFirst(ctx context.Context, account *Account) error {
	if err := s.httpUpstream.Do(nil, "", account.ID, account.Concurrency); err != nil {
		return err
	}
	if err := allowUpstreamSend(ctx); err != nil {
		return err
	}
	return nil
}
`

	violations := budgetUngatedSendViolations("sample_direct_first.go", source)
	require.Len(t, violations, 1, "闸门之前的直发必须被报出来")
	require.Equal(t, "sampleDirectFirst", violations[0].Func)
	require.Equal(t, 6, violations[0].Line)
}

// TestBudgetScanIgnoresGateMentionInComment 反例三：注释里出现闸门字串不算闸门
// （旧判定会把注释当成闸门而漏过直发）。
func TestBudgetScanIgnoresGateMentionInComment(t *testing.T) {
	source := `package service

import "context"

func (s *GatewayService) sampleComment(ctx context.Context, account *Account) error {
	// 这里曾经调用 allowUpstreamSend(ctx)，现在注释掉了。
	return s.httpUpstream.Do(nil, "", account.ID, account.Concurrency)
}
`

	violations := budgetUngatedSendViolations("sample_comment.go", source)
	require.Len(t, violations, 1, "注释里的闸门字串不能算闸门")
	require.Equal(t, "sampleComment", violations[0].Func)
}

// TestBudgetScanAcceptsGateGuardedSend 正向对照：闸门检查支配着那条直发时
// （现有 doOpenAIUpstream 的写法），不算违规。
func TestBudgetScanAcceptsGateGuardedSend(t *testing.T) {
	source := `package service

import "context"

func (s *GatewayService) sampleGuarded(ctx context.Context, account *Account) error {
	if err := allowUpstreamSend(ctx); err != nil {
		return err
	}
	return s.httpUpstream.Do(nil, "", account.ID, account.Concurrency)
}
`

	require.Empty(t, budgetUngatedSendViolations("sample_guarded.go", source),
		"闸门支配的直发不受约束")
}

// TestBudgetScanAcceptsGateGuardedSendInBranch 正向对照：闸门检查在 if 分支里
// 且失败即中止时，它支配分支体里的直发。
func TestBudgetScanAcceptsGateGuardedSendInBranch(t *testing.T) {
	source := `package service

import "context"

func (s *GatewayService) sampleGuardedBranch(ctx context.Context, account *Account) error {
	if account != nil {
		if err := allowUpstreamSend(ctx); err != nil {
			return err
		}
		return s.httpUpstream.Do(nil, "", account.ID, account.Concurrency)
	}
	return nil
}
`

	require.Empty(t, budgetUngatedSendViolations("sample_guarded_branch.go", source),
		"分支内被闸门支配的直发不算违规")
}
