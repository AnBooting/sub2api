# 01 · 令牌契约（冻结）

本文件是**实现契约**，不是建议。任何成员（含 Lead）改动视觉时按此执行；违反即为缺陷，对抗性审查会逐一列出。

## 1. 颜色

| 令牌 | 用途 | 禁止 |
|---|---|---|
| `gray-*` / `dark-*`（=`graphite-*`） | 纸面、痕量线、墨 | 不做强调色 |
| `primary-*` / `cobalt-*` | **唯一信号色**：主操作、当前页、选中项、实时读数 | 不得大面积铺底（>15% 面积），不得用于装饰性渐变 |
| `success-*`（moss） | 通路/成功/在线 | 不用作强调 |
| `warning-*`（ochre） | 测量/提醒/阈值 | 不用作强调 |
| `danger-*`（oxide） | 断线/错误/删除 | 不用作强调 |
| `lilac-*` | **仅**图表第 6 序列 | 任何界面元素都不许用 |
| `emerald/``teal/``cyan/``sky/``blue/``indigo/``violet/``purple/``fuchsia/``pink/``rose/``orange/``yellow/``lime/``slate/``zinc/``neutral` | **已删除**（Tailwind 默认调色板不再注册，写了就无效） | — |
| `paper` / `surface` / `ink` / `rule` / `signal` | 语义色，自动随明暗切换 | — |

**硬规则**

1. `.vue / .ts` 里不得出现 `#rrggbb` / `rgb(` / `hsl(` 字面量（图表配置、支付品牌色除外，见第 6 条）。
2. 渐变只允许「同一色阶内 2 档」或「signal → signal」，且总面积 < 5%。禁止 mesh/radial 装饰背景。
3. 玻璃/毛玻璃（`backdrop-blur`）只允许用在**覆盖层**（弹层、抽屉、吸顶条），不得用于卡片。
4. 阴影只有 `shadow-float` / `shadow-float-sm` 两个，且只给真正悬浮的层。

### 明暗主题中的颜色角色（PR #23 review 修正）

- `gray-*` 随主题适配，供没有显式深色变体的旧用法使用。显式 `dark:` 的文字改用 `ink / ink-soft / ink-faint`，背景、边框、ring 改用固定的 `dark-*`（graphite）色阶；不再叠加 `dark:*gray-*`，避免二次反转。
- `success / warning / danger` 的数字色阶仍可用于状态前景与半透明提示底。白字实底控件必须使用 `*-solid`、`*-solid-hover`、`*-solid-active`，这些底色不随主题提亮。
- `.btn-danger / .btn-success / .btn-warning` 已统一接入实底令牌。新增自定义实底操作也须用同一令牌，不再把数字 500/600/700 档直接放在 `text-on-signal` 后面。
- `AppLayout` 在工具坞隐藏前仅响应式调整顶部间距；`pb-dock` 的底部空间须覆盖所有小于 1024px 的断点，继续包含 `env(safe-area-inset-bottom)`。

浏览器回归：`python tools/verify-frontend-theme.py --base-url <local-url>`。脚本使用真实设置页和编辑账号弹窗、隔离 API fixture，并检查明暗主题按钮的默认/悬停/按下状态与 375/640/768/1023/1024px 底部操作。

## 2. 形

- 圆角只有 2px（基准）/3px/4px/5px/6px 与 `full`（仅用于头像、开关、圆点）。**不得**出现 `rounded-2xl` 以上的视觉膨胀（映射已压到 6px，写错也不会膨胀）。
- 层次靠 1px 痕量线（`border-rule`）+ 留白。**不得**用阴影表达层级。
- 分隔一律 1px；不允许双线、不允许粗边。

## 3. 字

- 一套字族 Archivo（`font-sans`），读数用 `font-read`（等宽数字）。
- 所有数值单元格必须 `tabular-nums`；金额、百分比、时间右对齐。
- 禁止：标题内单词变色/斜体强调；全大写标签；每段上方的小字眉标。
- 允许的小字：**读数注记**（如 `0.42 s`、`×3`），用 `font-read text-[11px]`，因为它是测量值。
- 正文 `max-w-measure`（68ch）；行高正文 1.6，读数 1.35。

