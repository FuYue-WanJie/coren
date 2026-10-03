# Coren 架构设计

> 目标：一个自用的、**一切皆插件**的 Agent 框架。CLI 与 WebUI 是同一套内核的两种外壳。
> 参考：DeepSeek Harness（Cordis 插件范式）、OpenCode（Go/服务化+SDK）、ZCode、Step Code。

## 1. 为什么是"一切皆插件"

参考项目里 DSH 把**模型适配、工具注册表、会话日志、Agent 循环本身**都做成插件，
没有任何"特权核心"需要打补丁——扩展方式是"在旁边挂载一个插件"，而不是改内核。

Coren 采用同样的内核哲学，但用 Go 的习惯表达：

- **没有特权内核**：Agent 循环、Provider、工具、会话存储、WebUI 路由，全部通过插件注册到共享上下文。
- **注册即效应（reversible effect）**：插件注册的东西（工具、provider、事件监听、路由）在插件卸载时自动撤销。热插拔、测试隔离都靠这个。
- **服务定位而非硬编码**：插件之间不互相 import 具体实现，而是通过 `ctx.Service("tools")` 这样的键找到能力。
- **显式依赖**：插件声明它需要哪些服务，框架据此决定加载顺序，而不是手工编排启动序列。

Go 没有 TS 的 declaration merging 和动态 `ctx.tools` 属性，因此用**显式的上下文键 + 泛型注册表**代替。

## 2. 核心抽象

### 2.1 Context（服务仓库）

```go
// 一个 Context 持有一组服务、事件总线和一个父子链。
type Context interface {
    Service(key string) (any, bool)          // 查服务
    Provide(key string, svc any)             // 注册服务（返回的 effect 撤销它）
    On(name string, h Handler) Unsubscribe   // 订阅事件
    Emit(name string, payload any)           // 广播
    Waterfall(name string, payload any, next Handler) (any, error) // 环绕中间件
    Effect(fn func() error) error            // 登记一个可撤销副作用
    Scope(pluginID string) Context           // 派生子上下文
}
```

要点：
- `Provide` 的第二个返回值是一个"撤销函数"，由 `Effect` 统一管理，卸载即回滚。
- `Scope` 让每个插件拥有自己的注册命名空间，一个插件的注销不影响其他插件。

### 2.2 Plugin（可挂载单元）

```go
type Plugin interface {
    ID() string
    // Inject 声明依赖的服务键；框架保证这些服务就绪后才 Apply。
    Inject() []string
    Apply(ctx Context) error
}
```

一个插件能做的事（全部是可撤销的）：
- 提供/覆盖服务（`Provide`）
- 注册工具（`tools` 服务）
- 注册模型适配器（`llm` 服务）
- 订阅/拦截事件（`On` / `Waterfall`）
- 注册 CLI 命令、WebUI 路由、配置项

### 2.3 Services（内置服务键）

| 键 | 职责 | 对应 DSH |
|---|---|---|
| `llm` | 模型适配器注册与流式调用 | `ctx.llm` |
| `tools` | 工具注册表 + 受控执行管线 | `ctx.tools` |
| `sessions` | 会话事件日志与读取 | `ctx.sessions` |
| `agents` | Agent 实例注册与生命周期事件 | `ctx.agents` |
| `agent-loop` | 默认 Agent 驱动实现 | `ctx.agentLoop` |
| `commands` | 无模型的斜杠命令分派 | `ctx.commands` |
| `fs` | 文件系统能力与策略 | `ctx.fs` |
| `shell` | 子进程执行后端 | `ctx.shell` |
| `http` | HTTP 请求能力 | — |
| `server` | HTTP/SSE 路由挂载点 | — |

### 2.4 Events（扩展点）

参考 DSH 的三种事件域，Coren 收敛为两类（Go 里不必分那么细）：

- **持久事件**：会进入会话日志、可重放。如 `message/*`、`tool/result`、`turn/end`。
- **能力事件**：用于拦截与策略，不落盘。如：
  - `agent/pre-step`（waterfall）：改写或拒绝本步输入
  - `agent/request`（waterfall）：改写发往模型的请求
  - `tools/pre-execute`（waterfall）：工具执行前拦截/改写参数
  - `tools/post-execute`（waterfall）：工具执行后处理结果
  - `llm/stream`（waterfall）：包裹流式输出

四种分派模式：`emit`（观察）、`waterfall`（环绕中间件，需 `next()`）、`serial`（有序）、`bail`（首个返回值即停）。

## 3. 分层与目录

