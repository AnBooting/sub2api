# PR #5 / #7 复审测试包

审查日期：2026-10-20（Asia/Singapore）。结论见 `REVIEW.md`。

本包只包含 Review 文档与复现测试，不含源码快照、依赖或凭据。
测试直接调用生产函数或挂载实际 LoginView，不发送真实上游请求、不连接生产服务。

## 运行

分别准备 PR #5 和 PR #7 的本地源码；Go 工具链须可用，前端依赖使用 pnpm 9 安装。
在 PowerShell 中执行：

```powershell
./run.ps1 -PR5RepoRoot C:/path/to/pr5
./run.ps1 -PR7RepoRoot C:/path/to/pr7
```

也可以同时指定两个参数。脚本临时复制测试文件，完成后清理，不执行 Git 命令。
在下列受审提交中测试预期失败；修复后应退出 0。

| PR | Head | 测试与预期 |
| --- | --- | --- |
| #5 | d6a990f60b97b5d270382d8fddcda35ef0da8496 | Live 余额和订阅两场景均应实际结算；panic 的唯一访问日志应为 500 |
| #7 | 08924854d77c05b6d70bc29e66716cf496a61f58 | 使用 App.vue 的真实路由出口离开 LoginView 后应渲染目标页 |

Go 测试复用受审版本现有 stub。前端仅 mock API、stores 和非关键子组件，保留真实 RouterView、Transition 和 LoginView。