## 4. 动

- 页面进入：仅一次痕量线绘制（`animate-trace-draw`，480ms）。
- 交互动效只回应操作，时长 ≤200ms；必须 `prefers-reduced-motion` 退化为无动画。
- 禁止：卡片逐张淡入上滑、hover 发光、滚动视差、无限循环脉冲装饰。

## 5. 移动端（硬约束，不是加分项）

1. **同一套视觉**：手机不换皮肤、不降级；仅编排改变。
2. **触控目标 ≥44×44px**：所有可点元素（含表格行内按钮、图标按钮、分页）。
3. **无横向溢出**：`document.documentElement.scrollWidth <= clientWidth + 1` 在 320/375/414/768 四个宽度下都必须成立。宽表格用 `.table-scroll` 容器横向滚动，页面本身不许滚。
4. **无遮挡**：固定在底部的工具坞不得盖住最后一行内容（容器留 `pb` 等于坞高 + 安全区）。
5. **键盘/视口**：输入框字号 ≥16px（避免 iOS 聚焦缩放）；弹层在手机上必须可整屏滚动。
6. **可读性**：正文在 320px 下 ≥14px；任何 `text-[10px]` 只能用于纯装饰刻度。
7. 手势：抽屉可滑出/滑回；底部工具坞只有 3–5 个入口。

## 6. 例外（允许写死色值的地方）

- Chart.js 配置里的数据序列色（必须从 `@/styles/chartPalette` 取，不允许就地写死）。
- 支付渠道品牌色（Stripe / Airwallex / 支付宝 / 微信），保留其品牌识别。
- 自定义站点 logo / 用户上传图片。

## 7. 兼容层

- `frontend/src/style.css` 的 `@layer components` 保留**全部既有类名**（`.card` `.btn` `.input` `.table` `.badge` …）作为语义类，视觉全部换新。这样上千个既有页面文件自动继承新设计，而不需要逐文件重写。
- 任何仍然写在 `.vue` 里的旧视觉写法（`bg-gradient-to-r from-primary-500`、`backdrop-blur`、`shadow-lg shadow-primary-500/25`）都由**页面组成员**清理。
## 8. v2 补充：编辑体字阶与排版件（覆盖前文的「只有一套字族」）

v1 的「只允许一套字族」已作废。v2 是**两族分工**：

| 角色 | Tailwind 名 | 字族 |
|---|---|---|
| 展示 / 标题 | font-display | Newsreader（衬线，光学尺寸轴） |
| 界面 / 正文 / 读数 | font-sans、font-read | Archivo（无衬线，宽度轴） |

**必须用衬线的地方**
- h1 / h2 自然继承（style.css 的 base 层已切）
- .title-block-title、.title-block-display、.readout-display、.lead、.pull-quote
- 首页 hero 的主标题、品牌名

**必须用无衬线的地方**
- 一切按钮、表单控件、表格单元格、标签、徽章、导航
- h3 / h4（小字号衬线会糊）

**衬线使用上限**：同一屏最多两处衬线（一句话 + 一个读数）。超过就是滥情。

**新增排版件白名单**（在 style.css @layer components 内，直接用，不要复制到 .vue）

| 类 | 用途 |
|---|---|
| .lead | 页面引导段：比正文大一号、行高 1.55、max-width 58ch |
| .drop-cap | 首字下沉；**只允许**用于叙述型页面（首页 / 关于 / 落地页），后台表格页禁止 |
| .readout-display | 衬线大读数（tabular + lining figures） |
| .pull-quote | 左侧 2px 信号色竖线的引文块 |
| .rule-soft | 比 1px 更轻的分段线 |
| .lift | 浮层：暖纸底 + 1px 线 + 暖影，替代 shadow-lg |
| .panel-inset | 凹陷面：表头、代码块、空槽位 |

**暖影代替纯黑影**：所有阴影必须是 rgb(58 53 48 / …) 而非 rgb(0 0 0 / …)。纯黑投影压在暖纸上会发灰发脏。
