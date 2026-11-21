# Git 协作规范

来源：课程设计指导书 5.3「Git 规范」。这些是**硬性要求**，不是建议。

## 分支

- `main` 必须始终保持可运行（随时能按 `start.sh` 起来）。
- 新功能一律在 `feature/<模块>` 分支开发，例如 `feature/furniture-library`、
  `feature/scene-engine`、`feature/floorplan-editor`。
- 合并走 Pull Request，**合并前需他人评审**（指导书 1.3/5.3 的硬性要求，
  评审记录计入过程考核 —— 所以评审要留在 GitHub 上，不要只在本地口头过一遍）。
  注意：仓库当前 `required_approving_review_count` 是 `0`，因为现阶段只有一人提交；
  组员接入后把它改成 `1`，让评审从"应该做"变成"做不到就合不了"。
- **`main` 只接受 Pull Request，不接受任何直接推送**。仓库的 ruleset
  `main-protected` 对 `refs/heads/main` 生效四条规则：禁止删除、禁止强推
  （`non_fast_forward`）、**必须经由 PR 合并**（`pull_request`）、
  **CI 必须全绿**（`required_status_checks`，两项：`backend（vet、测试、生成物无漂移）`
  与 `frontend（类型检查与构建）`）。规则没有绕过名单（`bypass_actors` 为空，
  `current_user_can_bypass: never`）——包括仓库管理员在内，谁也推不上去。
- 合并方式用 **squash**，一个 PR 对应 `main` 上一条提交；合并后删掉源分支。
  这样 `git log` 一行一件事，回滚以 PR 为单位。
- PR 正文要写清楚三件事：**做了什么、为什么、怎么验**；能对应到 issue 的，
  在正文里写 `Closes #<编号>`，合并时自动关闭。
- 分支从最新 `main` 切出，命名 `feature/<模块>` 或 `<类型>/<主题>`
  （`docs/pr-required`、`fix/room-hash-drift`）。

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
