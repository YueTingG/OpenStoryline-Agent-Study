# Eino Agent Loop Demo

这个 demo 用 Go/Eino 的术语复刻 OpenStoryline 里这条链路：

```text
messages + tools
-> LLM 返回 assistant content + tool_calls
-> Agent 根据 tool_calls 调用工具
-> 工具结果变成 role=tool 的消息
-> 下一轮再发给 LLM
```

对应真实 trace：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl
```

其中：

```text
第 1 行 http_request:
  messages = system + user
  tools = 18 个 OpenStoryline 工具 schema

第 2 行 http_response:
  SSE chunks
  最终拼出 content + tool_calls
  tool_calls = storyline_load_media {"mode":"auto"}
```

## 这个 demo 模拟什么

为了保持代码短，demo 只定义一个工具：

```text
storyline_load_media
```

它模拟 OpenStoryline MCP tool 的返回：

```json
{
  "summary": {
    "node_summary": {
      "INFO_USER": "Media indexing completed successfully: 3 video(s), 0 image(s)",
      "artifact_id": "load_media_demo"
    }
  },
  "isError": false
}
```

真实系统中还有：

```text
storyline_split_shots
storyline_understand_clips
storyline_group_clips
...
```

但第一轮理解 `tool_calls -> tool 执行 -> ToolMessage`，一个 `load_media` 就够。

## 模型配置

不要把 key 写进代码。运行前用环境变量：

PowerShell:

```powershell
$env:OPENAI_BASE_URL = "https://api.deepseek.com"
$env:OPENAI_API_KEY = "你的 key"
$env:OPENAI_MODEL = "deepseek-v4-flash"
```

Eino 的 OpenAI ChatModel 走 OpenAI-compatible 接口，所以 DeepSeek 可以通过 `OPENAI_BASE_URL` 接入。

## 和 OpenStoryline 的对应关系

OpenStoryline Python/LangChain：

```text
ChatOpenAI
MultiServerMCPClient.get_tools()
create_agent(model=llm, tools=tools+skills)
```

这个 Go/Eino demo：

```text
openai.NewChatModel(...)
utils.InferTool("storyline_load_media", ...)
chatModel.BindTools(toolInfos)
compose.NewToolNode(...)
```

两边本质一样：

```text
把工具 schema 交给模型
模型返回 tool_calls
Agent/框架根据 tool name 找到本地工具并执行
```

## 注意

`main.go` 是学习 demo，不是 OpenStoryline 的完整 Go 移植。

真实 OpenStoryline 的 MCP tool 是远程 MCP Server 执行；这里的 `storyline_load_media` 是本地 mock 函数。

如果要做真正 Go 版 OpenStoryline Agent，需要把工具执行层替换成：

```text
tool_calls.name
-> Go MCP client
-> OpenStoryline MCP Server
-> Node 执行
```
