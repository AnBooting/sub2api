# 开发环境（本机）

> 本机为 Windows 11 家庭中文版。所有命令均在 PowerShell 下验证。

## 1. 已安装的工具链

| 工具 | 位置 | 版本 | 说明 |
|---|---|---|---|
| git | `E:\Git\cmd\git.exe` | 2.56.0 | 已配置 `user.name` / `user.email` |
| gh | `C:\Program Files\GitHub CLI\gh.exe` | 已登录 | scope: `repo`, `gist`, `read:org` |
| Node.js | `C:\Program Files\nodejs\node.exe` | v24.18.0 | 注意：CI/Docker 用 Node 20/24 |
| pnpm | npm 全局 shim | 11.18.0 | **不要用它**，见下方「pnpm 版本陷阱」 |
| **Go** | `F:\tools\go1.27.2\go\bin\go.exe` | **go1.27.2** | 便携版，已写入用户环境变量 |
| ocr | `F:\tools\ocr\opencodereview.exe` | v1.12.13 | 阿里 Open Code Review CLI（SHA256 已核对） |

### Go 环境变量（已持久化到用户级）

```
GOROOT      = F:\tools\go1.27.2\go
GOPATH      = F:\tools\gopath
GOTOOLCHAIN = local
PATH        += F:\tools\go1.27.2\go\bin
```

Go 模块代理（本机网络需要）：
```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'
$env:GOSUMDB = 'sum.golang.google.cn'
```

## 2. ⚠️ pnpm 版本陷阱（必读）

上游要求 **pnpm 9**：
- `Dockerfile` 用 `corepack prepare pnpm@9 --activate`
- CI `pnpm/action-setup@v6` 指定 `version: 9`

本机全局 pnpm 是 **11.18.0**，且 `corepack prepare` **不会**覆盖 npm 全局 shim。用 pnpm 11 会直接失败：

```
[ERR_PNPM_LOCKFILE_CONFIG_MISMATCH] The current "overrides" configuration doesn't
match the value found in the lockfile
```

原因：pnpm 10+ 不再从 `package.json` 的 `pnpm.overrides` 读取配置（迁移到 `pnpm-workspace.yaml`），
而上游 `frontend/package.json` 里仍有 `pnpm.overrides`。

**正确用法**：
```powershell
cd F:\sub2\frontend
corepack pnpm@9 install --frozen-lockfile --registry=https://registry.npmmirror.com
corepack pnpm@9 run build
```

## 3. 网络与代理

| 场景 | 配置 |
|---|---|
| GitHub（git） | 仓库本地配置 `http.proxy=http://127.0.0.1:7877`（Clash 混合端口） |
| GitHub（网页/API） | 直连可用；大文件下载走系统代理 |
| Go modules | `GOPROXY=https://goproxy.cn,direct` |
| npm | `--registry=https://registry.npmmirror.com`（官方源实测 10–26 秒/请求） |

> 若 Clash 端口变化，执行：`git -C F:\sub2 config --local http.proxy http://127.0.0.1:<新端口>`

## 4. 常用命令

### 后端
```powershell
cd F:\sub2\backend
go build -o bin/server.exe ./cmd/server      # 构建（约 3 分钟冷编译）
go test -tags=unit ./...                     # 单元测试
go test -tags=integration ./...              # 集成测试（需 PostgreSQL + Redis）
go generate ./ent                            # 改 ent schema 后重新生成
```

### 前端
```powershell
cd F:\sub2\frontend
corepack pnpm@9 install --frozen-lockfile --registry=https://registry.npmmirror.com
corepack pnpm@9 run typecheck                # vue-tsc --noEmit
corepack pnpm@9 run test:run                 # vitest
corepack pnpm@9 run build                    # i18n 检查 + vue-tsc + vite build
```

### 一键（上游 Makefile，需要 make，本机没有）
```
make build          # 前后端
make test           # 后端 + 前端
make test-backend
make test-frontend  # lint:check + typecheck + 关键 vitest
```

## 5. Docker（待安装）

安装包已下载：`F:\tools\downloads\DockerDesktopInstaller.exe`（606 MB）。

**需要你手动完成**（当前会话没有管理员权限）：
1. 右键 → 以管理员身份运行该安装程序
2. 安装向导中勾选 **Use WSL 2 instead of Hyper-V**
3. 安装完成后**重启一次**
4. 重启后启动 Docker Desktop，等待引擎就绪

验证：
```powershell
docker --version
docker compose version
docker run --rm hello-world
```

## 6. 资源占用参考

| 操作 | 实测耗时/占用 |
|---|---|
| `go mod download`（首次） | 约 2 分钟 |
| `go build ./cmd/server`（冷） | 163 秒，产物 153.3 MB |
| 仓库体积（不含 .git） | 55.7 MB |
| 磁盘余量 | F: 459.9 GB（充足） |

**内存提示**：本机 15.6 GB，当前空闲仅约 2.1 GB（DSH、浏览器、QQ、钉钉、Clash 占用较多）。
Docker Desktop 默认会占用 2–4 GB（WSL2 动态）。建议在跑 compose 时先关闭不必要的大型应用。
若要长期舒适地跑 Docker + 编译，可考虑把内存升到 32 GB（2×16GB DDR5-4800，主板支持最大 64 GB，两槽位已占满需替换）。
