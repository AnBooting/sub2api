# 移植步骤与兼容说明

## 1. 数据与协议约定

「国模」是 UI 分类，实际 OpenAI 兼容账号仍为 `platform: "openai"`、`type: "apikey"`。不要新增一个后端网关平台 `domestic`。

| 对象 | 字段 | 约定 |
| --- | --- | --- |
| 分组 | `domestic_provider` | 字符串，普通分组为空字符串 |
| 账号 | `extra.domestic_provider` | 与分组相同的厂商 ID |
| 账号 | `extra.domestic_provider_label/company` | 展示信息，不能代替后端 ID 校验 |
| 账号 | `credentials.base_url/api_key/model_mapping` | 复用目标项目 API Key 凭证结构 |
| 账号 | `extra.openai_responses_mode` | `auto` / `force_responses` / `force_chat_completions` |
| 定价 | `provider + model` | 厂商隔离、具体模型定价；不能落入普通 GPT 兜底价格 |

厂商集合必须前后端一致。Base URL 是源站预设，部分为国际站端点，并不适用于所有地区的 Key。百川、零一万物和自定义需要填写地址。实际接入前核对服务商端点、模型名和地域权限。

## 2. 数据库与后端字段

1. 查看目标迁移记录，选择三个未占用的新编号，按 `196 → 197 → 198` 的原始顺序移植 SQL。不要替换或重写对方已执行的迁移。
2. 在 `ent/schema/group.go` 增加 `domestic_provider` 字段，使用目标项目的 ent 生成命令重新生成。不要从主站覆盖生成文件。
3. 合并 `service/group.go`、`admin_service.go`、分组 handler 请求结构、DTO 与 mapper；更新请求字段保持指针语义，区分未传入和清空。
4. 合并 `group_repo.go`、`api_key_repo.go` 中所有增删改查/扫描/投影/复制字段。API Key 鉴权缓存中的 Group 快照必须携带该字段；按目标现有机制使旧缓存失效。
5. 合并 `admin_group.go`、`admin_group_duplicate.go`、`admin_account.go` 和 `account_service.go` 的厂商匹配校验。创建、编辑、复制账号、批量更新、通用更新入口都要校验，事务失败不能残留半次更改。

源版本的一个兼容差异：Go 的 `NormalizeDomesticProvider` 允许 OpenAI 和 Anthropic，而 196/198 的数据库约束仅允许 OpenAI。截图对应的国模入口走 OpenAI，本包的验收范围也是 OpenAI。不要据此宣称支持 Anthropic 国模分组；若扩展到 Anthropic，需另写升级迁移统一约束并补数据库测试。

## 3. 价格服务与计费

新增 `core/backend` 中的价格 service、repository、handler；依赖目标项目的 `ModelPricing`、平台常量、错误/响应辅助函数、Gin 和 SQL DB。若 Go module 路径不同，调整 `github.com/Wei-Shaw/sub2api` 导入前缀。

按 `reference/shared` 合并 repository/service/handler 的 wire ProviderSet、AdminHandlers 字段、构造函数和 admin 路由，再使用目标项目 wire 命令生成装配代码。路由必须位于管理员鉴权分组内：

| 方法 | 相对管理路由 | 行为 |
| --- | --- | --- |
| GET | `/domestic-pricing/catalog` | 模型目录 |
| PUT | `/domestic-pricing/catalog` | 请求体指定 provider/model，写入覆盖价 |
| PUT | `/domestic-pricing/catalog/:provider/:model` | 路径指定模型覆盖价 |
| DELETE | `/domestic-pricing/catalog/:provider/:model` | 删除覆盖价，回到内置目录 |

前端 client 使用 `/admin/domestic-pricing/catalog`；实际 `/api/v1` 等前缀以目标项目为准。有斜杠的模型名优先使用请求体版 PUT，并确认 DELETE 的路由编码处理。

合并 `model_pricing_resolver.go`、`billing_service.go`、`openai_gateway_usage.go` 与各 OpenAI handler 的请求前检查。将分组的 DomesticProvider 贯穿解析、转发前校验和最终结算。若目标有 Anthropic 兼容转发，参考 `gateway_usage_billing.go`；不要为本功能整文件移植该网关。

**源站价格政策：国内人民币标价数字直接按站内 USD 数字计账，即界面写的「1 元 = 1 美元，无汇率换算」，之后叠加分组/用户有效倍率。这是源站的经营规则，不是实际汇率。** 本包保留原行为，没有重新核验服务商现价。朋友若按真实美元成本经营，必须同步修改目录、界面说明和计费测试，不能只改货币标签。

