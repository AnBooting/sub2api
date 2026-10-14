# 项目代码地图与影响面查询

## 定位

本文件定义 Sub2API 的第一版仓库导航入口。源码、测试、编译器输出和运行时证据仍是唯一事实源；代码地图只负责缩小读取范围、标记版本边界和生成候选影响面，不替代语义验证。

## 命令入口

在仓库根目录运行：

~~~powershell
node tools/repo-map.mjs overview
node tools/repo-map.mjs refresh
node tools/repo-map.mjs freshness
node tools/repo-map.mjs module backend/internal/service
node tools/repo-map.mjs impact pricingAt
~~~

overview 只读输出当前分支、commit、源码分类、Go package 和主要入口。
refresh 把 commit 绑定的概览写入已忽略的 .codegraph/。
freshness 检查索引是否对应当前 commit，以及工作树是否有未提交变化。
module 按路径前缀列出文件和分类统计。
impact 对符号、配置键或契约词生成词法候选集，并列出命中的测试文件。

## 通用 repository-intelligence 能力包

如果维护源已经部署独立能力包，先在仓库外运行通用生命周期入口，再进入 Sub2 wrapper：

~~~powershell
repository-intelligence discover --root F:/sub2
repository-intelligence doctor --root F:/sub2
repository-intelligence status --root F:/sub2
repository-intelligence context --root F:/sub2 --term ModelPricingResolver --allow-dirty
repository-intelligence query --root F:/sub2 --mode grep --term pricing_snapshot --allow-dirty
~~~

该能力包负责项目发现、分支/worktree cache、manifest freshness、refresh lock、watch、bootstrap、统一 context package 和可选 LSP/MCP；Sub2 wrapper 负责 Roam/repo-map 的具体实现。通用 context package 先比较语义和词法证据，再交给实现者。

## 项目级索引 wrapper

推荐通过 wrapper 调用外部 Roam cache；它不会把数据库写入仓库，也不会自动重建耗时索引：

~~~powershell
$env:SUB2_REPO_INTELLIGENCE_HOME = 'C:/path/to/external/sub2-cache'
$env:ROAM_BIN = 'C:/path/to/approved/roam.exe'
node tools/repo-intelligence.mjs status
node tools/repo-intelligence.mjs index --allow-dirty
node tools/repo-intelligence.mjs query --allow-dirty context ValidateNativePricingRequest
node tools/repo-intelligence.mjs mcp --allow-dirty
~~~

- `status` 输出当前 branch、HEAD、dirty fingerprint、外部缓存和 manifest freshness。
- `index` 必须显式使用 `--allow-dirty` 才会索引未提交工作；索引完成后写入外部 `repo-intelligence.manifest.json`。
- `query` 和 `mcp` 默认拒绝 dirty 工作树；只有确认当前索引对应未提交内容时才传 `--allow-dirty`。
- `mcp` 强制使用 Roam `core` profile、关闭自动索引并关闭模型摘要。
- Roam 不可用或索引缺失时使用 `node tools/repo-map.mjs impact <term>` fallback。
- 项目级触发和查询组合规范见 `project-skills/repository-navigation/SKILL.md`；本文件保留架构入口和缓存契约。

## 当前结构入口

| 领域 | 入口 |
|---|---|
| 后端启动 | backend/cmd/server |
| HTTP 路由 | backend/internal/server/routes |
| Handler | backend/internal/handler |
| 业务服务 | backend/internal/service |
| 数据访问 | backend/internal/repository |
| Ent schema/生成物 | backend/ent/schema、backend/ent |
| 数据库迁移 | backend/migrations |
| 前端 API/路由/状态/页面 | frontend/src/api、frontend/src/router、frontend/src/stores、frontend/src/views |
| 部署 | deploy |
| 需求与架构证据 | project-docs |

## 最小上下文流程

1. 先执行 overview 和 freshness，记录当前分支、commit 和工作树边界。
2. 对目标符号、配置键、路由或契约执行 impact。
3. 读取候选结果中的定义、直接调用者、配置/注册点、生成文件和关联测试。
4. 修改前确认数据库迁移、Ent 生成物、前端契约和部署入口是否被影响。
5. 修改后再次执行相同查询，比较候选集，并运行针对性测试、类型检查、构建或集成验证。
6. 只有在源码和测试证据闭合后，才把结果写入架构文档或提炼为通用 Skill。

## 证据和边界

- 当前实现是词法和结构导航，不声称完整语义调用图。
- .codegraph/ 只保存轻量概览；Roam SQLite 和 wrapper manifest 必须放在仓库外部缓存。
- 索引带有生成 commit 和 dirty fingerprint；工作树脏时必须显式放行并回到源码确认未提交改动。
- node_modules、构建产物、凭据、数据库数据和未跟踪用户文件不进入索引内容。
- 当前分支既有的未跟踪产物必须由其所有者处理，地图工具不得清理或覆盖。
- 通用能力包已提供 gopls definition/references/symbols 的只读适配接口；未来接入 ArkTS、SCIP 或其他代码图 MCP 时，保留本命令的输出契约作为低级 fallback。

## 首轮验证

在 feat/TS-01-request-locked-pricing 的 e489ff59759a4cdd6dbe51e44056affb5033ddfe 上验证：仓库有 4469 个跟踪文件和 110 个 Go package；impact pricingAt 返回 178 个词法命中、30 个候选文件。该结果用于验证导航流程，不作为永久统计，应通过 refresh 重新生成。
