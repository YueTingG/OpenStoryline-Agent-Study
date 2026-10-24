# `storyline_elementrec_transition`：为整条视频添加首尾淡入淡出

`storyline_elementrec_transition` 这个名字容易让人以为它会分析相邻片段，再为每两个片段挑选一种转场。

当前实现并不是这样。它的实际作用是：

```text
整条成片开始时淡入
+
整条成片结束时淡出
```

它不在 `clip_0001 → clip_0002`、`clip_0002 → clip_0003` 之间添加转场，也不调用 LLM 选择转场类型。

## 1. 它在本次真实链路中的位置

```text
elementrec_text
    ↓ 字体配置完成
elementrec_transition
    ↓ 生成整条视频的首尾淡入淡出配置
generate_voiceover
    ↓ 本次按用户要求跳过
select_bgm
    ↓ 选择背景音乐
plan_timeline_pro
    ↓ 生成时间线
render_video
    ↓ 真正把淡入淡出应用到成片
```

本次 session 是：

```text
e78297ae266440caac2cac2dc5575c51
```

## 2. Agent 怎样发起这次调用

`elementrec_text` 执行完成后，Agent LLM 收到的摘要包含：

```text
[elementrec_text] Use font `SourceHanSansSC`
```

随后 Agent LLM 返回：

```json
{
  "role": "assistant",
  "reasoning_content": "Now transitions.",
  "content": "字体样式确定。接下来推荐转场效果。",
  "tool_calls": [
    {
      "id": "call_00_low1ztIOXt2PSmCCdUpa0517",
      "type": "function",
      "function": {
        "name": "storyline_elementrec_transition",
        "arguments": "{\"mode\": \"auto\", \"duration\": 500}"
      }
    }
  ]
}
```

真正传给 MCP tool 的参数是：

```json
{
  "mode": "auto",
  "duration": 500
}
```

两个参数的含义是：

```text
mode = auto    使用当前节点的自动处理逻辑
duration = 500 淡入和淡出各持续 500 毫秒
```

原始用户只要求制作 5 秒测试视频，并没有指定转场类型或时长。`500` 毫秒是 Agent LLM 在调用 tool 时自行给出的参数。

完整触发证据：

- [01-agent-trigger-request.json](evidence/elementrec-transition/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/elementrec-transition/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/elementrec-transition/02-agent-trigger-response.parsed.json)

## 3. Agent 没传 `group_clips`，节点为什么能拿到它

因为 `group_clips` 的结果已经保存在当前 session 的 artifact 中。

后端的查找过程是：

```text
session_id
确定查哪个会话
    ↓
node_id = group_clips
确定查哪个节点
    ↓
在当前 session 已保存的 artifact 记录中
找到该节点最新的一次执行结果
    ↓
从记录中取得 artifact_id 和 path
    ↓
通过 path 打开 artifact 文件
    ↓
读取 payload，获得具体的 group_clips 结果
```

本次找到的是：

```text
artifact_id = group_clips_1789240983.9655333
path = /app/outputs/e78297ae266440caac2cac2dc5575c51/group_clips/group_clips_1789240983.9655333.json
```

它的 `payload` 包含一个 group：

```text
group_0001
    ├─ clip_0001
    ├─ clip_0002
    └─ clip_0003
```

后端把这份 payload 作为 `group_clips` 字段，与其他输入合并：

```text
Agent LLM 提供：
mode = auto
duration = 500

后端生成：
artifact_id = elementrec_transition_1789240996.9224887
lang = zh

从已有 artifact 读取：
group_clips = group_clips artifact 的 payload
```

所以节点最终拿到的字段是：

```text
mode
duration
artifact_id
lang
group_clips
```

本质上就是：

> `session_id` 确定哪个会话，`node_id` 确定查 `group_clips` 节点；后端找到该节点最新的一次执行结果，再通过保存的文件路径读取具体内容。

实际文件：

