# PR #7 Review

日期：2026-10-19（Asia/Singapore）。审查 `AnBooting/sub2api#7`，head `067d74dbcef41e482507c0db9fcb90b8625cda15`，base `d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`。共 105 个前端文件，未涉及后端。

发现四项 P2 回归，建议修复后合并。

## R1 [P2] 持久页面 transform 改变固定弹窗的定位参照

位置：`frontend/src/components/layout/AppLayout.vue:10`、`frontend/src/style.css:899-900,973-975`。

AppLayout 为整个 main 添加 `motion-page`；动画使用 `both` 填充模式，结束帧仍是 `transform: translateY(0)`。身份矩阵同样是非 none 的 transform，会为 fixed 后代建立包含块，并使主内容成为独立层叠上下文。

个人资料页里的 `TotpSetupModal`、`TotpDisableDialog`、Passkey 删除确认使用内联 `fixed inset-0`，没有 Teleport 到 body。因此这些弹窗会相对 main 定位，无法覆盖整个视口，还会随主内容滚动。在较高的资料页中，居中的弹窗会落到首屏外。

浏览器复现使用生产构建 CSS、实际 main 标签和实际 TotpSetupModal 根标签，在 1280×720 视口、1600px 高的内容区：

- 动画后 `main.transform = matrix(1, 0, 0, 1, 0, 0)`。
- 弹层坐标为 `(240,56)`，尺寸 `1032×1600`；测试面板顶部 y=796，已超出视口。
- 仅移除动画 class 后，transform=none；弹层恢复为视口尺寸，面板顶部 y=300。

建议避免在整页容器上持久保留 transform 动画 effect，或统一将这类弹窗 Teleport 到 body。不能只依赖把弹窗 z-index 调大。

## R2 [P2] 展开侧栏 256px，正文只偏移 240px

位置：`frontend/src/components/layout/AppLayout.vue:7`。

正文由 `lg:ml-64` 改成 `lg:ml-60`，但 AppSidebar 展开时仍绑定 `w-64`。虽然 `.sidebar` 组件样式改成 w-60，后面的 Tailwind w-64 utility 会覆盖它。

在桌面断点且侧栏未折叠时，实际侧栏宽度为 16rem，正文 margin-left 为 15rem，正文与顶栏左侧 1rem 被 z-40 侧栏覆盖。自动测试与浏览器都确认宽度 256px、正文起点 x=240px。折叠状态的 72px 不受影响。

建议统一侧栏展开 utility 与正文偏移。

## R3 [P2] 首页外部文档卡片被当成站内路由

位置：`frontend/src/views/HomeView.vue:87-90,199`。

未登录时，primaryEntries 将 docUrl 作为 entry.to；但所有卡片统一使用 router-link。配置 `https://docs.example.com/guide` 后，实际生成 href 是 `/https://docs.example.com/guide`，点击会进入本站路由而非文档站。原有顶栏与页脚普通 a 链接仍正常。

测试挂载实际 HomeView，使用真实 Vue Router memory history，确认错误 href。建议为外部 URL 使用 a/href；站内条目继续使用 router-link。

## R4 [P2] 模型广场选中筛选项变成白字透明背景

位置：`frontend/src/components/modelPlaza/PlazaFilterBar.vue:174`。

chipClass(true) 删除了渐变及起止色，却只保留 text-white 与 shadow，没有补充实色背景。平台/分组的“全部”、倍率的“全部”以及选中具体倍率都使用此函数，在浅色页面上近乎不可见。使用 chip-tinted-active 的具体平台/分组项不受此处影响。

实际组件配合生产 Tailwind 配置与 style.css 渲染，选中按钮 backgroundColor 为 rgba(0,0,0,0)，且没有 backgroundImage。基线有品牌渐变背景。

建议给选中态补充与白字对比足够的实色背景；同类批量移除渐变处也应检查白色图标是否还有背景。

## 验证

- 三个自动复现均调用实际组件或实际模板，并用生产 Tailwind 配置编译 style.css。将六个相关文件切回 PR base 后，三个测试全部通过；恢复 PR head 后三个全部失败。没有修改当前工作分支的业务源码。
- 布局测试在 jsdom 中显式应用 1280px 桌面媒体规则；持久 transform 的动画布局影响另由真实浏览器验证。
- 相关既有测试第一组：12 文件、116 项通过，涵盖路由、导航、首页、表格、设计结构与 locale keys。
- 第二组登录、插件页、管理仪表盘与用户统计卡片：4 文件、21 项通过。两组合计 16 文件、137 项既有测试通过。
- 完整前端类型检查通过；使用原 Vite 配置的生产构建通过。初次构建遇到本机子进程 PATH 问题，规范化 Node 子进程 PATH 后成功，未改 Vite 配置。
- 未跑全量前端套件，未连接真实 Go 后端，未做真实用户账号操作，未合并或部署。

运行复现：见 README.md 与 run.ps1。浏览器结果见 browser-evidence.json。
