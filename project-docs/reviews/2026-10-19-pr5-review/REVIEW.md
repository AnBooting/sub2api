# AnBooting/sub2api PR #5 Review

- 日期：2026-10-19（Asia/Singapore）
- PR：https://github.com/AnBooting/sub2api/pull/5
- 标题：audit: 全量审查修复（安全/健壮性/前端 API 类型）与审计文档
- Base：`d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`
- Head：`4da44953062b92407f2fcb50cebecbac9747dc64`
- 结论：发现两项可复现的 P2 回归，建议修正后合并。

## 1. [P2] SVG 片段消毒导致用量明细图标全部消失

位置：`frontend/src/views/KeyUsageView.vue:260`

变更把 `v-html="row.iconSvg"` 改为 `v-html="sanitizeSvg(row.iconSvg)"`。但是第 689–692 行的 SHIELD、CALENDAR、DOLLAR、CHECK 常量都是 SVG 子节点片段，如 `<path .../>`、`<rect .../>`、`<polyline .../>`，没有外层 `<svg>`。

现有 `frontend/src/utils/sanitize.ts` 使用 DOMPurify 的默认 HTML 解析命名空间和 SVG profile。该配置会将这些游离 SVG 子节点移除。实际四种图标传入后均得到空字符串；外层 `<svg>` 仍在，但不再有可见图形。

触发条件：查询 API Key 用量后显示配额、到期日、订阅类型等明细行。所有引用这四个常量的明细行均受影响。

复现证据：此前直接调用生产 `sanitizeSvg` 的四个临时用例全部失败，结果均为空字符串。本包进一步使用生产页面的实际 SVG 模板进行挂载，覆盖模板绑定和消毒函数之间的连接。

建议：采用能正确处理 SVG 片段命名空间的消毒方式，例如先包装成完整 SVG，消毒后提取安全子节点，或使用组件直接渲染图标。保留消毒保护，并补充图标节点可见性回归测试。

代码：https://github.com/AnBooting/sub2api/blob/4da44953062b92407f2fcb50cebecbac9747dc64/frontend/src/views/KeyUsageView.vue#L260

## 2. [P2] git grep 参数顺序使影响分析命令报错

位置：`tools/repo-map.mjs:127`

新参数为：

```javascript
['grep', '-n', '-I', '-F', '-e', '--', query]
```

`-e` 会消费紧随其后的 `--` 作为搜索模式，真正的搜索词则被 Git 当作 revision 或 path。普通符号名会触发 ambiguous argument 错误；若恰好是有效路径，还可能产生搜索 `--` 的错误结果。

复现命令：

```powershell
node tools/repo-map.mjs impact pricing_snapshot
```

实际结果：退出码 1，错误为 `fatal: ambiguous argument 'pricing_snapshot': unknown revision or path not in the working tree`。此前本地旧版本工具同一查询正常返回 127 处匹配；这一数量属于当时本地检出的结果，不代表固定 PR base 的精确匹配数。

建议恢复参数位置：

```javascript
['grep', '-n', '-I', '-F', '-e', query, '--']
```

## 验证记录与边界

交付包自身已重新运行验证：

| 验证源码 | 图标模板测试 | CLI 测试 | run.ps1 退出码 |
| --- | --- | --- | --- |
| 本地旧版 `1d86c6780de0092ea98dc6f20486f76bdc660cc6` | 4 项通过 | 1 项通过 | 0 |
| PR #5 `4da44953062b92407f2fcb50cebecbac9747dc64` | 4 项失败，实际子节点数均为 0 | 1 项失败，ambiguous argument | 1 |

PR 版本验证使用从固定提交提取的前端与工具源码，并复用本机依赖。此临时源码目录位于现有 Git 仓库内，CLI 的 Git 元数据与跟踪文件搜索范围由外层仓库提供；两个目标生产文件均来自上述 PR 提交。两次运行均确认临时测试文件自动清理。

原审查已完成：

- 后端五个包的定向 unit 测试通过：config、pkg/antigravity、service、handler、handler/admin。
- 前端 `vue-tsc -b` 通过。
- shop API、locale key 完整性、adminCompliance reset 三组既有测试，共 16 项通过。
- `node --test tools/repo-intelligence.test.mjs`：11 项通过。
- 新增图标隔离复现测试：4 项预期失败。
- CLI 生产命令：已复现退出码 1；未修改生产实现。

后端定向命令（在 `backend/` 中使用匹配 go.mod 的本地 Go）：

```powershell
go test -p 4 -tags=unit ./internal/config ./internal/pkg/antigravity ./internal/service ./internal/handler ./internal/handler/admin -run 'TestArtexGuard|TestValidateDingTalkConfig|TestAdminCompliance|TestAcceptAdminCompliance|Test.*CustomErrorCodes|Test.*SafeHeader|TestClient_.*LoadCodeAssist|TestClient_.*OnboardUser|TestDingTalkClient|TestBuildDingTalkUpstreamClaims|TestGroupHandlerEndpoints|TestProxyHandlerEndpoints|TestRedeemHandlerEndpoints|TestValidateDataProxy|TestImportData' -count=1
```

部分单测使用本地 httptest 服务，沙箱曾禁止回环网络连接；在允许本地连接的环境重跑后通过。未运行全量后端/前端套件，未做生产请求、合并或部署。未将既有 Live 计费、refresh token 等问题列成本 PR 新引入的缺陷。
