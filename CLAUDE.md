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

## Git 规范

主分支 `main` 保持可运行；功能在 `feature/<模块>` 分支开发，经 PR 合并且合并前需他人评审；提交信息用 `feat` / `fix` / `docs` / `refactor` / `test` 前缀；禁止向 `main` 强推；禁止提交密钥、数据库密码与 `node_modules`。详见 `docs/agents/git-workflow.md`。
