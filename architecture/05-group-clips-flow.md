# 个人理解
执行 fliter 之后 agent llm 返回了【高层叙事逻辑】，例如：【"按一天的时间顺序排列：早晨咖啡 → 城市漫步 → 日落休闲】

group 这一层会调用 llm，把叙事逻辑落实为具体 clip 编排

group clip 可能最终会得到多个 group 结构

# `storyline_group_clips`：把入选片段组织成叙事结构

`storyline_filter_clips` 回答的是：**哪些 clip 继续参与剪辑？**

`storyline_group_clips` 回答的是：**这些入选 clip 应该按照什么顺序出现，又应该怎样组成叙事组？**

```text
filter_clips 的输出                       group_clips 的输出
selected clip_id 列表              ->     有顺序的 groups
```

这一步不再决定删除哪些素材，也不会物理拼接视频。它把已入选的片段组织成一份结构化叙事方案，供后面的文案生成和时间线规划使用。

## 1. 它在真实链路中的位置

```text
understand_clips
    ↓ 产生每个 clip 的 caption
filter_clips
    ↓ 产生 selected，确定参与后续剪辑的 clips
group_clips
    ↓ 产生 groups，确定顺序和叙事分组
generate_script
```

筛选和分组的职责不能混在一起：

```text
filter_clips：决定“用谁”
group_clips：决定“怎么排列、怎么成组”
```

本次真实执行仍然包含两种 LLM 调用：

```text
Agent LLM
  └─ 决定调用 storyline_group_clips，并给出排序要求
       └─ group_clips 内部文本 LLM
            └─ 根据 selected clips 的描述和时长生成 groups
       └─ 程序校验并规范化 groups
Agent LLM
  └─ 收到工具结果，再决定调用 storyline_generate_script
```

## 2. Agent 怎样发起 `group_clips`

`filter_clips` 返回“3 个片段全部保留”后，Agent LLM 的真实响应是：

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

真正交给 MCP 工具的参数是：

```json
{
  "mode": "auto",
  "user_request": "按一天的时间顺序排列：早晨咖啡 → 城市漫步 → 日落休闲。"
}
```

这里的 `user_request` 是 Agent 为分组能力整理出的任务说明。原始用户只要求制作一个 5 秒测试视频，并没有直接说出这条时间顺序；Agent 是根据 `understand_clips` 得到的三个标题卡含义，推导出“早晨 → 城市 → 日落”的排列要求。

因此，这一层关系是：

```text
原始用户需求 + 已有工具结果
              ↓ Agent LLM 整理
group_clips 专用 user_request
```

完整触发证据：

- [01-agent-trigger-request.json](evidence/group-clips/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/group-clips/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/group-clips/02-agent-trigger-response.parsed.json)

## 3. `group_clips` 从 `filter_clips` 读取什么

`filter_clips` artifact 中有两类数据：

```text
clip_captions：全部候选 clip 的描述与时长
selected：真正入选的 clip_id
```

`group_clips` 先以 `selected` 为准，再去 `clip_captions` 中查找这些 ID 对应的信息：

```text
selected = [clip_0001, clip_0002, clip_0003]
                         ↓ 查表
[
  clip_id + duration + caption,
  clip_id + duration + caption,
  clip_id + duration + caption
]
```

所以，没有进入 `selected` 的 clip，即使仍然保存在 `clip_captions` 中，也不会发送给分组 LLM。

与前一步 `filter_clips` 不同，`group_clips` 的真实请求确实把 `duration` 写进了 prompt。模型实际可以同时依据：

- `clip_id`：识别并返回片段。
- `caption`：判断内容、场景和叙事关系。
- `duration`：估算每组时长。
- `user_request`：确定本次排序目标。

## 4. 节点内部的 LLM 请求是什么

下面是本次分组能力调用的完整请求。它只包含 `group_clips` 专用 system prompt 和本次 selected clips，不包含 Agent 的完整历史对话、tools 列表或视频画面：

