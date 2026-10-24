# `storyline_understand_clips`：片段怎样获得“内容含义”

`storyline_split_shots` 回答的是：**视频可以在哪些位置切成片段？**

`storyline_understand_clips` 回答的是：**这些片段里分别有什么，整体又在表达什么？**

这一步的核心不是继续剪视频，而是把机器易于处理的片段边界，转换成后续 Agent 可以据此判断的语义信息。

```text
split_shots 的输出                         understand_clips 的输出
clip_id + 时间范围 + 素材来源     ->      caption + aes_score + overall
```

## 1. 它在整条链路中的位置

```text
load_media
    ↓ 提供原始素材路径和媒体信息
split_shots
    ↓ 提供 clip_id、起止时间和 source_ref
understand_clips
    ↓ 提供每个片段的描述、评分和总体概括
filter_clips
```

这里有两个容易混淆的层次：

1. **Agent LLM 决定下一步调用哪个 MCP tool。** 它在 `split_shots` 返回后选择 `storyline_understand_clips`。
2. **`understand_clips` 节点内部再次调用模型。** 它用视觉模型逐个理解片段，再用文本模型汇总所有片段。

因此，这一步不是“一次 LLM 请求”，而是一段嵌套过程：

```text
Agent LLM
  └─ tool_call: storyline_understand_clips
       └─ MCP 节点执行
            ├─ 视觉模型：分析 clip_0001
            ├─ 视觉模型：分析 clip_0002
            ├─ 视觉模型：分析 clip_0003
            └─ 文本模型：生成 overall
       └─ tool result 返回 Agent LLM
Agent LLM
  └─ 决定下一步 storyline_filter_clips
```

## 2. 为什么 Agent 会调用 `understand_clips`

`split_shots` 执行完后，Agent LLM 的真实响应中有下面这一段：

```json
{
  "role": "assistant",
  "reasoning_content": "3 clips. Now understand clips.",
  "content": "切成 3 个片段。接下来理解每个片段的内容。",
  "tool_calls": [
    {
      "id": "call_00_WPjg22wxsnY2QxwmZeko6484",
      "type": "function",
      "function": {
        "name": "storyline_understand_clips",
        "arguments": "{\"mode\": \"auto\"}"
      }
    }
  ]
}
```

这段响应包含三种不同作用的信息：

- `reasoning_content` 表示模型内部形成的下一步判断：已有 3 个片段，现在需要理解片段。
- `content` 是同时准备展示给用户的自然语言进度说明。
- `tool_calls` 才是程序真正执行的指令：调用 `storyline_understand_clips`，参数为 `{"mode":"auto"}`。

`finish_reason` 为 `tool_calls`，意味着这一轮不是普通文字回答结束，而是停下来等待工具执行结果。

这个选择不是代码在 `split_shots` 后硬编码自动触发的。Agent 能做出选择，是因为它收到的 system prompt 把常规流程写成了：

```text
- 镜头切分 "split_shots"（可跳过）。将素材按镜头切分成片段。
- 内容理解 "understand_clips"（可跳过）。 为每个片段(clips)生成一段描述（captions）
- 镜头筛选 "filter_clips"（可跳过）。根据用户要求，筛选出符合要求的片段(clips)
```

同时，工具描述声明了 `understand_clips` 依赖 `load_media` 和 `split_shots`。流程提示告诉模型“通常下一步是什么”，依赖信息告诉模型“当前能不能调用”。

完整触发请求与完整原始 SSE 响应保存在：

- [01-agent-trigger-request.json](evidence/understand-clips/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/understand-clips/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/understand-clips/02-agent-trigger-response.parsed.json)

## 3. 为什么它同时需要 `load_media` 和 `split_shots`

`split_shots` 给出的 clip 只描述“原素材中的哪一段”，而不直接携带供视觉模型读取的完整视频数据。例如节点拿到的关键信息可以概括为：

```json
{
  "clip_id": "clip_0001",
  "start": 0,
  "end": 5000,
  "source_ref": {
    "media_id": "media_0001"
  }
}
```

其中：

- `start`、`end` 来自 `split_shots`，确定要分析的时间范围。
- `source_ref.media_id` 把片段指回原始素材。
- 原始素材的实际文件路径来自 `load_media`。

所以节点必须把两份上游结果拼起来：

```text
media_id
  ├─ 在 split_shots 中找到：片段起止时间
  └─ 在 load_media 中找到：原视频文件路径
                    ↓
        得到“分析哪个文件的哪一段”
```

源码中真正承担这层拼接关系的核心逻辑只有这些：

```python
load_media = inputs["media"]
clips = inputs["split_shots"]["clips"]

media_id = clip["source_ref"]["media_id"]
video_path = load_media[media_id]["path"]
media = Media(video=video_path,
              in_sec=clip["start"] / 1000,
              out_sec=clip["end"] / 1000)
```

这里贴源码不是为了讲 Python，而是为了确认一件事：节点没有凭空找到视频；它用 `media_id` 把“切分结果”和“素材路径”连接起来。

