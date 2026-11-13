# PR #23 F2：主按钮深色交互态对比度修复

本轮仅处理 `E:/Downloads/REVIEW (1).md` 的 F2。起始分支 `dsh-47f1877f-3135`，起始 HEAD `8b5bfc8d7f86acda98b0d94ed56ff926a9b0b15e`。由 Lead 统一处理提交及后续 Git 操作，本记录不代表已提交、推送或合并。

## 原因与修复范围

深色 `.btn-primary` 的亮字使用 `--on-signal`，原 hover/active 却将背景提亮为 cobalt 400/300。默认 cobalt 500 已满足 4.5:1。最小修复只将深色 hover/active 改为既有 cobalt 600/700，并同步注释；不新增或改动色板令牌。

浏览器回归沿用 [verify-frontend-theme.py](../../../tools/verify-frontend-theme.py) 的真实生产页面、fixture API、共享按钮类及状态矩阵；`BUTTON_KINDS` 同时用于创建探针与遍历检查，新增 primary 后为明暗 × 四类 × 三态 24 项，另有原来的 8 项页面/移动端/错误检查，总计 32 项。鼠标真实触发 hover/down/up，等待 250ms（生产 transition 为 150ms）后读取 computed style，并断言 `:hover` / `:active` 原生状态。

`git blame` 将问题规则归于 `abc890d81`（PR #23 视觉改造）；在本轮启动时 `git merge-base --is-ancestor abc890d81 origin/develop` 返回 1，因此未把它当成已合入集成分支的旧缺陷。

## 环境与执行方式

全部在本机 worktree 执行，无远程 SSH、真实账号或模型调用。Node 24.18.0、pnpm 9.15.9、Vite 5.4.21、Python Playwright 1.62.0、Chromium 151.0.7922.34。

```powershell
# frontend 目录：构建真正的 production bundle
corepack pnpm@9 exec vite build
corepack pnpm@9 exec vite preview --host 127.0.0.1 --port 4173 --strictPort

# 仓库根：浏览器回归（phase = baseline / fixed / mutation / final）
python tools/verify-frontend-theme.py --base-url http://127.0.0.1:4173 --report project-docs/reviews/2026-11-13-pr23-review-fixes/<phase>-theme.json
```

生产构建输出到此 worktree 的 `backend/internal/web/dist`，由 Vite preview 供给浏览器。每阶段 JSON 记录实际 computed RGB、比值与原生状态；同名 `.log` 保留完整命令输出，`.exit.txt` 保存命令原始退出码。资产记录保存全局 CSS 的 SHA256，避免把开发样式或陈旧 bundle 误记为最终产物。

## 已确认的失败基线

先仅修改浏览器矩阵，保留原 CSS，再构建和运行回归：构建退出 0；浏览器退出 1，30/32 通过。

| 深色 primary 状态 | 前景 RGB | 原背景 RGB | 对比度 | 结果 |
| --- | --- | --- | --- | --- |
| default | 255,253,250 | 74,100,195 | 5.300410:1 | PASS |
| hover | 255,253,250 | 115,140,219 | 3.180414:1 | FAIL |
| active | 255,253,250 | 158,177,233 | 2.085077:1 | FAIL |

证据：[baseline-theme.json](baseline-theme.json)、[完整浏览器输出](baseline-theme.log)、[生产构建输出](baseline-build.log)、[CSS 哈希](baseline-assets.json)。其余 30 项均通过，包括浅色 primary 三态、其余三类按钮所有状态、暗色真实页面文字和移动端末尾操作。

## 修复、变异与门禁

修复后重新构建：退出 0；同一浏览器回归退出 0，32/32 通过。深色 primary default/hover/active 分别为 5.300410:1、7.325565:1、9.735498:1，hover/active 背景分别为 RGB(56,78,168) / RGB(44,61,135)。证据：[fixed-theme.json](fixed-theme.json)、[完整浏览器输出](fixed-theme.log)、[生产构建输出](fixed-build.log)、[CSS 哈希](fixed-assets.json)。

