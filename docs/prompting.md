# 提示词、规则、记忆与 Token 优化

Coren 的目标之一是用更少的 Token 完成任务：既靠模型能力，也靠框架减少
无谓的往返、重复解释和重复发现。这一页说明相关机制。

## 系统提示词组装

系统提示词按固定顺序组装，保证前缀稳定（利于提示词缓存）：

```
身份(identity) → 指南(guidelines) → 项目上下文(规则文件) → 技能 → 追加内容
```

可配置项（`coren.json`）：

| 字段 | 作用 |
|---|---|
| `system` | 默认身份段落 |
| `custom_system_prompt` | 完全替换身份段落 |
| `guidelines` | 追加的指南条目（列表） |
| `append_system_prompt` | 追加到提示词末尾的自由文本（列表） |

内置了一组**精简指南**（优先行动、先读后改、简短作答、并行调用工具），
本身就是为了减少无效轮次。

## 全局规则 / 规范文件

启动时从工作目录**向上**查找规则文件，并支持固定路径，自动注入 system prompt：

- 向上查找候选：`AGENTS.override.md` → `AGENTS.md` → `AGENTS.MD` → `CLAUDE.md` → `CLAUDE.MD`
- 固定路径：`.coren/rules.md`（以及 `context_files.fixed_paths` 配置的路径）
- 同目录下 override 覆盖 base；就近优先

配置：

```json
{
  "context_files": {
    "disabled": false,
    "fixed_paths": [".coren/rules.md", "docs/conventions.md"]
  }
}
```

这样模型**开局就知道项目约定**，不必用工具读文件去发现——直接省 Token。

## 记忆（MEMORY.md）

模型可通过工具把稳定事实写入项目记忆，跨会话保留：

| 工具 | 作用 |
|---|---|
| `remember` | 追加一条记忆（可选标题分组） |
| `recall` | 读回完整记忆 |

记忆文件在启动时注入 system prompt，所以下次会话模型已知道这些事实，
无需重新询问或推导。

配置：

| 字段 | 作用 |
|---|---|
| `memory_path` | 记忆文件路径；空则用工作目录的 `MEMORY.md`；`-` 关闭 |

## Token 优化机制

| 机制 | 说明 | 配置 |
|---|---|---|
| **提示词缓存** | 请求携带 `prompt_cache_key`（=会话 id）；长保留发送 `prompt_cache_retention: 24h`。稳定前缀命中缓存后，重复部分按更低成本计费 | `cache_retention`: `short`(默认) / `long` / `none` |
| **推理等级** | 控制模型思考深度；自动挡依据模型能力决定 | `reasoning`: `auto`(默认) / `off` / `minimal` / `low` / `medium` / `high` |
| **会话压缩** | 投影消息超过阈值时，把较早历史摘要为一条 `session/compaction` 事件，替代之，约束上下文增长 | `compact_after`: 整数（0=关） |
| **精简指南** | 内置提示词鼓励直接行动、并行工具、简短作答，减少无谓轮次 | 默认启用 |
| **规则/记忆预注入** | 开局即知约定与事实，省去发现与重复解释 | 见上 |

### 提示词缓存如何生效

缓存命中取决于请求前缀是否稳定。Coren 的系统提示词顺序固定，工具列表按名排序，
因此同一会话内连续请求的同前缀部分可被缓存。`cache_retention=long` 适合长会话。

### 推理等级

`reasoning` 控制模型思考深度：

| 值 | 行为 |
|---|---|
| `auto`（默认） | 模型已知支持推理时用 `medium`；不支持或未知时不指定，交由服务决定 |
| `off` | 不发送任何推理参数（也接受 `none`/`disabled`） |
| `minimal` / `low` / `medium` / `high` | 发送对应 `reasoning_effort`（Responses API 为 `reasoning.effort`） |

显式等级优先于自动判断。未知等级会警告并回退到 `auto`。

> 注意：若两个 provider 都支持，按所选 API 使用对应参数；不同时冲突，
> Coren 依据当前 provider 形态发送。

### 会话压缩

```json
{ "compact_after": 40 }
```

当投影出的消息数超过 40 时，Coren 调用模型把较早历史压成一段摘要，
追加 `session/compaction` 事件。`DeriveMessages` 会忽略该事件之前的消息，
从而把上下文长度拉回阈值内，同时保留关键决策与文件路径。

## 模型能力参数

`coren models` 只列出模型名；`--info` 才展示**能力参数**（上下文、工具、推理、
模态、价格）。这些参数大多数 provider 的 API 不返回，因此 Coren 用三层来源合成：

**优先级（高 → 低）：配置覆盖 > 服务商 API 字段 > 目录快照**

1. **服务商 API 字段优先**：调用 `GET /models/<id>`，若返回 `context_length`、
   `max_tokens`、`architecture.input_modalities`、`supported_parameters`、
   `tool_call`、`reasoning` 等字段，则以 API 为准。API 明确给出的值（哪怕是
   `false`）都会覆盖目录。
2. **目录快照补缺**：API 未提供的字段，用 models.dev 快照补齐。快照缓存在
   `~/.config/coren/models.json`，离线可用。
3. **配置覆盖最高**：`models.<id>` 下的字段最终覆盖前面两者。

```bash
coren models --refresh            # 拉取并缓存 models.dev 快照
coren models --info gpt-4o        # 查看某模型解析后的能力
coren models --filter flash       # 只列模型名的子集
```

`--info` 会显示来源（`endpoint+catalog` / `catalog:xx` / `config`），便于判断
某个值来自哪一层。

配置覆盖单个模型：

```json
{
  "models": {
    "my-finetune": {
      "tool_call": false,
      "limit": { "context": 32000, "output": 4096 },
      "cost": { "input": 1.0, "output": 2.0 }
    }
  }
}
```

### 参数如何驱动行为

| 参数 | 用途 |
|---|---|
| `limit.context`（ContextWindow） | 未显式配 `compact_after` 时，据此推导自动压缩阈值 |
| `tool_call` | 为 false 时不向模型发送工具定义 |
| `reasoning` | 为 true 时发送推理参数（如 `reasoning_effort`） |
| `limit.output` | 未显式配 `max_tokens` 时作为请求上限 |
| `modalities` | 标记模型能处理的输入/输出模态（用于模态工具） |
| `cost` | 估算 token 花费 |

## 综合示例

```json
{
  "model": "agnes-2.5-flash",
  "guidelines": ["本项目所有 Go 代码必须 gofmt"],
  "append_system_prompt": ["危险操作前先征得确认"],
  "context_files": { "fixed_paths": [".coren/rules.md"] },
  "memory_path": "MEMORY.md",
  "cache_retention": "long",
  "reasoning": "auto",
  "compact_after": 40
}
```
