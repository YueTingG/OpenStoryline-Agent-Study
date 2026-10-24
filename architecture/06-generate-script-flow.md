# 个人理解
group clip 可能会产生多个 group

script 会为每个 group 生成文案。

把 Agent 给出的文案风格、`understand_clips` 的素材语义和 `group_clips` 的叙事结构组合成专用 prompt

llm 根据 prompt 生成文案

# `storyline_generate_script`：把叙事分组写成具体文案

`storyline_group_clips` 回答的是：**入选片段按照什么顺序组成哪些叙事段落？**

`storyline_generate_script` 回答的是：**每个叙事段落应该配什么文字，整条视频叫什么？**

```text
group_clips 的输出                         generate_script 的输出
groups + 有序 clip_ids              ->     title + 每组 raw_text + subtitle_units
```

这一步不再改变镜头选择和排列，而是给已经确定的画面结构写文案。文案既可以作为字幕，也可以继续交给配音节点；生成文案本身不等于生成配音。

## 1. 它在真实链路中的位置

```text
understand_clips
    ↓ 提供画面 caption 和整体描述 overall
group_clips
    ↓ 提供有序 groups 和每组 clip_ids
generate_script
    ↓ 提供 title、group_scripts、subtitle_units
字幕样式 / 配音 / 时间线
```

这一段链路的职责可以压缩成三句话：

```text
Agent LLM：决定文案要写成什么风格
Script LLM：根据画面与分组写出每组 raw_text 和标题
程序代码：把 raw_text 拆成结构化 subtitle_units
```

## 2. Agent 怎样发起 `generate_script`

`group_clips` 返回“排序完成，形成 1 个分组”后，Agent LLM 的真实响应是：

```json
{
  "role": "assistant",
  "reasoning_content": "Now generate script. For a test video, generate a short script. We could use script_template_rec. Let me generate script with auto mode, casual style.\n\nActually, should I generate script? The user said no voiceover. But script could be used as subtitles. For a test video, subtitles are fine. But maybe keep it minimal. Let me generate a light script. Hmm, but \"test video\" 5 seconds. Let me generate a short casual script.\n\nLet me do generate_script.",
  "content": "排序完成，合成 1 个分组。接下来生成一段简短的文案（不需要配音，只作为字幕氛围）。",
  "tool_calls": [
    {
      "id": "call_00_r1AovxSPUcdmZPHQrO1R5638",
      "type": "function",
      "function": {
        "name": "storyline_generate_script",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"极简 Vlog 风格的短文案，口语、轻松，配合三个章节标题：早晨咖啡、城市漫步、日落休闲。\"}"
      }
    }
  ]
}
```

真正传给 MCP 工具的参数是：

```json
{
  "mode": "auto",
  "user_request": "极简 Vlog 风格的短文案，口语、轻松，配合三个章节标题：早晨咖啡、城市漫步、日落休闲。"
}
```

这条 `user_request` 不是原始用户逐字说出的要求。Agent 综合了：

- 用户要做一个测试视频。
- 用户不要配音。
- 素材是三个极简标题卡。
- group 已按早晨、城市、日落的顺序组织。

然后为文案能力整理出“极简、口语、轻松”的局部创作要求。

这里再次体现两层职责：

```text
Agent LLM：确定文案创作方向
Script LLM：在这个方向下写出具体句子
```

完整触发证据：

- [01-agent-trigger-request.json](evidence/generate-script/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/generate-script/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/generate-script/02-agent-trigger-response.parsed.json)

## 3. `generate_script` 为什么依赖三个上游节点

节点从不同上游结果中取得不同信息：

```text
split_shots
  └─ 每个 clip 的真实时长

understand_clips
  ├─ 每个 clip 的 caption
  └─ 全部素材的 overall

group_clips
  ├─ group_id
  ├─ group summary
  └─ 有序 clip_ids
```

三份数据组合后，文案模型才能知道：

- 这段文案对应哪个 group。
- group 中有哪些画面以及排列顺序。
- 所有素材整体在表达什么。
- 这组画面持续多久，文案最多适合写多长。

所以 `group_clips` 没有把前面信息全部复制到自己的 artifact；`generate_script` 会按 ID 回到相应上游结果中重新组合所需信息。

## 4. 文案长度预算怎样产生

程序先把一个 group 中所有 clip 的时长相加，再根据时长估算文字预算：

