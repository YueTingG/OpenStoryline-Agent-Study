# 个人总结
其实就是调用大模型问是否保留。

大模型怎么判断是否保留，就是 Understand clip 返回的描述来看是否保留的

# `storyline_filter_clips`：从“理解全部片段”到“选择后续使用的片段”

`storyline_understand_clips` 为每个 clip 生成内容描述。

`storyline_filter_clips` 使用这些描述回答另一个问题：**后续剪辑应该使用哪些 clip？**

```text
understand_clips 的输出                  filter_clips 的输出
全部 clip 的 caption              ->     selected clip_id 列表
```

这一步第一次允许流程缩小素材范围。不过它并不删除视频文件，也不重新剪切视频；它只生成一份选择结果，告诉后续节点哪些 `clip_id` 应继续参与剪辑。

## 1. 它在真实链路中的位置

```text
split_shots
    ↓ 提供全部 clip 及其时长
understand_clips
    ↓ 提供全部 clip 的 caption
filter_clips
    ↓ 提供 selected clip_id 列表
group_clips
```

这里仍然存在两种性质不同的 LLM 调用：

```text
Agent LLM
  └─ 决定调用 storyline_filter_clips，并给出 user_request
       └─ filter_clips 节点内部的文本 LLM
            └─ 根据 user_request 和 captions 输出 keep=true/false
       └─ 节点把结果整理成 selected
Agent LLM
  └─ 收到工具结果，再决定调用 storyline_group_clips
```

Agent LLM 负责“下一步做什么”。节点内部的 LLM 只负责“哪些 clip 保留”。两者的 prompt 和上下文不是同一套。

## 2. Agent 怎样发起 `filter_clips`

`understand_clips` 返回后，Agent LLM 的真实响应是：

```json
{
  "role": "assistant",
  "reasoning_content": "These are title cards. Now filter clips. Since it's a test video with all 3 clips, filter maybe auto. Actually the user wants a 5-second test video using their material. Let's keep all clips. Let me run filter_clips with auto, requesting to keep all? Actually filter default filters based on requirements. Given it's a test, maybe skip? But we want all materials. Hmm, the user said \"基于我上传的这个素材\" - use this material. I'll run filter with a request to keep all clips.\n\nActually filter_clips might drop clips. For a test video of 5 seconds, keeping all 3 clips is fine. Let me set user_request to keep all clips.\n\nLet me run filter_clips.",
  "content": "这三个片段其实是三张标题卡：「Morning_coffee」「City_walk」「Sunset_chill」，像一组极简 Vlog 章节片头。我保留全部片段，不做删减。",
  "tool_calls": [
    {
      "id": "call_00_Wzc7NWrTYJ9jTGuVjYDy5758",
      "type": "function",
      "function": {
        "name": "storyline_filter_clips",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"这是测试视频，保留全部片段，不要丢弃任何素材。\"}"
      }
    }
  ]
}
```

这段响应里，真正交给 MCP 工具的参数是：

```json
{
  "mode": "auto",
  "user_request": "这是测试视频，保留全部片段，不要丢弃任何素材。"
}
```

其中两个字段的作用不同：

- `mode: auto` 表示执行自动筛选逻辑。
- `user_request` 是本次筛选的具体标准。它不是用户原话的机械复制，而是 Agent 根据“做一个 5 秒测试视频”和当前素材情况，为筛选节点整理出的明确要求。

因此，Agent 没有把整个对话过程作为工具参数传入，只把这个能力真正需要的筛选要求传入。

完整触发证据：

- [01-agent-trigger-request.json](evidence/filter-clips/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/filter-clips/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/filter-clips/02-agent-trigger-response.parsed.json)

## 3. `filter_clips` 实际使用哪些上游数据

节点声明依赖两份上游结果：

```text
split_shots       -> clips：clip_id、source_ref.duration 等结构信息
understand_clips  -> clip_captions：clip_id、caption、aes_score
```

节点先根据相同的 `clip_id`，把 `split_shots` 中的时长补到 `clip_captions`：

```text
clip_0001 + caption + aes_score
              +
clip_0001 + duration
              ↓
clip_0001 + caption + aes_score + duration
```

但在当前实现中，随后构造 LLM 请求的 `_build_clips_block()` 实际只写入：

```text
[clip_id=clip_0001]
caption: ...
```

也就是说，虽然节点内部的数据对象已经有 `aes_score` 和 `duration`，system prompt 也要求参考它们，**这次真实发给筛选 LLM 的内容并没有包含这两个字段**。

这是理解真实行为时必须区分的三层：

```text
上游 artifact 中有什么
≠ filter_clips 节点内存中有什么
≠ 最终 prompt 真正发送了什么
```

就当前 trace 而言，筛选模型真正看到的是 `user_request + clip_id + caption`。

## 4. 节点内部的 LLM 请求是什么

下面是本次筛选能力调用的完整请求，不包含 Agent 的 system prompt、历史对话、tools 列表，也不包含视频画面：

