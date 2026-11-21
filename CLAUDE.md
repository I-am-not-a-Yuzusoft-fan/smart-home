# smart-home

智能家居系统（Smart Home System）—— 软件工程课程设计项目。
指导书见 `课程设计书/2026智能家居系统-课程设计指导书.md`。

## Agent skills

### Issue tracker

Issues 存放在 `I-am-not-a-Yuzusoft-fan/smart-home` 的 GitHub Issues；在仓库根目录用 `gh` 操作。详见 `docs/agents/issue-tracker.md`。

### Triage labels

五个状态角色（`needs-triage`、`needs-info`、`ready-for-agent`、`ready-for-human`、`wontfix`），加上 GitHub 原生的分类标签 `bug` / `enhancement`。详见 `docs/agents/triage-labels.md`。

### Domain docs

单上下文：仓库根目录的 `CONTEXT.md` 与 `docs/adr/` 中的 ADR。详见 `docs/agents/domain.md`。

## 文档与图表

图表一律用 PlantUML / Graphviz 源码生成 SVG 再嵌入 Markdown。**禁止在 Markdown 里用代码块或 ASCII 字符画图**（代码清单、目录树不在此列）。源码放 `docs/diagrams/src/`，产物放 `docs/diagrams/svg/`，改完用 `scripts/build-diagrams.sh` 重新渲染，并转 PNG 目视核对一遍。字号与配色沿用现有图：中文用「苹方」，线宽 1px，黑白灰为主。

## Git 规范

主分支 `main` 保持可运行且**只接受 PR**（ruleset `main-protected` 禁止直接推送与强推，无绕过名单，squash 合并）；功能在 `feature/<模块>` 分支开发，合并前需他人评审；提交信息用 `feat` / `fix` / `docs` / `refactor` / `test` 前缀；禁止提交密钥、数据库密码与 `node_modules`。详见 `docs/agents/git-workflow.md`。
