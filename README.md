# Coren

自用的 Agent 框架，Go 实现。提供可复用的库、命令行工具和带 WebUI 的 HTTP 服务。

## 特性（首版）

- **Provider 抽象**：同时支持 OpenAI Chat Completions API 与 Responses API，任意 OpenAI 兼容端点（设置 `COREN_BASE_URL` 即可）
- **Agent 循环**：模型 → 工具调用 → 结果回填 → 继续，直到产出最终回答；步数有上限保护
- **流式输出**：文本增量实时推送（CLI 直接打印，WebUI 通过 SSE）
- **会话**：多轮上下文管理，HTTP 端按 `session_id` 隔离
- **工具**：内置文件读写、shell 执行、HTTP 请求，并提供工具注册接口
- **Skills（Agent Skills）**：目录内 `SKILL.md` 定义能力包，模型按需加载（渐进披露）；插件也可直接注册 skill
- **子代理**：`task` 工具把任务委派给进程内隔离子会话，带回最终答案，支持委派深度限制
- **提问工具**：`ask_user` 让模型在信息不足时向用户提问；CLI 外壳实现交互，无交互外壳明确降级
- **MCP**：连接远程 MCP 服务器（Streamable HTTP + SSE），把其 tools/resources/prompts 接入本地
- **提示词 / 规则 / 记忆**：可定制 system prompt；自动注入 `AGENTS.md`/`CLAUDE.md` 等规则文件；模型可用 `remember`/`recall` 维护跨会话记忆；`todo` 工具维护 `TODO.md` 待办清单（启动时注入）
- **Token 优化**：提示词缓存（`prompt_cache_key`）、会话压缩、精简指南、规则与记忆预注入
- **多模态查看**：`read_file` 读到图片/音频/视频时，模型支持该模态则把内容送入模型，否则返回类型与原因
- **模型信息**：`coren models --info <id>` 展示能力参数（上下文/工具/推理/模态/价格）；优先级为「配置 > provider API 字段 > models.dev 目录快照」
- **安全与可靠性**：授权等级（只读/授信/完全）、风险命令拦截、审批确认、文件更改审查、工具超时、模型重试、结构化日志
- **交付审批**：`deliver` 工具提交计划/文档/审查（可附文件）并暂停，获批后才继续（Plan 模式）
- **CLI**：`coren run`（单次或交互）、`coren serve`（API + WebUI）
- **WebUI**：Go `embed` 内嵌的轻量聊天前端，单二进制分发；首次启动可提取到磁盘供修改，内置界面更新时提示是否替换

## 构建

```bash
go build -o coren ./cmd/coren
go test ./...
```

## 使用

```bash
# 首次：生成配置并填入 base_url / api_key / model
./coren config init

# 启动 Web 界面（HTTP API + 内嵌 WebUI）
./coren serve
# 打开 http://127.0.0.1:8787

# 终端交互或单次提问
./coren run
./coren run "用一句话解释 goroutine"

# 查看可用 profile
./coren profile
```

## Profile（组合）

profile 决定启动哪些插件，是配置而非代码：

| profile | 内容 |
|---|---|
| `core` | 模型 + 工具 + 会话 + 循环，无外壳 |
| `web` | 上述 + HTTP API 与 WebUI |
| `cli` | 上述 + 终端外壳 |
| `headless` | 无外壳，供嵌入或调用方驱动 |
| `cli-clock` | cli + 示例 `current_time` 工具 |
| `web-verbose` | web + 事件追踪日志 |

```bash
./coren --profile cli-clock run "现在几点？"
```

## 配置

设置按优先级叠加（从低到高）：**默认值 < `./coren.json` < 用户配置 < 环境变量**。
配置好后启动无需再带参数：

```bash
./coren config init    # 生成 ./coren.json，然后编辑 api_key / model / base_url
./coren run            # 直接运行，读取配置
./coren config show    # 查看生效配置（密钥打码）
./coren config path    # 查看各级配置路径
```

`coren.json` 示例：

```json
{
  "api": "chat",
  "base_url": "http://localhost:1234/v1",
  "api_key": "sk-...",
  "model": "my-model",
  "work_dir": "/path/to/workdir",
  "addr": "127.0.0.1:8787",
  "temperature": 0.7,
  "max_tokens": 2048,
  "max_steps": 12
}
```

