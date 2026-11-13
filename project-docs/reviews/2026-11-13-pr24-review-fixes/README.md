# PR #24 F1/F3：弹窗焦点作用域与不可交互边界

本轮处理 `E:/Downloads/REVIEW (1).md` 的 F1、F3 与空容器持焦。起始 head `e1b6d74a3`，本轮改动基于已合入 `origin/develop`（`9a0088582`，PR #22）的分支。Git 提交、推送与合并由 Lead 执行。

## 根因

1. **F1**：`useFocusTrap` 只收集 `.modal-content` 的 DOM 后代。`UserApiKeysModal` 把分组菜单 `Teleport to="body"`，菜单在弹窗面板之外，Tab 到弹窗最后一个按钮即被强制绕回，永远进不去菜单；即便脚本移入焦点，下一按也会被当作背景焦点拉回。
2. **F3**：`isFocusCandidate` 只看元素自身的 `computedStyle.display`。祖先 `display:none` 不会让子元素的计算值变成 `none`，隐藏面板里的按钮因此进入候选列表，错误地成为"末项"。正向 Tab 在真实末项处不拦截，浏览器直接把焦点移出弹窗；反向 Tab 则尝试聚焦不可见节点、停在原地。`hidden` 祖先、`visibility:hidden`、`inert`、`fieldset disabled`、负 `tabindex` 同样被误判。
3. **空容器**：面板内没有可聚焦元素时只 `preventDefault`，焦点留在原处（可能在背景页面）。

## 修复

- `frontend/src/composables/useFocusTrap.ts`
  - 候选规则改为按"能否参与顺序键盘导航"判定：`tabIndex < 0`、`:disabled`、祖先 `[hidden]`/`[inert]`/`[aria-hidden="true"]`、`visibility:hidden|collapse`、任一祖先 `display:none` 或 `content-visibility:hidden` 均排除；再用 `checkVisibility()` 兜底（jsdom 无布局 API 时跳过）。
  - 新增 `getDialogFocusScope()`：portal 内容通过触发控件最近的 `[data-dialog-focus-scope]` 继承所属弹窗，不改变定位与堆叠上下文。
  - `useFocusTrap` 把面板与同作用域的 portal 根合并成一个有序列表；跨根移动显式接续（DOM 顺序上两段之间可能隔着背景控件）。
  - 无候选时把焦点交给面板本身。
- `frontend/src/components/common/BaseDialog.vue`：面板加 `data-dialog-focus-scope` 与 `tabindex="-1"`；初始聚焦在无候选时回落到面板。
- `frontend/src/components/admin/user/UserApiKeysModal.vue`：分组菜单 Teleport 根通过触发按钮继承作用域。

## 回归与变异

新增用例都调用生产代码（生产 `BaseDialog`、生产 `UserApiKeysModal` + Pinia + 最小 i18n），不是重写逻辑。

| 检查 | 修复前 | 修复后 |
| --- | --- | --- |
| `BaseDialog.focusTrap.spec.ts`（含 6 个不可交互边界参数化） | 6 failed / 7 passed | 13/13 pass（目录合计 22/22） |
| e2e「隐藏祖先与不可交互控件」 | FAIL（Tab 后 `#inner-first` 未获得焦点） | PASS |
| e2e「空弹窗把焦点留在面板」 | FAIL（`.modal-content` 未获得焦点） | PASS |
| e2e「真实分组菜单属于弹窗 Tab 环」 | FAIL（Tab 后未进入菜单） | PASS |

变异验证：同时回退三处修复（候选过滤改为恒真、去掉 menu 的 scope 属性、去掉面板 `tabindex="-1"`），重新跑上述 3 条 e2e → 3 failed，退出码 1；恢复后 11/11 通过。见 `mutation-browser.log`。

## 证据文件

| 文件 | 内容 |
| --- | --- |
| `repro-unit.log` | 修复前单测：6 failed / 7 passed，退出码 1 |
| `repro-browser.log` | 修复前 3 条浏览器场景全部失败，退出码 1 |
| `unit-fixed.log` | 修复后 BaseDialog 目录 5 files / 22 tests 通过 |
| `browser-fixed.log` | dialog.spec 11/11 通过 |
| `mutation-browser.log` | 回退修复后 3 条失败 |
| `browser-final.log` | 恢复后全量 e2e（dialog/select/date-range）通过 |
| `typecheck.log` | `vue-tsc -b` 无输出、退出码 0 |
| `frontend-gate.log` / `frontend-gate.exit.txt` | `pwsh -File tools/pre-push-check.ps1 -Mode full -Scope frontend -RunE2E` 原始输出与退出码 |

## 未覆盖

- 未做屏幕阅读器人工验收，也未在真实 iOS Safari/Android 上验证。
- e2e 的 API 为本地路由 fixture（一个用户、一条 Key、一个分组），未调用真实后端，也未覆盖真实扣费。
- 全栈 e2e（真实登录）仍需 PostgreSQL/Redis/Docker，本轮未执行。
