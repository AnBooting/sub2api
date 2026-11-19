# repository-navigation 触发评估用例

这些用例用于后续项目内复核，不把一次通过当成全局路由证明。

## 应触发

1. “修改请求锁价逻辑，确认 Handler、Service、Ent、迁移和前端展示的影响。”
2. “调整 pricing_snapshot JSON 契约，评估 API、数据库、生成代码和测试。”
3. “重构 ModelPricingResolver，先给出 callers、blast radius 和测试计划。”

## 不应触发

1. “把这个 Go 注释的错别字改掉。”
2. “读取指定文件的第 20 到 40 行。”

## 最近邻路由

“只检查一个已知函数是否有编译错误”走项目验证命令；“跨模块修改前需要影响面”走本 Skill；“需要创建或维护 Skill 本身”走 skill-creator。
