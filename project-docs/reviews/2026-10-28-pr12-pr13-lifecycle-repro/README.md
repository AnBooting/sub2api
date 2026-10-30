# 2026-10-28 生命周期测试退避复现

适用 AnBooting/sub2api：PR #12 `ed3029b71d27c96fa3db11d80d361461e6530407`、PR #13 `24dd8af2e3378d2104405cb71d4ad80538f8d966`。需要本机 Go 1.27.3 和项目依赖。

在 PowerShell 中运行：

```powershell
.\run.ps1 -BackendPath 'C:\path\to\sub2api\backend'
```

预期：一个顶层测试失败，打印生产退避 `[5s 30s 2m0s 5m0s] (total 7m35s)`。这是用于证明缺陷的负例，不是期待全绿的回归套件。复现调用仓库既有生命周期测试，记录其调用生产退避函数时请求的时长，将实际睡眠设为零，因此不必等待 455 秒。它不调用付费上游或真实数据库。

脚本只注入一个临时测试文件，拒绝覆盖已有同名文件，并通过 finally 删除；不会改动生产源码、分支或 Git 配置。调用方需要自行选择正确的审查 checkout；脚本不负责 fetch 或切换分支。

建议修正原生命周期测试的时钟隔离后重新运行此负例及完整 unit 套件。只在原测试中缩短 `liveSettlementRetryInterval` 并注册 cleanup，复现应转绿；不要降低生产重试预算。