变异验证：仅把修复的深色 hover/active 两个背景类回退为 cobalt 400/300（测试不变），重新生产构建退出 0；浏览器退出 1，30/32 通过，再次精确失败于上述两态，数值与基线一致。随后已恢复 600/700。证据：[mutation-theme.json](mutation-theme.json)、[完整浏览器输出](mutation-theme.log)、[生产构建输出](mutation-build.log)、[CSS 哈希](mutation-assets.json)。

## 与 #24 合并后的同步（2026-11-14）

`origin/develop` 已含 PR #22 与 PR #24（`f887dd35b`）。合入本分支时出现两处冲突，按语义合并、不整块取值：

| 文件 | 冲突内容 | 合并结果 |
| --- | --- | --- |
| `BaseDialog.vue` | 本 PR 的关闭按钮视觉与中文注释 vs #24 的面板作用域属性与 `useFocusTrap` 接线 | 保留 `btn btn-ghost btn-icon`、`aria-label="Close dialog"`；并入 `data-dialog-focus-scope`、`tabindex="-1"` 与 `useFocusTrap` |
| `UserApiKeysModal.vue` | 本 PR 去掉 `shadow-lg` 的菜单样式 vs #24 的作用域属性 | 保留本 PR 的类名，并入 `:data-dialog-focus-scope` |

**必须同步的断言**：本 PR 把关闭按钮的 `aria-label` 改为 `Close dialog`，而 #24 的 e2e 用 `Close modal` 定位该按钮。合并后已把 `frontend/e2e/specs/dialog.spec.ts` 的断言改为 `Close dialog`；不改会留下一条永远找不到元素的假绿/假红。

### 合并后暴露的编译缺陷（已修）

合并 #24 后首次跑 `-RunE2E` 门禁，`date-range.spec.ts` 5 条全部失败：Vite 报
`The `btn` class does not exist ... @apply btn btn-primary btn-sm`，页面只剩错误覆盖层。
本 PR 早先把 `.date-picker-apply` 改成 `@apply btn btn-primary btn-sm`，而 `btn` 定义在全局 `@layer components`，scoped 样式里的 `@apply` 解析不到它。

修复：模板改用共享类 `class="btn btn-primary btn-sm date-picker-apply"`，删除 scoped 里的 `@apply btn ...`。修复后 `date-range.spec.ts` **5/5 通过**（`date-range-verify.log` / `.exit.txt`）。

门禁首轮还因本工作树未安装合入的 `@playwright/test` 而失败（`error: unknown command 'test'`），`corepack pnpm@9 install --frozen-lockfile` 后复跑。两次失败证据分别保留为 `frontend-gate-attempt1-e2e-missing-deps.*` 与 `frontend-gate-attempt2-date-range-apply.*`。

## 最终验收（合并 #24 后的候选）

- 生产构建（`corepack pnpm@9 exec vite build`）：退出码 0，产物写入 `backend/internal/web/dist`。见 `final-build.log` / `final-build.exit.txt`。
- 生产产物浏览器矩阵（`python tools/verify-frontend-theme.py --base-url http://127.0.0.1:4173`）：**32/32 通过，退出码 0**；深色 primary default/hover/active = 5.300:1 / 7.326:1 / 9.735:1，且原生 `:hover`/`:active` 均命中。见 `final-theme.json` / `final-theme.log` / `final-theme.exit.txt`。
- 前端全量门禁（`pwsh -File tools/pre-push-check.ps1 -Scope frontend -Mode full -RunE2E`）：**exit 0**，5 项全 PASS（vue-tsc -b / check:i18n / vitest 全量 / vite build / e2e）。见 `frontend-gate.log` / `frontend-gate.exit.txt`。

> 证据日志已统一为 LF 并去除行尾空白，内容与命令输出逐行一致。
