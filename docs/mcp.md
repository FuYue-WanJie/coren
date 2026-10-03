# MCP（Model Context Protocol）

Coren 支持连接**远程 MCP 服务器**，把服务器暴露的 tools / resources / prompts
接入本地。MCP 工具会像内置工具一样注册到 `tools` 服务，模型无感调用。

## 传输

| 传输 | 值 | 说明 |
|---|---|---|
| Streamable HTTP | `streamable`（默认） | 当前 MCP 规范：单一端点，响应为 JSON 或 SSE 流 |
| HTTP+SSE | `sse` | 旧版传输：GET 打开事件流并告知 POST 端点，响应经事件流返回 |

客户端按 JSON-RPC id 匹配响应，会跳过通知与服务器主动请求等无关消息。

## 配置

### 配置文件（`coren.json`）

```json
{
  "mcp": [
    {
      "name": "deepwiki",
      "url": "https://mcp.deepwiki.com/mcp",
      "transport": "streamable",
      "headers": { "Authorization": "Bearer ..." },
      "enabled": true
    }
  ]
}
```

- `enabled` 缺省为 `true`；设为 `false` 可临时停用而不删条目。
- `transport` 缺省为 `streamable`。

### CLI 管理

```bash
coren mcp list
coren mcp add <name> <url> [--sse] [--header K=V]
coren mcp remove <name>
coren mcp test <name>     # 连接并列出服务器的原语
```

CLI 读写项目级 `coren.json` 的 `mcp` 字段，保留其他键。

## 原语如何映射

连接成功后（`initialize` 握手），按服务器声明的能力位注册：

| MCP 原语 | 暴露给模型的方式 |
|---|---|
| `tools/list` + `tools/call` | 每个工具注册为一个本地工具 |
| `resources/list` | `list_resources` 工具 |
| `resources/read` | `read_resource` 工具（按 URI） |
| `prompts/list` | `list_prompts` 工具 |
| `prompts/get` | `get_prompt` 工具 |

**命名空间**：远程工具名默认加前缀 `服务器名__`，避免与本地或彼此冲突，
例如 `deepwiki__read_wiki_structure`。可在插件配置里用 `Namespace: false` 关闭。

## 失败处理

- 启动时连接失败的服务器会**打印警告并跳过**，不会中止整个 boot。
- `enabled: false` 的服务器直接跳过。
- 单个工具调用失败通过工具错误回传给模型。

## 架构位置

```
pkg/mcp/                 # 契约与客户端
    client.go            # JSON-RPC + 两种传输 + 握手
    sse.go               # SSE 解析
    primitives.go        # tools/resources/prompts 调用
pkg/plugins/mcp/         # 插件：连接配置的服务器并注册原语
```

`pkg/mcp` 不依赖内核，可独立用于其它程序。

## 示例

```bash
coren mcp add deepwiki https://mcp.deepwiki.com/mcp
coren mcp test deepwiki
coren run "用 deepwiki 工具查一下 github.com/openai/openai-go 的文档结构"
```

模型会调用 `deepwiki__read_wiki_structure` 并基于返回内容作答。
