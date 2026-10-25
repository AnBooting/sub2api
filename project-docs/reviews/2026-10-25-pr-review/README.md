# 2026-10-25 PR 审查复现包

结论见 REVIEW.md，精确 head 见 manifest.json，实测结果见 TEST-RESULTS.md。

把三个精确 head 的源码放入**隔离目录**，使用本机 Go 1.27.3（也可用满足 go.mod 的版本）执行：

```powershell
./run.ps1 -PR11Root C:/review/pr11 -PR12Root C:/review/pr12 -PR13Root C:/review/pr13 -GoExe C:/tools/go/bin/go.exe
```

依赖需预先安装。脚本只临时放入测试、运行本机 unit 测试，并在 finally 删除自身创建的文件；不执行 Git、部署、真实数据库操作或付费上游请求。已存在同名测试时拒绝覆盖。

在 manifest 所列 head 上，预期退出 1：PR #11 一个新负例失败；PR #12 两个新负例失败。旧版正例与视频隐私用例用于区分已经修复的内容。迁移负例复用项目 sqlmock 装置，证明 runner 未验证旧历史即根据重复约束放行，不是 PostgreSQL 实例上的部分升级实测。Live 用例调用真实 observer/finalizer，依赖替身只注入故障和生产唯一键契约。

本次没有实现修复，因此不把任何代码改动或变异验证列为已完成。修复后应更新旧迁移正例的数据库历史装置，使它同时证明旧 filename/checksum 确实匹配，并补真实数据库升级演练。
