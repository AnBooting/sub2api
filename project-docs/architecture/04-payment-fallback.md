# 04 · 支付充值与兜底分组 — 架构侦察

> 任务 task-4（owner: recon-pay）｜分支 develop｜HEAD `9150a7a86`｜基线上游 v0.2.15 `3a6fd1c9`
> **只读侦察**，未修改任何源码。所有行号基于当前工作树。

---

## 0. 状态 / 证据来源 / 未确认项

### 0.1 状态

| 项 | 状态 |
|---|---|
| 支付架构盘点 | 完成 |
| 充值链路（下单→支付→回调→入账） | 完成 |
| 卡密/兑换码机制 | 完成 |
| 订单表结构 + 幂等机制 | 完成 |
| 卡网对接 API 现状与缺口 | 完成（缺口为**分析结论**，非源码事实） |
| 兜底分组机制（两个字段） | 完成 |
| 分组数据模型 + 账号绑定 | 完成 |
| 「默认兜底 AWSB 分组」可行性 | 部分完成（依赖需求澄清，见 §B4） |

### 0.2 证据来源

- 支付核心：`backend/internal/payment/`（`types.go` `registry.go` `load_balancer.go` `amount.go` `fee.go` `currency.go` `crypto.go` `wire.go`）+ `payment/provider/`（5 个渠道实现 + `factory.go`）
- 支付服务：`backend/internal/service/payment_*.go`（26 个文件，核心 `payment_fulfillment.go` `payment_order.go` `payment_service.go` `payment_config_service.go`）
- Handler：`handler/payment_handler.go` `handler/payment_webhook_handler.go` `handler/admin/payment_handler.go` `handler/admin/redeem_handler.go` `handler/admin/idempotency_helper.go`
- 路由：`server/routes/payment.go` `admin.go` `user.go`
- Schema：`ent/schema/{payment_order,payment_provider_instance,payment_audit_log,redeem_code,idempotency_record,group,account_group,api_key}.go`
- 调度：`service/gateway_scheduling.go` `handler/gateway_handler.go`
- 上游文档：`docs/ADMIN_PAYMENT_INTEGRATION_API.md` `docs/PAYMENT.md` `docs/PAYMENT_CN.md`
- 需求原文：`project-docs/13-requirements-mapping.md`（需求 3/4）

### 0.3 未确认项

| # | 内容 | 原因 |
|---|---|---|
| U1 | **「AWSB」的确切含义** | `git grep -rn -i awsb` 全仓库 **0 命中**，仅出现在 `project-docs/13-requirements-mapping.md`。推断是部署环境的分组名（可能指 AWS Bedrock 账号分组），**未确认** |
| U2 | 「默认兜底」触发场景 | 4 种可能语义，需需求方选择（§B4.1） |
| U3 | 「卡网」是发卡平台还是卡密批发站 | §A5.3 |
| U4 | 嵌入式充值页与卡网谁嵌谁 | §A5.3 |
| U5 | `payment_orders` 相对上游是否有 fork 魔改 | 本任务未做 diff，**未确认** |
| U6 | 是否有按 `code` 精确查兑换码的独立端点 | List 支持 `search`（`redeem_handler.go:90`），能否精确匹配 code **未确认** |

---

# A. 支付与充值

## A1. 支付架构

### A1.1 目录结构

| 路径 | 职责 |
|---|---|
| `payment/types.go` | 支付类型常量、订单状态常量、Provider DTO |
| `payment/registry.go` | 线程安全 Registry：PaymentType → Provider |
| `payment/load_balancer.go` | 服务商实例负载均衡（round-robin / least-amount） |
| `payment/{amount,fee,currency,crypto}.go` | 金额/手续费/币种/实例配置加解密 |
| `payment/wire.go` | 依赖装配 |
| `payment/provider/factory.go` | `CreateProvider(providerKey, instanceID, config)` |
| `payment/provider/{easypay,alipay,wxpay,stripe,airwallex}.go` | 5 个渠道实现 |

### A1.2 支持的渠道

支付类型常量（`payment/types.go:11-21`）：`alipay`、`wxpay`、`alipay_direct`、`wxpay_direct`、`stripe`、`card`、`link`、`easypay`、`airwallex`。

Provider 工厂（`payment/provider/factory.go:10-24`）只识别 5 个 provider key：easypay / alipay / wxpay / stripe / airwallex；card、link、alipay_direct、wxpay_direct 由 `GetBasePaymentType()`（`types.go:83-98`）归一化到基础类型。

### A1.3 注册与装配

- `Registry.Register(p)` 按 `p.SupportedTypes()` 展开注册（`registry.go:27-33`），重复注册覆盖。
- `Registry.GetProviderKey(t)`（`registry.go:59-67`）在 webhook 校验时确定"该订单应由哪个 provider 验签"。
- 服务商实例表 `payment_provider_instances`（`ent/schema/payment_provider_instance.go:31-66`）：provider_key、config（**加密存储**）、supported_types、enabled、payment_mode、sort_order、limits、refund_enabled、allow_user_refund。
- 币种默认值 `DefaultPaymentCurrency = "CNY"`（`payment/currency.go:10`）。

---

## A2. 充值流程（下单 → 支付 → 回调 → 入账）

### A2.1 路由全景（`server/routes/payment.go:27-133`）