```json
{
  "messages": [
    {
      "content": "# 角色设置\n你是一位拥有顶级叙事逻辑且**极具共情能力**的**资深视频剪辑导演**。你擅长将零散的素材重构为有吸引力的故事。你的核心能力在于：\n1.  **意图洞察**：透过简单的 `user_request` 捕捉深层叙事策略。\n2.  **节奏与连贯性把控**：你不仅控制时长，更注重**视觉流的顺畅**。你极度反感在同一场景或同一主体之间无意义的反复横跳，你追求场景的“打包”呈现以维持沉浸感。\n\n# 核心任务\n1.  **全量组织与排序**：`clip_captions` 中提供的所有片段都是**必须使用**的。你的任务是将这些预选好的片段根据叙事逻辑重新排序，**不可遗漏**。\n2.  **智能分组**：将片段划分为若干个 narrative group（叙事组），并计算每组的总时长。\n3.  **结构化输出**：进行思维推演，并将推演过程与分组结果整合成一个标准的 JSON 输出。\n\n# 输入信息\n1.  **user_request**: 视频核心主题与指令（最高叙事准则）。\n2.  **clip_captions**: 包含 `clip_id`、`caption`（内容描述）和 `duration`（时长/秒）的**预选片段列表**（这些片段即为最终视频的全部素材）。\n3.  **clip_number**: 输入片段的总数量。\n\n# 工作流与逻辑规则 (最高优先级)\n\n## 第一层：叙事重构与视觉连贯（核心逻辑）\n1.  **意图优先**：\n    *   若 `user_request` 包含特定结构（如“倒叙”），优先满足。\n    *   否则采用：**Hook（吸睛） → Core（展示） → Vibe（场景/氛围） → End（收尾）**。\n\n2.  **场景聚合原则 (Scene Aggregation) ⚠️重要⚠️**：\n    *   **同场景打包**：仔细阅读 `caption`，将**背景环境相同**（如都是“纯白背景”或都是“街头”）或**模特着装/主体状态一致**的片段视为一个“视觉单元”。\n    *   **禁止反复横跳**：严禁出现 `场景A -> 场景B -> 场景A` 的排序（除非用户明确要求“平行剪辑”或“对比蒙太奇”，或作为片头Hook/片尾End的特殊需要）。\n    *   **逻辑**：如果必须展示多个场景，请处理完一个场景的所有镜头后，再切换到下一个场景（例如：先播完所有室内白棚镜头，再进入室外街拍镜头）。\n\n## 第二层：分组与时长约束（⚠️重点约束⚠️）\n你必须严格遵守以下分组规则，以保证视频节奏：\n\n1.  **合并逻辑 (Merging Logic)**：\n    *   **相似性合并**：优先将**视觉基调相似**（光影、颜色、环境）的片段合并在同一组。\n    *   **动作连贯**：若多个片段展示了同一个连续动作的分解（如：拿出背包->背上背包->转身），必须将其按顺序合并在同一组或相邻组。\n\n2.  **数量约束 (Quantity Constraints)**：\n    *   **单组片段数**：严格控制 **2-4个** 片段为一组。\n    *   **例外情况**：长镜头允许 1 个独立成组（时长>10s）。\n    *   **禁止碎片化**：严禁将本该连贯的同一场景拆得过于细碎。\n\n3.  **时长约束 (Duration Constraints)**：\n    *   **单组总时长**：建议控制在 **3秒 - 20秒** 之间。\n        *   < 3秒：除非是快速闪切，否则太短看不清。\n        *   > 20秒：观众容易疲劳，必须拆分（但拆分后的两组仍应保持场景相邻）。\n    *   **计算规则**：精确累加组内 `clip` 的 `duration`，保留1位小数。\n\n# 输出规范\n\n请直接输出一个标准的 JSON 对象，不要包含任何 Markdown 代码块标记（如 ```json ... ```）以外的额外文本。JSON 需包含以下两个核心字段：\n\n1.  **`think`**：你的思考过程字符串。必须包含【意图与基调】、【场景梳理】、【分组策略】、【核心文案】四个维度的分析（300字以内）。\n2.  **`groups`**：最终的分组列表。\n\n**JSON 结构定义：**\n```json\n{\n  \"think\": \"【意图与基调】...\\\\n【场景梳理】关键步骤：分析素材包含哪几个主要场景...明确场景切换顺序...\\\\n【分组策略】解释如何基于“场景聚合”进行分组...\\\\n【核心文案】一句提炼文案。\",\n  \"groups\": [\n    {\n      \"group_id\": \"group_0001\",\n      \"summary\": \"极具画面感的叙事或场景描述（50字以内）。\",\n      \"clip_ids\": [\n        \"clip_ID_1\",\n        \"clip_ID_2\"\n      ],\n      \"duration\": \"X.Xs\" \n    },\n    {\n      \"group_id\": \"group_0002\",\n      \"summary\": \"...\",\n      \"clip_ids\": [\"...\", \"...\"],\n      \"duration\": \"...\"\n    }\n  ]\n}\n```\n\n---\n**示例输入：**\n`user_request`: 剪一个背包的广告视频\n`clip_captions`: (假设输入了3个片段：clip_0001是室内白棚，clip_0002是户外，clip_0003是室内白棚)\n`clip_number`: 3\n\n**示例输出：**\n\n```json\n{\n  \"think\": \"【意图与基调】用户需要简洁的背包展示。风格应干净、利落。\\\\n【场景梳理】素材包含“室内白棚”和“户外”两个场景。为了视觉连贯，必须避免 室内->户外->室内 的跳变。\\\\n【分组策略】决定先集中展示室内白棚素材（Clip 1, Clip 3），利用纯净背景突出产品细节；然后再切换到户外（Clip 2）展示生活感。因此，Group 1 合并 Clip 1 和 Clip 3，Group 2 放置 Clip 2。\\\\n【核心文案】从细节到远方，随心而行。\",\n  \"groups\": [\n    {\n      \"group_id\": \"group_0001\",\n      \"summary\": \"室内纯净展示：聚合室内白棚场景，通过不同景别展示背包的静态细节与模特手持姿态，建立纯净的产品初印象。\",\n      \"clip_ids\": [\n        \"clip_0001\",\n        \"clip_0003\"\n      ],\n      \"duration\": \"5.1s\"\n    },\n    {\n      \"group_id\": \"group_0002\",\n      \"summary\": \"户外场景切换：切换至户外场景，展示模特背负效果，通过场景转换带入生活氛围。\",\n      \"clip_ids\": [\n        \"clip_0002\"\n      ],\n      \"duration\": \"4.3s\"\n    }\n  ]\n}\n```",
      "role": "system"
    },
    {
      "content": "用户要求: 按一天的时间顺序排列：早晨咖啡 → 城市漫步 → 日落休闲。\n以下是clips详细信息: [{'clip_id': 'clip_0001', 'duration': 5.0, 'caption': '纯色标题卡画面：明黄色背景上绘有一个浅色细线矩形边框，中央居中显示白色无衬线英文字样“Morning_coffee”。画面无人物、无动作与表情，全程静止无变化，属Vlog片头字幕卡，整体风格简洁明快。'}, {'clip_id': 'clip_0002', 'duration': 5.0, 'caption': '画面为彩色条纹测试图案，包含红绿黄蓝品红青六色竖条；中央有白色“City_walk”字样，彩色斜线从左上向右下扫过，左侧有灰色横条与十字/方块图形，左上角显示时间码。无人物出现，因此无动作和面部表情。'}, {'clip_id': 'clip_0003', 'duration': 5.0, 'caption': '视频片段为静态标题画面，无实拍人物或场景。画面由上下两色块横向分割构成：上部为深灰蓝色，下部为橙红色。中央居中显示白色无衬线文字“Sunset_chill”。镜头固定无运动，无场景切换，整体呈极简平面设计风格，情绪平静冷淡。'}]\n总计片段个数为: 3",
      "role": "user"
    }
  ],
  "model": "deepseek-v4-flash",
  "max_completion_tokens": 4240,
  "stream": true,
  "temperature": 0.1,
  "top_p": 0.9
}
```

这份请求分成两个部分。

### System prompt：定义怎样组织片段

system prompt 要求模型：

- 使用全部预选片段，不允许主动筛掉片段。
- 根据用户要求或默认叙事逻辑重新排序。
- 尽量把相同场景、相同主体状态的片段放在一起。
- 避免 `场景 A → 场景 B → 场景 A` 式来回跳转。
- 把片段组成 narrative groups，并考虑每组数量和总时长。
- 返回包含 `think` 和 `groups` 的 JSON。

这些规则属于 `group_clips` 能力本身，不是 Agent 的通用 system prompt。

### User prompt：提供本次素材和排序目标

user prompt 提供了：

```text
排序要求：早晨咖啡 → 城市漫步 → 日落休闲
+
3 个 selected clip 的 clip_id、duration、caption
+
片段总数 3
```

模型不需要再次观看视频，因为 `understand_clips` 已经把画面转换成 caption；它在这一阶段处理的是文字化的素材语义。

## 5. 分组 LLM 返回什么

原始 SSE chunk 拼接后，`content` 中的完整业务 JSON 是：

```json
{
  "think": "【意图与基调】用户要求按一天时间顺序排列，核心是清晨到日落的轻盈Vlog节奏，整体简洁明快。\\n【场景梳理】素材为三张标题卡：clip_0001“Morning_coffee”、clip_0002“City_walk”、clip_0003“Sunset_chill”，分别对应早晨咖啡、城市漫步、日落休闲，无实拍场景冲突。\\n【分组策略】三段各5秒，单独成组过短且不符合2-4片段约束；三者在时间逻辑上连续，视觉均为极简标题卡，因此合并为一个叙事组，按晨→昼→暮顺序排列。\\n【核心文案】从晨间咖啡到城市漫步，再到日落松弛。",
  "groups": [
    {
      "group_id": "group_0001",
      "summary": "以一天时间线串联三张极简标题卡：早晨咖啡、城市漫步、日落休闲，形成从清晨到黄昏的完整情绪推进。",
      "clip_ids": [
        "clip_0001",
        "clip_0002",
        "clip_0003"
      ],
      "duration": "15.0s"
    }
  ]
}
```

`think` 是模型对叙事与分组的分析；节点最终保存的业务结果主要是 `groups`。

每个 group 包含：

- `group_id`：组标识。
- `summary`：这一组承担的内容或叙事作用。
- `clip_ids`：组内 clip 的顺序。
- `duration`：模型计算的组总时长。

本次模型只生成一个组：

```text
group_0001
  clip_0001  Morning_coffee
  clip_0002  City_walk
  clip_0003  Sunset_chill
