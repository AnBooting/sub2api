# 2026-10-26 当前 PR 复审包

精确 SHA 和预期失败项见 manifest.json；结论见 REVIEW.md。

在最新 PR #13 的隔离源码中运行（Go 1.27.3、Python、已有 Go 依赖）：

```powershell
./run.ps1 -SourceRoot C:/review/pr13 -GoExe C:/tools/go/bin/go.exe
```

PR #12 共用相同 Live 与迁移源码，也可运行完整套件。只检查 PR #11 的共同迁移问题：

```powershell
./run.ps1 -SourceRoot C:/review/pr11 -GoExe C:/tools/go/bin/go.exe -MigrationsOnly
```

当前精确 head 预期退出 1：完整模式恰好 3 个顶层负例失败；MigrationsOnly 仅旧集成断言的重放失败。旧的缺历史、检查点+账本同时失败、TotalCost 重复统计用例现在应通过。

脚本临时放入 4 个测试（迁移模式 2 个），并从待审源码动态提取原集成测试的旧库升级场景，保留其真实断言。仅替换 build tag、函数名和 DB 变量名，停止在后续无关的未登记迁移场景之前；通过 sqlmock 调用真实迁移执行器重放。**没有启动 PostgreSQL，也不声称执行了真正的 integration 套件。**

Live 故障复现从真实 observer 入口开始。隐私复现调用真实 finalizer 与用户错误列表，存储替身仅做生产 SQL 的字段映射。诊断中的地址与表名是测试数据，没有生产凭据。

脚本不执行 Git、不部署、不发付费请求。存在同名测试时拒绝覆盖，finally 删除本包创建的测试。请使用隔离源码。新 head 改变原测试结构或测试装配时，应按新代码检查装置，不要将装置编译失败当业务缺陷。