| 分组 | 路径 | Handler | 鉴权 |
|---|---|---|---|
| 用户 | GET /api/v1/payment/{config,checkout-info,plans,limits} | GetPaymentConfig / GetCheckoutInfo / GetPlans / GetLimits | JWT |
| 用户 | POST /api/v1/payment/orders | CreateOrder | JWT |
| 用户 | POST /api/v1/payment/orders/verify | VerifyOrder | JWT |
| 用户 | GET /api/v1/payment/orders/{my,:id} | GetMyOrders / GetOrder | JWT |
| 用户 | POST /api/v1/payment/orders/:id/{cancel,refund-request} | CancelOrder / RequestRefund | JWT |
| 用户 | GET /api/v1/payment/orders/refund-eligible-providers | GetRefundEligibleProviders | JWT |
| 公开 | POST /api/v1/payment/public/orders/verify | VerifyOrderPublic | 无（IP 限流 20/min） |
| 公开 | POST /api/v1/payment/public/orders/resolve | ResolveOrderPublicByResumeToken | 无（签名 resume token） |
| Webhook | GET+POST /api/v1/payment/webhook/easypay | EasyPayNotify | 无（验签） |
| Webhook | POST .../webhook/{alipay,wxpay,stripe,airwallex} | AlipayNotify / WxpayNotify / StripeWebhook / AirwallexWebhook | 无（验签） |
| 管理 | GET /api/v1/admin/payment/{dashboard,config}、PUT .../config | GetDashboard / GetConfig / UpdateConfig | Admin |
| 管理 | GET /api/v1/admin/payment/orders[/:id] | ListOrders / GetOrderDetail | Admin |
| 管理 | POST /api/v1/admin/payment/orders/:id/{cancel,retry,refund,refund/query} | CancelOrder / RetryFulfillment / ProcessRefund / QueryAndFinalizeRefund | Admin |
| 管理 | GET/POST/PUT/DELETE /api/v1/admin/payment/{plans,providers}[/:id] | 订阅套餐 / 服务商实例 CRUD | Admin |

### A2.2 下单

入口 `PaymentService.CreateOrder`（`service/payment_order.go:25-120`）：

| 步骤 | 关键函数 / 行号 |
|---|---|
| 归一化支付方式 | `NormalizeVisibleMethod`（order.go:29） |
| 校验支付开关 | `cfg.Enabled`，关闭返回 PAYMENT_DISABLED（order.go:36-38） |
| 校验金额/套餐 | `validateOrderInput`（order.go:122-137）；订阅单 `validateSubOrder`（order.go:139+） |
| 取消率限流 | `checkCancelRateLimit`（order.go:43） |
| 币种一致性 | `ValidateMethodCurrencyConsistency`（order.go:59） |
| 充值优惠阶梯 | `quoteRechargeBonus`（order.go:72）→ {PayBase, Bonus, Credited} |
| 计算实付 | `calculateCreateOrderPayAmountForOrderType`（order.go:77） |
| 选服务商实例 | `selectCreateOrderInstance`（order.go:81）→ `DefaultLoadBalancer.SelectInstance`（`payment/load_balancer.go:87-115`） |
| 落库（事务） | `createOrderInTx`（order.go:185-231） |
| 调渠道下单 | `invokeProvider`（order.go:112）；失败置 FAILED（order.go:113-118） |

**订单标识生成**：

- 外部订单号 out_trade_no：`generateOutTradeNo()`（`payment_service.go:56-62`），格式 `sub2_<yyyymmdd><8位随机>`，前缀 `orderIDPrefix = "sub2_"`（`payment_service.go:49`）；`allocateOutTradeNo` 最多 5 次重试保唯一（`payment_order.go:233-244`）。
- 内部充值码 recharge_code：`PAY-<orderID>-<unixNano%100000>`（`payment_order.go:222-223`），事务内二次 update 写入。

### A2.3 回调（Webhook）

入口 `PaymentWebhookHandler.handleNotify`（`handler/payment_webhook_handler.go:71-145`）：

| 步骤 | 行号 | 说明 |
|---|---|---|
| 读取原始 body | 72-84 | GET 取 RawQuery（易支付用 GET 回调）；POST 限 1MB（`maxWebhookBodySize` :27） |
| 提取 out_trade_no | 88 → `extractOutTradeNo`（:149-171） | 定位订单对应的 provider 实例（同渠道多实例） |
| 取候选 provider | 90 | `GetWebhookProviders(ctx, providerKey, outTradeNo)` |
| 逐个验签 | 106 → `verifyNotificationWithProviders`（:173-190） | 返回首个验签成功的 provider key |
| 处理通知 | 124 | `HandlePaymentNotification` |
| 未知订单 | 130-138 | `ErrOrderNotFound` → **仍回 2xx**，防渠道无限重试 |

### A2.4 入账（Fulfillment）

核心文件 `backend/internal/service/payment_fulfillment.go`：