## 4. 单个 clip 怎样变成视觉模型的输入

以 `clip_0001` 为例，节点把视频路径与时间范围交给采样层。采样层从 5 秒片段中取出 6 帧，再组成多模态消息。

真实请求中的可读部分是：

```json
{
  "model": "deepseek-v4-flash-vision-exp",
  "messages": [
    {
      "role": "system",
      "content": "你是一个专业的视频内容分析助手。你的任务是详细描述输入的视频画面。"
    },
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "Media 1: video segment ... (sampled frames in time order)"},
        {"type": "text", "text": "Frame 1 at 0.42s"},
        {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}},
        {"type": "text", "text": "Frame 2 at 1.25s"},
        {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,..."}}
      ]
    }
  ]
}
```

上面是**结构示意**，不是完整请求原文；其中省略了剩余帧及 base64 图片数据。完整、未删减的真实请求在 [03-clip-0001-vlm-request.json](evidence/understand-clips/03-clip-0001-vlm-request.json)。

这层转换的意义是：视觉模型不能只凭 `clip_id` 理解画面。它需要实际像素。项目没有把整个视频直接塞进请求，而是抽取若干时间点的画面，使模型能够以较小的数据量观察片段的主要视觉内容。

## 5. 三次视觉分析分别返回什么

每个 clip 都单独调用一次视觉模型。三次真实响应解析后的 `content` 如下。

### `clip_0001`

```json
{
  "caption": "纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。",
  "aes_score": 0.58
}
```

### `clip_0002`

```json
{
  "caption": "画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。",
  "aes_score": 0.72
}
```

### `clip_0003`

```json
{
  "caption": "视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。",
  "aes_score": 0.5
}
```

这些返回值的逻辑关系是：

- `caption` 把像素内容转换成文字语义，后续 Agent 和其他文本模型可以直接使用。
- `aes_score` 是模型给出的视觉美学评分，后续筛选可以把它作为判断依据之一。
- 此时模型只分析“一个片段”，还不知道三个片段合起来表达什么。

完整请求、原始 SSE 和解析结果均保存在 `evidence/understand-clips/` 的 `03` 至 `08` 号文件中。

## 6. 为什么还要再生成一次 `overall`

三个独立 caption 只能说明每段各自是什么。节点随后把它们拼成一条纯文本请求，让文本模型寻找三段素材之间的共同主题与顺序关系。

这次请求很小，完整内容如下：

```json
{
  "messages": [
    {
      "content": "你是一个短视频剪辑素材理解助手。\n你的任务是：根据给定的多个素材的文案描述，生成一段总体描述。重点包含：主体内容、人物或物体、场景和整体氛围，方便后续做高光检测和文案生成。不要输出客套话或多余说明。",
      "role": "system"
    },
    {
      "content": "下面是若干clips的单体描述，请你从整体角度总结：这些素材大致讲述了怎样的故事？请用 1~2 段话总结，并保持客观、中性。\n['- None: 纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。', '- None: 画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。', '- None: 视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。']",
      "role": "user"
    }
  ],
  "model": "deepseek-v4-flash",
  "max_completion_tokens": 1024,
  "stream": true,
  "temperature": 0.3,
  "top_p": 0.9
}
```

模型的真实返回内容是：

```json
{
  "content": "这些素材由三张无实拍人物与动作的标题卡组成，分别显示“Morning_coffee”“City_walk”“Sunset_chill”，并采用明黄色、彩色条纹测试图、深灰蓝与橙红色块等不同视觉设计。整体上，它们更像是一组极简风格的Vlog章节片头或转场标题，按“早晨咖啡—城市漫步—日落休闲”的顺序暗示一天中的生活片段。\n\n从叙事角度看，素材本身没有具体人物、情节或场景变化，主要承担主题提示与氛围铺垫功能，整体风格简洁、平面化，情绪在明快与平静冷淡之间切换。"
}
```

因此：

```text
caption = 单个片段的局部理解
overall = 所有片段放在一起后的整体理解
```

这里还能看到一个真实实现问题：请求中三行都以 `- None:` 开头。原因是构造文本时读取了 `clip_instance_id`，而前面保存的字段实际叫 `clip_id`。这没有阻止本次模型生成总体概括，但丢失了“描述对应哪个 clip”的标签。

完整证据：

- [09-overall-request.json](evidence/understand-clips/09-overall-request.json)
- [10-overall-response.sse.txt](evidence/understand-clips/10-overall-response.sse.txt)
- [10-overall-response.parsed.json](evidence/understand-clips/10-overall-response.parsed.json)

## 7. `understand_clips` 最终产出的 artifact

