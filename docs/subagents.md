# 子代理（Subagents）

子代理让一个代理把任务**委派**给子代理。它是一项可选能力，不是 Agent 循环的一部分，
因此独立成 `subagents` 服务；与 `llm` 适配器一样，**多个实现按名并存**，
内置一个进程内实现，未来可加外置运行时。

## 模型如何使用

框架注册一个内置工具：

| 工具 | 作用 |
|---|---|
| `task` | 把一个聚焦、自包含的任务委派给子代理，只返回子代理的最终答案 |

模型判断某个子问题适合独立处理时，就调用 `task`，子代理在**隔离的会话**里完成，
其过程不会污染父代理的上下文。

## 隔离语义

- 子代理使用**全新的会话**（独立消息历史），看不到父代理的历史。
- 子代理复用同一套 `llm` / `tools` 服务与内核，因此工具、技能对子代理同样可用。
- 子代理返回后，只有最终文本进入父代理的对话。

## 深度限制

委派深度通过 `context` 传递，嵌套委派会累加，超过上限即拒绝：

```go
// 工具侧：读取当前深度并 +1
depth := depthFromContext(ctx) + 1
// 提供者侧：超过 MaxDepth 直接报错
if req.Depth > maxDepth { return error }
```

默认上限 2 层。设置 `Config.MaxDepth` 可调整。

## 架构位置

```
pkg/subagents/           # 契约：Request/Result/Provider/Service + Registry
pkg/plugins/subagents/   # 内置实现：
    ├── ProviderPlugin        # 提供 subagents 服务
    ├── InProcessPlugin       # 注册 in-process 提供者
    └── ToolPlugin            # 注册 task 委派工具
```

`Service` 是提供者注册表，`Default()` 返回首个注册的实现。新增提供者只需实现：

```go
type Provider interface {
    Name() string
    Start(ctx context.Context, req Request) (Result, error)
}
```

### 示例：注册一个自定义提供者

```go
func (MyProviderPlugin) Inject() []string { return []string{subagents.Key} }

func (MyProviderPlugin) Apply(ctx coren.Context) error {
    registry, _ := coren.UnwrapKey[subagents.Service](ctx, subagents.Key)
    registry.Register(myProvider{})
    return nil
}
```

## 配置

| 位置 | 说明 |
|---|---|
| `subagents` 服务键 | 提供者注册表 |
| `in-process` 提供者 | 内置的进程内实现 |
| `task` 工具 | 模型可见的委派入口 |
| `Config.MaxDepth` | 委派深度上限（默认 2） |
| `Config.MaxSteps` | 子代理步数上限 |

## 与参考实现的对应

- DSH：`ctx.subagents` 提供者注册表 + `tool-subagent` 消费者 + 深度/工具过滤能力位。
- Step-Code：`step-subagent` 特性 + 独立的 `subagent` 目录。
- Coren 首版聚焦最常用路径：进程内、隔离子会话、一次性委派，其余留作后续扩展点。
