# Git 工作流

> 面向「不熟悉 git」的使用者。照抄命令即可。
> **2026-10-10 更新**：基线已从上游 Wei-Shaw/sub2api 改为 **AnBooting/sub2api fork**（含 2 个魔改提交）。

## 1. 三个远端

| 远端名 | 地址 | 用途 | 权限 |
|---|---|---|---|
| `origin` | `https://github.com/AnBooting/sub2api.git` | **魔改基线**，我们的起点 | ❌ 无推送权限（当前账号 `yylovehhy` 只有 pull） |
| `fork` | 同 origin（别名） | 同上 | 同上 |
| `upstream` | `https://github.com/Wei-Shaw/sub2api.git` | 原始上游，只读 | 只读 |

> ⚠️ **重要**：当前账号 `yylovehhy` 对 `AnBooting/sub2api` **没有推送权限**（实测 `git push --dry-run` 返回 403）。
> 要备份我们的工作，需要：
> - 方案 A：让 `AnBooting` 把 `yylovehhy` 加为 collaborator
> - 方案 B：把仓库 fork 到 `yylovehhy` 名下，用那个作为 `origin`

## 2. 分支模型

```
upstream/main    Wei-Shaw/sub2api              原始上游（只读）
       │
       └─(fork)─> fork/main = origin/main      AnBooting 的魔改基线
                  │  含 2 个提交：
                  │   11109acd  CNY 计价 + 额度计费开关
                  │   0e6f5efb  渠道模型映射 + 上游同步 + 价格快照
                  │
                  └─> fork-base               本地只读快照（跟踪 origin/main）
                        │
                        └─> develop           ★ 我们的集成分支
                              ├─ feat/<需求号>-<简述>
                              ├─ fix/<问题号>-<简述>
                              └─ chore/<内容>
```

| 分支 | 允许的操作 |
|---|---|
| `main` | 只 `git fetch upstream`；**禁止提交** |
| `fork-base` | **只读**。origin 更新时：`git fetch origin && git branch -f fork-base origin/main` |
| `develop` | 合并已验收的特性分支；打部署标签 |
| `feat/*` | 自由提交；完成后合回 develop |

## 3. 标签

| 标签 | 指向 |
|---|---|
| `baseline-upstream-v0.2.15` | `3a6fd1c9`（上游 v0.2.15） |
| `baseline-v0.2.15` | `3a6fd1c9` |
| `baseline-fork-2026-10-09` | `0e6f5efb`（fork 魔改基线） |

## 4. 日常操作

### 开始一个新需求
```powershell
cd F:\sub2
git checkout develop
git checkout -b feat/scheduling-concurrency
```

### 提交
```powershell
git status                      # 先看改了什么
git add -A
git commit -m "feat(scheduling): 为账号增加独立并发上限"
```

### 合并回 develop
```powershell
git checkout develop
git merge --no-ff feat/scheduling-concurrency
git branch -d feat/scheduling-concurrency
```

### 打部署标签
```powershell
git tag -a deploy-2026-10-15 -m "生产部署 2026-10-15"
```

### 查看 fork 的 2 个魔改提交改了什么
```powershell
git show --stat 11109acd          # CNY 计价
git show --stat 0e6f5efb          # 渠道映射
git show 11109acd -- <某个文件>    # 看具体文件改动
```

### 跟随上游（需要时，手动）
```powershell
git fetch upstream --tags
git log --oneline develop..upstream/main | Select-Object -First 20   # 先看有什么
git merge upstream/main          # 在 develop 上合并；有冲突逐个解决
```

## 5. 红线（不要做）

- ❌ 不在 `main` 或 `fork-base` 上提交
- ❌ 不 `git push --force`
- ❌ 不提交 `.env`、`data/`、`postgres_data/`、`redis_data/`、任何 API Key 或密码
- ❌ 不 `git reset --hard` 丢弃未提交工作
- ❌ 不把上游 `docs/` 目录当作我们文档的存放地（它被 `.gitignore` 忽略）

## 6. ⚠️ 两个 gitignore 陷阱

1. **`AGENTS.md` 被上游 .gitignore 忽略** → 提交需 `git add -f AGENTS.md`
2. **`docs/*` 被上游 .gitignore 忽略**（只放行白名单）→ 我们的文档放在 **`project-docs/`**

## 7. 提交前自检

```powershell
git status --short
git diff --cached --stat
# 确认没有敏感文件：
git diff --cached --name-only | Select-String -Pattern '\.env$|secret|credential|password'
```

## 8. 网络

GitHub 直连不稳定，仓库已配置代理：
```powershell
git -C F:\sub2 config --local http.proxy http://127.0.0.1:7877
```
Clash 端口变化时同步修改。
