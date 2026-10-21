# PR #7 Review 与回归测试

- PR：https://github.com/AnBooting/sub2api/pull/7
- Head：`067d74dbcef41e482507c0db9fcb90b8625cda15`
- Base：`d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`

`REVIEW.md` 包含四项 P2 发现与验证边界；`tests/review-pr7.spec.ts` 包含三个自动复现测试；`tests/fixed-overlay-fixture.mjs` 是第四项的本机浏览器 CSS 复现页；`browser-evidence.json` 保存已测量结果。

在已安装前端依赖的目标检出上运行：

```powershell
pwsh -File .\run.ps1 -RepoRoot 'C:\path\to\sub2api'
```

脚本使用目标仓库的 Vitest 配置，在 `frontend/src/` 临时复制测试，结束后自动删除。需要 Node.js、PowerShell 和仓库已有依赖，不会安装依赖或切分支。PR head 上三个测试都应失败，脚本退出码 1；修复后应通过。

第四项需要浏览器：先构建目标前端，再运行：

```powershell
node .\tests\fixed-overlay-fixture.mjs 'C:\path\to\sub2api'
```

打开 `http://127.0.0.1:18747/review-check.html` 和 `http://127.0.0.1:18747/without-animation.html` 比较弹窗位置。后者仅移除页面动画 class，作为最小对照。该页面使用目标检出的生产 CSS、AppLayout 主容器标签与 TotpSetupModal 根标签，主内容高度固定为 1600px；不是真实后端联调页面。按 Ctrl+C 停止服务。

本包只提供审查与复现，不包含修复补丁或业务源码。
