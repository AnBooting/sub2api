# Model Pricing Data

This directory contains a local copy of the mirrored model pricing data as a fallback mechanism.

## Source
The original file is maintained by the LiteLLM project and mirrored into the `price-mirror` branch of this repository via GitHub Actions:
- Mirror branch (configurable via `PRICE_MIRROR_REPO`): https://raw.githubusercontent.com/<your-repo>/price-mirror/model_prices_and_context_window.json
- Upstream source: https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json

## Purpose
This local copy serves as a fallback when the remote file cannot be downloaded due to:
- Network restrictions
- Firewall rules
- DNS resolution issues
- GitHub being blocked in certain regions
- Docker container network limitations

## Update Process
The pricingService will:
1. First attempt to download the latest version from GitHub
2. If download fails, use this local copy as fallback
3. Log a warning when using the fallback file

## Manual Update
To manually update this file with the latest pricing data (if automation is unavailable):
```bash
curl -s https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json -o model_prices_and_context_window.json
```

## File Format
The file contains JSON data with model pricing information including:
- Model names and identifiers
- Input/output token costs
- Context window sizes
- Model capabilities

Last updated: 2025-08-10

## Native CNY prices and credit billing

管理员可在 **系统设置 → 功能开关 → 模型价格与额度计费** 勾选“使用国模人民币价格源”。默认关闭，沿用原有计价方式。

- 开启后，国模按原始 CNY 单价、Claude/GPT 等按原有 USD 单价计算；¥1 或 $1 的模型费用都对应 1 额度，不换汇，已有分组倍率继续生效。
- 余额充值的基础到账比例固定为 1：人民币支付 100 或美元支付 100 均到账 100 额度；赠送额度、手续费和订阅商品换算继续使用各自配置。历史余额与用量金额不重算。
- 主源为 [basellm/llm-metadata](https://github.com/basellm/llm-metadata) 的明确 `CNY` 字段或 `currency_options.CNY`；[LLM Abacus](https://llmabacus.com)（[llm-prices-cn](https://github.com/szp2005/llm-prices-cn)，CC-BY-4.0）补齐缺失模型，只使用 `billing_currency=CNY` 的原币价格，忽略美元换算后的人民币字段。
- 人民币 Token 价支持输入、输出、缓存、上下文阶梯和峰谷时段。模型广场展示当前时段的参考价格。图片/视频等按次价格需配置手动价卡。
- 分组/渠道手动价卡优先，可指定 USD 或 CNY。人民币 Token 手动价必须填写输入、输出、缓存读取和缓存写入；免费项显式填 0，避免继承美元字段。
- 缺少有效人民币模型价时拒绝转发。上游返回了数据源没有定义的缓存计量时，计费返回错误，不记录为免费请求；应补全手动价卡。独立的推理 Token 单价暂需手动价卡。
- “允许远程价格拉取”关闭后，本实例停止启动拉取、定时拉取、哈希检查和手动刷新，取消在途请求并拒绝迟到结果。保留本地缓存和手动价，开关存入数据库，重启仍有效。多个服务实例需重启以读取同一份数据库开关。
- 缓存文件为 `pricing.data_dir/model_pricing_cny.json`；没有缓存且拉取关闭时，国模需要手动价卡。来源、原币种和实际采用的单价写入新用量记录的 `pricing_snapshot`。
- 账号成本统计仍用 USD，人民币售价不会被直接累加为美元账号成本；统计规则只接受 USD。

部署前需执行数据库迁移 `243_native_currency_pricing.sql`（正常启动迁移流程自动执行）。配置键与默认源见 `deploy/config.example.yaml` 的 `pricing` 部分；已有数据库开关优先于配置文件。
# 2026-10-10 评审修复

- 保存之外，网关请求前检查和计费入口也拒绝不完整的 CNY 手工 Token 卡（输入、输出、缓存读、缓存写四项）；显式 0 仍合法。
- 官方 DeepSeek CNY/USD 价共用北京时间的节假日日期与周末判断，周末补班仍为谷价。默认附带国办发明电〔2025〕7号的 2026 年放假日期，后台可维护 `deepseek.holidays`（完整 ISO 日期数组）；非空保存必须包含当前北京年份，空数组恢复内置日历。自定义表缺年时回落内置表，两者均缺年则禁用高峰加价并复用 Ops P1 告警；状态接口提供 `deepseek_calendar_years` 与 `deepseek_calendar_error`，仍需逐年维护完整日期。损坏的持久化日历回落内置值、告警，不阻断启动。管理员 PUT `/api/v1/admin/channels/pricing/holidays` 受 step-up 验证，body 为 `{"dates":["2026-10-01"]}`，审计记录修改前后日期。
- 分组/渠道自定义价格保留运营者售价，不自动继承官方峰谷规则；需分时变化时显式配置 `time_pricing`。USD 渠道继承的目录字段按请求 `PricingAt` 解析，避免历史补账套用当前 Pro/Flash 价。
- 请求入口同时捕获不可变日历快照，预留、异步结算与重新解析的 USD/CNY 官方峰谷价沿用该快照；WebSocket 每个 turn 单独捕获。日历编辑只影响新请求。普通价表和分组配置的完整版本快照仍未实现。查价不执行告警 I/O，非 DeepSeek CNY 查价不做日历判定；缺年告警由启动、同步及现有周期任务检查当前年份后发送。告警配置读取失败会释放抑制占位，允许后台重试。
- 国模匹配改为明确的模型族边界，并限制 Step/Spark 的具体族名；价表中的明确国模条目优先识别。缓存写入价格缺失仍拒绝计费，按提供商/缺失维度抑制重复 WARN，不假设缓存免费。
- 价格同步失败保留最近有效价格，状态提供 CNY/USD 错误，并复用现有 Ops 告警邮件开关、P1 阈值、收件人、限流、模板和 SMTP；哈希不可达或为空、价格/哈希持久化失败均作为同步失败。外发邮件只含来源与错误类别，不含源 URL 或响应体。同源持续故障一小时内抑制重复邮件，恢复后解除抑制；全部投递失败允许下次同步重试，部分成功时保留抑制窗口。手动关停导致的取消不告警。无需启用新的监控任务。
