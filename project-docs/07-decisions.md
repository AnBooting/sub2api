# 决策记录（ADR）

> 记录已确认的决策及其理由。新决策追加，不删除历史。

## ADR-001 代码布局

- **决策**：上游仓库 clone 到 `F:\sub2` 根目录，移植包移入 `_reference/domestic-provider-kit/`。
- **状态**：✅ 已执行
- **理由**：单一工作区，git 基线清晰；移植包作为只读参考，不参与构建。
- **备选**：clone 到 `F:\sub2\sub2api` 子目录（被否：多一层嵌套，路径冗长）。

## ADR-002 不使用远端构建机

- **决策**：**不碰** `dsh-ssh.json` 里的 `dwin-builder`（192.168.43.119）。
- **状态**：✅ 用户确认
- **理由**：用户明确指示「别碰它」。
- **影响**：Docker 构建与验证必须在本地完成，因此本地 Docker Desktop 成为必要项。

## ADR-003 定价政策

- **决策**：**沿用源站政策** —— 国内人民币标价数字直接按站内 USD 数字计账（1 元 = 1 美元，无汇率换算），之后叠加分组/用户有效倍率。
- **状态**：✅ 用户确认
- **理由**：与移植包行为一致，改动最小。
- **后果**：成本核算偏离真实美元成本，属**有意的经营规则**。前端界面说明、后端计费与测试必须保持一致口径。
- **相关**：移植包 `docs/INTEGRATION.md` 第 3 节。

## ADR-004 代码审查工具

- **决策**：安装并使用 **OpenCodeReview（`ocr`，阿里 `alibaba/open-code-review`）** 作为独立代码审查通道。
- **状态**：✅ 已安装并校验
- **事实**：
  - 仓库 `alibaba/open-code-review`，Apache-2.0，Go 编写，44,938 star
  - 安装位置 `F:\tools\ocr\opencodereview.exe`，版本 **v1.12.13**
  - SHA256 已与官方 `sha256sum.txt` 核对：`520b135f1e15e129643678c2f6ce1daff9e5299250b59aa849a19ad723e716a4` ✅
  - 用法：`ocr review`（工作区）、`ocr review --from main --to develop`（分支范围）、`ocr scan`（全文件扫描）
  - 支持「委派模式」（`ocr delegate`）：由宿主 AI 代理自行审查，**无需配置 OCR 的 LLM key**
- **待办**：首次使用前配置 LLM provider（`ocr config provider` / `ocr config model`），或走委派模式。
- **澄清**：此前对话中提到的「opencode review」经用户澄清即为**阿里的 OpenCodeReview**，不是 `opencode.ai` 那个编码代理。

## ADR-005 命名冲突处理（**待决策**）

- **问题**：上游已存在 `cn_provider`（余额/限流/调度）与前端 `keyGroupProviders.ts` 的 `domestic` 分类，
  与移植包的 `domestic_provider` 语义相近但不同。
- **选项**：
  1. 沿用 `domestic_provider`，与上游 `cn_provider` 并存（改动最小，但概念重叠）
  2. 复用/扩展现有 `cn_provider`（改动大，需重写移植包逻辑）
  3. 改名为更明确的名字（如 `domestic_provider_id`），并在前端统一 `domestic` 语义
- **状态**：⏳ 待需求明确后决策
- **建议**：选项 1 + 前端统一，即后端沿用 `domestic_provider`，前端把 `keyGroupProviders.ts` 与 `domesticProviders.ts` 的 `domestic` 概念对齐。

## ADR-006 国模账号协议模式（**待决策**）

- **问题**：上游已有 Responses 自动探测（`openai_apikey_responses_probe.go` 存在）。
  移植包源版的创建弹窗有 bug：设了 `force_chat_completions` 但没持久化。
- **选项**：
  1. 保留上游自动探测（若探测可靠，体验最好）
  2. 国模账号创建时显式写 `extra.openai_responses_mode = "force_chat_completions"`（最稳妥）
  3. 两者结合：默认显式，编辑时允许切回自动
- **状态**：⏳ 待需求明确后决策
- **建议**：选项 3。

## ADR-007 本地工具链形态

- **决策**：Go 用**便携版**（解压到 `F:\tools\go1.27.2`，用户级环境变量），不用安装程序。
- **状态**：✅ 已执行
- **理由**：无需管理员权限；版本精确锁定 1.27.2；不污染系统。
- **pnpm**：全局 pnpm 11 与上游要求的 pnpm 9 冲突，改用 `corepack pnpm@9`。
- **Docker**：安装包已下载，**待用户手动安装**（需管理员 + WSL2 + 重启）。

## ADR-008 项目文档位置

- **决策**：项目文档放在 **`project-docs/`**，不放 `docs/`。
- **状态**：✅ 已执行
- **理由**：上游 `.gitignore` 忽略 `docs/*`，只放行白名单；放 `project-docs/` 才能提交进仓库。
- **同类**：`AGENTS.md` 也被上游 `.gitignore` 忽略，需 `git add -f`。
