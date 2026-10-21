# PR #5 Review 交付包

本包包含 Review 文档、两组可执行回归测试和 PowerShell 运行脚本，不包含业务源码或依赖。

- 仓库：https://github.com/AnBooting/sub2api
- PR：https://github.com/AnBooting/sub2api/pull/5
- 审查版本：`4da44953062b92407f2fcb50cebecbac9747dc64`
- 对比基线：`d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`

## 文件

- `REVIEW.md`：两项 P2 发现、触发条件、证据与修复建议。
- `tests/keyusage-icons.spec.ts`：读取生产页面的实际 SVG 模板和四个图标常量，挂载模板并断言图标节点仍存在。
- `tests/repo-map-impact.test.mjs`：执行实际影响分析 CLI，检查退出状态与搜索结果。
- `run.ps1`：运行上述测试，并在 finally 中删除临时复制到前端的测试文件。

## 运行

准备已经安装前端依赖的本地仓库。请在独立检出中使用审查版本；脚本不会切分支、拉取依赖、修复代码或联网。

```powershell
pwsh -File .\run.ps1 -RepoRoot 'C:\path\to\sub2api'
```

脚本需要 Node.js、Git，以及目标仓库已有的 `frontend/node_modules`。它会在 `frontend/src/__tests__/` 临时放入一个带唯一名称的测试文件，运行完成后自动删除。

审查版本的预期结果：图标测试 4 项失败，CLI 测试 1 项失败，脚本退出码为 1。这些失败就是本包报告的回归复现。修复后应全部通过；图标修复同时应保留消毒保护。

本包不是修复补丁，也不包含后端新测试。Review 中提及的后端定向验证使用 PR 已有测试，命令见 `REVIEW.md`。
