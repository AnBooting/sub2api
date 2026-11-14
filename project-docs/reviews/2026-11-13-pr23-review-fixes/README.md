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

## 与 #24 的关系（2026-11-14 更新）

PR #24 曾合入 `develop`（`f887dd35b`），随后按维护者要求以 `38ce3715a` 回退。本分支因此重新合并了回退后的 `develop`（`0ea4f3704`），并**移除随回退一起消失的 #24 内容**：

| 文件 | 处理 |
| --- | --- |
| `BaseDialog.vue` | 保留本 PR 的关闭按钮视觉（`btn btn-ghost btn-icon`、`aria-label="Close dialog"`）与中文注释；移除 `data-dialog-focus-scope`、`tabindex="-1"` 与 `useFocusTrap` 接线 |
| `UserApiKeysModal.vue` | 保留本 PR 去掉 `shadow-lg` 的菜单类名；移除 `:data-dialog-focus-scope` 与对应 import |
| `frontend/e2e/**`、`playwright.config.ts`、`useFocusTrap.ts`、`BaseDialog.focusTrap.spec.ts` | 随回退删除（#24 引入的设施，不属于本 PR） |

因此本 PR 的门禁不再包含 e2e 项，命令回到 `-Scope frontend -Mode full`（4 项）。

### 合并期间暴露的编译缺陷（已修，保留）

合并 #24 期间首次跑 e2e 时暴露：`DateRangePicker.vue` 的 scoped 样式写 `@apply btn btn-primary btn-sm`，而 `btn` 定义在全局 `@layer components`，scoped 编译解析不到，dev 模式整页只剩 Vite 错误覆盖层。

修复：模板改用共享类 `class="btn btn-primary btn-sm date-picker-apply"`，删除 scoped 里的 `@apply btn ...`。该问题与本 PR 的视觉改造同源，**保留修复**；全仓复查 `@apply` 引用自定义类后无同类遗漏。

## 独立复核发现的同类残留（R2，已修）

2026-11-14 由独立复核（`opendesign/deepseek-v4.1-flash`）发现：F2 只修了 `.btn-primary`，但共享 helper `platformButtonClass()` 的深色三态仍是原数值，而它正是订阅/续费主操作按钮的来源。

| 位置 | 原值 | 原深色对比度 | 修复后 |
| --- | --- | --- | --- |
| `utils/platformColors.ts:133` | `dark:hover:bg-primary-400 dark:active:bg-primary-300` | 3.18 / 2.09:1 | `dark:bg-primary-500 dark:hover:bg-primary-600 dark:active:bg-primary-700` → 5.30 / 7.33 / 9.74:1 |
| `views/admin/RiskControlView.vue:503` | `bg-signal text-on-signal` | 3.18:1 | `bg-primary-500 text-on-signal` → 5.30:1 |
| `views/admin/ops/components/OpsDashboardHeader.vue:1128` | `bg-signal text-on-signal` | 3.18:1 | `bg-primary-500 text-on-signal` → 5.30:1 |

新增契约测试 `frontend/src/utils/__tests__/signalContrast.spec.ts`：从 `design-system.css` 读真实令牌复算 WCAG 比值，断言「深色明亮钴蓝不得压亮字」，并直接调用生产 `platformButtonClass()`。

- 修复前：4 failed / 3 passed（`r2-repro.log`）
- 修复后：7/7 通过（`r2-fixed.log`）
- 变异验证：回退三处修复 → 4 failed / 3 passed（`r2-mutation.log`），恢复后再次 7/7

## 最终验收（回退 #24 后的候选）

- 生产构建（`corepack pnpm@9 exec vite build`）：退出码 0，产物写入 `backend/internal/web/dist`。
- 生产产物浏览器矩阵（`python tools/verify-frontend-theme.py --base-url http://127.0.0.1:4173`）：**32/32 通过，退出码 0**；深色 primary default/hover/active = 5.300:1 / 7.326:1 / 9.735:1，原生 `:hover`/`:active` 均命中。见 `final-theme.json` / `final-theme.log` / `final-theme.exit.txt`。
- 前端全量门禁（`pwsh -File tools/pre-push-check.ps1 -Scope frontend -Mode full`）：**exit 0**，4 项全 PASS（vue-tsc -b / check:i18n / vitest 全量 / vite build）。见 `frontend-gate-r2.log` / `frontend-gate-r2.exit.txt`。

> 证据日志已统一为 LF 并去除行尾空白，内容与命令输出逐行一致。早期含 e2e 的 `frontend-gate*.log` 保留为历史记录，其基线（含 #24）已不适用于当前分支。
