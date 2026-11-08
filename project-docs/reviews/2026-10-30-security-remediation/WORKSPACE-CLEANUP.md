# 工作区与 Git 状态清理记录（2026-10-30）

目标：`F:\` 下只留 `F:\sub2` 一个项目目录，并让 `sub2` 的 Git 状态干净可读。

## 1. `F:\` 根目录变化

| 原路径 | 处置 | 理由 |
|---|---|---|
| `F:\sub2-fix-mig` | **删除**（git worktree） | 是 `chore/sync-upstream-0.2.17` 的 worktree。删除前三重确认：工作树 `git status` 干净、分支与 `origin/chore/sync-upstream-0.2.17` 零差异（`rev-list --left-right --count` = `0 0`）、分支已完全合入 `develop`（`merge-base --is-ancestor` = 0）。删除后 `git worktree prune`。 |
| `F:\sub2-system-backup-20261029` | **移到** `F:\_quarantine\20261030-sub2-workspace-cleanup\` | 内容与 sub2 无关，是 2026-10-29 的一次 Windows 系统调整（卸载微软电脑管家、去 Teams 自启、隐藏 Copilot 按钮）的撤销说明与注册表备份。**不是项目产物，但含不可再生的恢复信息**（`HKCU-Run-backup.reg`、Copilot 键的正确恢复命令），所以不删，只移出项目区。 |
| `F:\tools\sub2api-server.exe` | **移到** 同一 quarantine 目录 | 2026-10-10 的旧构建产物（153 MB），与当前源码不对应。全仓库（含文档、脚本、CI）零引用。 |

清理后 `F:\` 根目录只剩 `F:\sub2` 一个 `sub2*` 目录。

## 2. `F:\sub2` 内部变化

| 路径 | 处置 | 理由 |
|---|---|---|
| `ARTEX/` | **移出到** `F:\_reference\ARTEX` | 是 `Autumn-27/ARTEX` 的只读分析副本（嵌套 git 仓库，`.gitignore` 已忽略）。移出前确认：`git status` 干净、零未推送提交（`rev-list --count --all --not --remotes` = 0）、HEAD 为公开仓库的 `e6ec5699`，**随时可重新 clone**。`project-docs/18-handoff-agent-team-2026-10-11.md:90` 提到过这个路径，属历史记录，不回改。 |
| `.agents/` | **删除** | 空目录，无内容。 |
| `undefined/` | **删除** | 只有一个 `undefined/temp/task8-pricing-panels-prompt.md`（14 KB，2026-10-21 的一次任务提示词），是某个脚本把 `undefined` 当目录名写出来的产物。内容已过时。 |
| `.codegraph/` | **删除** | 2026-10-12 生成的两个 JSON（`manifest.json`、`overview.json`），`generatedFor` 指向一个早已不在 HEAD 上的提交。工具重跑即可再生，且该工具已不在使用。 |
| `backend/bin/embed.exe`、`backend/bin/good.exe` | **删除** | 两个 160 MB 级的临时构建产物（10-30 调试时留下的），`.gitignore` 已忽略，`go build` 随时重建。保留 `server.exe`（当前构建）。 |
| `.credkeys.txt` | **移出到** quarantine | 52 个 API Key 的本地记录文件（973 字节，`.gitignore` 已忽略）。它躺在项目根目录里，任何 `git add -f` 或误操作都会带进仓库。全仓库零引用（唯一的 `credKeys` 命中是 `gemini_oauth_service_test.go` 里一个同名局部函数，无关）。**移动而非删除**，因为里面是你在用的 Key。 |
| `.orphans.txt` | **删除** | 0 字节空文件。 |
| `sub2api-pr-review-tests-20261028/` | **整合进** `project-docs/reviews/2026-10-28-pr12-pr13-lifecycle-repro/` | 是 PR #12/#13 的生命周期退避复现材料（7 个文件）。它不是垃圾：`run.ps1` 能复现「生产退避预算 `[5s 30s 2m 0s 5m 0s]` 共 7m35s 被 unit test 真实消耗」这个缺陷。挪到 reviews 目录与其他审查材料并列，并作为一次独立提交入库。 |
| `.deployprobe/` | **保留**，补了 `README.md` | 线上探测的原始留痕（68 个文件，约 800 KB）。结论性内容已复制进 `project-docs/`，这里保留的是可复跑脚本与截图。仍被 `.gitignore` 忽略。新增的 `README.md` 说明了内容、引用方与何时可删。 |
| `frontend/node_modules`、`backend/bin/server.exe` | **保留** | 构建依赖与当前构建产物，删了要重装/重建。 |

## 3. Git 状态变化

### 3.1 分支

删除 4 个本地分支，**每一个都先证明「远端有同样的提交、且已合入 develop」**：

| 分支 | 与 origin 的差异 | 已合入 develop |
|---|---|---|
| `chore/sync-upstream-0.2.17` | `0 0` | 是 |
| `backup/develop-merged-20261021` | `0 0` | 是 |
| `chore/go-toolchain-1.27.3` | `0 0` | 是 |
| `feat/product-clarity-20261021` | `0 0` | 是 |

保留了三个：`develop`（集成分支）、`main`（上游镜像，只读）、`fix/security-remediation-20261030`（本轮工作，已推 PR #18）。

**没有删除任何远端分支** —— 4 个被删的本地分支在 `origin` 上都还在，需要时 `git checkout -b <name> origin/<name>` 即可恢复。

### 3.2 对象库

`git gc` 前：`size-pack` 182.78 MiB、`size-garbage` 39.86 MiB、松散对象 3022 个。
`git gc` 后：`size-pack` 163.74 MiB、`size-garbage` 0、松散对象 0。`.git` 从 280.7 MB 降到 165.4 MB。

**gc 前把 28 个 dangling commit 的清单留档**在 `F:\_quarantine\20261030-sub2-workspace-cleanup\DANGLING-COMMITS.md`（commit id + 日期 + 标题）。这些是被 rebase/squash 替换掉的旧版本，内容仍可用 `git cat-file -p <sha>` 取回（默认保留期 2 周）。

### 3.3 保留的本地提交

`git rev-list --count --all --not --remotes` = 2，全部由 tag `prepush/pr12-pr13-merged` 持有（`655323f37`、`2a8f70b09`）。这是 PR #12/#13 合并前的快照 tag，**故意保留**：它记录了 squash 之前的分支形态，删了就无法回溯。tag 未推送到远端，是本地的安全网。

### 3.4 清理后的最终状态

```
分支:  develop / fix/security-remediation-20261030 / main
工作树: 只有 F:\sub2 一个
未跟踪: 无（.deployprobe/ 与 backend/bin/ 均被 .gitignore 覆盖）
未提交: 无
```

## 4. 复查方式

```powershell
cd F:\sub2
git status --short              # 应为空
git branch -vv                  # 应为 3 个分支
git worktree list               # 应只有 F:/sub2
git rev-list --count --all --not --remotes   # 应为 2（prepush tag 持有）
Get-ChildItem F:\ -Directory | Where-Object Name -like '*sub2*'   # 应只有 sub2
```

## 5. 没有动的东西（以及为什么）

- **`F:\_quarantine\` 下其他 6 个历史目录**：不是本轮产物，不判断去留。
- **69 个 tag**：都是发布/基线/快照标记，删除会破坏可追溯性。
- **`origin` 上的 9 个远端分支**：其中 `feat/frontend-fluent-redesign-clean`（PR #7，CLOSED 未合并）仍可能被重新拾起，删远端分支需要针对性授权。
- **`main` 落后 `origin/main` 138 个提交**：`main` 是上游镜像，只在需要时 `git fetch upstream` 后重置，不是本轮清理范围。
