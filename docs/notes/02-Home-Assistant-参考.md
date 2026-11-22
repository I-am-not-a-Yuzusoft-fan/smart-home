# Home Assistant 参考笔记

> 目的：**不重复造劣质轮子**。把 HA 踩过的坑变成我们的设计约束，而不是从头再踩一遍。
> 核验日期 2026-11-19；版本线 2026.11.2（稳定）/ `2026.12.0.dev0`（main）。

## 0. 结论先行

| | 内容 |
| --- | --- |
| **必须借鉴（设计）** | ① 场景/脚本/自动化三者分工；② 动作的 target 寻址（实体/设备/区域/标签）；③ 执行语义（run mode、单步失败策略、单步启用开关） |
| **不要借鉴（实现）** | YAML 优先的配置模型、Python asyncio 事件总线、Jinja 模板、Blueprint、3000+ 集成 |
| **需要我们自己定** | 温控舒适度算法（HA 没有，这是指导书 F5 的评分区分点） |

## 1. 许可证与体量

- **Apache-2.0**（`pyproject.toml`: `license = "Apache-2.0"`，`LICENSE.md` 首行为 Apache License 2.0）。
- `requires-python = ">=3.14.2"`，`version = "2026.12.0.dev0"`。

含义：**读代码、借鉴设计符合其许可证要求**（保留声明即可），但把它的代码搬进我们的 Go/TS 仓库没有工程意义——语言、运行时、依赖全不同。所以这里是「读文档定契约」，不是「引依赖」。

## 2. 三个概念的分工（最容易搞错的地方）

