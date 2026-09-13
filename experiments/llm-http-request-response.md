# LLM HTTP Request/Response Trace

这份笔记只讲**真正发给模型服务的 HTTP 请求和响应**，不讲 LangChain callback 包装层。

## 1. 本次真实样本位置

Session:

```text
e78297ae266440caac2cac2dc5575c51
```

真实 HTTP trace:

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl
```

其中：

```text
第 1 行: event = http_request
第 2 行: event = http_response
```

已经拆开的第二行响应文件：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\http_response_line2_pretty.txt
```

注意：

```text
llm_http_trace.jsonl = 真正 HTTP 请求/响应
llm_raw_trace.jsonl = LangChain callback 层对象，不是真正 HTTP 原文
llm_trace.jsonl = OpenStoryline Agent 层消息/工具结果 trace
```

## 2. 请求是什么

第一行请求发往：

```text
POST https://api.deepseek.com/chat/completions
```

关键字段：

```text
model: deepseek-v4-flash
stream: true
messages: 2 条
tools: 18 个
```

真实请求的结构可以简化成：

```json
{
  "model": "deepseek-v4-flash",
  "stream": true,
  "temperature": 0.1,
  "messages": [
    {
      "role": "system",
      "content": "## 角色\n你是一个短视频剪辑助理...\n\n【User media upload status】..."
    },
    {
      "role": "user",
      "content": "基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。"
    }
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "storyline_load_media",
        "description": "Loads and indexes input media. Entry point with no dependencies; required by all downstream operations",
        "parameters": {
          "type": "object",
          "properties": {
            "mode": {
              "type": "string",
              "enum": ["auto", "skip", "default"],
              "default": "auto"
            }
          }
        }
      }
    }
  ]
}
```

本次请求里的 18 个工具名：

```text
storyline_load_media
storyline_search_media
storyline_split_shots
storyline_understand_clips
storyline_filter_clips
storyline_group_clips
storyline_generate_script
storyline_script_template_rec
storyline_generate_voiceover
storyline_select_bgm
storyline_elementrec_transition
storyline_elementrec_text
storyline_plan_timeline_pro
storyline_render_video
storyline_read_node_history
storyline_write_skills
subtitle_imitation_skill
create_profile_style_skill
```

理解：

```text
messages = 告诉 LLM 当前上下文、系统规则、用户输入、历史消息
tools = 告诉 LLM 当前能调用哪些工具，以及每个工具的参数格式
```

模型要做的是：

```text
根据 messages + tools，决定直接回答，还是返回 tool_calls。
```

## 3. 响应是什么

因为请求里有：

```json
"stream": true
```

所以响应不是一个完整 JSON，而是 SSE 流：

```text
data: {...}

data: {...}

data: [DONE]
```

每个 `data: {...}` 是一个 chunk。不能把整个 body 当成一个 JSON 解析。

本次第二行响应最终拼出来的核心结果是：

```text
content:
我先说下计划：加载你的素材 → 切分镜头 → 理解内容 → 分组排序 → 生成文案 → 元素推荐（花字/转场）→ 选背景音乐 → 组织时间线 → 渲染成片。因为你说不要配音，所以配音这步我会跳过。

先加载素材，建立索引。

tool_calls:
- name: storyline_load_media
  arguments: {"mode": "auto"}

finish_reason:
tool_calls
```

意思是：

```text
LLM 先生成一段给用户看的文本 content。
然后返回 tool_calls，表示下一步要调用 storyline_load_media。
finish_reason = tool_calls，表示这轮不是最终回答，而是停下来交给 Agent 执行工具。
```

## 4. content / reasoning_content / tool_calls

流式响应里常见 chunk 字段：

```text
delta.content
delta.reasoning_content
delta.tool_calls
finish_reason
```

含义：

```text
content = 模型正式输出，OpenStoryline 会流式展示给前端
reasoning_content = DeepSeek 返回的推理片段，不是 OpenAI tool calling 的通用必备字段
tool_calls = 模型生成的工具调用指令
finish_reason = 本轮结束原因
```

本次响应里：