```json
{
  "messages": [
    {
      "content": "# Role\n你是一名专业的视频素材筛选助手。你需要根据画面的描述、美学评分和时长，从一堆素材中挑选出最适合剪辑的片段。\n\n# Goal\n输出一个JSON结果，包含最终保留的视频片段ID列表。\n\n# Constraints (筛选规则 - 请严格按顺序执行)\n\n**第一步：计算“最多能删几个” (硬性数量指标)**\n请先统计输入片段的总数量，记为【总数】。\n1. **如果【总数】少于或等于 5 个**：\n   - 禁止删除任何片段，必须全部保留。\n2. **如果【总数】大于 5 个**：\n   - 你必须保证最终保留的片段数量 **严格大于** 【总数】的 80%。\n     - *（例如：总数是7个，7 x 0.8 = 5.6，保留数量必须大于5.6，即至少保留6个，意味着最多只能删 1 个）。*\n   - 同时，保留的数量也不能少于 5 个。\n\n**第二步：执行筛选 (内容质量优化)**\n只有在第一步计算出“有删除名额”的情况下，才进行此步。如果第一步要求全保留，则直接跳过此步。\n1. 阅读所有 `clip_captions`片段信息，找出画面描述**高度相似**（几乎一模一样）的片段组。\n2. 在这些相似片段中，对比 `aes_score` (美学分) 和 `duration` (时长)：\n   - **优先保留**：美学分高的、时长适中的。\n   - **考虑删除**：美学分低的、或者时长过短导致无法使用的。\n3. **注意**：删除的数量绝对不能超过第一步计算出的“最大删除名额”。如果名额用完了，即使还有相似片段也不允许再删。",
      "role": "system"
    },
    {
      "content": "用户要求: 这是测试视频，保留全部片段，不要丢弃任何素材。\n请根据用户要求，判断下面所有 clips 是否保留\n[clip_id=clip_0001]\ncaption: 纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。\n\n[clip_id=clip_0002]\ncaption: 画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。\n\n[clip_id=clip_0003]\ncaption: 视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。\n\n输出格式如下：\n注意:只输出以下要求格式的内容，严格禁止输出其他内容\n```json\n{\n  \"results\": [\n    {\"clip_id\": \"clip_0001\", \"keep\": true},\n    {\"clip_id\": \"clip_0002\", \"keep\": false}\n  ]\n}\n```",
      "role": "user"
    }
  ],
  "model": "deepseek-v4-flash",
  "max_completion_tokens": 2048,
  "stream": true,
  "temperature": 0.1,
  "top_p": 0.9
}
```

这个请求可以分为两块理解。

### System prompt：定义筛选规则

system prompt 规定：

- clip 总数不超过 5 个时，禁止删除，必须全部保留。
- 总数超过 5 个时，保留数量必须严格大于 80%，并且不少于 5 个。
- 只有存在删除名额时，才根据内容相似度、评分和时长进行取舍。

这套规则是 `filter_clips` 能力自身的规则，与 Agent 用来规划完整剪辑流程的 system prompt 无关。

### User prompt：提供本次筛选对象

user prompt 包含：

```text
本次 user_request
+
每个 clip_id 及其 caption
+
要求返回的 JSON 结构
```

本次一共只有 3 个 clip，所以即便不考虑“保留全部”的 `user_request`，system prompt 的硬规则也要求全部保留。

## 5. 筛选 LLM 返回什么

原始响应是 SSE 流。将所有 chunk 拼接后，`content` 中的完整业务结果为：

```json
{
  "results": [
    {
      "clip_id": "clip_0001",
      "keep": true
    },
    {
      "clip_id": "clip_0002",
      "keep": true
    },
    {
      "clip_id": "clip_0003",
      "keep": true
    }
  ]
}
```

这里的 `keep` 是对每个候选 clip 的判断：

```text
keep: true   -> clip_id 进入 selected
keep: false  -> clip_id 不进入 selected
```

这次三个结果都是 `true`，所以不存在被排除的 clip。

解析后的完整响应还包含 `reasoning_content`、模型名、结束原因和 token 使用情况，见：

- [04-filter-llm-response.sse.txt](evidence/filter-clips/04-filter-llm-response.sse.txt)
- [04-filter-llm-response.parsed.json](evidence/filter-clips/04-filter-llm-response.parsed.json)

## 6. 程序怎样把 `keep` 转换成 `selected`

LLM 返回的是建议，程序还要做一次约束和整理：

```text
LLM results
    ↓ 只接受输入中真实存在的 clip_id
    ↓ 只收集 keep=true 的 clip_id
    ↓ 按原始 input_clip_ids 的顺序排列
