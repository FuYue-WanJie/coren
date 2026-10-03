# Coren 开发计划

> 配套文档：[架构设计](./architecture.md)。
> 决定：**编译期插件** + **append-only 事件日志** + **内核重构优先**。

## 原则

- **渐进重构，不推倒重来**：每一步现有 CLI 都能跑、测试都过。
- **每阶段可交付**：阶段结束有一个可运行、可验证的成果。
- **先内核后外壳**：先把 Context/Plugin/事件立起来，再迁移 llm/tools/agent/server。

## 阶段总览

| 阶段 | 目标 | 产出 | 验收 |
|---|---|---|---|
| P0 | 框架内核 | `pkg/coren`：Context、Plugin、事件总线、撤销 | 单元测试覆盖服务定位、依赖排序、卸载回滚、四种事件分派 |
| P1 | 能力服务化 | `tools`/`llm`/`sessions` 服务 + 现有实现改为插件 | 现有工具与 provider 通过服务调用，测试全过 |
| P2 | Agent 循环服务化 | `agent-loop` 服务 + `agent/*` 事件钩子 | 工具循环走事件管线，可拦截 pre-step/request/tools |
| P3 | 会话事件日志 | append-only 日志 + 重放/派生 | 对话可从日志重建，支持 resume |
| P4 | 外壳插件化 | `cli`/`webapp` 插件 + profile 装配 | profile yaml 决定启动内容，单二进制 |
| P5 | 打磨 | 配置/日志/错误/文档 | 端到端可用 |

## 进度

- **P0 完成**：`pkg/coren` 内核，16 项测试。
- **P1 完成**：`pkg/llm`、`pkg/tools`、`pkg/session` 契约；`pkg/plugins/{openaillm,builtintools,memsession}` 实现；`pkg/agent` 改为经内核解析服务；`internal/app` 用 profile 式插件装配；删除旧的 `internal/agent|provider|tool`。
- **P2 完成**：`pkg/agents`（agents 服务 + `Loop` 契约）；`pkg/plugins/coreagent`（`ProviderPlugin` 提供 agents 服务、`LoopPlugin` 注册默认 `agent-loop`）；循环接入全部钩子——`agent/pre-step`（可改写/拒绝输入）、`agent/request`（改写请求）、`tools/pre-execute`、`tools/post-execute`、`agent/turn-stopping`（步数耗尽时触发）。当前共 42 项测试。
- **P3 完成**：会话改为 append-only 事件日志。`pkg/session` 新增 `Event`/`EventType`、`DeriveMessages`（从日志投影模型历史）、`Fork`（按序号截断）、`JSONLPersister`（JSONL 落盘、崩溃尾部自动丢弃）。循环写入 `turn/start`/`user/message`/`assistant/message`/`tool/call`/`tool/result`/`step/end`/`turn/end`。配置新增 `session_dir`（默认 `.coren/sessions`，`COREN_NO_PERSIST=1` 关闭）。重启可 resume。
- **P4 完成**：外壳插件化。`pkg/shell` 契约；`pkg/plugins/shellweb`（HTTP+SSE+内嵌 WebUI）、`pkg/plugins/shellcli`（终端，含 one-shot `Prompt`）；`internal/profile` 定义 `core`/`web`/`cli`/`headless` 四个内置 profile；`internal/app` 按 profile 名解析插件清单装配；`cmd/coren` 变为启动器（`serve`=web、`run`=cli、`coren profile` 列表）。删除旧的 `internal/server`。
- **当前共 62 项测试。**
- **P5 完成**：示例插件 `examples/clockplugin`（`current_time` 工具）；`pkg/plugins/logging`（事件追踪，纯观察者）；`internal/profile.Registry`（外部插件工厂）；`shell.Prompter` 可选接口让 one-shot 分派与具体外壳解耦；插件开发指南 `docs/plugin-guide.md`；README/示例配置更新。真实模型（agnes-2.5-flash）端到端验证：多轮工具调用、示例插件、日志、会话持久化全部协同工作。

**全部阶段（P0–P5）完成。**

---

## P0 · 框架内核

**目标**：实现"一切皆插件"的最小骨架，不含任何具体能力。