```text
最少字符数 = group 时长 × 3
最多字符数 = group 时长 × 5
```

本次只有一个 group：

```text
clip_0001  5 秒
clip_0002  5 秒
clip_0003  5 秒
----------------
group_0001 15 秒
```

因此得到：

```text
script_chars_budget: 45~75
```

这项预算的目的，是让文案长度与画面可用时长大致匹配，避免 5 秒画面配上一大段无法展示或念完的文字。

## 5. 节点内部的 LLM 请求是什么

下面是本次文案能力调用的完整请求。它不包含 Agent 的完整历史对话和 tools 列表，只包含文案能力自己的 system prompt，以及这次创作真正需要的数据：

```json
{
  "messages": [
    {
      "content": "# 角色设定\n你是一位资深的短视频及Vlog文案策划大师。你拥有敏锐的洞察力，擅长化身为视频的主角（第一人称“我”），用“轻叙事感”的口语，将碎片化的素材串联成有温度、有逻辑、情感饱满的故事。\n \n# 任务目标\n你的任务是根据用户提供的 [user_request]（视频核心主题）、[style]（文案风格）和 [group_infos]（分组素材详情），为每一个分组（Group）编写一段旁白文案，并为整个视频起一个标题。\n \n# 输入数据说明\n输入数据包含三部分：\n1. **[user_request]**: 视频的核心主题或创作者的感悟。\n2. **[overall]**: 用户提供的所有素材的总体叙事概述。\n3. **[style]**: 指定文案的风格偏好（如：文艺抒情、幽默有趣、日常碎碎念等）。\n4. **[group_infos]**: 包含多个分组，每个分组代表视频的一个段落。关键字段：\n   - `summary`: 该段落的叙事目的。\n   - `script_chars_budget`: **关键约束**。文案字数必须严格落在该区间内。\n   - `clips`: 该组包含的具体画面描述。\n \n# 风格效果配置 (Style Configuration)\n请根据输入中的 `[style]` 字段，严格采用对应的写作策略。如输入未指定，默认为**“日常碎碎念”**。\n \n1. **文艺抒情 (Lyrical & Poetic)**:\n   - **核心**: 治愈、浪漫、电影感、注重意象。\n   - **策略**: 弱化直白的动作描述，强化感官体验（光影、气味、温度、声音）。多用比喻和拟人，句式优美流畅，注重情感的流动和余韵，像在读一首散文诗。\n   \n2. **幽默有趣 (Humorous & Witty)**:\n   - **核心**: 玩梗（适度）、反转、自嘲、节奏轻快。\n   - **策略**: 寻找画面中意想不到的槽点或亮点。用词生动活泼，可以使用夸张的修辞，像个有趣的“显眼包”朋友在吐槽或分享趣事，拒绝沉闷。\n \n3. **日常碎碎念 (Daily Mumbling)**:\n   - **核心**: 真实、极度生活化、大脑独白、亲切。\n   - **策略**: 还原大脑里的真实想法，甚至可以有一点点逻辑跳跃。关注细枝末节（如“今天风有点大”），不刻意升华，主打一种“陪伴感”和“流水账”的真实美学。\n \n# 创作原则 (核心)\n请严格遵守以下创作原则，优先级从上到下：\n \n1. **主角视角与口吻 (Tone & Perspective)**:\n   - 全程使用**第一人称“我”**的视角叙事。\n   - 语言风格需符合上述 `[style]` 的设定，但必须保持**口语化**。\n   - **拒绝陈旧套路**: 开场**严禁**使用“家人们谁懂啊”、“姐妹们”等模板；结尾**严禁**使用“原来快乐如此简单”等空洞金句。\n \n2. **关键信息保真 (Information Fidelity)**:\n   - 必须敏锐识别并保留画面描述或主题中的**【专有名词】**（如品牌名、地名）、**【IP】**（如迪士尼）和**【具体事件】**。\n   - **不要泛化**: 结合具体的视觉元素（Visual）写作，切记胡编乱造。\n \n3. **字数与技术规范 (Technical Constraints)**:\n   - **字数严格控制**: 生成的 `raw_text` 长度必须严格落在 `script_chars_budget` 范围内。\n   - **标点符号限制**:\n     - **绝对禁止**使用任何形式的括号 `()` 或省略号 `...`。\n     - 标点需符合口语断句习惯。\n   - **Emoji使用**: 每段文案可适当使用 1 个与内容强相关的 Emoji。\n \n4. **画面关联与叙事 (Visual & Storytelling)**:\n   - **看图表达**: 文案必须是画面的注脚。\n   - **连贯性**: Group 之间要有逻辑衔接，使用自然的过渡词。\n   - **结构**:\n     - **开场 (Group 1)**: 迅速入题，根据风格设定基调。\n     - **结尾 (Last Group)**: 情感收束，根据风格进行升华或幽默收尾。\n \n5. **标题创作 (Title)**:\n   - 创作一个富有诗意、悬念感或总结性的 `title`，长度 8-15 字，需具备社交媒体（如小红书）的吸引力。\n \n# 输出格式\n请仅输出一个标准的 JSON 对象，不要包含 Markdown 符号，格式如下：\n \n```json\n{\n  \"group_scripts\": [\n    {\n      \"group_id\": \"对应输入的group_id\",\n      \"duration\": \"对应输入的duration_sec\",\n      \"raw_text\": \"生成的文案内容\"\n    }\n  ],\n  \"title\": \"生成的视频标题\"\n}\n```\n \n# 示例\n**Input:**\n[user_input]\n周末去公园野餐，感觉被治愈了\n[style]\n文艺抒情\n[group_infos]\n[group_id=group_0001]\nsummary: 展示准备食物和到达公园的过程。\nscript_chars_budget: 15~25\nclips: ...三明治特写，骑单车经过林荫道...\n \n**Output:**\n{\n  \"group_scripts\": [\n    {\n      \"group_id\": \"group_0001\",\n      \"duration\": 5.00,\n      \"raw_text\": \"带着手作的三明治，一头撞进这片绿色的风里🍃\"\n    }\n  ],\n  \"title\": \"想把自己寄给春天的风\"\n}",
      "role": "system"
    },
    {
      "content": "用户的风格需求: 极简 Vlog 风格的短文案，口语、轻松，配合三个章节标题：早晨咖啡、城市漫步、日落休闲。\n整体素材概述：这些素材由三张无实拍人物与动作的标题卡组成，分别显示“Morning_coffee”“City_walk”“Sunset_chill”，并采用明黄色、彩色条纹测试图、深灰蓝与橙红色块等不同视觉设计。整体上，它们更像是一组极简风格的Vlog章节片头或转场标题，按“早晨咖啡—城市漫步—日落休闲”的顺序暗示一天中的生活片段。\n\n从叙事角度看，素材本身没有具体人物、情节或场景变化，主要承担主题提示与氛围铺垫功能，整体风格简洁、平面化，情绪在明快与平静冷淡之间切换。\n\n分组输入开始：\n[group_id=group_0001]\nsummary: 以一天时间线串联三张极简标题卡：早晨咖啡、城市漫步、日落休闲，形成从清晨到黄昏的完整情绪推进。\nduration_sec: 15.00\nscript_chars_budget: 45~75\nclips:\n- clip_0001 (5.00s): 纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。\n- clip_0002 (5.00s): 画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。\n- clip_0003 (5.00s): 视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。\n分组输入结束\n\n完成以上任务。",
      "role": "user"
    }
  ],
  "model": "deepseek-v4-flash",
  "max_completion_tokens": 4096,
  "stream": true,
  "temperature": 0.1,
  "top_p": 0.9
}
```

### System prompt：定义文案写法

system prompt 规定了：

- 使用第一人称和口语化表达。
- 根据风格要求选择文艺、幽默或日常表达策略。
- 文案必须与实际画面相关，不能随意编造。
- 每组 `raw_text` 应符合对应的字符预算。
- 为整条视频生成一个标题。
- 返回结构化的 `group_scripts + title` JSON。

这些是 `generate_script` 能力自身的写作规则，不是 Agent 的通用 system prompt。

### User prompt：提供本次写作材料

user prompt 包含四类实际数据：

```text
Agent 整理的文案风格要求
+
understand_clips 的 overall
+
group_clips 的 summary 和有序 clip_ids
+
每个 clip 的 caption、duration 以及计算出的字符预算
```

文案模型不需要再次观看视频。它依据 `understand_clips` 已生成的文字描述来写作。

## 6. 文案 LLM 返回什么

原始 SSE 拼接后，`content` 中的完整业务 JSON 是：

```json
{
  "group_scripts": [
    {
      "group_id": "group_0001",
      "duration": 15.0,
      "raw_text": "早上先来杯 Morning_coffee，醒醒神。然后 City_walk 出门晃一圈，随便走走。到了 Sunset_chill，就坐着看天慢慢变橙。☕"
    }
  ],
  "title": "从早咖啡晃到日落时分"
}
```

这次只有一个 group，所以模型返回一条 `raw_text`：

```text
早上先来杯 Morning_coffee，醒醒神。
然后 City_walk 出门晃一圈，随便走走。
到了 Sunset_chill，就坐着看天慢慢变橙。
```

同时生成整条视频标题：

```text
从早咖啡晃到日落时分
```

模型返回中的 `duration` 用来表达它认为这段文案对应的时长，但当前节点后续主要提取的是 `group_id`、`raw_text` 和顶层 `title`；artifact 中没有保留模型返回的 `duration` 字段。

还可以观察到一个真实的中间处理：原始 HTTP 响应的 `raw_text` 末尾有 `☕`，最终 artifact 中没有。原因是 LLMClient 从 MCP sampling 响应提取文本时会调用 `EmojiManager.remove_emoji()`，所以节点实际解析 JSON 之前，emoji 已被移除。

另外，system prompt 要求文案严格符合 `45~75` 字符预算，但程序并没有对中文字符数执行严格的上下限验证。当前代码的超长保护使用空格分词数量进行判断，再按字符截断；对中文文本来说，这不等同于严格字符计数。本次去除 emoji 后的文案仍落在预算附近，没有触发截断。

完整响应：

- [04-script-llm-response.sse.txt](evidence/generate-script/04-script-llm-response.sse.txt)
- [04-script-llm-response.parsed.json](evidence/generate-script/04-script-llm-response.parsed.json)

## 7. `subtitle_units` 从哪里来

LLM 只返回整段 `raw_text`，并没有返回 `subtitle_units`。节点代码按逗号、句号、问号和感叹号等标点拆分文本：

```text
raw_text
  “早上先来杯 Morning_coffee，醒醒神。然后 City_walk……”
                         ↓ 按标点拆分