配置查找顺序：项目 `./coren.json`、用户配置（`~/.config/coren/coren.json`，或 `COREN_CONFIG` 指定）。

环境变量（优先级最高，用于临时覆盖）：

| 环境变量 | 说明 |
|---|---|
| `COREN_API` | Provider 类型：`chat` 或 `responses` |
| `COREN_BASE_URL` | OpenAI 兼容端点根地址 |
| `COREN_API_KEY` | Bearer Token |
| `COREN_MODEL` | 模型名 |
| `COREN_SYSTEM` | 系统提示词 |
| `COREN_WORKDIR` | 文件/shell 工具的工作目录 |
| `COREN_ADDR` | 服务监听地址 |
| `COREN_TEMPERATURE` | 采样温度 |
| `COREN_MAX_TOKENS` | 最大输出 token 数 |
| `COREN_MAX_STEPS` | 工具调用步数上限 |
| `COREN_CONFIG` | 指定额外配置文件路径 |

## HTTP API

- `GET /api/health` — 健康检查
- `POST /api/chat` — 流式对话（SSE）
- `GET /api/sessions` — 会话列表（摘要：id / title / turns / message_count / updated_at）
- `POST /api/sessions` — 新建会话，返回 id（新会话延迟创建：发出第一条消息前才建）
- `GET /api/sessions/{id}` — 该会话的可渲染历史
- `PATCH /api/sessions/{id}` — 重命名（追加 `session/meta` 事件）
- `DELETE /api/sessions/{id}` — 软删除（改名到回收区，保留 24 小时）
- `GET /api/webui/status` — 提取的 WebUI 是否为旧版
- `POST /api/webui/update` — 用内置版本覆盖磁盘 WebUI（先备份）

`POST /api/chat` 请求体：

```json
{ "session_id": "optional", "message": "你好" }
```

响应为 `text/event-stream`，事件类型：`text`、`reasoning`、`tool_call`、`tool_result`、`rejected`、`done`、`error`。

## 目录结构

```
Coren/
├── cmd/coren/                # 启动器：读 profile -> 装配内核 -> 运行外壳
├── internal/
│   ├── app/                 # 内核装配：profile + 配置 -> 可用应用
│   ├── config/              # 环境与文件配置
│   └── profile/             # profile 定义与外部插件注册表
├── examples/                # 示例插件（clockplugin）
├── pkg/
│   ├── coren/                # 插件内核（Context / Plugin / 事件总线 / 撤销）
│   ├── llm/                 # 模型契约 + llm 服务
│   ├── tools/               # 工具契约 + tools 服务
│   ├── skills/              # Agent Skills 契约 + 目录发现
│   ├── subagents/           # 子代理契约 + 提供者注册表
│   ├── ask/                 # 人机问答契约（Asker）
│   ├── authz/               # 授权等级与工具分类
│   ├── approval/            # 审批服务
│   ├── risk/                # 风险规则引擎
│   ├── mcp/                 # MCP 客户端（JSON-RPC + 传输 + 原语）
│   ├── ctxfiles/           # 项目规则文件发现与加载
│   ├── prompt/             # 系统提示词组装
│   ├── memory/             # 项目记忆存储
│   ├── todo/               # 项目待办清单存储
│   ├── delivery/           # 交付物审批契约
│   ├── session/             # 会话契约 + append-only 事件日志
│   ├── agents/              # agents 服务 + Loop 契约
│   ├── shell/               # 外壳契约
│   ├── agent/               # Agent 循环（经内核解析服务）
│   └── plugins/             # 一等公民插件
│       ├── openaillm/       # OpenAI Chat / Responses 适配器
│       ├── builtintools/    # 文件 / shell / HTTP 工具
│       ├── memsession/      # 会话存储（内存或 JSONL）
│       ├── coreagent/       # agents 服务 + 默认 agent-loop
│       ├── skills/          # skills 服务 + list_skills/use_skill 工具
│       ├── subagents/       # subagents 服务 + task 委派工具
│       ├── ask/             # ask_user 提问工具
│       ├── mcp/             # 远程 MCP 服务器接入
│       ├── guard/           # 授权/风险/审批/文件审查拦截
│       ├── memory/          # remember/recall 记忆工具
│       ├── todo/            # todo 待办清单工具
│       ├── deliver/         # deliver 交付审批工具
│       ├── shellweb/        # HTTP API + 内嵌 WebUI
│       ├── shellcli/        # 终端外壳
│       └── logging/         # 事件追踪
└── docs/                    # architecture / roadmap / plugin-guide
```

