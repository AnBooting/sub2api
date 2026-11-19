# PR #23：处理 2026-11-11 外部 review F1/F3/F4

用户提供的审核报告：E:\Downloads\REVIEW.md。报告固定 head 为 c4b0e9a805adb573cbc83f7bfdd8ba0119ef536d；本轮在同一个 dsh-47f1877f-3135 分支追加修复，不新建 PR。

## 修复

| 发现 | 根因 | 本轮处理 |
|---|---|---|
| F1 / P1 | 自适应 gray 色阶与旧 dark:gray 类叠加，使深色文字/背景再次反转 | 盘点并迁移显式深色消费者。低档文字使用 ink/ink-soft/ink-faint；显式深色背景、边框、ring 等使用固定 graphite（dark-*）色阶。源码扫描不再有 dark:*gray-*，语义令牌的未显式深色旧用法保持自动适配。 |
| F3 / P2 | main 的 sm:py-5 覆盖 pb-dock，640–1023px 只剩 20px 底部留白 | sm 断点只调整顶部 padding；工具坞显示期间保留 68px + safe-area 底部空间，lg 恢复桌面间距。 |
| F4 / P2 | 深色状态前景令牌被提亮，却仍被用于白字实底按钮 | 新增主题稳定的 *-solid / *-solid-hover / *-solid-active 角色；共享 btn-* 和自定义白字实底消费者改用它们。数字状态色阶继续服务文字、图标和半透明提示底。 |

不修改业务逻辑、接口、权限或持久化数据；不增加依赖；没有合并 PR 或部署。

## 复现与浏览器验证

回归脚本：tools/verify-frontend-theme.py。脚本加载实际构建出来的 Vue 页面，使用本地 API fixture（没有真实凭据或上游请求）：

- 有内容的 /admin/settings 安全设置卡片，检查实际 masked key 文本；
- 有账号数据的 /admin/accounts，实际点击编辑并检查 endpoint capability 选项；
- 在 375 / 640 / 768 / 1023 / 1024px 下滚到底部，比较真实保存按钮与工具坞 bounding box，并调用 Playwright click(trial=True) 验证按钮可点击；
- 使用生产 btn-danger / btn-success / btn-warning 样式，覆盖明暗主题 × default/hover/active 共 18 个状态。等待 transition 结束后读取 computed style，按 WCAG sRGB 相对亮度计算对比度。

| 项 | 修复前 | 最终生产构建 |
|---|---:|---:|
| 暗色设置文字 | 1.302:1 | 10.047:1 |
| 暗色账号选项 | 1.249:1 | 14.500:1 |
| 640/768/1023px 底部 padding | 20px，按钮被遮挡 | 68px，按钮可点击 |
| danger / success / warning 默认实底 | 2.165 / 1.932 / 1.794:1 | 6.248 / 5.225 / 5.738:1 |
| 浏览器检查 | 12/26 通过（14 项失败） | 26/26 通过，退出码 0 |

变异验证：临时把两处文字、AppLayout 的 sm:py-5、三类按钮实底恢复为旧实现，回归再次捕获上述同样的 14 项失败（退出码 1）。随后完整恢复最终源码。最终生产 bundle 已独立通过 26/26 检查。

## 工程验证

- vue-tsc -b：退出码 0。
- vitest run：435/435 文件，3457/3457 测试通过，退出码 0；localeKeyCompleteness 的 3 项检查包含在内。
- corepack pnpm@9 exec vite build：退出码 0（仅有既有的大 chunk 提示）。
- git diff --check：通过。
- 颜色角色契约已同步至 project-docs/design/01-token-contract.md。

复现命令（先启动本地生产 preview）：

~~~powershell
cd frontend
node node_modules/vite/bin/vite.js preview --host 127.0.0.1 --port 5184 --strictPort
~~~

在另一个进程从仓库根执行：

~~~powershell
python tools/verify-frontend-theme.py --base-url http://127.0.0.1:5184 --report <external-report-path>
~~~

## 边界

这次验证使用完整页面与隔离 API 数据，不能替代真实后端的端到端验收。未使用真实账号、未运行付款或上游模型。safe-area 的 env() 表达式保持在生产规则中；当前 Chromium 不支持通过 CDP 注入非零安全区，本轮没有宣称做过真实 iOS 安全区验收。源码消费面经过机械迁移与全量类型/组件测试，浏览器只对上述代表页面与按钮状态做了运行验收。
