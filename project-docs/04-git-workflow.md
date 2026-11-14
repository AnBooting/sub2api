# Git 工作流

> 面向「不熟悉 git」的使用者：照抄命令即可。
> **2026-11-09 修订**：在原有分支/日常操作基础上，补齐官方工作流口径（PR、合并策略、撤销恢复、行尾属性、保护分支、钩子），并把本仓库实测到的门禁与权限现状写清楚。
> 框架侧的 `git-workflow-and-versioning` Skill 另有一套「按需路由」文档，与本文分开维护；本文只讲**这个仓库怎么做**。

## 1. 两个远端

| 远端名 | 地址 | 用途 | 权限 |
|---|---|---|---|
| `origin` | `https://github.com/AnBooting/sub2api.git` | **团队基线**（含魔改提交） | 协作者可推 |
| `upstream` | `https://github.com/Wei-Shaw/sub2api.git` | 原始上游，只读参考 | 只读 |

`upstream` 已配置为**只跟随 `main`**（`+refs/heads/main:refs/remotes/upstream/main`），避免 40+ 个上游分支污染本地视图。需要看某个上游分支时临时 fetch：

```powershell
git fetch upstream <分支名>:refs/remotes/upstream/<分支名>
```

`upstream` 的 push URL 已设哨兵值 `DISABLED-upstream-is-read-only`，误推会**立即失败**而不是推到上游。要恢复默认行为：

```powershell
git remote set-url --push upstream https://github.com/Wei-Shaw/sub2api.git
```

## 2. 分支模型

```
upstream/main   Wei-Shaw/sub2api              原始上游（只读，仅跟随 main）
     │
     └─(团队 fork)─> origin/main = main        AnBooting 魔改基线
                         │
                         └─> develop  ★ 我们的集成分支
                               ├─ feat/<需求号>-<简述>
                               ├─ fix/<问题号>-<简述>
                               ├─ docs/<主题>
                               └─ chore/<内容>
```

| 分支 | 允许的操作 |
|---|---|
| `main` | 只跟踪 `origin/main`。不直接提交；`develop` 验收后经 PR 快进合入 |
| `develop` | 合并已验收的特性分支；**部署来源**；文档/chore 允许直接推送 |
| 特性分支 | 自由提交；完成并验证后合回 `develop`，随后删除 |

官方口径（GitHub flow / Pro Git）：默认用**短生命周期 topic 分支**，一个分支只做一件事，合回后立刻删；`develop` 这类长期分支是"更稳定的仓"，用来吸收 topic 分支。不要用活了几周的特性分支替代 feature flag。

## 3. 开始新需求（先同步再切分支）

```powershell
cd F:\sub2
git fetch origin --prune
git switch develop
git merge --ff-only origin/develop      # 本地落后时先快进，不在旧基线上开分支
git switch -c fix/smtp-config-guard-20261108
```

> 只在最新 `develop` 上切分支，能避免合并时才发现迁移/生成物冲突。

## 4. 提交规范

### 4.1 格式（Conventional Commits 1.0.0）

```
type(scope)!: 简述
<空行>
为什么（可选正文）
<空行>
BREAKING CHANGE: 说明（可选页脚）
```

常用 type：`feat`、`fix`、`refactor`、`perf`、`test`、`docs`、`build`、`ci`、`chore`、`revert`。
项目内约定（沿用已有实践）：

| type | 用途 | 示例 |
|---|---|---|
| `feat` | 新功能 | `feat(scheduling): 为账号增加独立并发上限` |
| `fix` | 缺陷修复 | `fix(auth): SMTP 未配置时提示可行动文案` |
| `refactor` | 不改行为 | `refactor(service): 抽出 mergeSearchSurcharge` |
| `docs` | 文档 | `docs(reviews): 订正 PR 状态与编号沿革` |
| `test` | 测试 | `test(pricing): 补缺价拒绝的失败用例` |
| `chore`/`build` | 依赖/构建 | `build: Go 工具链升到 1.27.3` |
| `migrate` | 数据库迁移 | `migrate: 新增 xxx 表` |

### 4.2 一个提交一个可回退的变化

- 不把格式化与行为改动混在一个提交；不把其他写者的改动带进自己的提交。
- 正文写"为什么"和影响，不复述 diff；破坏性变更用 `!` 或 `BREAKING CHANGE:` 显式标注。
- 提交前自查：`git status --short`、`git diff --cached --stat`、`git diff --cached --check`（行尾/空白问题会被这里拦住）。

### 4.3 数据库迁移编号

当前仓库最新编号是 **260**（`259_live_settlements.sql`、`260_usage_stats_repair.sql`），新增迁移从 **261** 起接号；**不改写已存在的迁移**，编号重复或跳号都要在 PR 里说明。