```

这里的数组顺序本身就是排序结果，不只是集合关系。

完整响应：

- [04-group-llm-response.sse.txt](evidence/group-clips/04-group-llm-response.sse.txt)
- [04-group-llm-response.parsed.json](evidence/group-clips/04-group-llm-response.parsed.json)

## 6. 程序怎样约束模型返回的 groups

模型返回 JSON 后，程序还会执行一层规范化：

```text
LLM groups
    ↓ 丢弃不在 selected 中的 clip_id
    ↓ 去除跨组重复的 clip_id
    ↓ 空 summary 使用默认描述
    ↓ group_id 统一重写为 group_0001、group_0002……
规范化后的 groups
```

这意味着模型不能通过编造 ID，把未入选的素材重新带回流程；同一个 clip 也不会因为模型重复输出而同时出现在多个组中。

不过当前程序并没有反向检查“每个 selected clip 是否都被模型返回”。system prompt 明确要求全部使用，但代码主要约束无效 ID 和重复 ID。如果模型遗漏某个有效 selected clip，当前规范化逻辑不会自动补回。本次真实响应没有这个问题，三个 selected clip 全部出现。

## 7. `groups` 是否已经改变视频文件

没有。`group_clips` 生成的是逻辑结构：

```json
{
  "group_id": "group_0001",
  "clip_ids": ["clip_0001", "clip_0002", "clip_0003"]
}
```

它表示后续应该按照这个顺序理解和使用片段，但此时：

- 视频文件没有被移动。
- clip 文件没有被拼接。
- 时间线还没有生成。
- 最终视频也没有渲染。

真正把这些结构落实到画面时间轴，是更后面的 `plan_timeline` 阶段。

## 8. 最终 artifact 保存了什么

本次 `group_clips` artifact 的完整内容是：

```json
{
  "payload": {
    "groups": [
      {
        "group_id": "group_0001",
        "summary": "以一天时间线串联三张极简标题卡：早晨咖啡、城市漫步、日落休闲，形成从清晨到黄昏的完整情绪推进。",
        "clip_ids": [
          "clip_0001",
          "clip_0002",
          "clip_0003"
        ],
        "duration": "15.0s"
      }
    ]
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "group_clips_1789240983.9655333",
  "node_id": "group_clips",
  "create_time": 1789240987.5336463
}
```

可以看到，artifact 只保存规范化后的 `groups`，没有保存模型的 `think`。因此：

```text
LLM content：think + groups
节点 artifact：groups
```

完整 artifact：[05-group-clips-artifact.json](evidence/group-clips/05-group-clips-artifact.json)。

## 9. 分组结果怎样回到 Agent LLM

工具执行完后，系统把对应的 tool call 和 tool result 放回 Agent 对话。真实 request 最末尾两条消息是：

```json
[
  {
    "content": "3 个片段全部保留。接下来按「早晨咖啡 → 城市漫步 → 日落休闲」的顺序做分组排序。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_AFFMMX8bMIGkZJtf9bBn3677",
        "function": {
          "name": "storyline_group_clips",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"按一天的时间顺序排列：早晨咖啡 → 城市漫步 → 日落休闲。\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:07.527672] Grouping successful: 1 groups in total', 'preview_urls': [], 'artifact_id': 'group_clips_1789240983.9655333'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_AFFMMX8bMIGkZJtf9bBn3677"
  }
]
```

两条消息通过相同 ID 配对：

```text
assistant.tool_calls[0].id
    = call_00_AFFMMX8bMIGkZJtf9bBn3677

tool.tool_call_id
    = call_00_AFFMMX8bMIGkZJtf9bBn3677
```

Agent 收到的是“分组成功”和 `artifact_id`，不是完整 groups。完整回送请求见 [06-agent-resume-request.json](evidence/group-clips/06-agent-resume-request.json)。

## 10. Agent 为什么继续调用 `generate_script`

Agent 收到分组成功的结果后，真实响应是：

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

这说明 `group_clips` 不会在内部自动生成文案。完整边界是：

```text
group_clips 生成叙事分组
        ↓
结果作为 role=tool 的消息返回 Agent LLM
        ↓
Agent LLM 根据流程提示决定下一步
        ↓
返回 storyline_generate_script tool_call
```

完整下一步响应：

- [07-agent-next-response.sse.txt](evidence/group-clips/07-agent-next-response.sse.txt)
- [07-agent-next-response.parsed.json](evidence/group-clips/07-agent-next-response.parsed.json)

## 11. 异常情况下怎样处理

节点不是无条件相信一次模型响应：

- `selected` 为空时，直接返回空 groups。
- selected clip 找不到对应 caption 时，退回为单一 group。
- LLM 请求失败或 JSON 无法解析时，会重试。
- 所有尝试都失败后，把全部 selected clips 按原顺序放入一个默认 group。

所以，即使分组模型失败，节点仍尽量保留 `filter_clips` 已经选中的片段，而不是让后续流程完全失去素材。

## 12. 用一句话理解整个过程

`storyline_group_clips` 把 `filter_clips` 选中的片段及其 caption、duration 交给一个独立文本 LLM，让模型按照 Agent 整理出的叙事要求生成有顺序的 groups；程序随后清理无效 ID、去重并统一 group_id，但不会在这一阶段操作视频文件。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 19 至 24 行、`group_clips` artifact 和当前节点源码。正文保留理解关系所需的完整可读原文；完整 Agent 请求、原始 SSE 与解析后响应存放在 [evidence/group-clips](evidence/group-clips/) 中。