目录中的单价单位是 token，页面显示每百万 token。`pending_pricing` 或 null 单价表示尚未定价，不能转成零费用，也不能回退 GPT 价格。输入/输出覆盖价必须为正数；缓存单价允许显式零值。当前内置峰谷规则使用上海时区；管理员覆盖价按覆盖值计费。内置模型名、别名、上下文档位、来源 URL、价格均为源码快照，不是实时订阅服务。

目标没有 `ModelPricingResolver` 时，应在现有计费入口实现同样的厂商隔离和缺价拒绝行为，不要为编译通过删除安全检查。定价源码用到的 `ModelPricing` 缓存写入和长上下文字段，需适配目标类型。

## 4. 国模分组 UI

新增 `DomesticProviderPicker.vue`、`domesticProviders.ts`、`api/admin/domesticPricing.ts`。把 API 加入 `api/admin/index.ts`。合并 `types/index.ts` 的分组字段。

在 GroupsView 的创建、编辑平台选项增加「国模 · OpenAI 兼容」。选择时内部仍写 OpenAI，另外写 domestic_provider；切回普通 OpenAI 时清空。添加价格目录加载、loading、错误反馈、provider picker，打开编辑时恢复厂商值。复制账号来源按真实平台及厂商筛选；切换后清理不兼容选择。

UI 复用 Vue 3、Tailwind、vue-i18n、现有 Select/Icon 及 API client。ModelIcon 中包含已内联的品牌 SVG，不需要引入 React 图标组件。目标已有 ModelIcon 时合并品牌映射，保留对方原图标。Select/Icon 的参考文件只用于补齐必要 props/图标，不能覆盖对方整个设计系统。

GroupBadge、GroupOptionItem、GroupSelector、PlatformTypeBadge 和 AccountGroupsCell 的国模显示/过滤一并合并。共享组件中的峰时费率依赖目标的 app store 与 peak-rate 工具；若目标没有，保留原组件布局，只移植 domestic_provider 相关逻辑，勿把无关峰时系统整套带入。

## 5. 添加和编辑账号 UI

原始完整弹窗在 `reference/shared/frontend/src/components/account/`，相关片段逐段列在 MERGE-INDEX。

- CreateAccountModal：平台栏国模入口、厂商网格、provider 状态/切换函数、Base URL 自动填充、默认命名、模型与分组选择重置、API Key 表单、extra 写入以及 GroupSelector 的 domestic-provider 属性。
- EditAccountModal：识别 `extra.domestic_provider`、厂商网格、回填、协议选项、保存合并 extra。修改时保留其它 extra 字段，避免删除目标项目自己的设置。
- AccountsView：把账号厂商信息传给 PlatformTypeBadge；分组列表同样显示厂商图标。

**源版创建弹窗的兼容注意事项：** `selectPlatformFamily` 会把本地协议状态设为 `force_chat_completions`，但 `buildOpenAIExtra` 的国模分支提前返回，只写厂商信息，没有把协议状态持久化。源项目另有自动 Responses 探测逻辑。因此不要只复制国模卡片就假设能够正确调用上游。

目标已有自动探测时，保留并验证探测结果；目标没有时，建议在国模创建 payload 中显式写 `extra.openai_responses_mode = "force_chat_completions"`，让仅支持 Chat Completions 的服务商正常工作。这是移植适配建议，未修改原始参考文件。提供的账号示例采用显式模式。编辑允许切回自动或强制 Responses。

`reference/protocol` 包含选择模式、自动探测和直转/回退参考。它依赖目标已有 HTTP、SSE、apicompat 等基础设施，不是独立协议 SDK。老版本需沿现有架构补齐 Chat Completions 直转；若开放 `/v1/responses`，还需完整转换并验证流式工具调用。不要直接覆盖几千行网关以追齐无关功能。

## 6. 验证与上线

在隔离分支和测试数据库集成，重编号迁移后更新迁移测试中的名称。测试辅助 mock 来自原测试套件，目标没有时按接口补充，避免把大量不相关测试照搬。

完成目标项目的类型检查、相关前后端测试和构建；再用目标站点自己的测试 Key 做真实厂商调用和结算核对。对方生产部署、数据库变更与账号配置由对方执行，本移植包不含部署命令或自动改库脚本。
