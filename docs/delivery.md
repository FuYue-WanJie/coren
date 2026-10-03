# 交付审批（deliver）

`deliver` 让代理把**产物**（计划、文档、变更审查）连同**附件文件**提交给用户，
**暂停等待批准**，获批后才继续执行。这就是"先规划、确认、再执行"的工作流。

## 为什么单独一个工具

| 机制 | 语义 |
|---|---|
| `ask_user` | 问答：模型问一句，用户答一句 |
| 审批服务（approval） | 工具执行的**门禁**：这个动作能不能跑 |
| **deliver** | **产物审批**：把成果交出来，等用户决定后才行动 |

三者正交。deliver 面向"行动前先交方案"，并可**附带文件**。

## 用法

```
deliver(kind="plan",     title="给项目加 CI", content="...")
deliver(kind="document", title="接口文档",    content="...", attachments=["docs/api.md"])
deliver(kind="review",   title="本次变更",    content="...", attachments=["src/x.go"])
```

- `kind`：`plan` / `document` / `review`
- `content`：正文（Markdown）
- `attachments`：可选文件路径列表（相对工作目录或绝对路径）

## 交互

CLI 会渲染交付物：

```
========== 交付物 [plan] 给项目加 CI ==========
<正文>
---- 附件 ----
  [1] docs/api.md (1234 bytes)
======================================
```

- 有附件时，可先输入编号查看内容，回车进入审批。
- 附件过大会标注"太大，需按路径查看"；读取失败会给出原因。

审批选项：

```
[?] 批准？[y=approve / r=revise / n=reject]
      y 批准 → 模型继续执行
      r 修改 → 输入修改意见，模型改后重新提交
      n 拒绝 → 输入原因，模型停止
```

## 暂停语义（会话状态机）

`pkg/session` 有显式状态机：

```
active ──deliver 未获批──▶ awaiting_approval ──新输入──▶ active
```

- 未获批准时，deliver 工具发出 `delivery/pending` 事件。
- Agent 循环捕获后**立即结束本轮**（返回 `Paused=true`），并写入 `awaiting_approval`。
- 状态随事件日志持久化，`resume` 时恢复。

模型**不会**在未获批时继续执行——工具描述明确要求，循环在机制上也强制停止。

## 无交互外壳

WebUI / headless 无人工审批时，按配置：

| `deliver_auto_approve` | 行为 |
|---|---|
| `false`（默认） | 无审批人 → 交付被判为拒绝，模型停止 |
| `true` | 无审批人 → 自动视为批准（适合无人值守但信任的场景） |

## 配置

| 字段 | 作用 | 默认 |
|---|---|---|
| `deliver_auto_approve` | 无审批人时是否自动批准 | `false` |

## 示例

```bash
coren run "先给我一份重构计划，用 deliver 提交，我确认后再动手"
```

模型会提交计划并暂停；你批准后才继续。
