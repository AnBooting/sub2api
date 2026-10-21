# 仓库状态与全量审计执行记录（2026-10-19）

> 审计基线：`develop = origin/develop = d5c2b5edb`；本记录基于 `audit/full-20261019` 分支。
> 最终结果以 Git、测试输出和 `F:\sub2--audit\full-20261019\` 下原始 artifacts 为准。

## 1. 基线

- HEAD：`d5c2b5edbcb0335cb80e4cfd7371098cf8982d8c`
- 本地 `develop` 与 `origin/develop` 一致，无未推送本地提交
- `origin`：AnBooting/sub2api；`upstream`：Wei-Shaw/sub2api（只读）
- `main` 未改动；审计分支 `audit/full-20261019` 记录本轮修复
- 未跟踪 `ARTEX/`：本地只读分析对象，本轮仅加入 `.gitignore`，未执行、未提交、未删除

## 2. 执行方式

1. OCR：`F:\tools\ocr\opencodereview.exe` v1.12.13，`colink` provider，端点 `http://colink-faster.site/v1`，模型 `deepseek-v4.1-flash`，`ocr llm test` 成功。
2. 分批 `ocr scan`：B3（handler 非 admin/dto/yunmao）、B1c（internal/pkg）、B9（deploy/tools）成功；其余批次因远端扫描任务全部失败退出（错误为 `scan failed: all N file scan(s) failed`），已多次降并发/降预算/缩小目录重试仍失败。
3. Agent Team：`audit-backend-quality` 与 `audit-frontend` 完成；`audit-backend-security`、`audit-deploy-docs` 在会话期间未返回可归档结论，结论不视为完成。

## 3. 本轮修复

- `auth_dingtalk_client.go`：全部上游响应 `io.ReadAll` 错误改为返回错误，不把截断数据当成功
- `auth_dingtalk_oauth.go`：`buildDingTalkUpstreamClaims` 对 nil staff 做空值兜底；回调后清理绑定 cookie
- admin group/redeem/proxy stats：不再返回伪造零统计，改为 `implemented:false` + 明确原因
- `antigravity/client.go`：`LoadCodeAssist`/`OnboardUser` 上游非流式响应限制为 2 MiB
- `tools/repo-map.mjs`：移除本机 Go 硬编码，`execFileSync` stdin 设为 ignore，`impact -foo` 使用 `--`
- `tools/repo-intelligence.mjs`：manifest 损坏/缺 `sourceRoot` 给出明确错误；query/mcp 检查 `result.error`
- 前端：`shopErrorKey('order_not_paid')` 映射与 zh/en 文案；`KeyUsageView` 的 `iconSvg` 经 `sanitizeSvg` 渲染

## 4. OCR 覆盖与未覆盖

| 批次 | 范围 | 结果 |
|---|---|---|
| B3 | backend/internal/handler 主包 | 成功，79 文件，15 条意见 |
| B1c | backend/internal/pkg | 成功，87 文件，11 条意见，3 条预算/未完成警告 |
| B9 | deploy/tools | 成功，26 文件，17 条意见，2 条预算/未完成警告 |
| 其余 | cmd/config/domain/server/admin/dto/yunmao/frontend | 远端 LLM 任务失败，未产生 JSON |

未覆盖的目录必须在后续轮次用其它审查方式补上；本记录不声称全仓库 OCR 已完成。