任务：
1. `pkg/coren/context.go`：`Context` 接口 + 服务仓库（`Provide`/`Service`）+ 父子链 + `Scope`。
2. `pkg/coren/events.go`：事件总线，四种分派 `Emit`/`Waterfall`/`Serial`/`Bail`；事件名常量。
3. `pkg/coren/plugin.go`：`Plugin` 接口（`ID`/`Inject`/`Apply`）+ 可选 `Start`/`Stop`。
4. `pkg/coren/kernel.go`：装配器——按 `Inject` 拓扑排序、逐个 Apply、登记撤销、逆序卸载。
5. `pkg/coren/effect.go`：`Effect`/`On` 返回撤销函数，统一由内核管理。

验收测试：
- 服务注册后可通过键找到；重复注册覆盖并旧值可回滚。
- 依赖未满足的插件不 Apply；满足后才 Apply。
- 卸载内核后所有注册（服务+监听器）回滚干净。
- 四种事件分派语义正确（观察/包裹/有序/短路）。
- 循环依赖被检测并报错。

## P1 · 能力服务化

**目标**：把现有 `tool`、`provider`、会话状态迁到服务。

任务：
1. `tools` 服务：把 `internal/tool.Registry` 变成服务的实现，暴露 `Register`/`Specs`/`Run`。
2. `llm` 服务：把 `provider.Provider` 注册表化（按名找适配器），`openai` 变成默认适配器。
3. `sessions` 服务：先做内存实现（P3 再换成事件日志）。
4. 每个内置工具（read/write/shell/http）变成独立插件，向 `tools` 服务注册。
5. `internal/app` 改为通过内核装配。

验收：现有全部测试迁移后仍通过；CLI 行为不变。

## P2 · Agent 循环服务化

**目标**：`Agent` 变成 `agent-loop` 服务的默认实现，循环节点插入事件。

任务：
1. `agents` 服务：Agent 实例注册与生命周期事件。
2. `agent-loop` 服务：现有 `Agent.run` 迁移，去掉对 `provider`/`tool` 的直接依赖，改服务查找。
3. 插入 waterfall 事件：`agent/pre-step`、`agent/request`、`tools/pre-execute`、`tools/post-execute`、`llm/stream`。
4. `agent/turn-stopping`（serial）供策略使用。

验收：
- 循环通过 `tools`/`llm` 服务执行。
- 可用一个测试插件在 `tools/pre-execute` 拦截并改写参数。
- 可用测试插件在 `agent/pre-step` 拒绝某输入、不产生 step。

## P3 · 会话事件日志

**目标**：append-only 日志成为上下文唯一真相来源。

任务：
1. 定义 `SessionEvent`（`turn/start`、`user/message`、`assistant/message`、`tool/call`、`tool/result`、`step/end`、`turn/end`）。
2. `deriveMessages()`：从日志投影出模型可见历史。
3. 持久化：JSONL（后续可加压缩/迁移版本）。
4. resume / fork 基础能力。

验收：
- 一次对话后从日志能重建完整消息历史。
- 落盘后重启可 resume。
- 日志是不可变追加，崩溃尾部可修复。

## P4 · 外壳插件化

**目标**：CLI 与 WebUI 都变成插件，由 profile 组合。

任务：
1. `cmd/coren` 变成启动器：读 profile yaml → 装配内核 → 运行。
2. `plugins/cli`：终端交互外壳（现有 `run` 逻辑）。
3. `plugins/webapp`：HTTP + SSE + 内嵌 WebUI（现有 `server`）。
4. `profiles/web.yaml`、`cli.yaml`、`headless.yaml`。
5. 配置分层与 profile 合并。

验收：
- `coren --profile web` / `--profile cli` 启动不同组合。
- 增删一个插件只改 yaml，不改代码。

## P5 · 打磨

- 日志与可观测性（结构化日志、turn 追踪）。
- 错误模型与重试策略。
- 文档：插件开发指南、profile 参考、事件目录。
- 端到端冒烟 + 一个示例插件。

---

## 里程碑

- **M1 = P0+P1**：内核可用，现有能力服务化，行为不变。
- **M2 = P2+P3**：循环可拦截、会话可持久化重放。
- **M3 = P4+P5**：profile 装配、双外壳、可交付自用。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 事件分派语义写错 | 每个模式先写测试再接入 |
| 重构破坏现有 CLI | 每阶段末跑全套测试 + 手动冒烟 |
| 服务键字符串易拼错 | 集中定义常量 + 泛型取值辅助 |
| 会话日志过早复杂化 | P3 先做最小 JSONL，不加压缩/迁移 |
| 范围膨胀 | 严格按阶段走，P5 之外的需求记 backlog |
