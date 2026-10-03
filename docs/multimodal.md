# 多模态：查看文件

Coren 用**统一的一个工具** `read_file` 处理所有文件：文本直接返回内容；图片、
音频、视频在模型支持对应模态时作为**多模态内容**送入模型；不支持时返回文件类型
与原因，让模型如实告知用户，而不是猜测内容。

## 行为

`read_file` 读取文件后按类型分派：

| 文件类型 | 模型支持该模态 | 结果 |
|---|---|---|
| 文本（text/json/xml/js…） | — | 返回文本内容 |
| 图片 / 音频 / 视频 | 是 | 内容作为媒体附加到消息，模型可"看到/听到" |
| 图片 / 音频 / 视频 | 否 | 返回「这是 image/png 文件，当前模型不支持图片输入，无法查看」 |
| 其他二进制 | — | 返回「这是 application/octet-stream 文件，无法作为附件展示」 |

这样模型只需记住一个工具；不支持的情形是**明确的降级信息**，含类型和原因。

## 判断依据

- **类型识别**：先按扩展名（`mime.TypeByExtension`），未知再按内容签名探测
  （PNG / JPEG / GIF / WebP / PDF 的魔数）。
- **模型能力**：来自解析后的 `modelinfo.Info.Modalities.Input`（见
  `docs/prompting.md` 的三层优先级）。未知模型（无信息）默认按"支持图片"处理。
- **大小限制**：内联媒体默认上限 20 MiB（`MaxMediaBytes` 可调）；超限不附加，
  返回说明。

## 媒体如何进入模型上下文

1. 工具通过 `tools.ResultTool` 的 `RunResult` 返回 `Result{Text, Parts}`。
2. `Parts` 是 `llm.ContentPart`（`type` + `mime_type` + base64 `data` 或 `uri`）。
3. 事件日志的 `tool/result` 事件持久化 `Parts`；`DeriveMessages` 投影时把它们放回
   该工具结果消息。
4. 适配器编码：
   - **Chat Completions**：工具结果文本走 `tool` 消息，媒体作为紧随的 `user`
     消息的 `image_url` / `input_audio` 部分（tool 角色只接受文本）。
   - **Responses**：`function_call_output` 之后追加一条带 `input_image` /
     `input_audio` 的 `user` 消息。

## 使用示例

```bash
coren run "用 read_file 打开 logo.png，描述画面内容"
coren run "读一下 diagram.svg 并解释流程"
```

若当前模型是多模态（如 `agnes-2.5-flash`，支持 image 输入），模型会直接看到图片；
若是纯文本模型，会得到"文件是图片但无法查看"的说明。

## 扩展

新增媒体类型只需在 `pkg/plugins/builtintools/files_read.go` 的 `kindForMIME` /
`detectMIME` 中补充识别，并在适配器里增加对应内容部分的编码。协议本身不限制类型。