- [group_clips 来源 artifact](evidence/elementrec-transition/03-group-clips-source-artifact.json)
- [elementrec_transition 最终输入](evidence/elementrec-transition/04-node-resolved-input.json)

## 4. 节点有没有根据三个 clip 推荐三个转场

没有。

虽然节点收到了 `group_clips`，但当前处理逻辑没有读取 `groups`、`group_id` 或 `clip_ids`。它只读取：

```text
duration
```

然后直接生成两个固定配置：

```json
[
  {
    "type": "fade_in",
    "position": "opening",
    "duration": 500
  },
  {
    "type": "fade_out",
    "position": "ending",
    "duration": 500
  }
]
```

因此，本次三个 clip 的关系是：

```text
成片开头        clip_0001 → clip_0002 → clip_0003        成片结尾
  fade_in             中间仍然直接衔接                    fade_out
   500ms                                                     500ms
```

`duration: 500` 同时用于开头淡入和结尾淡出，不是把 500 毫秒分成两份。

## 5. 这一步有没有调用内部 LLM

没有。

字体推荐的结构是：

```text
Agent LLM 选择 elementrec_text
    ↓
elementrec_text 内部再次调用 Font LLM
    ↓
Font LLM 从候选字体中选择 font_name
```

转场推荐的结构是：

```text
Agent LLM 选择 elementrec_transition，并给出 duration
    ↓
节点代码固定生成 fade_in + fade_out
```

所以这次 trace 中，Agent 的 tool call 之后立刻出现 tool result；两者之间没有第二组转场推荐 LLM request/response。

## 6. artifact 保存了什么

节点输出被保存为：

```json
{
  "payload": [
    {
      "type": "fade_in",
      "position": "opening",
      "duration": 500
    },
    {
      "type": "fade_out",
      "position": "ending",
      "duration": 500
    }
  ],
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "elementrec_transition_1789240996.9224887",
  "node_id": "elementrec_transition",
  "create_time": 1789240996.9579668
}
```

这里的 `payload` 只是转场配置，不是已经处理过的视频，也没有生成新的转场视频文件。

完整 artifact：[05-elementrec-transition-artifact.json](evidence/elementrec-transition/05-elementrec-transition-artifact.json)。

## 7. tool result 怎样返回 Agent LLM

节点执行完成后，完整 artifact 留在后端。下一次请求 Agent LLM 时，末尾追加的是摘要形式的 `tool` message：

```json
[
  {
    "content": "字体样式确定。接下来推荐转场效果。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_low1ztIOXt2PSmCCdUpa0517",
        "function": {
          "name": "storyline_elementrec_transition",
          "arguments": "{\"mode\": \"auto\", \"duration\": 500}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:16.953046] [elementrec_transition] Adding fade transitions: 500ms fade-in at start, 500ms fade-out at end', 'preview_urls': [], 'artifact_id': 'elementrec_transition_1789240996.9224887'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_low1ztIOXt2PSmCCdUpa0517"
  }
]
```

相同的 ID 将调用和结果配对：

```text
assistant.tool_calls[0].id
    = call_00_low1ztIOXt2PSmCCdUpa0517

tool.tool_call_id
    = call_00_low1ztIOXt2PSmCCdUpa0517
```

Agent LLM 直接得到的信息是：

```text
执行成功
开头添加 500ms fade-in
结尾添加 500ms fade-out
完整结果的 artifact_id
```

它没有在 `tool_excute_result` 中再次收到完整的两个配置对象。完整回送请求见 [06-agent-resume-request.json](evidence/elementrec-transition/06-agent-resume-request.json)。

## 8. 淡入淡出什么时候真正作用于视频

`elementrec_transition` 只生成配置。真正处理画面的是后面的 `render_video`。

渲染阶段先把时间线中的视频片段按顺序拼成一条完整的基础视频，然后读取 `transition_rec`：