```
Coren/
├── cmd/coren/                 # 启动器：装配 profile -> 运行
├── pkg/
│   ├── coren/                 # 框架内核（可被外部 import）
│   │   ├── context.go        # Context / 服务仓库 / 事件总线
│   │   ├── plugin.go         # Plugin 接口、生命周期
│   │   ├── kernel.go         # 内核装配、加载顺序、卸载回滚
│   │   └── events.go         # 事件名与分派模式定义
│   ├── provider/             # llm 服务的默认实现（openai chat/responses）
│   ├── agent/                # agent-loop 默认实现
│   ├── tool/                 # tools 服务 + 内置工具
│   ├── session/              # sessions 服务（事件日志）
│   ├── session/              # sessions 服务（事件日志）
│   ├── plugins/httpserver/   # http-server 插件：HTTP API + 认证（不含 UI）
│   └── plugins/shellweb/     # 浏览器外壳：只提供内嵌静态资源
├── plugins/                  # 一等公民插件（每个可独立启停）
│   ├── core/                 # 基础：llm+tools+sessions+agent-loop
│   ├── fs/                   # 文件工具
│   ├── shell/                # 子进程工具
│   ├── http/                 # HTTP 工具
│   ├── webapp/               # 浏览器外壳
│   └── cli/                  # 终端外壳
├── profiles/                 # 命名组合（web / api / headless / cli）
│   └── web.yaml, api.yaml, cli.yaml, headless.yaml
└── docs/architecture.md
```

前后端分离：`http-server` 插件独占 HTTP API（`/api/*` + 会话令牌认证），`shell.web`
只提供静态资源并在存在 http-server 时把资源挂上去。`api` profile（或 `coren serve
--no-ui`）只跑 API，供独立前端反代；`web` profile 两者都挂，仍是开箱即用的整合界面。

与 DSH 的 **profile / bundle** 概念对应：
- **profile**：一组有序插件的命名组合（`web`、`cli`、`headless`）。
- **bundle**：可分发的插件集合，上层 profile 可覆盖其配置。

## 4. 装配流程

```
启动器读取 profile（yaml）           # 有序插件清单
  → 依次加载 bundle / 插件
  → 拓扑排序（按 Inject 依赖，而非书写顺序）
  → 逐个 Apply(ctx)，登记可撤销副作用
  → 运行外壳（CLI 交互 / HTTP 服务）
  → 退出时逆序卸载（effect 回滚）
```

配置分层沿用现有设计：`默认 < ./coren.json < 用户配置 < 环境变量 < 命令行列`。

## 5. 数据流（一次对话）

```
turn/start
  claim 用户输入
  组装系统提示 + 工具 schema（来自 tools/llm 服务）
  → agent/pre-step       (waterfall) 可拒绝/改写输入
    step/start
    → agent/request      (waterfall) 可改写请求
    → llm/stream         (waterfall) 流式产出
      text/delta*        → 推给外壳（CLI 打印 / SSE）
      tool/call*
        → tools/pre-execute   (waterfall) 拦截/改写参数
        → tools/execute       调用插件注册的工具
        → tools/post-execute  (waterfall) 处理结果
        → tool/result*        写入会话日志
    step/end
    还有未完成的工具调用 → 回到 step/start
  turn/end
```

"模型可见即已记录"：所有进入模型上下文的内容都必须能从事务日志重建（沿用 DSH 的原则），会话日志是上下文的唯一真相来源。

## 6. 迁移路径（从现有 Coren 到插件化）

现有代码已经具备雏形（Provider 接口、Tool 接口+注册表、Agent 循环、Server）。
插件化是**渐进重构**，不推倒重来：

1. **引入 Context + Plugin 抽象**，把现有 `Tool` 接口注册表包装成 `tools` 服务。
2. **把 `Agent` 变为 `agent-loop` 服务的默认实现**，循环里的直连调用改为服务查找。
3. **给关键节点插入事件**：`agent/pre-step`、`agent/request`、`tools/*`、`llm/stream`。
4. **Provider 变为 `llm` 服务**，`Internal/provider/openai` 变成默认插件。
5. **Server 变为 `server` 服务 + `webapp` 插件**，路由通过服务挂载，不再硬编码在 server.go。
6. **引入 profile yaml**，把"启动哪套东西"从代码变成配置。

每一步都可独立跑通测试，不破坏 CLI。

## 7. 关键设计取舍

| 议题 | 决定 | 理由 |
|---|---|---|
| 插件语言 | 先只支持 Go 插件（编译期） | 自用优先，避免进程隔离与 RPC 复杂度；后续可加外置进程插件 |
| 热重载 | 服务级可撤销注册，但不做进程内热替换 | Go 静态编译，热重载收益低、风险高 |
| 服务键 | 字符串键 + 泛型取用辅助 | Go 无 declaration merging，字符串键最直接 |
| 事件模式 | emit / waterfall / serial / bail | 覆盖观察、拦截、有序、短路四类需求 |
| 会话存储 | 事件日志（append-only），可重放 | 对齐 DSH，支持 fork/resume/审计 |
| 外壳 | CLI 与 Web 都是插件 | "一切皆插件"落到最外层 |

## 8. 与参考项目的对应

| Coren 概念 | DSH (Cordis) | OpenCode | 说明 |
|---|---|---|---|
| `Context` + `Provide` | `ctx.<key>` + Service | 依赖注入层 | 服务定位 |
| `Plugin.Apply` | `apply(ctx)` | plugin loader | 挂载单元 |
| `Inject` | `inject` | — | 依赖声明决定加载序 |
| 持久事件 | SessionEvent | session log | 可重放 |
| 能力事件 | agent/* tools/* | hook | 拦截与策略 |
| profile/bundle | profile/bundle | config | 组合与分发 |
| `agent-loop` | `core/agent-loop` | session engine | 默认驱动可替换 |
