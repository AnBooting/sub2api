# ARTEX 专属识别与防护交付报告

## 结论与范围

已在本地特性分支 `feat/security-artex-specific-defense` 实现仅针对 ARTEX 核心请求协议的识别、HTTP/WS 拦截、元数据日志和独立回滚开关。规则绑定用户提供的 [ARTEX 固定尾巴源码](<F:/sub2/ARTEX/agent/worker.go#L260-L295>) 版本 `e6ec56999175509551a1e43a3ee3b053d0941bd9`，最后代码候选 `8046ae4361c786d00ad4ce73f7da2b066f9d65db`。

**已完成本地代码验收：三项 Required 全部关闭，最终独审 Approve；同源码完整单测 58 个包通过、52 个包无测试、0 失败，vet 成功，正常 VCS 标记编译成功。仅批准本地代码交付，未部署，不能称线上已经生效。**

不新增通用风险词分类、攻击意图评分、永久 IP/Key 封禁、跨请求风险画像。不运行 ARTEX、不安装其依赖、不读真实私密配置，不触碰用户工具仓库。

## 从源码提炼的识别依据

硬拦截必须同时满足：当前协议合法有效的系统/开发者/顶层 instructions 文本含完整代码拥有的固定产物尾巴（只允许目录槽变化），以及同一角色族的三个真实工具声明均具有原始名称、类型、参数类型、数组 items 类型与 required 集合。单个词、工具或尾巴片段都不够。

| 角色族 | 固定尾巴 | 必须共同出现的三个真实工具 |
|---|---|---|
| W：worker | workerArtifactSpec | report_finding、record_fact、get_worker_trace |
| P：planner | artifactSpec | add_intent、prove_goal、get_worker_trace |
| M：main | artifactSpec | add_intent、set_goals、get_worker_trace |

以[检测器](<F:/sub2--wt--artex-defense/backend/internal/service/artex_guard.go>)和[独立字面量测试](<F:/sub2--wt--artex-defense/backend/internal/service/artex_guard_test.go>)为准。这里是结构指纹，不是密码学客户端身份，不证明请求目的违法。DB 工具 schema 可以被改造；该规则只承诺当前版本默认核心形状，不承诺覆盖所有改造变种。

## 防误伤与发送前拦截

- 不以 ARTEX 品牌、User-Agent、会话 header、安全研究、漏洞/PoC/CTF 字样、Bash/Read/Skill 或流量工具作为硬封依据。
- schema 关键词大小写精确；TYPE/Properties/Items/REQUIRED 不能充当真实小写关键词，额外无效大写字段也不能覆盖合法小写字段。
- Responses 的数组文本块仅取 input_text；Chat/Anthropic 仅取 text。合法字符串和顶层 instructions 保留。真实完整工具族保留时，尾巴只在 user/assistant/tool-result/metadata 不升为有效指令；工具目录/schema 仅以 JSON 字符串引用，不升为实际声明。检测器不递归 JSON，但不会语义排除所有 active-system 引文：active-system 中完整尾巴加实际工具族的复刻仍观测等价。
- [HTTP gate](<F:/sub2--wt--artex-defense/backend/internal/server/middleware/artex_guard.go>) 位于原鉴权、分组权限和 body 限制之后、业务账户选择与上游发送之前。保留 body 原字节、ContentLength、相关 header、TransferEncoding、Content-Encoding、GetBody；不额外解压。
- 覆盖现有 Messages/Chat/Responses 文本别名，Responses 只识别裸入口及 compact；安全但不属于已证生成语义的后缀按原行为分发，不凭“unknown”新增 403/404。真正不可转发的畸形后缀保持原 404。
- [WS guard](<F:/sub2--wt--artex-defense/backend/internal/service/artex_ws_guard.go>) 在首帧和后续发送前检查。Passthrough 只由真实上游 session.updated echo 建立连接本地 instructions/tools 状态；pending/rejected 不提交，缺字段保持 unknown，不继承旧证据，单次 response.create 覆盖不持久化。
- enforce：HTTP 返回 403 与稳定 code artex_client_blocked；WS 使用政策关闭 1008。被拒的该次模型请求不发上游；后续帧拒绝不抹去此前合法完成的请求。实际 DB 扣款集成未测。
- 日志只记录目标事件、rule_id、规则版本、协议、mode 和 decision；不记录原 prompt、schema、目录、目标、Key/token 或 response body。
- 无后缀时立即结束尾巴扫描，避免重复前缀造成理论二次扫描。空目录候选仍可继续查找后续合法尾巴；无性能基准，不报告虚构倍数。

## 配置与回滚

```yaml
gateway:
  artex_guard:
    mode: enforce  # off / monitor / enforce
```

[模式配置](<F:/sub2--wt--artex-defense/backend/internal/config/artex_guard.go>)：默认 enforce（只启用上述窄规则）；monitor 记录强命中元数据但原样放行；off 不读取专属 gate 的 body、不拦截。显式非法模式在配置加载时失败。修改配置后按现有方式重启才生效，不提供或宣称热更新。本次未执行任何真实服务重启、推送、发布或生产启用。首次上线建议由运维先用 monitor 回放经授权的正常流量，再明确启用 enforce。

## 已实际通过的定向验证

以下为最终生产代码对应的[定向原输出](<F:/sub2--evidence/20261011-artex/artex-directed-v6.log>)，不是作者自述或生成源码计数。

| 验证项 | 实际通过/总数 |
|---|---:|
| 三角色 × 三协议独立源码字面量强族 | 9/9 |
| 原普通/近似请求阴性 | 63/63 |
| schema 关键词精确匹配与无效扩展矩阵 | 69/69 |
| JSON 成员顺序阴影断言 | 2/2（同一测试内） |
| 完整工具族的正确/错误协议块 | 18/18 |
| 完整声明保留的非系统角色/metadata | 12/12 |
| 仅 JSON 工具引用，不是真声明 | 9/9 |
| 本机实际 HTTP 的 schema/模式组合 | 90/90 |
| 本机实际 HTTP 的协议块/角色/引用组合 | 63/63 |
| 10 真实注册文本别名 × 三模式 × 正常/命中 | 60/60 |
| WS 三入口 × 三模式 × 首帧/后续帧 | 18/18 |
| Passthrough accepted/rejected/pending/cleared 实际 ACK | 12/12 |
| 尾巴扫描三局部语义边界 | 3/3（并各验证三协议完整声明） |

注册路由测试保留原注册/鉴权/权限/限制链，仅用 private terminal DI 向本机 httptest mock 上游发真实 HTTP；正常/monitor/off 上游调用与原字节检查均成立，强命中 enforce 调用为 0。公开生产注册始终 nil DI，没有请求字段可以开启它。这不是具备真实账户、PostgreSQL/Redis、结算扣款的完整生产集成。WS 用真实服务入口与实际 write/HTTP Do 计数，并消费真实关闭帧、验证政策 code 后退出。handler 包在定向运行中是 no tests to run，仅编译通过，不冒充 handler 行为测试。

旧全量质量门曾唯一失败于未改的 late-success deadline fixture（10ms timeout 与 30ms sleep 并未同步实际取消状态）。仅该测试改成现有回调等待实际 Done，保原 10ms、deadline error、nil 结果、零持久化，并新增实际执行一次断言；OAuth 生产逻辑和共享 stub 未改。[100 次原输出](<F:/sub2--evidence/20261011-artex/artex-quality-deadline-count100-v5.log>) 确 100/100，通过[原刷新回归](<F:/sub2--evidence/20261011-artex/artex-quality-refresh-regression-v5.log>)。具体 Windows 调度机制未经跟踪实证，不作确定根因声明，原失败日志保留。

## 尚未验证与明确漏检边界

- 未运行 ARTEX、未取得真实线上 ARTEX wire capture；未安装/运行 Norma v0.4.3 取得 SDK 原始包，不用未知 SDK header 猜客户端。
- 无可靠上游成功 echo 的拆分更新、仅通过 previous_response_id 保存在远端的系统/工具历史无法安全复原。未被原链解压的压缩 body 不假称可识别。
- 主动完整复刻当前系统尾巴与实际工具族的正常授权客户端，和相同 ARTEX 核心协议观测等价，无法凭本规则区分；不承诺零误伤。
- 未执行 PG/Redis integration、真实上游端到端、真实结算扣款、生产正常流量回放、生产发布或性能基准。
- 原 PRD/计费树未覆盖或 reset；此源码在独立安全特性分支，尚未合入用户当前计费工作树，更不能据本地 binary 已编译称线上已启用。

更详细的设计约束见[设计文档](<F:/sub2--wt--artex-defense/project-docs/security/ARTEX-targeted-defense.md>)。

## 最终验收记录（2026-10-11，本地）

| 质量门 | 实际结果 | 原证据 |
|---|---|---|
| `go test -p 2 -tags=unit ./... -count=1` | exit 0，58 PASS / 52 no tests / 0 FAIL；只限制包并行，不跳包 | [最终单测日志](<F:/sub2--evidence/20261011-artex/artex-full-unit-v6.log>) |
| `go vet ./...` | exit 0 | [真实工具结果](<F:/sub2--evidence/20261011-artex/artex-final-validation-tool-results.json>) |
| 正常 `go build ... ./cmd/server` | 元数据恢复后独立执行 exit 0，未设置 `-buildvcs=false` | [真实工具结果](<F:/sub2--evidence/20261011-artex/artex-final-validation-tool-results.json>) |
| 冻结对象只读独审 | Approve；0 Critical / 0 未关闭 Required；只批准当前本地切片 | 代码 `8046ae4361c786d00ad4ce73f7da2b066f9d65db`，树 `208ba1191f18439e4f02adc5d13b0ea9a6e09ced` |

原同源码 compound 实际 exit 1（unit/vet 为 0，build 因 VCS 128 失败）照实保留，不伪造组合 stdout 或全零结果。只恢复本任务自有全零分支引用及损坏工作树索引：先备份，确认已有提交对象与树完整，重建临时索引匹配已验树，再恢复自身元数据；源码未 reset，其它分支未更改。损坏原因未知。随后同源码正常 VCS 构建实际 exit 0，作为独立修后证据。恢复前失效作业句柄没有日志/产物，不计成功；检查无旧 Go 进程后才重新启动实际验证。

以上不覆盖旧 PRD/请求锁价整体验收；其剩余 Required 和调用入口接线仍独立跟进。