```text
content 被拼成给用户看的计划和说明。
tool_calls 被拼成 storyline_load_media {"mode": "auto"}。
finish_reason 最后是 tool_calls。
```

## 5. tool_calls 怎么理解

`tool_calls` 不是工具执行结果，而是 LLM 给 Agent 的调用指令。

概念上等价于：

```json
[
  {
    "id": "call_...",
    "type": "function",
    "function": {
      "name": "storyline_load_media",
      "arguments": "{\"mode\":\"auto\"}"
    }
  }
]
```

翻译成人话：

```text
请调用 storyline_load_media 工具。
参数是 {"mode": "auto"}。
这次调用编号是 call_...。
```

其中：

```text
id = 这次工具调用的配对编号
name = 要调用的工具名
arguments = 工具参数，通常是 JSON 字符串
```

`id` 不是 LLM 的长期记忆。它只是本次工具调用和后续 tool result 之间的配对编号。

## 6. 执行完工具后发生什么

LLM 返回：

```text
tool_calls: storyline_load_media {"mode": "auto"}
```

LangChain Agent 会：

```text
1. 在已注册 tools 中找到 storyline_load_media
2. 调用对应 MCP tool
3. MCP Server 执行 load_media node
4. 工具结果返回 Agent
5. Agent 把结果包装成 role=tool 的消息
6. 下一轮请求再把这个 tool message 放回 messages
```

下一轮请求里会出现类似：

```json
{
  "role": "assistant",
  "content": "先加载素材，建立索引。",
  "tool_calls": [
    {
      "id": "call_...",
      "type": "function",
      "function": {
        "name": "storyline_load_media",
        "arguments": "{\"mode\":\"auto\"}"
      }
    }
  ]
}
```

紧接着：

```json
{
  "role": "tool",
  "tool_call_id": "call_...",
  "content": "{'summary': {'node_summary': ... 'artifact_id': 'load_media_xxx'}, 'isError': False}"
}
```

外层结构是协议要求：

```text
role = tool
tool_call_id = 对应前面 tool_calls 的 id
content = 工具结果
```

但 `content` 里面怎么写，是应用自己定义的。OpenStoryline 当前放的是工具 summary、artifact_id、preview_urls、日志等信息。

## 7. Agent 什么时候继续，什么时候结束

判断逻辑主要由 LangChain/LangGraph Agent runtime 完成：

```text
如果 AIMessage.tool_calls 非空：
  执行工具
  把 ToolMessage 放回上下文
  再调用 LLM

如果没有 tool_calls，finish_reason = stop：
  Agent 结束
  OpenStoryline 发 assistant.end 给前端
```

OpenStoryline 代码入口：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\agent_fastapi.py
```

关键调用：

```python
stream = sess.agent.astream(
    {"messages": _merged_messages},
    context=sess.client_context,
    stream_mode=["messages", "updates"],
)
```

`messages` 模式：

```text
拿到模型流式文本 chunk，发给前端 assistant.delta。
```

`updates` 模式：

```text
拿到完整 AIMessage / ToolMessage，用于保存上下文和继续执行。
```

## 8. 快速查看命令

查看第二行响应是否是 HTTP response：

```powershell
$p = "C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl"
$r = (Get-Content -LiteralPath $p)[1] | ConvertFrom-Json
$r.event
$r.payload.status_code
```

拆开第二行 SSE 响应：

```powershell
$p = "C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl"
$out = "C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\http_response_line2_pretty.txt"
$r = (Get-Content -LiteralPath $p)[1] | ConvertFrom-Json
$r.payload.body -split "`n`n" | Set-Content -LiteralPath $out -Encoding UTF8
```

输出文件：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\http_response_line2_pretty.txt
```

## 9. 一句话总结

这次请求/响应的完整逻辑是：

```text
OpenStoryline 把 system prompt、用户输入、18 个 tool schema 发给 DeepSeek。
DeepSeek 用 SSE 流式返回 content、reasoning_content、tool_calls。
LangChain/OpenAI SDK 把 tool_calls chunk 拼完整。
LangChain Agent 根据 tool_calls 调用 storyline_load_media。
工具结果作为 role=tool 的消息进入下一轮 LLM 请求。
```