插件开发见 `docs/plugin-guide.md`，Skills 见 `docs/skills.md`，子代理见 `docs/subagents.md`，MCP 见 `docs/mcp.md`，提示词与 Token 优化见 `docs/prompting.md`，多模态见 `docs/multimodal.md`，安全与可靠性见 `docs/security.md`，交付审批见 `docs/delivery.md`。

开发进度与交接见 `当前工作区 的 /.monkeycode/docs/coren-handoff.md`。

## 插件模型

一切皆插件：内核没有特权核心，能力通过服务注册到 `coren.Context`，
扩展方式是挂载一个新插件，注册项在卸载时自动撤销。

> 注意：这是**架构层面**的插件化。当前插件仍在编译期装配，新增 Go 插件需要重编
> 主程序。详见下方「一切皆插件的现状与边界」。

```go
k := coren.NewKernel(ctx)
k.Boot(
    openaillm.ProviderPlugin{},                         // 提供 llm 服务
    openaillm.Plugin{Config: cfg},                      // 注册适配器
    builtintools.ProviderPlugin{},                      // 提供 tools 服务
    builtintools.Plugin{WorkDir: "."},                  // 注册内置工具
    memsession.Plugin{},                                // 提供 sessions 服务
)
```

拦截一个请求只需监听事件，无需改动循环：

```go
ctx.OnWaterfall(coren.EventToolsPreExecute, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
    if p, ok := payload.(*agent.PreExecutePayload); ok {
        p.Arguments = rewrite(p.Arguments)
    }
    return next(payload)
}, false)
```

详见 `docs/architecture.md` 与 `docs/roadmap.md`。

## 「一切皆插件」的现状与边界

「一切皆插件」是设计目标，当前实现只完成了**架构上**的一半：内核没有特权核心，
所有能力（模型、工具、会话、循环、外壳）都以插件形式注册到 `coren.Context`，
拦截行为靠监听事件完成，注册项在卸载时自动撤销。**代码组织层面**这一点是成立的。

尚未实现的是**运行时的插件加载**。诚实地说：

- **加一个新的 Go 插件，需要重新编译整个二进制。** 插件的工厂函数在
  `internal/app` 里以 `switch` 硬编码注册（`buildPlugin`），编译器在构建期把插件
  符号静态链接进 `coren`。当前没有「只编译插件、不动主程序」的路径。
- **没有进程内动态加载。** Go 的 `plugin` 包（`.so`）只支持 Linux/macOS，
  且要求主程序与插件的 Go 版本、依赖、类型完全一致，实际很少使用；Coren 未采用。
- **没有热插拔 / 热重载。** 插件集在启动时确定，运行中无法增删。
- **profile 是编译期组合。** 换一套插件组合意味着重新构建（见上方 Profile 表）。

**哪些扩展点已经是运行时的**（改配置或放文件即可，无需重编）：

| 扩展点 | 方式 | 是否需重编 |
|---|---|---|
| Skills | 目录内 `SKILL.md` | 否 |
| MCP server | `coren.json` 的 `mcp` | 否 |
| 记忆 / 待办 | `MEMORY.md` / `TODO.md` | 否 |
| 项目规则 | `AGENTS.md` / `CLAUDE.md` | 否 |
| 提示词 / 模型能力 | 配置项 | 否 |
| 外部进程插件 | 见分支 `radical` 的 `plugin-host` | 否（该特性尚未并入主线） |

**这意味着**：日常改行为、加知识、接 MCP、调模型，不需要重编；
写一个**全新的 Go 工具或改动 agent 内部行为**，目前必须重编主程序。

若要做真正的运行时插件，Go 生态里可行的方向是**进程隔离**（子进程 + 协议，
参考 `radical` 分支的 `plugin-host` 实验：NDJSON + JSON-RPC），代价是每次调用有一次
进程间往返，且能力面受协议限制。这是一个明确但未落地的方向。

## 扩展

实现 `tools.Tool` 并注册即可新增工具：

```go
registry := tools.NewRegistry()
registry.Register(myTool{}) // Spec() + Run()
```

实现 `llm.Adapter` 可接入新的模型后端。注意：以上都需要把插件编译进主程序。
