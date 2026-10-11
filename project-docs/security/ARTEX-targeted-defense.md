# ARTEX 专属识别与拦截设计（实现候选，待最终验证独审）

## 范围与事实绑定

用户明确新增紧急要求：仅针对 ARTEX 工具识别防护，不做通用风险拦截审计。参考源码只读，版本 e6ec56999175509551a1e43a3ee3b053d0941bd9；不运行 ARTEX、扫描、安装依赖或外部探测。防护在独立分支 feat/security-artex-specific-defense、工作树 F:/sub2--wt--artex-defense，基于已验 e47345ca9；原计费脏树保持不变。

识别的是请求携带的 ARTEX 核心执行协议指纹，不是违法判定，不保证识别修改源码/解绑工具/改 schema 的全部变体，也不保证观测等价请求可被区分。Norma v0.4.3 源未在本机核得，不声称已捕获其 Chat/Responses 真实网络包或默认 UA；解析采用现有公开协议合法 fixtures，Anthropic wire 另有 ARTEX 测试源码引用（未执行）。

## 线上证据与强规则 artex-e6ec569-core-v1

源码路径以 F:/sub2/ARTEX 为根：agent/prompt.go:29-52 只替换 BODY；worker.go:250-293 在 BODY 后追加代码自有尾巴；planner.go:342-410、mainagent.go:103-140 把拼装结果与实际 tools 交 agentcore.Options。后面还有 constraints/intent/deferred 段，故不可仅匹配最后 N 字符。工具可 DB 解绑、改结构；server/server_mgmt.go:769-803、db/tools.go:218-231 并无注释宣称的结构相等校验。

只在有效 system/developer 指令或 Responses instructions 的合法文本中找完整固定前后缀（变量只限目录插槽），同时要求对应三个真实声明各自的 name 与 schema 成立：

- Worker：worker.go:267-268 完整 workerArtifactSpec + report_finding / record_fact / get_worker_trace。
- Planner：worker.go:260-261 完整 artifactSpec + add_intent / prove_goal / get_worker_trace。
- Main：同 artifactSpec + add_intent / set_goals / get_worker_trace。

尾巴固定开头为“**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到”，Worker 接“本次意图的专属工作目录 ”，P/M 接“本任务工作目录 ”。Worker 固定结束为“**（已自动建好，直接用相对路径写在这里即可，无需再手动建目录）——**不要写 /tmp、不要用其它绝对路径**。”；P/M 固定结束为“**（相对路径即写在这里，也可用该绝对路径）——**不要写 /tmp、不要用其它绝对路径**。”。目录值可变，不靠 /data、tasks/id 或其它部署路径认身份；CRLF 统一为 LF，JSON 解码转义，不能模糊匹配删除词句后再硬封。

各声明 schema 根必须 object，属性类型严格、名称大小写严格；忽略 description/default 与成员顺序，required 作为集合，允许额外属性，不把多个同名残缺声明拼起来。

- report_finding（tools.go:1220-1227）：vulnclass/severity/summary/evidence 为 string，intent_id integer，asset_ids array(integer)，required 包含 vulnclass/severity/summary；traffic_refs 可关闭，severity 非 enum，不额外猜要求。
- record_fact（1354-1369）：facts array(object)，summary/detail/evidence/confidence string，intent_id integer，asset_ids array(integer)，原无 required。
- get_worker_trace（1828-1833）：intent_id integer，q string，step_ids array(integer)，limit integer；required 含 intent_id。
- add_intent（1094-1100）：intents array(object)，summary string，asset_ids/parent_ids array(integer)，priority integer；原无 required，不能虚构 items 内必需字段。
- prove_goal（1159-1163）：goal_id/evidence_id integer，reason string；required 含 goal_id/evidence_id。
- set_goals（1479-1483）：goals array(object)，text/vulnclass string；原无 required。

只有尾巴与完整三工具族 BOTH 成立才 enforce。单品牌词、漏洞/渗透/PoC、Bash/Read/Skill/MCP、流量工具、session header、UA、普通文件规约、1-2 项工具都不能硬封。不做打分兜底扩大匹配。

## 提取边界与入口

- Anthropic：top-level system 字符串或 text blocks，tools[].name/input_schema。
- Chat：messages 的 system/developer 合法文本，tools[type=function].function.name/parameters。
- Responses：instructions 与 input 的 system/developer 文本，flattened tools[type=function].name/parameters。
- 不递归搜索 user/assistant/tool 结果、quoted JSON、metadata、图像/base64、function_call_arguments 或 tool_choice。用户研究/代码引用中的完整源码不可被当成实际声明。
- 首版覆盖现有上述三兼容 LLM JSON POST 的 /v1、根 aliases、已存在 /antigravity 兼容入口；按真实路由核实覆盖，不给模型列表/count_tokens/支付/上传/媒体/本机 ARTEX UI 加拦截。Gemini 原生形状非该工具已证 provider 格式，不冒充已覆盖。
- HTTP 在既有 body limit 与 API Key 鉴权后、选账号/转发/计费前检查；完整读后恢复字节级原 body，原 MaxBytesReader 上限保留。读取错误/413 用原协议错误语义，不解析任意 URL 或做 decompress/网络查询。
- WS Responses：首个实际模型请求 payload 在选号/上游首次发送前检查；已支持 native/passthrough/HTTP bridge 的后续请求，在现 BeforeRequest 或归一化后实际生效 payload 的验证点共享同一检测器，先于 upstream write；不能用只检查握手或 BeforeTurn 无 payload 代替逐帧。会话 instructions/tools 的生效合并若现实现支持必须在对应真实点测试；若存在不能安全复原的 previous_response_id 远端历史，明确不能检查远端未携带内容，不编造已完整覆盖。

