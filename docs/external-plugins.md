# 外部进程插件

Coren 的内置插件是 Go 代码，编译进主程序。**外部进程插件**（out-of-process
plugin）是另一条扩展路径：插件是一个独立子进程，可以与宿主用任意语言编写，
通过标准输入输出交换 JSON-RPC 消息。它让你在不重新编译主程序的前提下扩展工具。

## 为什么用子进程

- **免重编**：新增、替换、移除插件只改配置，主程序不动
- **语言无关**：协议是换行分隔的 JSON-RPC 2.0，任何能读写行的语言都能实现
- **进程隔离**：插件崩溃不影响宿主；权限、工作目录各自独立
- **跨平台**：stdio 在 Linux、macOS、Windows 上行为一致

代价是每次工具调用有一次进程间往返（毫秒级），对工具调用场景可以忽略。

## 协议

传输是**换行分隔的 JSON**（NDJSON）：每行一条完整 JSON-RPC 2.0 消息。
子进程的 `stdout` 只用于协议，日志必须写到 `stderr`。

### 握手

宿主启动插件后先发 `initialize`：

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocol_version":1,"work_dir":"/path","host_version":"0.1.0"}}
```

插件回复自身信息与工具清单：

```json
{"jsonrpc":"2.0","id":1,"result":{"protocol_version":1,"name":"echo","version":"0.1.0","tools":[{"name":"echo","description":"...","parameters":{...}}]}}
```

`protocol_version` 必须等于宿主版本，否则宿主拒绝该插件。

### 工具调用

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}
```

成功：

```json
{"jsonrpc":"2.0","id":2,"result":{"output":"echo: hi"}}
```

失败（工具业务错误，非协议错误）：

```json
{"jsonrpc":"2.0","id":2,"result":{"output":"missing argument","is_error":true}}
```

### 关闭

宿主在卸载时发送 `shutdown` 通知（无 id），随后关闭管道并结束子进程。

## 配置

在 `coren.json` 中启用宿主插件并声明外部插件：

```json
{
  "add_plugins": ["plugin-host"],
  "external_plugins": [
    { "name": "echo", "command": "/path/to/echoplugin", "args": [], "env": ["KEY=VALUE"], "dir": "." }
  ]
}
```

- `add_plugins` 把 `plugin-host` 加到当前 profile
- 每个 `external_plugins` 条目会被 `exec` 拉起并握手
- 工具名与已有工具冲突会导致启动失败（避免静默覆盖）

## 用 Go 写一个插件

```go
package main

import "coren/pkg/pluginproto"

func main() {
	srv := &pluginproto.Server{
		Name:    "echo",
		Version: "0.1.0",
		Tools: map[string]pluginproto.ServerTool{
			"echo": {
				Spec: pluginproto.ToolSpec{Name: "echo", Description: "Echo text back."},
				Run:  func(args string) (string, error) { return "echo: " + args, nil },
			},
		},
	}
	if err := srv.ServeStdio(); err != nil {
		panic(err)
	}
}
```

完整示例见 `examples/echoplugin`。

## 用其他语言写

任何语言都可以，只需：读一行 JSON、处理、回一行 JSON。伪代码：

```
while line := readline(stdin):
    msg := json(line)
    if msg.method == "initialize":
        reply(msg.id, {protocol_version: 1, name: "x", tools: [...]})
    elif msg.method == "tools/call":
        reply(msg.id, {output: handle(msg.params)})
    elif msg.method == "shutdown":
        exit(0)
```

## 版本与兼容

当前协议版本为 `1`。宿主与插件都必须在握手中声明该版本；未来不兼容的变更会提升
版本号，宿主据此拒绝旧插件。工具声明的 `parameters` 是标准 JSON Schema，直接用于
模型提示，因此插件对参数结构的描述即模型看到的定义。
