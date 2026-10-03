# 安全、审批与可靠性

Coren 用一层统一的策略管线守护工具调用，并提供超时、重试与结构化日志。
这些默认开启但可配置，目标是**放心长期运行**。

## 授权等级

`authz` 决定代理能做什么：只读 / 授信 / 完全控制。

| 等级 | 允许 | 说明 |
|---|---|---|
| `readonly` | 只读（读文件、列目录、资源） | 写与执行一律拒绝 |
| `trusted`（默认） | 写 + 执行 | 危险操作需确认 |
| `full` | 全部 | 跳过确认；**但 deny 规则仍然生效** |

配置：`authz` 或环境变量 `COREN_AUTHZ`。

工具按行为分类：`read` / `write` / `execute` / `network`。等级是外层硬门禁；
是否弹确认由审批服务决定。

## 审批服务

独立的 `approval` 服务，与工具解耦。带交互外壳（CLI）时向用户提问；无交互外壳时，
被等级允许的操作直接放行（拦截由风险规则负责）。

CLI 审批提示：

```
[?] run_shell: rm file.txt
  reason: file deletion requires confirmation
  allow? [y/N]
```

## 风险规则

`pkg/risk` 用正则匹配工具参数（含 shell 命令文本），命中后：

- `confirm` → 需用户确认
- `deny` → 直接拒绝（**任何等级都不放行**）

内置规则（可用 `disable_default_risk_rules` 关闭）：

| 规则 | 级别 | 匹配 |
|---|---|---|
| rm-recursive | deny | `rm -rf ...` |
| destructive-rm | confirm | 任何 `rm` |
| privilege-escalation | confirm | `sudo` / `su` / `doas` |
| pipe-to-shell | confirm | `curl ... \| sh` |
| force-push | confirm | `git push --force` |
| disk-write | deny | `dd` / `mkfs` / `fdisk` |
| system-control | deny | `shutdown` / `reboot` |
| history-rewrite | confirm | `git reset --hard` / `git clean -f` |

自定义规则（`risk_rules`）：

```json
{
  "risk_rules": [
    { "name": "deploy", "severity": "confirm", "tools": ["run_shell"],
      "pattern": "\\b(kubectl|terraform)\\b", "reason": "基础设施变更需复核" }
  ]
}
```

`tools` 省略时对所有工具生效；`severity` 为 `confirm` 或 `deny`。

## 文件更改审查

`write_file` 在 `trusted` 及以下等级会先计算 diff 并请求确认：

- 新建：`create path (N bytes)`
- 修改：`modify path (+A/-D lines)`
- 内容相同：不触发审查

`full` 等级跳过审查；`deny` 规则仍生效。

## 工具超时

每次工具调用有超时（`tool_timeout` 秒，默认 120s），超时返回明确错误，
不会挂起整个回合。`run_shell` 另有自己的命令超时。

```json
{ "tool_timeout": 120 }
```

## 模型重试

连接/传输失败（超时、断连）与瞬时 HTTP（429、5xx）会**自动重试**，指数退避，
尊重 `Retry-After`。终止性错误（4xx 非 429）不重试。

- 默认：首次 + 最多 3 次重试
- 配置：`max_retries`（重试次数，0 用默认）
- 上下文取消立即停止重试

> 注意：模型**业务**错误（参数错、命令失败）不会自动重试——重试无用。

## 软沙箱

当前为**软沙箱**：文件工具的 `Root` 限制在工作目录内，越界路径（`..`、绝对路径）
被拒绝；shell 命令受风险规则约束。真正的容器级隔离留作可选插件。

## 结构化日志

`logging` 插件（如 `web-verbose` profile）输出每个事件一行：

```
coren: 2026/10/03 17:34:07 request model=agnes-2.5-flash messages=1 tools=14
coren: 2026/10/03 17:34:07 tool start name=current_time
coren: 2026/10/03 17:34:07 tool done name=current_time status=ok duration=0s
coren: 2026/10/03 17:34:08 usage input=1234 output=56
```

涵盖模型请求、工具起止与耗时、token 用量，可重建一次会话的时间线。

## 配置汇总

| 字段 | 作用 | 默认 |
|---|---|---|
| `authz` | 授权等级 `readonly`/`trusted`/`full` | `trusted` |
| `risk_rules` | 追加/覆盖风险规则 | 内置规则 |
| `disable_default_risk_rules` | 关闭内置规则 | false |
| `tool_timeout` | 工具超时（秒） | 120 |
| `max_retries` | 模型传输重试次数 | 3 |