节点把三条局部分析和一条总体分析保存为 artifact。真实文件内容如下：

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
        }
      },
      {
        "clip_id": "clip_0002",
        "caption": "画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。",
        "aes_score": 0.72,
        "source_ref": {
          "media_id": "media_0002"
        }
      },
      {
        "clip_id": "clip_0003",
        "caption": "视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。",
        "aes_score": 0.5,
        "source_ref": {
          "media_id": "media_0003"
        }
      }
    ],
    "overall": "这些素材由三张无实拍人物与动作的标题卡组成，分别显示“Morning_coffee”“City_walk”“Sunset_chill”，并采用明黄色、彩色条纹测试图、深灰蓝与橙红色块等不同视觉设计。整体上，它们更像是一组极简风格的Vlog章节片头或转场标题，按“早晨咖啡—城市漫步—日落休闲”的顺序暗示一天中的生活片段。\n\n从叙事角度看，素材本身没有具体人物、情节或场景变化，主要承担主题提示与氛围铺垫功能，整体风格简洁、平面化，情绪在明快与平静冷淡之间切换。"
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "understand_clips_1789240964.6216764",
  "node_id": "understand_clips",
  "create_time": 1789240980.2166595
}
```

artifact 不是视频文件。它是这次节点执行结果的持久化 JSON：

- `payload` 是真正的业务结果。
- `artifact_id` 是这份结果的标识。
- `session_id` 表示它属于哪次会话。
- `node_id` 表示它由哪个节点生成。

完整文件：[13-understand-clips-artifact.json](evidence/understand-clips/13-understand-clips-artifact.json)。

## 8. 节点执行完后，怎样回到 Agent LLM

MCP 执行器不会把整个 artifact 原样塞回 Agent 对话，而是追加一条 `role: tool` 消息。为了让这条结果能与之前的调用配对，对话里同时保留上一条 assistant tool call。

真实 request 最末尾的两条消息是：

```json
[
  {
    "content": "切成 3 个片段。接下来理解每个片段的内容。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_WPjg22wxsnY2QxwmZeko6484",
        "function": {
          "name": "storyline_understand_clips",
          "arguments": "{\"mode\": \"auto\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:00.210758] Clip understanding completed. Analyzed 3 clips in total. Overall description: 这些素材由三张无实拍人物与动作的标题卡组成，分别显示“Morning_coffee”“City_walk”“Sunset_chill”，并采用明黄色、彩色条纹测试图、深灰蓝与橙红色块等不同视觉设计。整体上，它们更像是一组极简风格的Vlog章节片头或转场标题，按“早晨咖啡—城市漫步—日落休闲”的顺序暗示一天中的生活片段。\\n\\n从叙事角度看，素材本身没有具体人物、情节或场景变化，主要承担主题提示与氛围铺垫功能，整体风格简洁、平面化，情绪在明快与平静冷淡之间切换。', 'preview_urls': [], 'artifact_id': 'understand_clips_1789240964.6216764'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_WPjg22wxsnY2QxwmZeko6484"
  }
]
```

二者通过同一个 ID 关联：

```text
assistant.tool_calls[0].id
    = call_00_WPjg22wxsnY2QxwmZeko6484

tool.tool_call_id
    = call_00_WPjg22wxsnY2QxwmZeko6484
```

这个 ID 的作用是“说明这条 tool result 回答的是哪一次 tool call”，不是给模型提供跨会话记忆。

`tool` 消息只带摘要和 `artifact_id`，而不是重复完整的 `clip_captions`。Agent 若之后需要完整数据，可以通过 artifact 读取机制取得；普通流程中，摘要已经足够让它决定下一步。

完整回送请求：[11-agent-resume-request.json](evidence/understand-clips/11-agent-resume-request.json)。

## 9. 下一步为什么是 `filter_clips`

Agent 收到上面的 tool result 后，下一次真实响应是：

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

这说明 `understand_clips` 本身并没有自动调用 `filter_clips`。实际边界是：

```text
understand_clips 返回结果
        ↓
结果作为 role=tool 的 message 发回 Agent LLM
        ↓
Agent LLM 结合流程提示、历史消息和工具列表
        ↓
返回 storyline_filter_clips tool_call
```

完整响应：

- [12-agent-next-response.sse.txt](evidence/understand-clips/12-agent-next-response.sse.txt)
- [12-agent-next-response.parsed.json](evidence/understand-clips/12-agent-next-response.parsed.json)

## 10. 用一句话理解整个过程

`storyline_understand_clips` 是结构数据与语义数据之间的桥梁：它用 `load_media` 找到原视频，用 `split_shots` 确定要看的时间范围，用视觉模型产生每个片段的 caption 和评分，再用文本模型形成 overall，最后把摘要作为 tool result 交还 Agent，让 Agent 决定下一步调用什么工具。

## 证据说明

本文基于同一会话 `e78297ae266440caac2cac2dc5575c51` 的真实 trace 第 5 至 16 行，以及实际生成的 artifact。正文只保留理解关系所需的原文；完整请求、未解析 SSE 与解析后响应都存放在 [evidence/understand-clips](evidence/understand-clips/) 中，正文中的省略均明确标注，不把结构示意伪装成完整原文。
