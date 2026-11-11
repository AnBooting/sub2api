# 02 · 执行简报（所有成员共用）

## 项目位置
`F:\sub2\.agents\worktrees\dsh-47f1877f-3135` —— git worktree，分支 `dsh-47f1877f-3135`。不要切分支、不要 commit、不要 push。
前端目录 `frontend/`，命令都在仓库根或 frontend 下执行。

## 必须先读的冻结文档（优先于任何个人审美）
- `project-docs/design/00-philosophy.md` —— 设计哲学「配线图 / Patch Diagram」
- `project-docs/design/01-token-contract.md` —— 令牌契约（硬规则）
- 参考范式：`_reference/anthropic-skills/frontend-design.md`（最相关）、`canvas-design.md`、`algorithmic-design.md`→ 实为 `algorithmic-art.md`

## 已完成的基座（只消费，不要改）
| 文件 | 作用 |
|---|---|
| `frontend/src/styles/design-system.css` | 唯一令牌源。CSS 变量：`--c-gray-*`/`--c-graphite-*`/`--c-cobalt-*`/`--c-oxide-*`/`--c-ochre-*`/`--c-moss-*`/`--c-lilac-*`/`--c-azure-*`；语义 `--paper`/`--surface`/`--ink`/`--rule`/`--signal`；度量 `--radius-cut`/`--touch-min`/`--measure`；动效 `--motion-*` |
| `frontend/tailwind.config.js` | 封闭调色板：只注册 `gray` / `dark`(=graphite) / `primary`(=cobalt) / `cobalt` / `success`(=moss) / `warning`(=ochre) / `danger`(=oxide) / `accent`(=azure) / `lilac` / `paper` / `surface` / `ink` / `rule` / `signal` / `white`(=`--surface`) / `black`(=`--ink`)。**Tailwind 默认的 emerald/green/teal/lime/amber/yellow/orange/red/rose/pink/blue/indigo/sky/cyan/purple/violet/fuchsia/slate/zinc/neutral/stone 已删除 —— 写了不报错但完全没有样式。** |
| `frontend/src/style.css` | 语义类层：`.panel .card .panel-header .panel-body .panel-foot .title-block .title-block-title .btn* .input .input-label .table .table-scroll .badge* .dropdown .dropdown-item .modal-* .toast .sidebar* .mobile-dock* .empty-state .stat-* .tabs .tab* .progress .switch .code* .pb-dock .safe-top .safe-bottom` |
| `frontend/src/styles/chartPalette.ts` | 图表序列色唯一来源：`chartPalette()` → `{ series[], grid, axis, ink, inkSoft, surface }`；`withAlpha(color, a)` |
| `frontend/src/composables/useTheme.ts` | 主题唯一入口：`isDarkTheme`、`toggleTheme()`、`applyTheme()`、`initTheme()`。**不要再自己写 localStorage + classList.toggle**，否则 theme-color 不同步、手机浏览器栏会裂色 |

## 硬规则（违反即缺陷，对抗性审查会逐条列）
1. `.vue/.ts` 不得写死颜色字面量（`#rrggbb` / `rgb(` / `hsl(`）。图表从 `chartPalette.ts` 取；支付渠道品牌色是唯一例外。
2. 禁止：渐变 `bg-gradient-*`、容器上的 `backdrop-blur`、装饰性 `shadow-sm/md/lg/xl/2xl/inner`。只有真正浮起的层才用 `shadow-float`。
3. 圆角只有 2/3/4/5/6px 与 `full`（头像/开关/圆点）。不得出现 `rounded-2xl/3xl/4xl`。
4. 禁止：标题里单词变色/斜体强调、全大写标签（`uppercase`）、每段上方的小字眉标、卡片逐张淡入上滑、hover 发光/位移/缩放、装饰性 `animate-pulse/bounce/ping`（skeleton 与真实进行中状态除外）。
5. 移动端不是降级版：同一套纸与线，只改编排。触控目标 ≥44px；页面不得横向溢出（宽表格用 `.table-scroll`）；弹层用 `.modal-*`（<640px 自动底部浮层）；输入框字号 ≥16px。
6. 文案：说人话、动词开头、按钮名与结果名一致（「保存」→「已保存」），不要营销腔。

## 允许的例外
- `design-system.css` 自身（令牌定义）
- 图表配置色（但必须经 `chartPalette()`）
- 支付渠道品牌色
- 覆盖层（弹层/抽屉/吸顶条/底部工具坞）的 `backdrop-blur`
- `skeleton` 的 `animate-pulse`

## 验证（每条都必须真跑，不接受「应该没问题」）
```powershell
cd frontend
.\node_modules\.bin\vue-tsc.cmd -b            # 必须 EXIT=0
node node_modules/vitest/vitest.mjs run         # 必须全绿（除被派出的预期红）
node node_modules/vite/bin/vite.js build        # 必须 built in
```
依赖已装好（pnpm 9 + node_modules 就绪）。等价入口：`corepack pnpm@9 run typecheck / test:run / build`。

## 违规扫描脚本（自己跑，不要只信 Lead 的清单）
```powershell
node C:\Users\ASUS\AppData\Local\Temp\dsh-audit.mjs "<repo>\frontend\src" "<repo>\.backup\audit-<你的名字>.json"
```
输出 11 项计数：`gradient / backdrop-blur / deco-shadow / rounded-xl+ / uppercase-label / hex-literal / rgb-literal / old-family / hover-move / hover-scale / animate-pulse-deco`，明细在 json 里（文件:行 | 内容）。**你写域内这 11 项都要为 0**（除上面的例外）。

## 写域纪律
- 只改自己写域内的文件；跨域需求写进报告交 Lead 派活。
- 同一文件同阶段只能有一个写者；遇到 `FS_STALE_VERSION` 先重读再合并，不要盲目覆盖。
- 不改 `tailwind.config.js`、`src/style.css`、`src/styles/**`、`src/main.ts`（Lead 写域）。
- 不整目录删除既有文件；不改 props/emits/slots 等对外契约。
