# Git 协作规范

来源：课程设计指导书 5.3「Git 规范」。这些是**硬性要求**，不是建议。

## 分支

- `main` 必须始终保持可运行（随时能按 `start.sh` 起来）。
- 新功能一律在 `feature/<模块>` 分支开发，例如 `feature/furniture-library`、
  `feature/scene-engine`、`feature/floorplan-editor`。
- 合并走 Pull Request，**合并前需他人评审**（指导书 1.3/5.3 的硬性要求，
  评审记录计入过程考核 —— 所以评审要留在 GitHub 上，不要只在本地口头过一遍）。
  仓库已把 `required_approving_review_count` 设为 **`1`**，且开启
  `dismiss_stale_reviews_on_push`（approve 之后又推了新提交，旧 approve 作废，
  防止"批完又改"）。

  实际执行口径：

  | who | 需要几次 approve | 说明 |
  |---|---|---|
  | 组员（仓库 `write` 权限） | **1 次，且必须是组长** | 没有 bypass，凑不够就合不了 |
  | 组长（仓库管理员） | 0（走 bypass） | 见下 |

  组长账号在 ruleset 的 `bypass_actors` 里（`RepositoryRole:5:always`），
  所以自己的 PR 可以直接合。**但课程 1.3 要求"评审记录计入过程考核"**，
  自己合等于没有记录；建议组长的 PR 也照样找一名组员点一次 Approve，
  只是在赶进度时保留"没人可批也能合"的余地。
- **`main` 只接受 Pull Request，不接受任何直接推送**。仓库的 ruleset
  `main-protected` 对 `refs/heads/main` 生效四条规则：禁止删除、禁止强推
  （`non_fast_forward`）、**必须经由 PR 合并**（`pull_request`）、
  **CI 必须全绿**（`required_status_checks`，两项：`backend（vet、测试、生成物无漂移）`
  与 `frontend（类型检查与构建）`）。绕过名单只有仓库管理员角色（`RepositoryRole:5`），也就是组长本人；
  组员没有 bypass，必须走 PR + 组长 approve + CI 全绿。
- 合并方式用 **squash**，一个 PR 对应 `main` 上一条提交；合并后删掉源分支。
  这样 `git log` 一行一件事，回滚以 PR 为单位。
- PR 正文要写清楚三件事：**做了什么、为什么、怎么验**；能对应到 issue 的，
  在正文里写 `Closes #<编号>`，合并时自动关闭。
- 分支从最新 `main` 切出，命名 `feature/<模块>` 或 `<类型>/<主题>`
  （`docs/pr-required`、`fix/room-hash-drift`）。

## 跨平台（Windows 队友必读）

三个人不一定用同一个系统，仓库按"**任何平台克隆下来都能跑**"准备，代价是两条纪律：

- **行尾一律 LF**。仓库根有 `.gitattributes`（`* text=auto eol=lf`，脚本与 SQL 显式 `eol=lf`），
  但前提是本地别开 `core.autocrlf=true`。检查：`git config --get core.autocrlf` 应为空或 `false`。
  如果已经用 CRLF 检出过，执行 `git rm --cached -r . && git reset --hard` 重新落盘。
  症状：`bash: $'\r': command not found`，或容器初始化 SQL 报语法错。
- **`start.sh` 是 bash 脚本**。Windows 上用 Git Bash 或 WSL2 跑 `bash start.sh`；
  PowerShell / cmd 直接双击是不行的。没有 `psql` 也没关系，`scripts/reset.sh`
  会自动改用容器内的 psql（`docker compose exec -T db psql`）；有 `psql` 就用本地客户端连 5433。
- Docker Desktop 建议开 WSL2 后端；仓库路径尽量放在纯 ASCII 目录下
  （中文路径在个别工具与卷挂载上会出问题，本项目没为此做兼容）。

## 提交信息

前缀从下列五种中选，格式 `前缀: 简述`（中文简述即可）：

| 前缀 | 用途 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | 修缺陷 |
| `docs` | 文档 |
| `refactor` | 重构（不改变外部行为） |
| `test` | 测试 |

示例：

```
feat: 支持家具类型模板的属性差异化配置
fix: 平面图刷新后家具坐标回到原点
docs: 补需求规格说明书 F5 的温控策略说明
```

## 禁止提交

- 密钥、token、证书
- 数据库密码、`.env` 中的真实凭据
- `node_modules/`、构建产物、缓存

仓库根的 `.gitignore` 已排除这些默认路径。**在 `.gitignore` 之外新增的同类文件也要手动排除**：
提交前用 `git status` 确认，不要 `git add -A` 一把梭。

## 节奏

- 每周至少一次完整合并与联调（所有人把分支合回 `main` 并跑通一遍）。
- 第 15 周代码冻结：之后只修缺陷，不加功能。