| 函数 | 行号 | 职责 |
|---|---|---|
| `HandlePaymentNotification` | 39-57 | 按 out_trade_no 查订单；查不到时对 legacy `sub2_<id>` 载荷兼容（`parseLegacyPaymentOrderID` :59-76） |
| `confirmPayment` | 78-117 | ① provider key 一致性（:89-96）② metadata 校验（:97-103）③ 金额有效性 + 容差（:104-115，`paymentAmountToleranceForCurrency` :119-125） |
| `toPaid` | 150-187 | **状态 CAS**：仅 PENDING / CANCELLED /（EXPIRED 且在 5 分钟宽限内，`paymentGraceMinutes = 5` `payment_service.go:42`）时置 PAID；影响行数 0 走 `alreadyProcessed` |
| `alreadyProcessed` | 189-214 | COMPLETED/REFUNDED 直接返回；FAILED/PAID/RECHARGING 继续 `executeFulfillment`；EXPIRED 超宽限仅记审计 |
| `executeFulfillment` | 216-225 | 按 order_type 分流 |
| `ExecuteBalanceFulfillment` | 227-253 | 余额单：状态门禁 → 抢 lease → `doBalance` |
| `ExecuteSubscriptionFulfillment` | 509-538 | 订阅单：状态门禁 → 抢 lease → `doSub` |
| `acquirePaymentFulfillmentLease` | 255-304 | **入账互斥锁**：CAS 把 PAID/FAILED（或超 5 分钟未动的 RECHARGING）置 RECHARGING；租约 `paymentFulfillmentLeaseDuration = 5 * time.Minute`（:31） |
| `doBalance` | 364+ | 兑换码幂等：`resolveRedeemAction`（:320-334）；`validatePaymentRedeemCode`（:336-362）校验码值/类型/归属 |
| `doSub` | 540-554 | `ensurePaymentSubscriptionAssigned`（:556+）→ `applyAffiliateRebateForOrder` → `markCompleted` |
| `sendBalanceRechargeSuccessNotification` | 454-474 | 余额到账邮件（变量 recharge_amount / current_balance / order_id） |
| `sendSubscriptionPurchaseSuccessNotification` | 476-507 | 订阅购买成功邮件 |

**订单状态机**（`payment/types.go:24-38`）：PENDING → PAID → RECHARGING → COMPLETED；旁支 EXPIRED / CANCELLED / FAILED；退款族 REFUND_REQUESTED / REFUNDING / REFUND_PENDING / PARTIALLY_REFUNDED / REFUNDED / REFUND_FAILED。

**三层幂等**（本模块最值得复用的设计）：

1. **状态 CAS**：`toPaid` 的条件更新，靠数据库行锁保证只成功一次。
2. **入账租约**：RECHARGING + updated_at 作乐观锁版本（`paymentFulfillmentLease{version: claimed.UpdatedAt}` :33-35、:303），并发与超时重入都有界。
3. **业务幂等键**：`payment_orders.recharge_code` ↔ `redeem_codes.code` 一一对应，重放时 `resolveRedeemAction` 返回 skip-completed 直接补记完成。

---

## A3. 卡密 / 兑换码（Redeem Codes）

### A3.1 数据模型（`ent/schema/redeem_code.go:36-75`，**硬删除** :19-25）

| 字段 | 类型 | 说明 |
|---|---|---|
| code | string(32) unique | 兑换码本体 |
| type | string(20) | balance / concurrency / subscription / invitation（`domain/constants.go:75-78`） |
| value | decimal(20,8) | 面值（余额金额 / 并发数；订阅天数由 validity_days 决定） |
| status | string(20) | unused / used（`domain/constants.go:8-9`） |
| used_by / used_at | int64 / timestamptz，nullable | 使用者（边 → users）与时间 |
| group_id | int64 nullable | 订阅类型绑定分组（边 → groups） |
| validity_days | int default 30 | 订阅有效期 |
| expires_at | timestamptz nullable | 码本身过期时间 |

### A3.2 兑换执行（`service/redeem_service.go`）

`redeem`（:414+）：

| 步骤 | 行号 | 说明 |
|---|---|---|
| 限流策略分派 | 415-419 | 仅 `enforceRedeemRateLimit` 才计入失败计数 |
| 分布式锁 | 422-425 | `acquireRedeemLock(ctx, code)`，防并发同码 |
| 查码 + 过期/可用校验 | 428-451 | |
| 类型前置校验 | 454-462 | subscription 必须有 group_id；invitation 走注册流程，普通兑换拒绝（`unsupportedRedeemTypeError` :386-391） |
| 事务开始 | 471-478 | `txCtx = dbent.NewTxContext(ctx, tx)` |
| **先标记已用** | 482-487 | `redeemRepo.Use`，DB 层乐观锁 `WHERE status = 'unused'` |
| 发放权益 | 490-541 | balance → UpdateBalance；concurrency → 并发调整；subscription → 订阅赋值 |

三种调用入口（**权限边界清晰**）：

| 函数 | 行号 | 限流 | 场景 |
|---|---|---|---|
| `Redeem` | 394-396 | 计入失败计数 | 用户公开兑换（POST /api/v1/redeem） |
| `redeemForPaymentFulfillment` | 402-404 | 绕过 | 内置支付回调入账（`ContextSkipRedeemAffiliate`，跳过推广返利） |
| `RedeemForAdminFulfillment` | 410-412 | 绕过 | Admin 对接（**保留**正常推广返利） |

### A3.3 管理端 API（`server/routes/admin.go:541-555`）

| 端点 | Handler |
|---|---|
| GET /admin/redeem-codes（支持 type/status/search/sort_by/sort_order，`redeem_handler.go:86-99`） | List |
| GET /admin/redeem-codes/{stats,export}、GET /:id | GetStats / Export / GetByID |
| **POST /admin/redeem-codes/create-and-redeem** | CreateAndRedeem |
| POST /admin/redeem-codes/{generate,batch-delete,batch-update}、POST /:id/expire | Generate / BatchDelete / BatchUpdate / Expire |
| DELETE /admin/redeem-codes/:id | Delete |

用户端：POST /api/v1/redeem、GET /api/v1/redeem/history（`routes/user.go:123-126`）。

---

## A4. 订单表结构与幂等机制

### A4.1 payment_orders（`ent/schema/payment_order.go`，**硬删除** :17-21）

