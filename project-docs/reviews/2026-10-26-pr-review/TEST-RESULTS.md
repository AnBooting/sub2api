# 验证结果

- Go 1.27.3，最新 #13 的隔离源码，公共文件 blob 与相关 #11/#12 逐一核对。
- 首次定向套件：44 个顶层测试通过；瞬时 Redis 故障的循环位置负例失败。新旧迁移护栏、Live 限额、简易模式、结算恢复与审计可观测性等已有定向回归均通过。
- 第二次针对性套件：上轮“缺可信旧历史不能放行”的负例通过；本轮三个负例失败。
- Redis 故障位置对照：入口回读失败能恢复；仅将失败移到循环首次回读，就只有 1 次 Apply、最终仍为 pending。
- git blame 已确认读错即 return 在 develop 基线中就存在，来源 6ec185397；这是既有 P1，新增回归是审计信息泄露和旧集成断言失配。
- 隐私：普通用户错误列表的 message 包含虚构内网地址及内部账本表名。
- 原集成断言重放：真实执行器发出新历史 INSERT 后，原 require.Zero 收到 count=1。不是实际 PostgreSQL 执行，也未把未来 CI 结果当已观测事实。
- 后端构建退出 0，包含新的 Wire 依赖装配；使用 -ldflags=-w 的验证二进制在交付前删除。
- 前端 vue-tsc -b 通过；VideoTasksView、videos API、locale 完整性共 3 文件 / 23 测试通过。
- 没有启动完整 unit/Vitest、真实 PostgreSQL/Redis、Docker 全栈或部署，也没有付费上游调用。

最终 run.ps1 的实跑输出见 runner-output.txt；应恰好出现 manifest 中 3 个失败，且 finally 清理所有注入文件。
