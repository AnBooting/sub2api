# Git 工作流

> 面向「不熟悉 git」的使用者。照抄命令即可。

## 1. 仓库结构

```
F:\sub2\                     ← 仓库根
├── .git\
├── backend\  frontend\  deploy\  docs\  skills\  openspec\  tools\   ← 上游代码
├── project-docs\             ← 我们写的项目文档（会提交）
├── _reference\               ← 参考资料（会提交，但不参与构建）
│   ├── domestic-provider-kit\          ← 国模移植包
│   └── domestic-provider-kit-1.0.65.zip ← 原始压缩包备份
├── Dockerfile  Makefile  README_CN.md  ...  ← 上游文件
└── AGENTS.md                 ← ⚠️ 被上游 .gitignore 忽略（见下）
```

### ⚠️ 两个 gitignore 陷阱

1. **`AGENTS.md` 被上游 .gitignore 忽略**（`.gitignore` 里有 `AGENTS.md` 一行）。
   如果要把项目规则提交进仓库，需要用 `git add -f AGENTS.md`，或改名（如 `PROJECT-AGENTS.md`）。
2. **`docs/*` 被上游 .gitignore 忽略**，只放行白名单里的几个文件。
   这就是我们把项目文档放在 **`project-docs/`** 而不是 `docs/` 的原因。

## 2. 远端

| 远端名 | 地址 | 用途 |
|---|---|---|
| `upstream` | `https://github.com/Wei-Shaw/sub2api.git` | 只读，用于拉取上游更新 |
| `origin` | **待创建**（私有仓库） | 存放我们的二开代码 |

创建私有 origin（等你确认后执行）：
```powershell
gh repo create sub2api-fork --private --source F:\sub2 --remote origin --push
```

## 3. 分支模型

| 分支/标签 | 作用 | 规则 |
|---|---|---|
| `main` | 上游镜像 | **永不在此提交**；只用 `git fetch upstream` 更新 |
| `develop` | 二开集成分支 | 所有功能合并到这里；部署来源 |
| `feature/<名字>` | 单个需求 | 从 `develop` 切出，完成后合回 |
| `baseline-v0.2.15` | 基线标签 | 指向 `3a6fd1c9`，永不移动 |
| `baseline-upstream-v0.2.15` | 同上（别名） | 保留 |

## 4. 日常操作

### 开始一个新需求
```powershell
cd F:\sub2
git checkout develop
git pull                       # 如果已有 origin
git checkout -b feature/国模分组
```

### 提交
```powershell
git status                     # 先看改了什么
git add -A
git commit -m "feat(group): 支持国模厂商选择与校验"
```

### 合并回 develop
```powershell
git checkout develop
git merge --no-ff feature/国模分组
git branch -d feature/国模分组
```

### 打一个部署标签
```powershell
git tag -a deploy-2026-10-15 -m "生产部署 2026-10-15"
```

### 跟随上游（需要时，手动）
```powershell
git fetch upstream --tags
git log --oneline HEAD..upstream/main | Select-Object -First 20   # 先看有什么
git merge upstream/main        # 在 develop 上合并；有冲突逐个解决
```

## 5. 红线（不要做）

- ❌ 不在 `main` 上提交
- ❌ 不 `git push --force`
- ❌ 不提交 `.env`、`data/`、`postgres_data/`、`redis_data/`、任何 API Key 或密码
- ❌ 不 `git reset --hard` 丢弃未提交工作（除非明确知道会丢什么）
- ❌ 不把上游 `docs/` 目录当作我们文档的存放地

## 6. 提交前自检

```powershell
git status --short
git diff --cached --stat
# 确认没有敏感文件：
git diff --cached --name-only | Select-String -Pattern '\.env$|secret|credential|password'
```
