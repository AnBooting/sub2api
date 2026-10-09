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
