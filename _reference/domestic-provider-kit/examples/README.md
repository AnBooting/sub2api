# 示例说明

group-create.json 和 account-create.json 分别是管理员分组/账号创建接口的请求体示例。先创建分组，再把实际返回的分组 ID 填进账号 group_ids 数组。空数组示例不会绑定任何分组。

API Key 为明确占位值，不可直接用于上游。协议模式字段是移植建议，见 INTEGRATION.md，不是原始弹窗实际请求的逐字录制。目标可能增加了其它必填字段，应沿用对方的 API 契约。

没有提供真实价格覆盖示例，以免将示例数字误用于生产；覆盖价字段和单位见 core 中的 handler 与 INTEGRATION.md。