selected
```

本次转换结果是：

```json
{
  "selected": [
    "clip_0001",
    "clip_0002",
    "clip_0003"
  ]
}
```

程序没有直接信任模型随意生成的 ID。如果模型声称保留某些片段，但这些 ID 全都不属于输入，解析会被视为失败。

另一个重要的保护行为是：如果模型输出无法解析，节点会退回到“使用全部 clip”，而不是把所有素材意外丢掉。

## 7. `selected` 是否意味着视频文件被删除

不是。真实 artifact 同时保留：

- `clip_captions`：全部候选 clip 的描述。
- `selected`：后续节点应采用的 clip ID。

本次 artifact 的完整内容是：

```json
{
  "payload": {
    "clip_captions": [
      {
        "clip_id": "clip_0001",
        "caption": "纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。",
        "aes_score": 0.58,
        "source_ref": {
          "media_id": "media_0001"
        },
        "duration": 5.0
      },
      {
        "clip_id": "clip_0002",
        "caption": "画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。",
        "aes_score": 0.72,
        "source_ref": {
          "media_id": "media_0002"
        },
        "duration": 5.0
      },
      {
        "clip_id": "clip_0003",
        "caption": "视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。",
        "aes_score": 0.5,
        "source_ref": {
          "media_id": "media_0003"
        },
        "duration": 5.0
      }
    ],
    "selected": [
      "clip_0001",
      "clip_0002",
      "clip_0003"
    ]
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "filter_clips_1789240982.0975573",
  "node_id": "filter_clips",
  "create_time": 1789240982.8928146
}
```

即使某个 clip 没有进入 `selected`：

- `split_shots` 生成的视频文件仍然存在。
- 该 clip 的 caption 仍可保留在 artifact 中。
- 只是正常的后续剪辑流程不再选择它。

因此，`filter_clips` 更接近“生成入选名单”，而不是“删除素材”。

完整 artifact：[05-filter-clips-artifact.json](evidence/filter-clips/05-filter-clips-artifact.json)。

## 8. 筛选结果怎样回到 Agent LLM

工具执行完成后，系统把原来的 tool call 和对应的 tool result 放回 Agent 对话。真实 request 最末尾两条消息是：

```json
[
  {
    "content": "这三个片段其实是三张标题卡：「Morning_coffee」「City_walk」「Sunset_chill」，像一组极简 Vlog 章节片头。我保留全部片段，不做删减。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_Wzc7NWrTYJ9jTGuVjYDy5758",
        "function": {
          "name": "storyline_filter_clips",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"这是测试视频，保留全部片段，不要丢弃任何素材。\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:02.887112] Successfully filtered 3 clips', 'preview_urls': [], 'artifact_id': 'filter_clips_1789240982.0975573'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_Wzc7NWrTYJ9jTGuVjYDy5758"
  }
]
```

它们通过同一个调用 ID 配对：

```text
assistant.tool_calls[0].id
    = call_00_Wzc7NWrTYJ9jTGuVjYDy5758

tool.tool_call_id
    = call_00_Wzc7NWrTYJ9jTGuVjYDy5758
```

返回 Agent 的是执行摘要和 `artifact_id`，不是完整 artifact。完整请求见 [06-agent-resume-request.json](evidence/filter-clips/06-agent-resume-request.json)。

## 9. Agent 为什么继续调用 `group_clips`

Agent 收到筛选结果后，真实响应是：

```json
{
  "role": "assistant",
  "reasoning_content": "All 3 kept. Now group clips. Arrange in logical order Morning coffee -> City walk -> Sunset chill.",
  "content": "3 个片段全部保留。接下来按「早晨咖啡 → 城市漫步 → 日落休闲」的顺序做分组排序。",
  "tool_calls": [
    {
      "id": "call_00_AFFMMX8bMIGkZJtf9bBn3677",
      "type": "function",
      "function": {
        "name": "storyline_group_clips",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"按一天的时间顺序排列：早晨咖啡 → 城市漫步 → 日落休闲。\"}"
      }
    }
  ]
}
```

这说明 `filter_clips` 没有在内部自动执行 `group_clips`。边界仍然是：

```text
filter_clips 产生 selected
        ↓
结果作为 role=tool 的消息返回 Agent LLM
        ↓
Agent LLM 结合流程提示和当前执行历史
        ↓
返回 storyline_group_clips tool_call
```

完整下一步响应：

- [07-agent-next-response.sse.txt](evidence/filter-clips/07-agent-next-response.sse.txt)
- [07-agent-next-response.parsed.json](evidence/filter-clips/07-agent-next-response.parsed.json)

## 10. 三种“不调用筛选 LLM”的情况

`filter_clips` 并不保证每次都会调用内部 LLM：

- 没有 `user_request`：直接使用全部 clip。
- 使用默认处理分支：直接使用全部 clip。
- LLM 响应解析失败：回退为使用全部 clip。

本次因为参数是 `mode: auto`，并且 `user_request` 非空，所以实际调用了筛选 LLM。

这也说明：MCP tool 被调用，不等于工具内部一定还会调用一次 LLM。是否调用取决于节点参数和节点自身逻辑。

## 11. 用一句话理解整个过程

`storyline_filter_clips` 把 Agent 整理出的筛选要求与 `understand_clips` 生成的文字描述交给一个独立的文本 LLM，再把模型返回的 `keep` 判断约束为可信的 `selected clip_id` 列表；它改变的是后续采用范围，而不是原始视频文件。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 15 至 20 行、`filter_clips` artifact 和当前节点源码。正文保留理解关系所需的完整可读原文；完整 Agent 请求、原始 SSE 与解析后响应存放在 [evidence/filter-clips](evidence/filter-clips/) 中。
