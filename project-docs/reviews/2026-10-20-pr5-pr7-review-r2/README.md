# PR #5 / #7 第二次复审

2026-10-20，PR #5 head `e417c3508856400888f0581dfeb46006fd3b4ecf`，PR #7 head `d52241fdbf7da8b42d6a6e180e5d5c55865a19a0`。

本包包含本轮 Review、三项上轮回归测试，以及新增 Live 计费复现测试。无源码快照、依赖、凭据或线上请求。

准备对应 PR 源码；Go 可用，前端依赖用 pnpm 9 安装，然后执行：

```powershell
./run.ps1 -PR5RepoRoot C:/path/to/pr5
./run.ps1 -PR7RepoRoot C:/path/to/pr7
```

可同时传入两个参数。脚本临时复制测试，结束后删除，不执行 Git 命令。
当前 PR #5：原问题测试通过，新增测试预期失败；PR #7：路由复现测试通过。
新增测试复用 `e417c350` 已有的 `newLiveSettlementTestService` 等测试夹具，因此需要该提交或其后继版本。

`TestReviewLiveCommandPreservesKeyLimitCosts` 比较真实 Live finalizer 生成的命令，与配置限额密钥经过生产 `buildUsageBillingCommand` 生成的命令；不是数据库或鉴权端到端测试。其他新增测试直接调用真实 finalizer。