## 行为与恢复

仅新增目标专属 gateway.artex_guard.mode = off / monitor / enforce。正常加载配置默认 enforce：只拦三种高可信组合；空零值采用同默认，非法显式 mode 校验失败，不能悄悄降级。monitor 对相同强命中仅记录而放行；off 不读取/改变 body。模式文件变更重启生效，首版不新增通用管理风控 UI/长期封禁或假热更新。

HTTP 强命中返回协议兼容 403、稳定 code artex_client_blocked/type permission_error 或 policy_violation；WS 返回已有政策拒绝消息/1008 或协议错误，具体沿现桥接契约，保证不二次转发。上游调用次数 0，未产生成功 usage/扣费；不存在永久封 IP/Key 或跨请求风险画像。目标日志只包含事件 artex_guard_decision、rule_id、规则版本、协议、mode、decision/request-id；不记录原 prompt、tools schema、参数、目录、目标域名、API Key/token 或 response body。事件结构与 existing ingress reason 可复用，但不触发泛恶意画像/其它 cyber session ban。

回滚仅将专属 mode=off 后按既有方式重启本机/发布环境；本任务仅本地实现验证，不重启用户现有服务或部署生产。原 branches、工具源与计费未保存差异全部保留。

## 验证与完成门

纯检测矩阵三角色 × Anthropic/Chat/Responses（9 强正例），合法转义/CRLF/工具顺序/成员顺序/可编辑 BODY/无 traffic 仍识别；工具缺项/类型错/name case 不一致不硬封；每项只在一个真实 schema 中取证。

正常负例至少：普通聊天/代码助手，授权安全研究/漏洞报告/CTF，README/ARTEX 品牌文本，user/tool-result 中完整尾巴+伪 schema，system 中引用尾巴但普通 tools，完整工具族但普通 system，只有短尾巴标题，所有常见无 tools SDK，images/audio/metadata 路由跳过。主动当前 system+真实工具完整复刻协议是观测等价，必须在文档中说清例外/误伤上限，不承诺零误伤。

真实中间件/注册路由 fixtures：三协议和全部既有 alias matched upstream=0/非匹配 upstream=1，monitor/off 放行且 body 精确保留，bodylimit边界/取消/读取失败不损坏，身份校验顺序和分组权限不放宽，SSE/stream 参数、normal toolcalls 不变。WS 首帧与后帧以及每个 native/passthrough/HTTP bridge 支持路径强匹配均在真实 write 前拒；无变更请求仍通过，session/远端历史边界不虚报。

最后运行 detector/middleware/routes/config/WS 定向 + 后端全 unit/vet/build，绑定最后 Git 候选、原始输出及 readonly 独审。设计门未批准前不实施入口封禁；最终独审不覆盖实际生产开启/流量回放，未部署不称已生效。

## 实践覆盖边界（源码候选，尚未运行验证）

- HTTP 实际文本入口：`/v1/messages`、`/antigravity/v1/messages`；`/v1/chat/completions`、`/chat/completions`；`/v1/responses`、`/responses`、`/backend-api/codex/responses` 及各自已有 `/compact` 生成请求子路径（包括裸入口尾斜线兼容）。原不可安全转发的畸形后缀仍由既有守卫返回 404；字符合法但非已证模型生成语义的其它后缀（包括 `/unknown`）跳过本专项 gate，保留原路由分发/上游行为，不新造 403 或 404。`input_tokens` 等计数入口不新增此拦截；无根 `/messages` 或 Antigravity Chat/Responses 路由，不虚构其覆盖。
- 此 gate 只读既有 MaxBytesReader 限定字节并恢复 Body cursor；不改 ContentLength、Content-Length、TransferEncoding、Content-Encoding、GetBody，不新增解压。前置原中间件已经变换并回填的 PrereadBody 按当时实际生效字节检查；未被原链解压的压缩包不假称可识别。
- native pool/shared/dedicated 与 HTTP bridge 原实现只接受逐次 response.create，不支持 session.update 的 instructions/tools 继承；检测当前实际帧，不给这些路径凭空造历史。Passthrough 对当前 response.create/session.update 的合法字段及**真实上游 session.updated echo**确认的 instructions/tools 两字段进行检查，客户端提交不等于远端接受；pending/rejected/error 不升 known。每次确认只以本次 echo 中实际字段建立 fresh known，缺字段为 unknown，不沿用旧证据。null/空字符串/空数组显式覆盖；每个 response.create 覆盖只对该次有效，不持久化。
- 上游不提供可靠成功 echo 时，拆分提交且未确认的有效会话内容、仅通过 previous_response_id 保存在远端的历史均不可安全复原，识别存在明确漏检边界；不为补齐覆盖引入等待引擎或跨请求画像。主动把完整尾巴与真实工具族放在有效系统指令中复刻执行协议依然观测等价，无法靠本规则区分正常授权实验；不是“零误伤”或全变种保证。
- 注册路由测试仅用 private terminal DI 保留真实注册/auth/permission/limit/guard/subpath 链，终端向本机 httptest mock 上游发真实 HTTP 并记录 fixture usage。公开注册签名不变且生产永远 nil DI；该 fixture 不是带真实账号与数据库扣款的完整生产计费集成证明。WS fixtures 走真实 service ingress/native WriteJSON/passthrough WriteFrame/HTTP bridge Do 调用计数，尚待 Lead 执行并独审。
