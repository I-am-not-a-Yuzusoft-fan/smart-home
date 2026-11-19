# Issue tracker

本仓库的 issues 存放在 **GitHub Issues**：`I-am-not-a-Yuzusoft-fan/smart-home`。

使用 `gh` CLI，并在仓库根目录下运行，这样 `gh` 能从 git remote 解析出仓库。

## 命令

| 任务 | 命令 |
| --- | --- |
| 列出未关闭 | `gh issue list --state open --limit 100` |
| 新建 | `gh issue create --title "<标题>" --body-file <路径>` |
| 读取（含评论） | `gh issue view <编号> --comments` |
| 评论 | `gh issue comment <编号> --body-file <路径>` |
| 加标签 | `gh issue edit <编号> --add-label "<标签>"` |
| 关闭 | `gh issue close <编号> --reason "not planned"` |

多行内容请用 `--body-file` 而不是 `--body`：既避开 shell 引号问题，
也允许先用 Write 工具把正文写成文件，再交给 `gh`。

## 把 PR 作为请求入口

**关闭。** PR 不进入 triage 队列，`triage` 只处理 issues。

注意这**不等于「不使用 PR」**：按课程设计指导书 5.3 的要求，功能分支必须
经 PR 合并并且合并前需他人评审（见 `docs/agents/git-workflow.md`）。
本开关只决定「外部 PR 是否被当作待 triage 的请求」，与「用不用 PR 合并」无关。

如果你希望外部 PR 也像 issue 一样被 triage（「PR 就是附带代码的 issue」），
把它改成**开启**。开启后，`triage` 会把裸 `#42` 也解析为 PR，
并为每行标注 `[PR]` 或 `[issue]`。
