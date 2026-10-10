# 提取验证记录

日期：2026-09-20。验证对象为主站 1.0.65 的源码快照及本交付包；没有修改或部署主站，也没有运行任何真实上游付费请求。

## 源码测试

后端在原源码快照中执行，全部通过：

```sh
go test -tags=unit ./internal/service ./internal/repository ./internal/handler ./internal/handler/dto -run 'Domestic' -count=1
go test ./migrations -run 'Domestic' -count=1
go test ./internal/pkg/openai_compat -count=1
```

覆盖国模厂商校验、定价与计费集成、SQL mock 仓储、网关缺价拒绝、DTO 字段，以及迁移内容和协议模式。迁移测试为源码断言，不等同于接收方真实 PostgreSQL 升级测试。

前端从同一快照复制到临时测试目录，使用现有 Node 22 / Vitest 2.1.9 依赖执行：

```sh
vitest run \
  src/utils/__tests__/domesticProviders.spec.ts \
  src/api/admin/__tests__/domesticPricing.spec.ts \
  src/components/common/__tests__/GroupSelector.domestic.spec.ts \
  src/components/common/__tests__/GroupOptionItem.domesticRate.spec.ts \
  src/components/common/__tests__/GroupBadge.domesticRate.spec.ts
```

结果：5 个测试文件、8 个用例全部通过。测试工具提示 caniuse-lite 浏览器资料较旧，不影响本次结果。

## 包验证

提取文件按 SOURCE-MANIFEST.json 逐一校验，保持源字节不变。交付前检查文件白名单、符号链接、JSON 语法、文档本地链接及常见私钥/令牌/连接串特征，并验证 ZIP 解压后的 SHA256SUMS。

只读预检脚本已针对原快照运行，所有列出的源文件均为 identical；也检查了不存在项目路径的错误处理。预检只比较路径与内容，不承诺目标项目接口兼容。

## 尚需接收方验证

未获得接收方项目版本，未执行其全量构建、真实数据库升级、浏览器截图验收或上游调用。tests 目录依赖原项目测试辅助接口，不能当作独立可执行测试项目。移植后的最终标准见 ACCEPTANCE.md。
