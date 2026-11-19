# Triage 标签

每个被 triage 过的 issue 恰好携带一个 **category** 角色和一个 **state** 角色。
角色名是规范名，右列才是本仓库实际要应用的标签字符串。

| 角色 | 类型 | 标签字符串 |
| --- | --- | --- |
| `bug` | category | `bug` |
| `enhancement` | category | `enhancement` |
| `needs-triage` | state | `needs-triage` |
| `needs-info` | state | `needs-info` |
| `ready-for-agent` | state | `ready-for-agent` |
| `ready-for-human` | state | `ready-for-human` |
| `wontfix` | state | `wontfix` |

`bug` 和 `enhancement` 是 GitHub 自带的默认标签。
五个 state 标签已在本仓库创建好，直接用即可。

若要改，只改右列字符串，不要改角色名 —— 角色名是 `triage` skill 的接口。