```text
时间线中的所有视频片段
    ↓
拼成一条完整 base video
    ↓
读取 elementrec_transition artifact
    ↓
对整个 base video 应用 FadeIn(0.5s)
    ↓
对整个 base video 应用 FadeOut(0.5s)
```

这解释了为什么 `position` 是：

```text
opening
ending
```

而不是某两个 `clip_id`。当前配置描述的是整条成片的边界，不是片段之间的连接点。

如果目标是在相邻 clip 之间生成过渡视频，那是另一个能力 `generate_ai_transition`，不属于本次 `elementrec_transition` 调用。

## 9. `mode` 的实际行为

tool schema 暴露三个值：

```text
auto
skip
default
```

当前代码的真实行为是：

```text
mode = auto
    → 返回 fade_in + fade_out

mode = skip
    → 返回空列表，不使用转场

mode = default
    → 同样返回空列表，不使用转场
```

schema 对 `default` 的描述是“Use default transitions”，但当前实现中，所有非 `auto` 模式都会进入同一个 `default_process`，结果都是空列表。这是 schema 描述与实际实现不一致的地方。

`duration` 默认值是 `1000` 毫秒；本次 Agent 显式传入 `500`，所以没有使用默认值。

## 10. 当前实现边界

当前 tool 的 description 提到根据用户需要和片段数量推荐转场，但实际实现存在明确边界：

- 没有 `user_request` 参数。
- 没有转场候选集合。
- 不调用 LLM。
- 不分析画面内容。
- 不读取相邻 `clip_id`。
- 不根据 group 或 clip 数量改变输出。
- 固定输出整条视频的开头淡入和结尾淡出。

因此，理解这个节点时应以真实输入、代码行为和 artifact 为准，不能只根据 tool 名称中的 `recommend` 或 description 推断能力。

## 11. Agent 下一步做了什么

Agent 收到转场执行摘要后，下一次真实响应是：

```json
{
  "role": "assistant",
  "reasoning_content": "Now voiceover skip. Then BGM select. Then plan timeline with 5 second target? The timeline is fixed tool but has parameters. 5-second total. Hmm plan_timeline doesn't have duration parameter. It says image_duration_ms for AI transition branch. These are videos though.\n\nLet me do generate_voiceover with mode skip. Then select_BGM. Then plan_timeline.\n\nLet me also consider the \"5秒\" requirement - timeline arranges clips. Total 5 seconds might be too short for 3 clips. But let's just proceed. Maybe the timeline tool will handle.\n\nLet me do voiceover skip.",
  "content": "按你的要求，跳过配音。",
  "tool_calls": [
    {
      "id": "call_00_qpH1vZqMScuCz6JzZXTj4670",
      "type": "function",
      "function": {
        "name": "storyline_generate_voiceover",
        "arguments": "{\"mode\": \"skip\"}"
      }
    }
  ]
}
```

因此下一步是 `storyline_generate_voiceover`，并按原始用户要求使用 `mode: skip`。`elementrec_transition` 本身不会自动调用配音工具；下一步仍由 Agent LLM 决定。

完整响应：

- [07-agent-next-response.sse.txt](evidence/elementrec-transition/07-agent-next-response.sse.txt)
- [07-agent-next-response.parsed.json](evidence/elementrec-transition/07-agent-next-response.parsed.json)

## 12. 用一句话理解整个过程

`storyline_elementrec_transition` 接收 Agent 给出的持续时间，由节点代码固定生成整条成片的开头淡入和结尾淡出配置，保存为 artifact，最后由 `render_video` 应用到已经拼接完成的整条视频；它没有为相邻 clips 推荐转场，也没有调用内部 LLM。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 31 至 34 行、节点解析后的完整输入、`elementrec_transition` artifact 和当前节点/渲染源码。完整 Agent 请求、原始 SSE、解析后响应、节点输入和 artifact 存放在 [evidence/elementrec-transition](evidence/elementrec-transition/) 中。