subtitle_0001  早上先来杯 Morning_coffee
subtitle_0002  醒醒神
subtitle_0003  然后 City_walk 出门晃一圈
...
```

本次得到 6 个 subtitle unit。`unit_id` 由程序按全局顺序生成，`index_in_group` 表示它在当前 group 内的位置。

因此需要明确区分：

```text
raw_text：LLM 写出来的内容
subtitle_units：程序根据 raw_text 拆出来的字幕结构
```

这不是第二次 LLM 调用。

## 8. 最终 artifact 保存了什么

本次 artifact 的完整内容是：

```json
{
  "payload": {
    "group_scripts": [
      {
        "group_id": "group_0001",
        "raw_text": "早上先来杯 Morning_coffee，醒醒神。然后 City_walk 出门晃一圈，随便走走。到了 Sunset_chill，就坐着看天慢慢变橙。",
        "subtitle_units": [
          {
            "unit_id": "subtitle_0001",
            "index_in_group": 0,
            "text": "早上先来杯 Morning_coffee"
          },
          {
            "unit_id": "subtitle_0002",
            "index_in_group": 1,
            "text": "醒醒神"
          },
          {
            "unit_id": "subtitle_0003",
            "index_in_group": 2,
            "text": "然后 City_walk 出门晃一圈"
          },
          {
            "unit_id": "subtitle_0004",
            "index_in_group": 3,
            "text": "随便走走"
          },
          {
            "unit_id": "subtitle_0005",
            "index_in_group": 4,
            "text": "到了 Sunset_chill"
          },
          {
            "unit_id": "subtitle_0006",
            "index_in_group": 5,
            "text": "就坐着看天慢慢变橙"
          }
        ]
      }
    ],
    "title": "从早咖啡晃到日落时分"
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "generate_script_1789240988.822574",
  "node_id": "generate_script",
  "create_time": 1789240992.0985181
}
```

其中：

- `title` 是整条视频标题。
- `group_scripts` 将文案绑定到对应 `group_id`。
- `raw_text` 保存经过 sampling 文本提取与 emoji 清理后的整段文案。
- `subtitle_units` 为后续字幕、配音和时间线提供更细的文本单元。

完整 artifact：[05-generate-script-artifact.json](evidence/generate-script/05-generate-script-artifact.json)。

## 9. 不要配音，为什么仍然生成 script

因为 script 有两种用途：

```text
script -> 直接作为视频字幕
script -> 继续交给 generate_voiceover 生成配音
```

用户说“不要配音”，只是否定第二条用途，不等于视频不能有字幕文案。

本次 Agent 在调用前也明确说：

```text
不需要配音，只作为字幕氛围
```

所以 `generate_script` 与“跳过配音”并不矛盾。

## 10. 文案结果怎样回到 Agent LLM

工具执行完成后，系统把 tool call 和 tool result 放回 Agent 对话。真实 request 最末尾两条消息是：

```json
[
  {
    "content": "排序完成，合成 1 个分组。接下来生成一段简短的文案（不需要配音，只作为字幕氛围）。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_r1AovxSPUcdmZPHQrO1R5638",
        "function": {
          "name": "storyline_generate_script",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"极简 Vlog 风格的短文案，口语、轻松，配合三个章节标题：早晨咖啡、城市漫步、日落休闲。\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '', 'preview_urls': [], 'artifact_id': 'generate_script_1789240988.822574'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_r1AovxSPUcdmZPHQrO1R5638"
  }
]
```

两条消息通过相同 ID 配对：

```text
assistant.tool_calls[0].id
    = call_00_r1AovxSPUcdmZPHQrO1R5638

tool.tool_call_id
    = call_00_r1AovxSPUcdmZPHQrO1R5638
```

这里有一个重要事实：这次 `tool` 消息的 `INFO_USER` 是空字符串，所以 Agent 没有直接收到生成出的文案正文，只收到执行成功状态和 `artifact_id`。完整文案保存在 artifact 中，供后续节点通过依赖关系读取。

完整回送请求：[06-agent-resume-request.json](evidence/generate-script/06-agent-resume-request.json)。

## 11. Agent 下一步做了什么

Agent 收到 `generate_script` 完成结果后，真实响应是：

```json
{
  "role": "assistant",
  "reasoning_content": "Let me continue. Now element recommendation: text (花字) and transition. Let me do text effects first, then transition.\n\nActually let me check script output. It's empty summary. Let me just proceed. Element rec text.",
  "content": "文案生成好了。接下来推荐字幕字体样式。",
  "tool_calls": [
    {
      "id": "call_00_7EQkvw0GBuvG8MmqUMns6619",
      "type": "function",
      "function": {
        "name": "storyline_elementrec_text",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"极简、干净的字体风格，适合 Vlog 标题。\"}"
      }
    }
  ]
}
```

这次下一步是 `storyline_elementrec_text`，用于推荐字幕文字样式。它不是 `generate_script` 内部自动触发的，仍然是 Agent LLM 根据完整剪辑流程选择的新 tool call。

完整下一步响应：

- [07-agent-next-response.sse.txt](evidence/generate-script/07-agent-next-response.sse.txt)
- [07-agent-next-response.parsed.json](evidence/generate-script/07-agent-next-response.parsed.json)

## 12. 哪些情况下不调用文案 LLM

节点并非每次都会调用内部 LLM：

- 没有可用 group：返回空 `group_scripts` 和空标题。
- `mode: default`：使用默认处理，返回空文案结构。
- 提供格式有效的 `custom_script`：直接使用用户编辑后的文案，并由程序补充 `subtitle_units`。
- `mode: skip`：由节点执行框架跳过文案生成。

因此，调用 `storyline_generate_script` 不一定等于内部必然发起一次 LLM 请求。本次是 `mode: auto` 且没有 `custom_script`，所以真实调用了文案 LLM。

## 13. 用一句话理解整个过程

`storyline_generate_script` 把 Agent 给出的文案风格、`understand_clips` 的素材语义和 `group_clips` 的叙事结构组合成专用 prompt，让文本 LLM 为每个 group 写出 `raw_text` 并生成标题，再由程序把文本按标点拆成可供字幕、配音和时间线使用的 `subtitle_units`。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 23 至 28 行、`generate_script` artifact 和当前节点源码。正文保留理解关系所需的完整可读原文；完整 Agent 请求、原始 SSE 与解析后响应存放在 [evidence/generate-script](evidence/generate-script/) 中。
