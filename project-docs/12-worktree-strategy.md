# Worktree 策略（前后端是否隔离）

> 结论先给：**默认不用 worktree；只在「两条流必须同时编译/运行且会互相阻塞」时才开。**
> 下面给出本机实测的成本数据与具体判断规则。

## 1. 实测成本数据（2026-10-10）

| 对象 | 大小 | 是否随 worktree 复制 |
|---|---|---|
| `.git` 对象库 | **180 MB** | ❌ 共享（worktree 只加一个指针文件） |
| 工作树源码（不含 .git / node_modules） | **62.8 MB** | ✅ 每份 |
| `frontend/node_modules` | **516 MB** | ✅ 每份（pnpm 硬链接可省部分磁盘） |
| Go 模块缓存 `GOPATH/pkg/mod` | **917.8 MB** | ❌ 共享（`GOPATH` 是全局的） |
| 前端构建产物 `backend/internal/web/dist` | 5.9 MB | ✅ 每份 |
| Go 构建缓存 | ~1–2 GB | ❌ 共享（`GOCACHE` 默认全局） |

**每个 worktree 的净增量 ≈ 62.8 MB + 516 MB ≈ 580 MB。**
磁盘不是瓶颈（F: 剩余 459.9 GB），**真正的瓶颈是内存与 CPU**：

| 资源 | 本机 | 说明 |
|---|---|---|
| 内存 | 15.6 GB（空闲约 2.1 GB） | 一个前端 dev server + 一个 Go 编译就能吃掉 2–3 GB |
| CPU | i7-12650H，16 线程 | Go 编译本身就会吃满多核 |

→ **本机同时跑 >2 个 worktree 的重型构建（前端 install + vite build 或 Go 全量编译）会明显卡顿。**

## 2. 决策规则

### 不需要 worktree 的情形（覆盖 80% 的工作）

- 后端与前端改**不同目录**，本仓库的实际情况就是如此（`backend/` vs `frontend/`），
  在一个工作树里用**写域（write scope）**约定即可，不需要物理隔离。
- 顺序推进的任务（先改后端再改前端）。
- 单文件或小范围改动。
- 文档、配置、脚本类改动。

### 需要 worktree 的情形

| 情形 | 为什么单树不行 |
|---|---|
| 长期特性分支要保持可编译，同时 develop 上要紧急修 bug | 切分支会打断编译与测试 |
| 需要**并排对比两个分支的行为**（改前 vs 改后） | 同一时刻只能 checkout 一个 |
| 大重构（如 ent schema 变更）与另一个独立特性并行 | `go generate ./ent` 会产生大量生成文件，切换分支会反复重建 |
| 需要**同时运行两个实例**做端到端对比 | 端口/数据目录冲突 |

## 3. 具体用法

### 创建

```powershell
cd F:\sub2
git worktree add ..\sub2-feat-sched -b feat/scheduling-concurrency develop
```

### 首次准备（每个 worktree 各做一次）

```powershell
cd F:\sub2-feat-sched\frontend
corepack pnpm@9 install --frozen-lockfile --registry=https://registry.npmmirror.com
```

> Go 侧不需要额外准备：`GOPATH` / `GOCACHE` 是全局的，模块与编译缓存自动共享。

### 列出与清理

```powershell
git worktree list
git worktree remove ..\sub2-feat-sched     # 必须工作树干净
git worktree prune
```

## 4. 资源冲突清单（多 worktree 并行时必须注意）

| 资源 | 冲突表现 | 处理 |
|---|---|---|
| **端口** | 两个实例都想用 8080 / 5432 / 6379 | 用 `SERVER_PORT` / compose 端口映射错开 |
| **Docker 容器名** | compose 里容器名写死 `sub2api` | 用 `-p <项目名>` 或 `COMPOSE_PROJECT_NAME` 隔离 |
| **数据目录** | `./data`、`./postgres_data` 各 worktree 独立 | 天然隔离，但别指向同一路径 |
| **Go 编译缓存** | 并发编译会争用 `GOCACHE` 锁 | 一般安全（Go 内部有锁），但会变慢 |
| **前端 dev server** | 端口 + `.vite` 缓存 | 端口错开；`.vite` 是每 worktree 独立的 |
| **Docker 镜像 tag** | 都打 `sub2api:dev` 会互相覆盖 | 用分支名做 tag：`sub2api:feat-sched` |
| **数据库迁移** | 两个实例连同一个库会互相迁移 | **每个 worktree 用独立数据库** |

## 5. 本项目当前建议

**现阶段（P0/P1）不开 worktree。** 理由：

1. 国模移植（P1）是**跨前后端的单一特性**，拆开反而增加合并成本。
2. 需求 2/3/4/6 尚未拆解到可以并行的粒度。
3. 本机内存余量小，并行重构建会拖慢一切。

**等到以下任一条件成立再开：**

- 同时有 ≥2 个**独立特性**处于实现阶段，且都需要跑构建/测试；
- 需要在服务器上对比验证两个版本；
- 内存升级到 32 GB 后（主板支持，2 槽位已满需替换为 2×16GB）。

**推荐过渡方案（更省资源）：**

- 前后端分离**用目录约定 + 写域**解决，不用 worktree。
- 需要并排验证时，用 **Docker 容器**隔离而不是 worktree（镜像 tag 区分，端口错开）。
- 真要用 worktree，**最多 2 个**，且错开重型构建时间。

## 6. 与 Agent Team 的关系

Agent Team 成员**共享同一个工作目录**，所以：

- 成员之间用**写域（write scope）**避免冲突，而不是 worktree。
- 侦察类任务**只读源码 + 只写各自的一个文档**，天然无冲突。
- 若两个成员必须改同一批文件 → **串行**，或拆成不同 worktree（此时才值得开）。
