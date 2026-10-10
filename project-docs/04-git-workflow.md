# Git 工作流

> 面向「不熟悉 git」的使用者。照抄命令即可。
> **2026-10-10 定稿**：基线 = 团队账号 **AnBooting/sub2api**（含 2 个魔改提交）。

## 1. 两个远端

| 远端名 | 地址 | 用途 | 权限 |
|---|---|---|---|
| `origin` | `https://github.com/AnBooting/sub2api.git` | **团队基线**（你们的魔改版） | 加协作者后可推 |
| `upstream` | `https://github.com/Wei-Shaw/sub2api.git` | 原始上游，只读参考 | 只读 |

`upstream` 已配置为**只跟随 `main`**（`+refs/heads/main:refs/remotes/upstream/main`），
避免 40+ 个上游特性分支污染本地视图。需要看某个上游分支时临时 fetch：

```powershell
git fetch upstream <分支名>:refs/remotes/upstream/<分支名>
```

## 2. 分支模型（最终）

```
upstream/main   Wei-Shaw/sub2api              原始上游（只读，仅跟随 main）
     │
     └─(团队 fork)─> origin/main = main        AnBooting 魔改基线
                        含 2 个提交：
                          11109acd  CNY 计价 + 额度计费开关
                          0e6f5efb  渠道模型映射 + 上游同步 + 价格快照
                        │
                        └─> develop  ★ 我们的集成分支
                              ├─ feat/REQ-XXX-<简述>
                              ├─ fix/<问题号>-<简述>
                              └─ chore/<内容>
```

| 分支 | 允许的操作 |
|---|---|
| `main` | 跟踪 `origin/main`。**只 `git pull`，禁止本地提交** |
| `develop` | 合并已验收的特性分支；打部署标签；**部署来源** |
| `feat/*` | 自由提交；完成后合回 develop |

> 早期用过的 `fork-base` 分支已删除（内容由标签 `baseline-fork-2026-10-09` 保留）。

## 3. 标签

| 标签 | 指向 | 含义 |
|---|---|---|
| `baseline-upstream-v0.2.15` | `3a6fd1c9` | 上游 v0.2.15 |
| `baseline-v0.2.15` | `3a6fd1c9` | 同上（别名） |
| `baseline-fork-2026-10-09` | `0e6f5efb` | **团队魔改基线**（当前起点） |

## 4. 日常操作

### 开始一个新需求
```powershell
cd F:\sub2
git checkout develop
git checkout -b feat/REQ-002-scheduling-concurrency
```

### 提交
```powershell
git status
git add -A
git commit -m "feat(scheduling): 为账号增加独立并发上限"
```

### 合并回 develop
```powershell
git checkout develop
git merge --no-ff feat/REQ-002-scheduling-concurrency
git branch -d feat/REQ-002-scheduling-concurrency
```

### 推送到团队仓库（需要协作者权限）
```powershell
git push -u origin develop
git push origin feat/REQ-002-scheduling-concurrency
```

### 打部署标签
```powershell
git tag -a deploy-2026-10-15 -m "生产部署 2026-10-15"
git push origin deploy-2026-10-15
```

### 查看团队魔改的 2 个提交改了什么
```powershell
git show --stat 11109acd          # CNY 计价
git show --stat 0e6f5efb          # 渠道映射
git show 11109acd -- <某个文件>    # 看具体文件
```

### 跟随上游（需要时，手动）
```powershell
git fetch upstream --tags
git log --oneline develop..upstream/main | Select-Object -First 20   # 先看有什么
git merge upstream/main          # 在 develop 上合并；有冲突逐个解决
```

## 5. 红线（不要做）

- ❌ 不在 `main` 上提交
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

## 9. 权限（待办）

当前账号 `yylovehhy` 对 `AnBooting/sub2api` **只有 pull 权限**（实测 push 返回 403）。

**AnBooting 是团队自己的组织账号**，直接由组织 Owner 操作即可：
仓库 `Settings` → `Collaborators and teams` → `Add people` → 输入 `yylovehhy` → 角色选 **Write**。

加完后验证：
```powershell
git push --dry-run origin develop
```
