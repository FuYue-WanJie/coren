# 插件开发指南

Coren 的内核没有特权核心：模型适配器、工具注册表、会话日志、Agent 循环本身都是插件。
扩展 Coren 的方式是**写一个插件挂上去**，而不是修改框架。

## 一个插件是什么

```go
type Plugin interface {
    ID() string
    Inject() []string          // 依赖的服务键；内核保证就绪后才 Apply
    Apply(ctx coren.Context) error
}
```

`Apply` 里的所有注册都是**可撤销的副作用**：插件卸载时自动回滚。
因此热插拔、测试隔离都不需要额外清理逻辑。

## 最小示例

```go
type MyPlugin struct{}

func (MyPlugin) ID() string       { return "my.plugin" }
func (MyPlugin) Inject() []string { return []string{tools.Key} }

func (MyPlugin) Apply(ctx coren.Context) error {
    registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
    if !ok {
        return fmt.Errorf("my.plugin: tools service missing")
    }
    registry.Register(myTool{})
    return nil
}
```

`Spec()` + `Run()` 定义一个工具（见 `examples/clockplugin`）：
工具实现完后，模型即可通过 function calling 调用它。

## 四类扩展点

| 目标 | 做法 |
|---|---|
| 增加模型后端 | 实现 `llm.Adapter`，向 `llm` 服务 `Register` |
| 增加模型可用能力 | 实现 `tools.Tool`，向 `tools` 服务 `Register` |
| 拦截请求/工具/输入 | 订阅 `waterfall` 事件（见下） |
| 增加用户界面 | 实现 `shell.Shell`，`Provide(shell.Key, ...)` |
| 增加持久化 | 实现 `session.Persister`，用 `memsession.Plugin{Dir:...}` 或自定义 |

## 事件

事件是拦截与策略的统一入口。四种分派模式：

| 模式 | 方法 | 语义 |
|---|---|---|
| 观察 | `Emit` | 监听者按注册顺序观察，无返回值 |
| 环绕 | `Waterfall` | 中间件，调用 `next()` 继续，不调即短路 |
| 有序 | `Serial` | 按注册顺序串行，值依次传递 |
| 短路 | `Bail` | 首个返回非空决定者即停止 |

内置事件：

| 事件 | 模式 | 用途 |
|---|---|---|
| `agent/pre-step` | waterfall | 改写或**拒绝**用户输入 |
| `agent/request` | waterfall | 改写发往模型的请求 |
| `tools/pre-execute` | waterfall | 拦截/改写工具参数 |
| `tools/post-execute` | waterfall | 处理工具结果 |
| `agent/turn-stopping` | serial | 步数耗尽前的策略钩子 |
| `plugin/loaded` / `plugin/unloaded` | emit | 生命周期观察 |

拦截示例（改写工具参数）：

```go
ctx.OnWaterfall(coren.EventToolsPreExecute, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
    if call, ok := payload.(*agent.PreExecutePayload); ok {
        call.Arguments = rewrite(call.Arguments)
    }
    return next(payload)
}, false)
```

拒绝输入示例：

```go
ctx.OnWaterfall(coren.EventAgentPreStep, func(_ context.Context, payload any, _ func(any) (any, error)) (any, error) {
    decision := payload.(*agent.PreStepResult)
    decision.Accepted = false
    decision.Reason = "blocked"
    return decision, nil   // 不调用 next：短路
}, false)
```

## 服务键

框架定义的服务键集中在 `pkg/coren/plugin.go`，各能力另有自己的 `Key` 常量
（如 `llm.Key`、`tools.Key`、`session.Key`、`agents.Key`）。插件的 `Inject`
应引用这些常量，避免拼写漂移。

用 `coren.UnwrapKey[T](ctx, key)` 做类型安全取值：

```go
registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
```

## 组合：profile

profile 是"启动哪些插件"的声明，位于 `internal/profile/profile.go`：

```go
"cli-clock": {
    Name:        "cli-clock",
    Description: "Interactive terminal session with the example clock tool.",
    Plugins: []string{
        PluginLLM, PluginSessions, PluginTools, PluginAgents,
        PluginLLMOpenAI, PluginToolsBuiltin, PluginAgentLoop,
        PluginToolsClock,   // 示例插件
        PluginShellCLI,
    },
},
```

外部插件可通过 `profile.Registry` 注册工厂，在 `app.Options.Registry` 中传入：

```go
registry := profile.NewRegistry()
registry.Register("my.plugin", func(cfg any) (coren.Plugin, error) { return MyPlugin{}, nil })
app.New(ctx, cfg, app.Options{Profile: "cli", Registry: registry})
```

## 依赖

插件的 `Inject` 决定加载顺序：内核在 `Apply` 前检查所列服务是否存在，
缺依赖则 `Boot` 失败并回滚已挂载的插件。因此**不要在插件里手动排序启动**，
把依赖写进 `Inject` 即可。

## 完整示例

- `examples/clockplugin`：注册一个工具（`current_time`）。
- `pkg/plugins/logging`：纯观察者，订阅三个事件并打点，不改行为。
- `pkg/plugins/builtintools`：内置文件/shell/HTTP 工具。
- `pkg/plugins/shellweb`：把 HTTP 服务与内嵌 WebUI 挂成外壳。