| 分组 | 字段 | 行号 |
|---|---|---|
| 用户冗余 | user_id / user_email / user_name / user_notes | 35-43 |
| 金额 | amount(decimal 20,2 到账) / pay_amount(实付) / fee_rate / bonus_amount(赠送，用于剔除推广返利基数) | 46-56 |
| 支付信息 | recharge_code / out_trade_no / payment_type / payment_trade_no / pay_url / qr_code / qr_code_img | 57-79 |
| 类型与订阅 | order_type(默认 balance) / plan_id / subscription_group_id / subscription_days | 82-93 |
| 渠道快照 | provider_instance_id / provider_key / provider_snapshot(jsonb) | 94-104 |
| 状态 | status(默认 PENDING) | 107-109 |
| 退款 | refund_amount / refund_reason / refund_at / force_refund / refund_requested_at / refund_request_reason / refund_requested_by | 112-136 |
| 时间 | expires_at / paid_at / completed_at / failed_at / failed_reason | 139-156 |
| 来源 | client_ip / src_host / src_url | 159-166 |

索引（:190-202）：out_trade_no **部分唯一索引**（`WHERE out_trade_no <> ''`）、user_id、status、expires_at、created_at、paid_at、(payment_type, paid_at)、order_type。

### A4.2 幂等键清单（回答"operation_id 之类"）

| 幂等键 | 位置 | 作用域 |
|---|---|---|
| out_trade_no | `payment_orders.out_trade_no` 部分唯一索引 | 渠道回调 → 订单 |
| recharge_code | `payment_orders.recharge_code` ↔ `redeem_codes.code` | 入账 → 权益发放 |
| status + updated_at | `acquirePaymentFulfillmentLease` | 入账互斥租约 |
| (scope, idempotency_key_hash) | `idempotency_records` 唯一索引（`ent/schema/idempotency_record.go:46`） | Admin API 请求级幂等 |
| duplicate_operation_id | `groups.duplicate_operation_id` 部分唯一索引（`ent/schema/group.go:71-76`、:335-338`） | 分组一键复制恢复（**不是支付订单的 operation_id**） |

> **注意**：`payment_orders` 表**没有** operation_id 字段。`project-docs/13-requirements-mapping.md:89` 提到的 `affiliate_ledger_operation_id`（迁移 240）属于**推广返利台账**，不是支付订单幂等键；本任务未对迁移 240 取证，标注**未确认**。

### A4.3 Admin API 幂等实现（`handler/admin/idempotency_helper.go`）

- `executeAdminIdempotentJSON`（:78-86）→ `executeAdminIdempotentJSONWithMode`（:109+），默认 `idempotencyStoreUnavailableFailClose`（Redis 不可用时**拒绝**而非放行）。
- `executeAdminIdempotentWithTimeout`（:34-68）组装 `IdempotencyExecuteOptions`：Scope、ActorScope、Method、Route、IdempotencyKey（读 `Idempotency-Key` header）、Payload、**`RequireKey: true`**（:64）、TTL。
- `adminActorScope`（:70-76）：`admin:<userID>`，幂等键按管理员身份隔离。

### A4.4 审计（`ent/schema/payment_audit_log.go:31-47`，硬删除）

字段：order_id / action / detail / operator(默认 system) / created_at。
已知 action：PAYMENT_PROVIDER_MISMATCH、PAYMENT_PROVIDER_METADATA_MISMATCH、PAYMENT_INVALID_AMOUNT、PAYMENT_AMOUNT_MISMATCH（`payment_fulfillment.go:90/98/105/113`）、ORDER_PAID、ORDER_RECOVERED（:178/:185）、PAYMENT_AFTER_EXPIRY（:205）。

---

## A5. 为「卡网嵌入网站、自动化充值」定位

### A5.1 现成可用的对外 API

`docs/ADMIN_PAYMENT_INTEGRATION_API.md`（243 行，中英双语）：

| 文档章节 | 端点 | 用途 |
|---|---|---|
| §1 一步创建并兑换 | POST /api/v1/admin/redeem-codes/create-and-redeem | **原子**「建码 + 兑换到指定用户」，卡网自动充值核心接口 |
| §2 查询用户 | GET /api/v1/admin/users/:id | 前置校验用户存在 |
| §3 余额调整 | POST /api/v1/admin/users/:id/balance | 人工补偿/扣减（set/add/subtract） |
| §4 URL Query 透传 | （前端行为） | 打开 purchase_subscription_url / 自定义菜单 iframe 时追加 user_id/token/theme/lang/ui_mode |
| §5 失败处理建议 | — | 支付成功与充值成功**分状态落库**；重试保持同 code、换新 Idempotency-Key |

**鉴权**：`x-api-key: admin-<...>`（`middleware/admin_auth.go:50-57` → `validateAdminAPIKey` :121-154），与存储值做**常量时间比较**（:134）；也接受管理员 JWT（:60-75）。

**create-and-redeem 请求契约**（`handler/admin/redeem_handler.go:46-58`）：

| 字段 | 约束 |
|---|---|
| code | required，长度 3–128 |
| type | 可选，oneof=balance concurrency subscription invitation；**缺省按 balance**（向后兼容旧版 Sub2ApiPay，:180-184 注释） |
| value / user_id | required；user_id > 0 |
| group_id / validity_days | subscription 类型必填（:186-194） |
| notes | 建议写外部订单号（文档示例：sub2apipay order: cm1234567890） |
| expires_at / expires_in_days | 二者互斥（:60-63） |

**幂等语义**（实现于 `redeem_handler.go:203-267`）：

| 场景 | 结果 |
|---|---|
| 缺 Idempotency-Key | 400 IDEMPOTENCY_KEY_REQUIRED（`RequireKey: true`） |
| 同 code 且同 used_by | 200，返回既有码（:262-264） |
| 同 code 但 used_by 不同 | 409 REDEEM_CODE_CONFLICT（:266） |
| 码已建但未兑换（上次崩在中途） | 自动补兑换（:244-252） |
| 唯一约束竞态 | 回退到"按 used_by 判定"的幂等分支（:222-229） |

**嵌入能力**（前端已实现）：`frontend/src/utils/embedded-url.ts:16-46` `buildEmbeddedUrl` 追加 user_id / token / theme / lang / ui_mode=embedded / src_host / src_url；配置键 purchase_subscription_url（`service/domain_constants.go:386`，注释「"购买订阅"页面 URL（作为 iframe src）」）。

### A5.2 缺口分析（结论性，非源码事实）

| # | 缺口 | 证据 / 影响 |
|---|---|---|
| G1 | **没有「代客下单」API** | 用户侧支付端点全部挂 `jwtAuth`（`routes/payment.go:40-44`）。卡网无法替用户建渠道支付单，只能走"卡网自己收款 → 调 create-and-redeem 入账" |
| G2 | **没有「按外部订单号反查」端点** | 无 GET /admin/redeem-codes/by-code。List 有 search（`redeem_handler.go:90`），能否精确匹配 code **未确认**（U6）。卡网对账只能自建映射表 |
| G3 | **没有出站 webhook / 回调** | 入账成功只发**邮件**（`payment_fulfillment.go:454-474` 余额、:476-507` 订阅）。卡网只能**轮询** GET /admin/users/:id/balance 或订单列表 |
| G4 | **单一全局 Admin API Key** | `validateAdminAPIKey` 读 `settingService.GetAdminAPIKey`（`admin_auth.go:127`），只有一个 key，无法按调用方隔离 / 吊销 / 审计 |
| G5 | **充值页嵌入方向单一** | 现成能力是 **Sub2API 嵌别人**（purchase_subscription_url 作 iframe src）；无「把 Sub2API 充值页嵌进卡网站」的官方支持（需卡网侧自行 iframe /payment 并处理 JWT） |
| G6 | **用户查询以 ID 为主** | GET /admin/users?search= 支持 email/username（`handler/admin/user_handler.go:121-138`）；另有 GET /admin/affiliates/users/lookup（`routes/admin.go:836`）。够用但非专用 |

