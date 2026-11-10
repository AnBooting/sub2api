# 状态文件核实报告（2026-11-08）

> 核实对象：project-docs/22-repository-status-2026-10-19.md（审计基线快照）
> 核实方式：直接对本机 F:\sub2 仓库执行 git/文件命令，逐条取证。
> 背景：原计划由 Ralph 循环完成，因前台执行 10 分钟上限两轮均未产出，改由主 Agent 直接核实。
> **归档（2026-11-10）**：本报告是 2026-11-08 时点的快照（核实时 HEAD 为 `7c2848bed`），正文按当时证据原样保留；之后 `develop` 已前进到 `32ed7e6ee`（PR #21 于 11-09 合入、分支已删）。最新的分支 / PR / 部署事实见 [00-overview.md](00-overview.md) §7–§8，按本报告格式复核时请重新取证，而不是照抄结论。

## 逐条结论

### 1. 远端指向与 upstream 只读 —— ✅ 一致
- 断言：origin=AnBooting/sub2api；upstream=Wei-Shaw/sub2api（只读）
- 证据：`git remote -v`
  - origin → https://github.com/AnBooting/sub2api.git (fetch/push)
  - upstream fetch → https://github.com/Wei-Shaw/sub2api.git
  - upstream push → DISABLED-upstream-is-read-only（哨兵值，误推立即失败）

### 2. 审计分支 audit/full-20261019 —— ❌ 不存在（已过时但可解释）
- 断言：审计分支 audit/full-20261019 记录本轮修复
- 证据：`git branch -a --list "*audit*"` → 空，本地与远程均无该分支
- 说明：安全修复已经 PR #18 于 2026-11-08 18:59 合并进 develop（4a0064318），分支清理属正常运维；文档快照为 2026-10-19 时点，今日核实为「分支已不存在」

### 3. .gitignore 含 ARTEX/ —— ✅ 一致
- 断言：未跟踪 ARTEX/ 仅加入 .gitignore
- 证据：.gitignore 第 175 行：`ARTEX/`

### 4. auth_dingtalk_client.go io.ReadAll 错误处理 —— ✅ 一致
- 断言：全部上游响应 io.ReadAll 错误改为返回错误
- 证据：该文件 6 处 io.ReadAll（L59/118/185/236/294/347），抽查 L59-62：读取错误以 fmt.Errorf 包装返回，不把截断数据当成功

### 5. tools/repo-map.mjs 无硬编码本机 Go 路径 —— ✅ 一致
- 断言：移除本机 Go 硬编码
- 证据：grep `F:\\|gopath|go1\.2` 于 tools/repo-map.mjs 无匹配

### 6. 基线提交 d5c2b5edb 仍是 develop 祖先 —— ✅ 一致
- 断言：审计基线 develop = origin/develop = d5c2b5edb
- 证据：`git merge-base --is-ancestor d5c2b5edb... HEAD` 退出码 0；`git cat-file -t` 确认对象存在
- 说明：develop 此后已前进（当前 HEAD 7c2848bed，含 0.2.17 同步与 PR #18），基线历史有效

## 汇总

| # | 断言 | 结论 |
|---|---|---|
| 1 | 远端指向+只读哨兵 | ✅ 一致 |
| 2 | audit/full-20261019 分支存在 | ❌ 已不存在（合并后清理，快照过时） |
| 3 | .gitignore 含 ARTEX/ | ✅ 一致 |
| 4 | 钉钉 io.ReadAll 错误处理 | ✅ 一致 |
| 5 | repo-map.mjs 无硬编码 | ✅ 一致 |
| 6 | 基线为 develop 祖先 | ✅ 一致 |

**5/6 一致；1 条快照过时（分支已清理），无发现内容性错误。**
