# 异步视频报价复审

本记录对应 PR #3 的 F1–F4 二次审查，基线为 3fb7337cd5bdbfdb5893571e794fbba6b1f0b557，修复 PR 目标为 AnBooting/sub2api 的 develop。验证时间为 2026-10-17，所有构建和测试均在本机执行。

## 修复范围

| 问题 | 生产行为 | 回归证据 |
| --- | --- | --- |
| F1 配置价格被默认价覆盖 | 创建请求前与实时媒体路径共用 resolveVideoTariff；分组价卡、模型槽、平铺槽、渠道价及 Grok 默认价均参与冻结 | TestReReview_FreezeRetainsConfiguredGrokTariff，TestVideoQuoteTariffPrecedence |
| F2 用户倍率被组默认值覆盖 | FreezeVideoBilling 解析真实 user/group 倍率，再应用独立视频或 token 高峰倍率 | TestReReview_FreezeRetainsUserGroupRate，TestVideoQuoteCreateFreezesUserRateBeforeUpstream |
| F3 Seedance 轮询丢冻结字段 | ApplyToVideoResult 统一传递报价、倍率、来源及输入视频事实；轮询保留创建分辨率与时长回落 | TestReReview_SeedancePollCarriesFrozenQuote，TestVideoQuoteSeedancePollUsesFrozenModeAndCreationFacts |
| F4 实时配置控制已受理任务 | 冻结报价先于实时计价分派，必需 token 以冻结模式为准，计价错误保留重试 | 三项 Seedance 配置变更复现及 TestVideoQuoteMissingUsageCannotCompleteAtZero |

首次运行同事六个复现用例时全部失败：Grok 配置价 8.4 被算成 0.84；用户倍率案例 2 被算成 8；Seedance 冻结字段丢失、按次任务受实时 token 模式阻断、方案删除后出现 completed/实扣 0。随后保留调用真实生产函数的永久回归，未重写测试版计价算法。

## 报价与兼容契约

- 请求上游前捕获报价，同一请求账号重试复用；受理时将该值写入 Redis pending 及任务 JSONB 的同一结构。上游调用期间改配置不改变报价。
- VideoPricePlan 表示视频方案和传统价格槽。FrozenVideoPricing 保存无法无损转换成该方案的分组/渠道价卡；显式携带基础价、区间、按次档位、源价卡、币种和定价时点，恢复私有 manualTokenPricing 标记。
- 现有显式 Grok token 价卡仍走 token 计价；媒体价卡保留原按次/按秒单位及运营币种。渠道分时/推理倍率经现有计算器处理，不为媒体新增另一套分时规则。
- 快照独占其 map/指针，JSON 往返和执行期配置修改不改变报价。客户端和 worker 使用同一份输入视频事实，任务表回落不依赖 Redis 再查询。
- 新冻结价卡优先，其次已有冻结 plan；完全没有冻结标记才走历史实时路径。存在冻结标记而价格缺失、模式损坏、缺必需时长/token 时返回错误，不写零成本成功账、不关闭任务。显式零价、零倍率有效。
- 不新增数据库迁移、不修改公开 HTTP 请求/响应。已保存的历史错误报价无法反推真实受理价格，不猜价补扣或重算历史账。

## 补充完成入口验收

独立只读审查与 Lead 回归补齐以下同源路径：

- Seedance 创建读取请求 resolution/duration，720p-only 方案可创建，未定价 1080p 在请求上游前拒绝。
- Grok worker 共享终态解析提取 usage；token 报价缺 completion usage 时保留 processing。内容下载这一替代完成入口同样传递 usage。
- Seedance queued/running 响应不取得 claim。按次/按秒不要求 token，不等于允许非终态计费；原生命周期与归属测试断言 queued 后 claim 为空。
- Seedance 用户倍率缓存过期后，账单 RateMultiplier 仍与冻结实际扣费一致。

## 验证方法

后端完整门禁使用现有 Git 安装的本地 sh；仅改变本次测试进程 PATH，不安装依赖、不改备份源码：

~~~powershell
$env:PATH = 'E:/Git/usr/bin;' + $env:PATH
go build -mod=readonly ./...
go vet -tags=unit ./...
go test -mod=readonly -p 2 -tags=unit ./... -count=1 -timeout=600s
~~~

变异通过 Go overlay 替换单个生产文件，原工作树保持修复候选；每个变异须因预期行为断言失败而退出 1，编译失败不计有效证据。覆盖配置价选择、用户倍率、Seedance 轮询复制、冻结计价分派、worker 模式、创建分辨率、Grok usage 提取及缺 usage 防护、非终态门槛、账单倍率和内容 usage 传递。

最终实测结果：

| 检查 | 结果 |
| --- | --- |
| go build -mod=readonly ./... | 退出码 0 |
| go vet -tags=unit ./... | 退出码 0 |
| 完整 backend unit（上述 PATH，-p 2，-count=1，-timeout=600s） | 退出码 0；60 个有测试包全部通过，53 个包无测试；handler 218.260s，service 219.705s |
| 最终定向入口/报价/任务回归 | 退出码 0；63 个顶层测试、含子用例共 92 条 PASS，零失败 |
| 同事六个原复现 | 首轮 6/6 失败，最终 6/6 通过 |
| Go overlay 变异验证 | 11/11 单点变异因预期断言失败（各退出码 1），最终日志无编译失败；恢复源码后定向门禁通过 |
| 独审 | 核心报价与最终终态/账单增量分别只读复核，均无未处理阻断；不以静态结论代替测试 |
| Git 暂存检查 | diff --check 退出码 0，无非本轮文件/凭据添加 |

首次完整套件因 Seedance 旧生命周期 fixture 未指定唯一已定价 720p、以及 Windows 进程 PATH 缺 sh 失败。fixture 显式使用 720p 并加强 queued 不得 claim 断言；备份测试源码未改，仅本次 PATH 加入已有本地 Git shell。最终完整套件未跳过测试，已知堆内存 flaky 本轮最终运行未出现。

原始详细日志及变异 overlay 存于仓库外本机证据目录，不向 PR 提交运行日志、源码快照或用户数据。

## 边界与回滚

未运行真实 PostgreSQL/Redis 集成、真实上游和生产部署；替身/JSON 回归不替代这些证据。前端、schema、迁移与生成物未改。独审仅作静态正确性检查，测试与构建由 Lead 执行。

该分支回退会使创建报价重新使用旧逻辑。已持久化的新增冻结价卡只能由支持该字段的版本正确结算；有这种任务待处理时应先暂停新建并完成或人工核对在途任务，再回退，不将旧版本忽略新字段当作安全回滚。