### A5.3 待澄清（阻塞方案设计）

1. 「卡网」是**发卡平台**（独角数卡/异次元发卡等）还是**卡密批发站**？
2. 「嵌入网站」是 Sub2API 充值页 iframe 进卡网，还是卡网支付页 iframe 进 Sub2API？
3. 「自动化充值」是**卡密自动发货 → 自动入账**（→ 用 G1+G2+G3 补），还是**用户自助充值**（已具备）？
4. 对接方式：出站回调（webhook）/ 主动轮询 / 共用数据库？

---

## A6. 相关配置项

### A6.1 系统设置（DB settings 表；常量见 `service/payment_config_service.go:18-44`）

| 键名 | 说明 |
|---|---|
| payment_enabled | 支付总开关 |
| MIN_RECHARGE_AMOUNT / MAX_RECHARGE_AMOUNT / DAILY_RECHARGE_LIMIT | 单笔最小 / 单笔最大 / 每用户日累计上限 |
| ORDER_TIMEOUT_MINUTES / MAX_PENDING_ORDERS | 订单超时（默认 30）/ 待支付订单上限（默认 3），:48-49 |
| ENABLED_PAYMENT_TYPES | 启用的支付方式 |
| LOAD_BALANCE_STRATEGY | round-robin（默认）/ least-amount（`payment/load_balancer.go:20-23`、`types.go:76`） |
| BALANCE_PAYMENT_DISABLED / BALANCE_RECHARGE_MULTIPLIER | 禁用余额充值 / 充值倍率 |
| SUBSCRIPTION_USD_TO_CNY_RATE / RECHARGE_FEE_RATE | 订阅 CNY 换算（0 = 关闭）/ 手续费率 |
| PRODUCT_NAME_PREFIX / PRODUCT_NAME_SUFFIX | 商品名前后缀 |
| PAYMENT_HELP_IMAGE_URL / PAYMENT_HELP_TEXT | 充值页帮助内容 |
| CANCEL_RATE_LIMIT_ENABLED / _MAX / _WINDOW / _UNIT / _WINDOW_MODE | 防刷取消（5 项） |
| ALIPAY_FORCE_QRCODE / ALIPAY_MOBILE_PRECREATE_DEEP_LINK | 移动端强制扫码 / 唤起支付宝 |
| RECHARGE_BONUS_TIERS / RECHARGE_BONUS_MODE / RECHARGE_BONUS_NOTICE | 充值优惠（`payment_recharge_bonus.go:28-32`；模式 bonus/discount） |
| payment_visible_method_{alipay,wxpay}_{source,enabled} | 可见方式路由（`payment_config_service.go:127-130`） |
| purchase_subscription_url | iframe src |

### A6.2 环境变量

| 变量 | 位置 | 说明 |
|---|---|---|
| PAYMENT_RESUME_SIGNING_KEY | `service/payment_service.go:52` | 公共订单恢复 token 的签名密钥 |
| 配置加密密钥 | `payment/crypto.go` + config | 服务商实例 config 字段加解密 |

> 本仓库支付配置**绝大部分走 DB settings**（`payment_config_service.go` 通过 SettingService 读取），而非 env。**未确认**是否存在额外的支付专属 env 覆盖。

---

# B. 兜底分组

## B1. 「兜底分组」= 两个独立字段，两种触发场景

上游 `groups` 表有**两个**兜底字段（`ent/schema/group.go:198-209`）：

| 字段 | 源码注释（原文） | 行号 |
|---|---|---|
| fallback_group_id | 「非 [CC] 请求降级使用的分组 ID」 | 202-205 |
| fallback_group_id_on_invalid_request | 「无效请求兜底使用的分组 ID」 | 206-209 |

设计说明（`ent/schema/group.go:321-322`）：「fallback_group_id 直接作为字段使用，不定义 edge，这样允许多个分组指向同一个降级分组（M2O 关系）」。

Go 侧映射：`service/group.go:78-82`；DTO：`handler/dto/types.go:139-141`、`handler/admin/group_handler.go:232-233 / 307-308`；前端类型：`frontend/src/types/index.ts:616-617`。

**中文 UI 文案**（`frontend/src/i18n/locales/zh/admin/overview.ts`）：

- fallback_group_id → `fallbackGroup: '降级分组'`（:1229）
- fallback_group_id_on_invalid_request → `invalidRequestFallback.title: '无效请求兜底分组'`，`hint: '仅当上游明确返回 prompt too long 时才会触发，留空表示不兜底'`（:1270-1273）

> **注意**：字段名里的 "invalid request" 指**上游返回 prompt too long**，**不是**"分组 ID 无效"。源码中**没有**"请求带了不存在的分组 → 兜底"这条路径。

### B1.1 场景 1：fallback_group_id（非 [CC] 客户端降级）

触发点 `GatewayService.resolveGatewayGroup`（`service/gateway_scheduling.go:922-949`）：

    for {
        group := resolveGroupByID(currentID)
        if !group.ClaudeCodeOnly || IsClaudeCodeClient(ctx) { return group, &currentID, nil }
        if group.FallbackGroupID == nil { return nil, nil, ErrClaudeCodeOnly }
        currentID = *group.FallbackGroupID      // 逐级降级，visited 防环
    }

调用链：

| 位置 | 说明 |
|---|---|
| `gateway_scheduling.go:115` / :41 | `SelectAccountWithLoadAwareness` / `SelectAccountForModelWithExclusions` 入口做 CC 限制检查 |
| `handler/gateway_handler_chat_completions.go:111-115` | /v1/chat/completions：claude_code_only && FallbackGroupID == nil → 403 |
| `handler/gateway_handler_responses.go:115` | Responses 端点同款判断 |
| `service/gateway_service.go:423-424` | `ErrClaudeCodeOnly` 定义 |

**配置校验**（`service/admin_group.go:690-722` `validateFallbackGroup`）：不能自指（691-693）、检测环（695-704）、降级分组必须存在（707-710）、**降级分组不能启用 claude_code_only**（否则死循环，712-715）。

### B1.2 场景 2：fallback_group_id_on_invalid_request（上游 prompt too long）

触发点 `handler/gateway_handler.go:1005-1054`：

| 步骤 | 行号 |
|---|---|
| 捕获 `*service.PromptTooLongError` | 1005-1006 |
| 变量初始化 `fallbackGroupID = apiKey.Group.FallbackGroupIDOnInvalidRequest` | 634-637 |
| 仅当 `!fallbackUsed && fallbackGroupID != nil && *fallbackGroupID > 0` | 1012 |
| 解析兜底分组 `ResolveGroupByID` | 1013 |
| 二次校验：必须 PlatformAnthropic、非订阅、且自身未配 on-invalid-request 兜底 | 1019-1029 |
| 克隆 API Key 切分组 `cloneAPIKeyWithGroup` | 1030 |
| **重新做计费资格校验** `CheckBillingEligibility` | 1031 |
| **清除 ForcePlatform**，允许按兜底分组平台调度 | 1039-1041 |
| 切换 currentAPIKey，置 fallbackUsed / retryWithFallback = true，break 重试 | 1042-1051 |
| 外层循环靠 retryWithFallback 决定是否继续（否则 return） | 1138-1141 |

**配置校验**（`service/admin_group.go:728-753` `validateFallbackGroupOnInvalidRequest`）：当前分组平台必须是 anthropic 或 antigravity（729-731）、非订阅类型（732-734）、不能自指（735-737）、兜底分组必须存在（739-742）、兜底分组平台必须是 anthropic（743-745）、非订阅类型（746-748）、兜底分组自身不能再配该兜底（**仅一级**，749-751）。

---

## B2. 分组数据模型（`ent/schema/group.go`）

表名 groups（:23-27）；Mixin：TimeMixin + SoftDeleteMixin（:29-34，**软删除**，与订单/兑换码的硬删除不同）。

| 类别 | 字段 | 行号 |
|---|---|---|
| 标识 | name(100，部分唯一索引) / description(text) / status(默认 active) / duplicate_operation_id(内部幂等) | 40-76 |
| **平台** | platform(50，默认 anthropic) | 79-81 |
| **倍率** | rate_multiplier(decimal 10,4 默认 1.0)；高峰：peak_rate_enabled / peak_start / peak_end / peak_rate_multiplier | 47-65 |
| 订阅 | subscription_type(默认 standard) / daily_limit_usd / weekly_limit_usd / monthly_limit_usd / default_validity_days(30) | 82-98 |
| 图片/视频/搜索/音频定价 | allow_image_generation / allow_batch_image_generation / image_rate_independent / image_rate_multiplier / image_price_1k,2k,4k / batch_image_{discount,hold}_multiplier / video_* / video_model_prices / web_search_price_per_call / search_price_per_1k / audio_{realtime,tts,stt}_* | 100-189 |
| **逐模型定价** | long_context_pricing_enabled / model_pricing(jsonb，**优先级高于渠道和内置定价**) | 190-196 |
| **[CC] 与兜底** | claude_code_only / fallback_group_id / fallback_group_id_on_invalid_request | 199-209 |
| 模型路由 | model_routing(jsonb：模型模式 → 优先账号 ID 列表) / model_routing_enabled | 212-220 |
| **[OI] 调度** | allow_messages_dispatch / allow_live / force_openai_fast / free_openai_fast / require_oauth_only / require_privacy_set / default_mapped_model / messages_dispatch_model_config | 239-264 |
| **模型白名单** | model_allowlist(jsonb，「同时约束模型列表接口与请求准入」) / codex_models_manifest_config | 265-272 |
| **排序** | sort_order(int 默认 0，「数值越小越靠前」) | 233-236 |
| 限流 | rpm_limit(0 = 不限；设置后接管分组用户限流) | 274-277 |
| 推理强度 | max_reasoning_effort / max_reasoning_effort_over_limit(默认 downgrade) / reasoning_effort_mappings | 279-291 |
| 利润控制 | profit_control_enabled / profit_min_margin / profit_safety_buffer | 293-305 |
| 其他 | is_exclusive(专属分组) / mcp_xml_inject / supported_model_scopes | 66-67 / 222-231 |

**Edges**（:309-324）：api_keys(O2M)、redeem_codes(O2M)、subscriptions(O2M)、usage_logs(O2M)、**accounts(M2M through account_groups)**、**allowed_users(M2M through user_allowed_groups)**；fallback_group_id **无 edge**（:321-322，有意为之）。

---

## B3. 账号选择：分组如何决定用哪个账号

### B3.1 分组 → 账号的绑定

中间表 `account_groups`（`ent/schema/account_group.go`）：复合主键 `(account_id, group_id)`（:24-25, 31-32）、`priority`（账号在该分组内的优先级，默认 50，:33-34）、created_at（:35-38）；索引 group_id、priority（:55-59）。

**调度候选排序 SQL**（`repository/account_repo.go:2073-2093`）：

    SELECT ag.group_id, a.id, a.concurrency, ...
    FROM account_groups ag JOIN accounts a ON a.id = ag.account_id
    WHERE ag.group_id = ANY($1)
      AND a.deleted_at IS NULL AND a.status = $2
      AND a.schedulable = TRUE
      AND (a.temp_unschedulable_until IS NULL OR a.temp_unschedulable_until <= $3)
      AND (a.expires_at IS NULL OR a.expires_at > $3 OR a.auto_pause_on_expired = FALSE)
      AND (a.overload_until IS NULL OR a.overload_until <= $3)
      AND (a.rate_limit_reset_at IS NULL OR a.rate_limit_reset_at <= $3)
    ORDER BY ag.group_id ASC, ag.priority ASC, a.priority ASC, a.id ASC

即**双层优先级**：分组内绑定优先级 `ag.priority` 优先，其次账号全局 `a.priority`，最后按 ID 稳定排序。

### B3.2 分组来源：API Key

API Key 绑定单个分组（`ent/schema/api_key.go:44-46` `group_id` nullable；边 :128-131）。

| 路径 | 说明 |
|---|---|
| is_exclusive = true（专属标准分组） | 绑定时**自动授予** user_allowed_groups 并更新 Key（`service/admin_group.go:1377-1415`，事务保证原子性） |
| is_exclusive = false（公开分组） | 用户需已有 allowed_groups；判定 `user.CanBindGroup(group.ID, group.IsExclusive)`（`service/api_key_service.go:488`） |

`ReplaceUserGroup`（`admin_group.go:1457-1505`）在专属分组间迁移时，事务内完成「授新权限 → 迁移 Key → 撤旧权限」。

### B3.3 调度时的分组决策链

    apiKey.GroupID（或 ForcePlatform）
      → resolveGatewayGroup()          gateway_scheduling.go:922-949   ← [CC] 降级在此发生
      → resolvePlatform()              gateway_scheduling.go:973-1012  ← composite 分组解析目标平台
      → listSchedulableAccounts()      gateway_scheduling.go:1014+     ← 按 groupID + platform 取候选
      → Layer 1 模型路由 / 粘性 / 负载感知                            gateway_scheduling.go:276+
      → 过滤：excluded / schedulable / profit / platform / model mapping / channel restriction / quota

关键入口：`SelectAccountWithLoadAwareness`（`gateway_scheduling.go:100`）、`SelectAccountForModelWithExclusions`（:34）。

**无分组时的行为**（`gateway_scheduling.go:63-66`）：「无分组时只使用原生 anthropic 平台」→ `platform = PlatformAnthropic`。即 `groupID == nil` 落到 **anthropic 平台的全平台账号查询**，**不是**某个"默认分组"。

### B3.4 简单模式的"默认分组"（另一个概念，勿混淆）

`config.SimpleMode.AutoCreateDefaultGroups`（`config/config.go:116`，默认 true :2040）驱动启动时播种（`repository/ent.go:171-181` → `repository/simple_mode_default_groups.go:14-59`）：anthropic / openai / gemini / grok 各保证 1 个 `<platform>-default`，antigravity 保证 2 个 `antigravity-default-{1,2}`。

创建参数（`simple_mode_default_groups.go:72-81`）：description = "Auto-created default group"、status = active、subscription_type = standard、rate_multiplier = 1.0、is_exclusive = false、grok 额外 allow_image_generation = true。

> 这是**启动播种**，与"请求时的兜底"无关，也不会被自动选为请求目标。

---

## B4. 「默认兜底 AWSB 分组」可行性分析

### B4.1 四种可能语义与现状

| 语义 | 现有机制 | 能否满足 | 证据 |
|---|---|---|---|
| (a) 非 CC 客户端访问 claude_code_only 分组 | fallback_group_id | 可直接配置 | `gateway_scheduling.go:922-949` |
| (b) 上游返回 prompt too long | fallback_group_id_on_invalid_request | 可直接配置 | `gateway_handler.go:1005-1054` |
| (c) 用户/Key **没有绑定任何分组** | 无全局默认；落到 anthropic 平台全平台查询 | **需新做** | `gateway_scheduling.go:63-66` |
| (d) 分组内**无可用账号 / failover 耗尽** | 无分组级兜底，直接报错返回 | **需新做** | `gateway_handler.go:683-707`（选择失败）、:1070-1072（`FailoverExhausted`） |

### B4.2 若 AWSB 是 anthropic 平台分组（最可能）

假设 AWSB 分组 `platform = anthropic`（Bedrock 账号属该平台：`AccountTypeBedrock` 见 `service/domain_constants.go:165`，判定 `a.Platform == PlatformAnthropic && a.Type == AccountTypeBedrock`，`service/account.go:1313-1315`）。

**场景 (b) 完全可用，无需改代码**，只需配置：

1. 建名为 AWSB 的分组：platform = anthropic、subscription_type = standard、status = active。
2. 把 AWSB 账号（Bedrock 类型）绑到该分组（account_groups）。
3. 在**业务分组**上设置 fallback_group_id_on_invalid_request = AWSB 分组 ID。

校验会通过（`admin_group.go:728-753`），因为 AWSB 满足：存在、PlatformAnthropic、非订阅、自身未配 on-invalid-request 兜底。

**场景 (a)** 同样可用，但注意：源分组必须 `claude_code_only = true`，且 **AWSB 分组不能开 claude_code_only**（`admin_group.go:712-715`）。

### B4.3 若要「全局默认」（场景 c / 需求方明确要"默认"）

**现状**：兜底是**逐分组字段**，没有全局配置项，也没有"未配置时统一兜底到 AWSB"的逻辑。

**需要改的地方**（按最小改动排序，均**未实施**）：

| # | 改动点 | 文件 | 说明 |
|---|---|---|---|
| 1 | 新增系统设置键（如 DEFAULT_FALLBACK_GROUP_ID） | `service/setting*.go` + `handler/admin/setting_handler*.go` + `handler/dto/settings.go` | 参照现有 purchase_subscription_url（`service/domain_constants.go:386`）的接入方式 |
| 2 | 在分组解析层接入全局默认 | `service/gateway_scheduling.go` `resolveGatewayGroup`（:922-949）与 `groupID == nil` 分支（:63-66） | 当前 nil 直接 platform = PlatformAnthropic，不查分组 |
| 3 | 若"无可用账号"也要兜底 | `handler/gateway_handler.go` :683-707 与 :1070-1072 | 需新增一条与现有 prompt-too-long 分支同构的 retry 路径（`retryWithFallback` 机制已存在，可复用） |
| 4 | 前端配置项 + i18n | `frontend/src/views/admin/GroupsView.vue` 或设置页 + `frontend/src/i18n/locales/{zh,en}/` | 前端 build 会先跑 check:i18n，**zh 与 en 必须同步**，否则构建失败 |
| 5 | 迁移 | `backend/migrations/259+` | 仅当需要新增列/表；若只加 setting 键则**不需要迁移**（settings 是 KV 表） |

**注意**：现有 `validateFallbackGroupOnInvalidRequest` 限制「兜底分组必须是 anthropic 平台」且「不能是订阅类型」。若 AWSB 不是 anthropic 平台（例如 openai 平台分组），**场景 (b) 的现成路径无法使用**，必须改校验与重试逻辑——这点**强烈依赖 U1/U2 的澄清**。

### B4.4 结论

- 「兜底分组」**不是要新建的功能**，上游已有两个字段与完整校验 / 重试链路。
- 若需求 = (a) 或 (b)，且 AWSB 是 anthropic 平台分组 → **纯配置，零代码改动**。
- 若需求 = (c)/(d) 或要求"全局默认" → 需按 §B4.3 新增，**改动集中在 gateway_scheduling.go + gateway_handler.go + 设置项 + 前端 i18n**。
- **必须先澄清 U1（AWSB 是什么）与 U2（哪种"默认"）**，否则方案无法定型。

---

# C. 关键结论速览

1. **支付系统完整度很高**：5 个渠道 provider、用户/公开/Webhook/Admin 四组路由、三层幂等（状态 CAS + 入账租约 + 兑换码幂等键）、独立审计表。**不需要重造**。
2. **卡网对接的现成抓手是 POST /api/v1/admin/redeem-codes/create-and-redeem**：原子建码+兑换、强制 Idempotency-Key、同码不同用户返回 409、中途崩溃可自愈补兑换；契约已在 `docs/ADMIN_PAYMENT_INTEGRATION_API.md` 中英文写清。
3. **卡网三处缺口**：G1 无代客下单 API、G2 无按外部订单号反查、G3 无出站回调（只有邮件）。补 G2/G3 是**增量**工作，不需要动支付核心。
4. **兜底分组机制已存在**，但语义是「非 CC 降级」和「上游 prompt too long」两种，**不是**"分组 ID 无效"或"无可用账号"兜底。
5. **「默认兜底 AWSB」大概率是配置问题**（若 AWSB 为 anthropic 平台分组）；只有要求"全局默认"或"无账号时兜底"时才需写代码。
6. **最大风险点**：`git grep -i awsb` 全仓库 **0 命中**，AWSB 的确切含义与目标场景**必须由需求方澄清**后才能定方案。

---

*文档结束。所有行号基于 develop 分支当前工作树（HEAD = 9150a7a86）。*
