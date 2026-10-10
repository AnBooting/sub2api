# 协作规范（多人 / 多 Agent）

> 适用对象：人类协作者 + AI Agent（含 Agent Team 成员）。
> 目标：让「谁在改什么、改到哪一步、怎么验收」始终可查，避免互相覆盖。

## 1. 角色与职责

| 角色 | 职责 | 权限边界 |
|---|---|---|
| **项目所有者**（人类） | 定需求、拍板决策、提供服务器与凭据 | 唯一能授权生产部署与不可逆操作 |
| **Lead Agent** | 拆任务、分派、集成、最终验收 | 不代替所有者做需求决策 |
| **领域 Agent** | 在自己被分配的 write scope 内产出 | **不越界写别人的文件** |
| **审查者**（人或 Agent） | 独立复核（不复用实现者的自述） | 只读，不改实现者代码 |

## 2. 分支模型

```
upstream/main   Wei-Shaw/sub2api           只读，用于跟随上游
fork-base       AnBooting/sub2api main     只读快照（魔改基线 0e6f5efb）
  └─ develop    本仓库集成分支             ← 日常提交与集成
       ├─ feat/<需求号>-<简述>              每个需求一条
       ├─ fix/<问题号>-<简述>
       └─ chore/<内容>
```

| 分支 | 允许的操作 |
|---|---|
| `main` | 只 `git fetch upstream`，**禁止提交** |
| `fork-base` | 只读；上游 fork 更新时用 `git fetch fork && git branch -f fork-base fork/main` |
| `develop` | 合并已验收的特性分支；打部署标签 |
| `feat/*` | 自由提交；完成后合回 develop |

**红线**：不 force push；不在 main/fork-base 提交；不提交 `.env`/密钥/生产数据。

## 3. 任务板与 write scope

所有非平凡工作先在任务板建卡，卡里必须写清：

- **产出物**：具体文件路径（不是「改后端」这种模糊描述）
- **write scope**：预期修改的文件/目录前缀，**互不重叠**
- **依赖**：被哪些任务阻塞
- **验收标准**：可执行的命令或可观测的行为

约定：
- 一个文件同一时间只有一个写者。
- 需要改别人 scope 内的文件时，先沟通，不直接改。
- 任务完成前，产出物必须已落盘且能通过该任务自己的验收命令。

## 4. 提交规范

格式：`<type>(<scope>): <简述>`

| type | 用途 |
|---|---|
| `feat` | 新功能 |
| `fix` | 缺陷修复 |
| `refactor` | 重构（不改行为） |
| `chore` | 构建/依赖/杂务 |
| `docs` | 文档 |
| `test` | 测试 |
| `migrate` | 数据库迁移 |

示例：`feat(scheduling): 为账号增加独立并发上限字段`

**迁移编号**：接在上游最新编号之后（当前 **243** 已被 fork 占用，新增从 **244** 起），不改写已有迁移。

## 5. 质量门（合并到 develop 前必须全过）

| 门 | 命令 | 通过标准 |
|---|---|---|
| 后端编译 | `cd backend && go build ./cmd/server` | 退出码 0 |
| 后端单测 | `go test -tags=unit ./...` | 全绿（注意：基线有 4 个已知失败，见 §7） |
| 后端静态检查 | `go vet ./...` | 无输出 |
| 前端类型 | `corepack pnpm@9 run typecheck` | 退出码 0 |
| 前端测试 | `corepack pnpm@9 exec vitest run` | 全绿 |
| i18n | `corepack pnpm@9 exec vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts` | 全绿 |
| 代码审查 | `ocr review --from develop --to <branch>` 或人工审查 | 无高危问题 |

## 6. 审查规范

- 审查者**不复用实现者的结论**，直接看 diff 与原始证据。
- 审查至少覆盖：正确性、安全（凭据/越权/注入）、计费正确性、迁移可回滚性。
- 计费相关改动**必须**给出「改前 vs 改后」的具体数值对比。

## 7. 已知基线问题（不是我们引入的）

`go test -tags=unit ./...` 在**未改动的基线上**已有 4 个失败：

| 包 | 失败用例 |
|---|---|
| `internal/repository` | `TestPgDumperHoldsMigrationLockThroughReaderClose` |
| `internal/repository` | `TestPgDumperReleasesMigrationLockWhenProcessFails` |
| `internal/repository` | `TestPgDumperReportsUnlockFailureAndDiscardsConnection` |
| `internal/repository` | `TestServerTimingConnectorRecordsDriverCallsWithoutRowLifetime` |
| `internal/service` | `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` |

→ 验收时以「**不新增失败**」为准，不要求把这些修掉（除非单独立项）。

## 8. 环境约定（本机）

| 项 | 值 |
|---|---|
| Go | `F:\tools\go1.27.2\go`（已持久化 GOROOT/GOPATH/PATH） |
| 前端包管理 | **必须** `corepack pnpm@9`（全局 pnpm 11 不兼容） |
| GitHub 代理 | `git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877` |
| npm 源 | `--registry=https://registry.npmmirror.com` |
| Go 模块源 | `GOPROXY=https://goproxy.cn,direct` |
| 代码审查 | `F:\tools\ocr\ocr.exe`（阿里 OpenCodeReview） |

## 9. Agent Team 使用约定

- 只在**确有并行价值**时组队；单文件小改不需要。
- 每个成员一张任务卡、一个独立产出文件。
- 成员**只读源码**做侦察时，绝不写 `backend/`、`frontend/`。
- 成员汇报必须含：产出文件路径、关键发现、**未确认项**。
- Lead 在收尾前必须亲自核对成员产出，不以成员自述代替验收。
- 长任务用后台作业，不阻塞；同一资源（构建目录、Docker、浏览器）同一时间一个写者。