HA 把「让设备变成某个样子」拆成三件不同的事，官方原文（[Scenes](https://www.home-assistant.io/docs/scene/)）：

| | 场景 Scene | 脚本 Script | 自动化 Automation |
| --- | --- | --- | --- |
| 有没有触发 | 无 | 无（手动/被调用） | **有 trigger** |
| 有没有条件 | 无 | 无 | **有 condition** |
| 干什么 | **只设状态**，无步骤、无顺序、无等待 | 有顺序的步骤序列 | 条件满足则跑步骤序列 |
| 有没有开关状态 | **没有**（不是 on/off 实体） | 无 | 有（enabled/disabled） |
| 记得之前吗 | 不记得，要恢复得先 `scene.create` 快照 | 不记得 | 有 `last_triggered` 等属性 |

原文关键句：「A scene sets states. It doesn't run steps, and it doesn't remember what came before.」「A scene sets all its states, without wait times or a set order. If you need wait times or a set order, use a script.」

**这直接决定我们的数据模型。** 指导书 F5 写的是「每个模式由『条件 + 动作集』构成，动作可带参数与延时」——那是**脚本语义**，不是场景语义。如果我们把「回家模式」做成一组 `{设备: 状态}` 的键值对，就永远做不出「延时」和「顺序」，也就拿不到 F5 的过程反馈分。

## 3. 状态模型

HA 的核心抽象（[Entities & domains](https://www.home-assistant.io/docs/configuration/entities_domains/)）：

- **entity**：`entity_id`（如 `light.ceiling`）、**domain**（`light`/`climate`/`cover`/`sensor`）、**state**（当前值）、**attributes**（亮度、色温、目标温度…）、`last_changed`、`last_updated`。
- **device**：物理设备，一个 device 可含多个 entity。
- **area / floor / label**：空间与分组维度。
- 状态对象三件套：`state`（单值）+ `attributes`（结构化附加信息）+ 时间戳。

与我们指导书 3.4 的「类型—能力—状态」三层是一一对应的：`domain + attributes` ≈ **能力**，`state` ≈ **状态**，`device/area` ≈ **类型与归属**。差异在于我们把「能力」升格成**配置数据**（F2 要求新增类型不改前端），HA 是**代码里的集成**——这点我们必须自己走，不能照抄。

## 4. 动作的寻址（target）

HA 的动作不绑定单个实体，而是可以指向 **entity / device / area / label** 四类 target；多 target 时还能选 `each` / `first` / `all` 语义（[Automation targets](https://www.home-assistant.io/docs/automation/targets/)）。

对我们的价值：F5 的「回家模式」如果写成 13 个设备 ID 的硬编码列表，第 12 周加一盏灯就要改代码。**正确做法是动作指向「区域 + 类别」**——「客厅的所有灯」「主卧的空调」——再由后端解析成具体设备。这也顺带满足 F2 的「新增类型不改前端」（新增设备自动被区域查询命中）。

## 5. 执行语义（我们八成会漏的边界）

来自 [Building blocks](https://www.home-assistant.io/docs/automation/building-blocks/) 与 [Automation modes](https://www.home-assistant.io/docs/automation/modes/)：

1. **run mode 四选一**：`single`（重复触发直接忽略）/ `restart`（中断上一次重跑）/ `queued`（排队）/ `parallel`（并发）。
   → 必须回答的问题：**「回家模式」执行到一半又点了一次，怎么办？** 我们的选择：默认 `restart`（用户手动点=我想重来），定时触发用 `single`。
2. **单步失败默认中止**，可 `continue_on_error: true`；**目标不存在不算失败**（跳过并记 warning）。→ 这正好对应 F4 的「成功/失败/超时反馈」三态 + 「部分成功」。
3. **每步可 `enabled: false`**，且可以是模板（运行时求值）。→ 调试期关掉某步而不删掉它，答辩现场改配置时非常有用（对应指导书 6.3「现场即兴改配置」）。
4. **每步都有 `alias`**，trace 里显示的是 alias。→ F5 要求「执行需有过程反馈」，打包成：每步 `{alias, status: pending|running|ok|failed|skipped, startedAt, error}`，前端直接渲染成时间线。

## 6. 场景快照与恢复

`scene.create` 把若干设备的当前状态存成一个临时场景，之后再 `scene.turn_on` 恢复（[Scenes: 临时场景](https://www.home-assistant.io/docs/scene/)）。

对我们是个便宜又出彩的加分项：**离家时自动快照 → 回家时提示「要恢复离开前的样子吗」**。实现成本低（一张 `scene_snapshot` 表），但对答辩很好讲，且与 E1 的「手动接管后策略让位」是同一套机制的两个用法。

## 7. 借鉴与取舍

| 借鉴 | 理由 |
| --- | --- |
| 场景 / 脚本 / 自动化三分 | 直接决定 F5 的数据模型，且解释力强 |
| target 寻址（区域 + 类别） | 避免硬编码设备列表；顺带满足 F2 |
| run mode 四种语义 | 边界情况现成答案，不用自己发明 |
| 单步 `enabled` / `continue_on_error` / `alias` | 直接产出 F5 的过程反馈 UI |
| `scene.create` 快照恢复 | 低成本加分项 |

| 不借鉴 | 理由 |
| --- | --- |
| YAML 优先配置 | 我们是 UI-first + 数据库；YAML 只作为导入导出格式（F1 已要求 JSON/CSV） |
| Python asyncio 事件总线 | 我们用 Go goroutine + channel，且规模小两个数量级 |
| Jinja 模板 | 用户不该写模板；参数化用表单 + 有限的表达式 |
| Blueprint | 参数化配置复用，课设用不上 |
| 3000+ 集成 / 自动发现 | 我们是模拟器 + 6 类设备 |

## 8. 对数据模型的最小改动（草案）

**已定：做通用步骤编辑器**（不收敛成 5 个内置模式，见第 9 节）。因此步骤是**可嵌套的树**，不是平铺列表——`choose`（if-then-else）、`repeat`、`parallel` 都会再带子步骤。

树用 **JSONB 文档**存，不拆成邻接表：编辑器的保存/撤销/复制整棵树是一次写，拆表反而要维护 `parent_id + seq` 的一致性。

```
mode    id, name, definition jsonb, run_mode, enabled, updated_at
run     id, mode_id, started_at, finished_at, status, definition_snapshot jsonb
run_step_run_id, node_id, path, alias, status, started_at, finished_at, error
```

`definition` 的形状（每个节点都有稳定 `nodeId`，执行记录按 `nodeId` 回填）：

```json
{
  "version": 1,
  "trigger": { "type": "state", "entity": "binary_sensor.door", "to": "on" },
  "conditions": [{ "type": "numeric_state", "entity": "sensor.illuminance", "below": 50 }],
  "runMode": "restart",
  "steps": [
    { "nodeId": "n1", "alias": "玄关灯", "action": "light.turn_on", "target": {"area": "玄关"}, "data": {"brightness": 30} },
    { "nodeId": "n2", "alias": "等 2 秒", "block": "delay", "duration": "2s" },
    { "nodeId": "n3", "alias": "客厅灯", "action": "light.turn_on", "target": {"area": "客厅"}, "data": {"brightness": 80} },
    { "nodeId": "n4", "alias": "空调", "action": "climate.set_temp", "target": {"area": "客厅", "domain": "climate"},
      "data": { "mode": "auto" },
      "valueFrom": "comfort", "reason": "computed" },
    { "nodeId": "n5", "block": "choose",
      "branches": [
        { "conditions": [{ "type": "state", "entity": "person.li", "state": "home" }],
          "steps": [{ "nodeId": "n5a", "action": "climate.set_temp", "target": {"area": "主卧"}, "data": {"temp": 25} }] },
        { "steps": [{ "nodeId": "n5b", "action": "climate.turn_off", "target": {"area": "主卧"} }] }
      ]
    },
    { "nodeId": "n6", "alias": "净化器", "action": "fan.turn_on", "target": {"area": "客厅"}, "data": {"speed": 1},
      "continueOnError": true }
  ]
}
```

注意 `"valueFrom": "comfort"` —— 空调那一步的参数由策略层算出来（F5 的动态温控），不是写死的 26，且把依据文案一起写进执行记录。**这是 HA 也不管、我们必须自己做的部分。**

## 9. 待办与已决

- [x] **已决（m00639）：做通用步骤编辑器**，不收敛成 5 个内置模式。理由：效率上来得及，且通用编排器本身就是答辩区分点。代价是模型从平铺列表升级成嵌套树（见第 8 节），编辑器 UI 要处理 `choose` / `repeat` / `delay` 等块。
- [ ] 把第 8 节的 JSONB 树模型并进 ADR 0002（数据模型与分层，第 11 周）。
- [ ] 编辑器的最小可用范围：首版支持 `action` / `delay` / `choose` 三种节点，`repeat` / `parallel` 留到增量。
- [ ] E1 的「手动接管后让位」与 run mode 的关系：需要一条 `suppressed_until` 字段。