> `AGENTS.md` 里仍写"最新 258、从 259 起"，与当前文件不同 —— 以仓库实际文件为准，这条规则待同步（改动规则文件前先确认）。

## 5. PR 流程

### 5.1 提 PR 前

1. 分支已 rebase/快进到最新 `develop`，本地门禁（§9）全过——直接跑 `pwsh -File tools/pre-push-check.ps1 -Mode full`。
2. `gh` 在本目录**没有默认仓库**，涉及 PR 的命令**一律显式传** `--repo AnBooting/sub2api`，否则会解析到上游同名 PR。
3. PR 正文用仓库自带的 [`.github/PULL_REQUEST_TEMPLATE.md`](../.github/PULL_REQUEST_TEMPLATE.md)（四段：Why / What Changed / Testing / Risk & Rollback + 自检清单），测试结果贴**实际命令与退出码**。

```powershell
gh pr create --repo AnBooting/sub2api --base develop `
  --head fix/smtp-config-guard-20261108 `
  --title "fix(auth): 本地化 SMTP 未配置提示" `
  --body-file .\pr-body.md --reviewer <同事账号>
```

### 5.2 评审

- **作者 ≠ 审者**：安全、契约、计费类改动必须由独立的人/会话复核。
- 评审结论与证据归档到 `project-docs/reviews/<日期>-pr<N>-review/`，PR 正文链接过去。
- 评审方可以**不参与实现**；实现者的自述不能代替复核。
- **证据从第一次落盘就写进证据目录**，不要先放仓库根再搬：根目录的 `*.log` 被 `.gitignore` 拦下，容易到合并后才发现证据链不全（2026-11-14 PR #24 补过第二个提交才补齐）。
- 证据文件统一 LF、去行尾空白、保留单个结尾换行，否则 `git diff --check` 会拦住提交；生成的 JSON 同样处理。

### 5.3 合并

| 类型 | 方式 |
|---|---|
| 功能/安全 PR | `gh pr merge <N> --repo AnBooting/sub2api --merge`（保留合并提交，便于整体回滚） |
| 纯文档/chore | 允许直接 `git push origin develop`（快进）；GitHub 会把对应 PR 自动标 MERGED，**这不等于在页面上点过合并**，要在文档里写清 |

`main` 只从 `develop` 快进合入，永远不在 `main` 上提交。

> **合并要单独授权**：任务说「完成 git 相关操作」只覆盖提交与推送。合并 PR、直推 `develop`、删远端分支、回退已合并提交，都要用户明确点头。
> 开工时先确认「谁合并」，把结论写进任务描述；不沿用默认。

### 5.4 PR 编号

PR 编号由 GitHub 自增分配，**不可选、不可复用**（关闭未合并的 PR 也永久占号）。顺序只代表创建时间，不代表重要性；#20 是文档 PR、#21 是 Nit 收口 PR 就是这个原因。当前状态表见 [00-overview.md](00-overview.md) §7。

## 6. 合并后收尾

1. 本地 `develop` 快进到远端：`git fetch origin && git switch develop && git merge --ff-only origin/develop`，核对合并提交号。
2. 删本地已合并分支。**不要只用 `git branch -d` 判断**（它只比较当前 HEAD）：

```powershell
git rev-list --count --all --not --remotes   # 结果为 0 才说明没有仅本地的提交
git branch -d fix/smtp-config-guard-20261108
```

3. 清理本轮临时产物；**不要动**别人的未跟踪文件（如用户自己的截图、报告）。
4. 更新 `project-docs/00-overview.md` §7 状态表——这一步是每个 PR 的收尾动作，别等下一轮才发现文档落后。

## 7. 打标签与发布

```powershell
git tag -a deploy-2026-11-09 -m "生产部署 2026-11-09"   # annotated tag：带作者/日期/说明
git push origin deploy-2026-11-09
```

- 发布用 **annotated tag**；lightweight tag 只适合临时标记。
- 版本口径遵循 SemVer：不兼容 → MAJOR，兼容新增 → MINOR，兼容修复 → PATCH；预发布写 `-rc.N`。
- 变更记录按 Keep a Changelog 的类型分组（Added/Changed/Deprecated/Removed/Fixed/Security），标题写日期，保留 `Unreleased`。
- 标签、版本声明（`go.mod`/`package.json`/构建产物）与变更记录三处要保持一致；打标签、推送、发布是**不同动作**。

## 8. 撤销与恢复（按"是否已共享"分层）

| 状态 | 命令 | 要点 |
|---|---|---|
| 未提交，改错文件 | `git restore <file>` | 丢弃工作区改动；先确认没别的会话在同一文件上工作 |
| 已暂存，想取消 | `git restore --staged <file>` | 只取消暂存，不改内容 |
| 已提交未推送 | `git reset --soft HEAD~1` | 保留改动、只退回提交边界 |
| 已推送/共享 | `git revert <sha>` | 生成反向提交，不改历史；**共享分支默认用它** |
| 回滚**合并提交** | `git revert -m 1 <merge-sha>` | `-m 1` 指定主线父提交；官方注明：revert 合并后**再次合并同一分支不会重新带回被拒的树改动**，需要显式处理 |
| 找回落掉的提交 | `git reflog` 后 `git reset HEAD@{n}` | 只对已提交对象有效；**未提交内容丢失不可恢复** |

红线：`git reset --hard` 不得用于丢弃未提交工作；共享分支不得 reset 改写历史；必要时先建临时分支/标签作为恢复点。

## 9. 提交前门禁（本机真实命令）

> 本仓库 **Actions 现状**：工作流定义（CI / Security Scan / Release / CLA）齐全且都写了 `push`+`pull_request` 触发，但**最近一次运行是 2026-10-22**；2026-11-08 的推送与 PR 都没有产生任何 run（PR 的 `statusCheckRollup` 为空）。原因需 admin 在仓库 Settings/Billing 侧确认。**因此合并判断不能等 CI，必须本机验证。**

**一条命令跑完**（脚本把下面各项串起来，输出逐项 PASS/FAIL，并以退出码汇总）：

```powershell
pwsh -File tools/pre-push-check.ps1 -Scope docs                 # 只查空白/行尾/敏感文件
pwsh -File tools/pre-push-check.ps1 -Scope backend -Mode quick   # 后端编译
pwsh -File tools/pre-push-check.ps1 -Mode full                   # 合并前全量
```

> 退出码语义：**PASS** 全过；**WARN** 是已知环境/用例外的未处理错误（本机没有 `sh` 时的 3 条 `TestPgDumper*`、vitest 全绿但报告 1 条 unhandled rejection），只提示不阻断；只有 **FAIL** 才返回 1。调试单个包用 `-BackendTestPattern ./internal/repository`。

| 门 | 命令 | 通过标准 |
|---|---|---|
| 后端编译 | `cd backend; go build -o bin/server.exe ./cmd/server` | 退出码 0 |
| 后端单测 | `go test -p 1 -tags=unit ./...` | 全绿；3 条 `TestPgDumper*` 因本机无 `sh` 失败属环境问题 |
| 后端格式 | `gofmt -l <改动文件>` | 无输出 |
| 前端类型 | `corepack pnpm@9 exec vue-tsc -b` | 退出码 0 |
| 前端测试 | `node node_modules/vitest/vitest.mjs run` | 全绿 |
| 前端构建 | `corepack pnpm@9 exec vite build` | 退出码 0 |
| 空白/行尾 | `git diff --check` | 无输出 |

注意：全局 pnpm 是 11.x，与锁文件不兼容，**永远用 `corepack pnpm@9`**；`pnpm run build` 会嵌套调用 pnpm 而失败，直接用 `exec vite build`。

## 10. Worktree（多会话并行）

默认**不用** worktree；只有"两条流必须同时编译/运行且互相阻塞"时才开（成本与判断规则见 [12-worktree-strategy.md](12-worktree-strategy.md)）。

```powershell
cd F:\sub2
git worktree add ..\sub2-feat-sched -b feat/sched develop   # 创建
git worktree list                                            # 查看
git worktree remove ..\sub2-feat-sched                      # 清理（须干净）
git worktree prune
```

Windows 清理顺序：**先删 `node_modules`**（长路径会拦住 `worktree remove`），再 deregister；两步失败都不要强删目录，先确认没有会话还在用。不同会话不要在同一 checkout 里切分支。

## 11. 冲突解决

1. 先读双方的目标与当前文件，再按语义合并；不要凭"哪边更新"整块取值。
2. `-X ours`/`-X theirs`/`checkout --ours --theirs` 是整块覆盖，会静默丢另一侧改动，**不作默认手段**。
3. 解决后跑受影响检查；提交前确认没有冲突标记残留。

## 12. 行尾与 .gitattributes

仓库已用 `.gitattributes` 固定 LF：`backend/migrations/*.sql`、`*.go`、`*.ts/.tsx/.js/.jsx/.vue`、`*.sh`、`*.yaml/.yml`、`Dockerfile`。新增文本类型要一并登记。

- `text=auto` 让仓库内统一 LF，`eol=lf` 控制工作区；未声明时由 `core.autocrlf` 决定，**不同平台结果不一致**——这是"本地正常、提交后 diff 全变"的常见根因。
- 实测教训：CRLF 文档提交时 `git diff --check` 会报 trailing whitespace 直接拦住提交；提交前统一 LF。
- 二进制文件用 `-text` 阻止转换。

PowerShell 侧统一处理（`*.log/*.txt/*.json` 证据文件适用）：

```powershell
$enc = New-Object System.Text.UTF8Encoding($false)
foreach ($f in Get-ChildItem <目录> -Recurse -File -Include *.log,*.txt,*.json) {
  $lines = [IO.File]::ReadAllLines($f.FullName) | ForEach-Object { $_.TrimEnd() }
  [IO.File]::WriteAllText($f.FullName, ($lines -join "`n") + "`n", $enc)
}
```

> 2026-11-13/14 在 PR #23/#24 上踩了四次（日志 CRLF、JSON CRLF、EOF 多空行、EOF 缺换行）。`Set-Content`/`Out-File` 默认写 CRLF，别用它写证据。

## 13. 本地钩子（可选，建议）

- `pre-commit`：`gofmt`、`git diff --check`、敏感文件名扫描。
- `pre-push`：直接调 §9 的脚本，例如 `pwsh -File tools/pre-push-check.ps1 -Scope docs`。
- `--no-verify` 只作显式例外，并在 PR/提交说明里注明原因。

> 脚本与本文 §9 是同一份门禁清单的两个入口：改门禁时**两处同步**，不要只改一边。

## 14. 平台机制与权限现状

| 项 | 现状 | 影响 |
|---|---|---|
| 账号权限 | `yylovehhy`：push/maintain ✅，admin ❌ | 可推非保护分支、管 PR/issue；**改不了仓库设置、开不了分支保护** |
| 分支保护 | `develop`、`main` 均无保护 | 技术上都可直推，靠本文与项目 `AGENTS.md` 自律 |
| Required checks | 无；且 Actions 当前不产生 run（§9） | 合并结论只能基于本机命令与评审记录 |
| 合并方式 | merge / squash / rebase 都允许 | 功能 PR 固定 merge；文档/chore 可直推 |

没有 admin 时，用**本地门禁 + PR 模板 + 评审证据归档**承担同等约束；不要把"没有保护"读成"不需要检查"。

## 15. 红线

- ❌ 不在 `main` 上直接提交（只经 PR 从 `develop` 快进）
- ❌ 不 `git push --force`；共享历史确需更新时用 `--force-with-lease` 并事先取得授权
- ❌ 不提交 `.env`、`data/`、`postgres_data/`、`redis_data/`、任何 API Key 或密码
- ❌ 不 `git reset --hard` 丢弃未提交工作
- ❌ 不用 `git branch -d` 单独判断分支能否删除（用 `rev-list --count --all --not --remotes`）
- ❌ 不把上游 `docs/` 当作我们文档的存放地（被 `.gitignore` 忽略，用 `project-docs/`）
- ❌ 不覆盖其他会话/协作者的未提交改动

## 16. 两个 gitignore 陷阱

1. **`AGENTS.md` 被上游 `.gitignore` 忽略** → 提交需 `git add -f AGENTS.md`。
2. **`docs/*` 被上游 `.gitignore` 忽略**（只放行白名单）→ 我们的文档放 `project-docs/`。

## 17. 网络

GitHub 直连不稳定，仓库已配置代理：

```powershell
git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877
```

Clash 端口变化时同步修改。

## 18. 速查

| 想做什么 | 命令 |
|---|---|
| 同步 develop | `git fetch origin --prune; git switch develop; git merge --ff-only origin/develop` |
| 开分支 | `git switch -c fix/<主题>-<日期> develop` |
| 看将要提交什么 | `git status --short; git diff --cached --stat` |
| 检查空白/CRLF | `git diff --check` |
| 合并前门禁 | `pwsh -File tools/pre-push-check.ps1 -Mode full` |
| 提 PR | `gh pr create --repo AnBooting/sub2api --base develop ...`（正文用 `.github/PULL_REQUEST_TEMPLATE.md`） |
| 合并 PR | `gh pr merge <N> --repo AnBooting/sub2api --merge` |
| 回滚共享提交 | `git revert <sha>`（合并提交加 `-m 1`） |
| 找回提交 | `git reflog` |
| 打发布标签 | `git tag -a deploy-<日期> -m "..."` |
| 清 worktree | `git worktree remove <路径>`（先删 node_modules） |

## 19. 官方来源

- GitHub flow：https://docs.github.com/en/get-started/using-github/github-flow
- Pro Git 分支工作流：https://git-scm.com/book/en/v2/Git-Branching-Branching-Workflows
- Conventional Commits 1.0.0：https://www.conventionalcommits.org/en/v1.0.0/
- git-push（`--force-with-lease`）：https://git-scm.com/docs/git-push
- git-revert（`-m` 与合并回滚语义）：https://git-scm.com/docs/git-revert
- gitattributes（text/eol）：https://git-scm.com/docs/gitattributes
- SemVer 2.0.0：https://semver.org/
- Keep a Changelog 1.1.0：https://keepachangelog.com/en/1.1.0/
- 保护分支：https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches
