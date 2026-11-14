<!--
  提 PR 前自检（详见 project-docs/04-git-workflow.md）：
  1. gh 在本目录没有默认仓库：所有 gh 命令必须带 --repo AnBooting/sub2api
  2. base 是 develop（main 只镜像上游，不接收 PR）
  3. 本地门禁：pwsh -File tools/pre-push-check.ps1 -Mode full
  4. 测试结果来自实际输出；未执行/失败的检查如实标注
  5. 不包含 .env、data/、凭据或大体积二进制
-->

## Why

<!-- 要解决的问题、需求或约束。背景写进正文，不要只给外链。 -->

## What Changed

<!-- 行为/结构变化；重要取舍、兼容与迁移信息；生成物或机械变更写清工具与人工改动范围。 -->

## Testing

<!-- 每条：命令 → 退出码/结果。未执行或失败的项目也要写。 -->

- `pwsh -File tools/pre-push-check.ps1 -Mode full` →
- 其他针对性验证（单测/集成/手工/变异验证）→

## Risk / Rollback

- **Risk**：<!-- 已知风险、未覆盖范围、需要人工确认的部署前置 -->
- **Rollback**：<!-- git revert / 标签回退 / 配置回滚 -->

## Checklist

- [ ] 分支基于最新 `develop`，未把其他写者的改动带进本 PR
- [ ] 提交信息符合 Conventional Commits；破坏性变更已用 `!` 或 `BREAKING CHANGE:` 标注
- [ ] 迁移编号接在最新编号之后（当前最新 **260**，新增从 **261** 起）；未改写既有迁移
- [ ] i18n 文案已同时补 `zh` 与 `en`
- [ ] 缺陷修复做过变异验证（回退修复后测试确实失败）
- [ ] 评审证据归档到 `project-docs/reviews/<日期>-pr<N>-review/`（安全/契约/计费类必填）
- [ ] **合并方式已确认**：合并 PR / 直推 `develop` / 删远端分支是否已获明确授权（默认只提交与推送）
