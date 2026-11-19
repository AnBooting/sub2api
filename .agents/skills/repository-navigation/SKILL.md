---
name: repository-navigation
description: "Use before cross-module Sub2 changes to build a fresh impact context from the project wrapper, Roam graph, and lexical fallback."
---

# Sub2 仓库导航与影响面

## 触发条件

用于 Sub2 中涉及多个模块、公共接口、请求计费、调度、数据库迁移、Ent 生成物、路由、前端契约、配置键或不确定影响面的任务。

以下情况不触发：单文件拼写或格式修复、已经明确且只读的文件查看、与 Sub2 无关的通用问题。只改一个文件但涉及公共符号时仍触发。

## 入口与门禁

1. 如果维护的 repository-intelligence 能力包可用，先运行 repository-intelligence discover/doctor/status；随后运行项目 wrapper status，记录 branch、HEAD、dirty state、cache root、lifecycle、lock 和 freshness。Sub2 的项目 wrapper 仍是 Roam/repo-map 的具体适配入口。
2. freshness.ready=false 时不要让 wrapper 自动重建；先判断是否确实需要当前未提交内容，再显式运行 node tools/repo-intelligence.mjs index --allow-dirty。
3. dirty worktree 只有在确认索引包含同一 dirty fingerprint 后，才能给 query 或 mcp 传 --allow-dirty。
4. Roam 不可用、索引缺失或查询不覆盖目标时，先使用 repository-intelligence 的 context/grep 组合和 node tools/repo-map.mjs fallback；源码、测试和编译器始终优先。

## 查询组合

### 符号和调用关系

对 Go、TypeScript 或 Vue 中的函数、类型、服务和接口：

~~~powershell
node tools/repo-intelligence.mjs query --allow-dirty context <Symbol>
node tools/repo-intelligence.mjs query --allow-dirty preflight <Symbol>
node tools/repo-intelligence.mjs query --allow-dirty impact <Symbol>
~~~

记录定义、直接 callers/callees、相关测试、blast radius、coupling 和风险等级。preflight 的风险是导航证据，不是验收结论。

### 数据、配置和跨语言契约

对数据库字段、JSON key、API 路径、事件名、迁移编号和配置键，必须同时运行语义查询和词法查询：

~~~powershell
node tools/repo-intelligence.mjs query --allow-dirty grep <term>
node tools/repo-map.mjs impact <term>
~~~

图查询报告“无 dependents”不能证明动态字符串、JSON、SQL、Ent 生成物、前端字段或文档契约不存在；词法结果必须回读并分类为源码、生成物、迁移、测试、文档或示例。

## 生成最小上下文包

交给实现者或主 Agent 的上下文至少包含：

- 任务目标、当前 branch/HEAD 和 freshness 证据；
- 定义位置、直接调用者、被调用者和跨模块边界；
- 相关 API、配置、迁移、Ent 生成物和前端类型；
- 关联测试、建议验证命令和风险等级；
- 词法查询与语义图不一致的地方及待人工确认项；
- 允许写入的文件域和不应触碰的既有用户改动。

不要把完整 Roam 输出或整个仓库复制进上下文；按文件、符号和验收条件裁剪。

## MCP 与安全边界

通过 node tools/repo-intelligence.mjs mcp --allow-dirty 启动 Roam 时，wrapper 会固定外部 cache、关闭自动索引、使用 core profile 和关闭模型摘要。默认只读；不使用 full、compile 或会把源码发送给外部模型的能力。

不要把 Roam SQLite、parser cache、凭据、数据库数据或未跟踪用户文件写入仓库。不要为了让 freshness 变绿而清理、回滚或覆盖其他人的改动。

## 修改后验收

修改后重复同一组 context、impact 和契约词 grep，比较候选文件和测试集合，再按项目规则执行 Go/Vue 类型检查、单元测试、构建、迁移或端到端验证。只把源码、测试、编译器和运行结果作为完成证据。

## 相邻入口

- 一般 Sub2 开发、构建、迁移和部署：继续使用 sub2api-dev-deploy。
- 代码地图命令、缓存和架构说明：读取 project-docs/architecture/06-repository-map.md。
- 复杂跨模块交付：先用本 Skill 生成上下文包，再按项目协作规则分配写域。
