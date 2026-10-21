# 2026-10-21 上游 PR 审查复现包

精确基线和 PR SHA 见 manifest.json；结论见 REVIEW.md。

环境：Go 1.27.2、Node、PR 对应的前端依赖。前端安装必须使用 `corepack pnpm@9 install --frozen-lockfile`。

把四个精确 SHA 的源码分别放在四个目录后执行：

```powershell
./run.ps1 -PR5Root C:/review/pr5 -PR7Root C:/review/pr7 -PR8Root C:/review/pr8 -PR9Root C:/review/pr9 -GoExe C:/tools/go/bin/go.exe
```

脚本不会拉代码或执行 Git 命令。临时加入测试文件，全部检查完成后在 finally 中删除；存在同名文件时拒绝覆盖。请使用隔离源码目录。Go 依赖需预先可用。

在本轮审查 SHA 上，脚本**预期退出 1**：新增 6 个顶层 Go 测试失败，代表前 5 项发现（视频信息泄露有 DTO 与 worker→DTO 两个用例）；原有 5 个顶层 Go 复现和前端路由回归应通过。

审查期间新增合入的 PR #10 另查出根 Dockerfile 版本遗漏，第 6 项发现可单独执行：

```powershell
python tests/review_toolchain.py C:/review/pr10
```

当前 develop 与 #10 的这一构建契约检查预期失败。对应的 Go 最低版本报错也已用实际 PR #10 go.mod 与本机 Go 1.27.2 重现；完整 Docker 构建未运行。

测试边界：

- Live 测试调用实际 finalizer/observer；余额仓储注入瞬时失败。日志一致性用例模拟生产 SQL 已有的唯一键去重契约，并未连接 PostgreSQL。
- 迁移测试调用真实迁移执行器与嵌入的 251 SQL；sqlmock 模拟已执行旧 243 的数据库返回重复约束错误。这是执行流程复现，不是真实 PostgreSQL 升级演练。
- 视频信息泄露同时调用真实 worker 与用户 DTO；仓储使用项目现有测试替身。
- 路由用例挂载实际 App 与 LoginView。

本包未实现修复，因而未做修复回退的变异验证。补丁完成后应以完整旧库升级、真实仓储一致性及重启恢复测试复验。
