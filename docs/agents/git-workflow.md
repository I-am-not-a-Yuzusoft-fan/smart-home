# Git 协作规范

来源：课程设计指导书 5.3「Git 规范」。这些是**硬性要求**，不是建议。

## 分支

- `main` 必须始终保持可运行（随时能按 `start.sh` 起来）。
- 新功能一律在 `feature/<模块>` 分支开发，例如 `feature/furniture-library`、
  `feature/scene-engine`、`feature/floorplan-editor`。
- 合并走 Pull Request，**合并前需他人评审**。组内至少每周一次代码互评，
  评审记录计入过程考核 —— 所以评审要留在 GitHub 上，不要只在本地口头过一遍。
- **禁止向 `main` 强推**。仓库已开启分支保护：`main` 不接受
  `git push --force` / `--force-with-lease`。

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
