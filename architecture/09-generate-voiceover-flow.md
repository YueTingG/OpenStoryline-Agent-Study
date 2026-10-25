# `storyline_generate_voiceover`：生成配音，或明确跳过配音

`storyline_generate_voiceover` 处理的是已经生成好的解说文案：

```text
generate_script 产生 raw_text
              ↓
generate_voiceover 决定是否把 raw_text 合成为语音
              ↓
生成配音 WAV，或者返回空的 voiceover 列表
```

本次真实调用选择的是 `mode: "skip"`，因此没有生成语音。理解这一点很重要：本文的真实请求与响应展示的是“跳过配音”；`auto` 模式的生成流程只作为代码逻辑的对照说明。

## 1. 它在本次真实链路中的位置

```text
elementrec_transition
    ↓ 已生成整条视频的首尾淡入淡出配置
generate_voiceover
    ↓ 本次返回 voiceover = []
select_bgm
    ↓ 选择背景音乐
plan_timeline_pro
    ↓ 编排视频、字幕、音频时间线
render_video
    ↓ 输出最终视频
```

本次 session 是：

```text
e78297ae266440caac2cac2dc5575c51
```

## 2. Agent 为什么选择跳过配音

原始用户请求是：

```text
基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。
```

其中已经明确包含“不要配音”。在 `elementrec_transition` 执行完成后，Agent LLM 返回：

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

真正传给 MCP tool 的参数只有：

```json
{
  "mode": "skip"
}
```

所以这次不是节点自己判断不需要配音，而是 Agent LLM 根据原始用户要求，主动选择 `storyline_generate_voiceover`，并明确传入 `skip`。

完整触发证据：

- [01-agent-trigger-request.json](evidence/generate-voiceover/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/generate-voiceover/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/generate-voiceover/02-agent-trigger-response.parsed.json)

## 3. 节点最终拿到了什么输入

Agent 只传了 `mode: "skip"`，但后端知道 `generate_voiceover` 声明依赖：

```text
group_clips
generate_script
```

后端按照当前 session 查找这两个节点最新保存的结果，并把它们的 `payload` 加入本次节点输入；同时生成本次执行所需的 `artifact_id`，并补入语言 `lang`。

本次输入的组成关系是：

```text
Agent LLM 提供：
mode = skip

后端生成：
artifact_id = generate_voiceover_1789240998.1452188
lang = zh

从当前 session 已有结果中读取：
group_clips
generate_script
```

合并后，节点实际拿到的可读 JSON 是：

```json
{
  "mode": "skip",
  "artifact_id": "generate_voiceover_1789240998.1452188",
  "lang": "zh",
  "group_clips": {
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
  "generate_script": {
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
  }
}
```

其中真正可用于配音的文字是：

```text
generate_script.group_scripts[0].raw_text
```

`group_clips` 也被后端补入，但当前 `generate_voiceover` 的处理代码没有读取它。当前代码实际读取的是 `generate_script.group_scripts`。

来源文件：

- [group_clips 来源 artifact](evidence/generate-voiceover/03-group-clips-source-artifact.json)
- [generate_script 来源 artifact](evidence/generate-voiceover/04-generate-script-source-artifact.json)
- [节点最终输入](evidence/generate-voiceover/05-node-resolved-input.reconstructed.json)

这里的节点输入文件是根据真实 tool 参数、真实依赖 artifact 和后端固定的合并规则重建的可读版本，不是额外捕获到的一份原始 HTTP 请求；文件名中的 `reconstructed` 用来明确这一区别。

## 4. 为什么这次没有生成任何语音

节点框架会先根据 `mode` 选择处理路径：

```text
mode = auto
    → 执行 GenerateVoiceoverNode.process

mode != auto
    → 执行 GenerateVoiceoverNode.default_process
```

本次 `mode` 是 `skip`，因此直接进入 `default_process`。该方法只做两件事：

```text
记录提示：Voiceover not generated
返回：{"voiceover": []}
```

所以本次执行没有发生以下步骤：

- 没有读取 `raw_text` 来生成语音。
- 没有调用配音参数推断 LLM。
- 没有请求 MiniMax、字节跳动或 302 的 TTS API。
- 没有生成 WAV 文件。
- 没有计算配音时长。

虽然 `generate_script` 和 `group_clips` 已经被补入输入，但 `skip` 路径在使用它们之前就结束了。

## 5. 本次 artifact 保存了什么

节点输出被保存为：

```json
{
  "payload": {
    "voiceover": []
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "generate_voiceover_1789240998.1452188",
  "node_id": "generate_voiceover",
  "create_time": 1789240998.1925874
}
```

`voiceover: []` 的含义是：

```text
没有额外生成的旁白音轨
```

它不表示删除字幕，也不等于删除素材原声。字幕仍来自 `generate_script`；素材原声是否进入最终视频，由后面的时间线与渲染配置决定。

完整 artifact：[06-generate-voiceover-artifact.json](evidence/generate-voiceover/06-generate-voiceover-artifact.json)。

## 6. tool result 怎样返回 Agent LLM

工具执行完成后，系统没有把完整 artifact 的 `payload` 展开给 Agent，而是在下一次 Agent 请求末尾加入下面两条消息：

```json
[
  {
    "content": "按你的要求，跳过配音。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_qpH1vZqMScuCz6JzZXTj4670",
        "function": {
          "name": "storyline_generate_voiceover",
          "arguments": "{\"mode\": \"skip\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:18.186969] Voiceover not generated', 'preview_urls': [], 'artifact_id': 'generate_voiceover_1789240998.1452188'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_qpH1vZqMScuCz6JzZXTj4670"
  }
]
```

这两条消息的关系是：

```text
assistant.tool_calls[0].id
    = call_00_qpH1vZqMScuCz6JzZXTj4670

tool.tool_call_id
    = call_00_qpH1vZqMScuCz6JzZXTj4670
```

相同 ID 告诉 Agent：第二条 `tool` 消息，就是第一条 `tool_call` 的执行结果。

Agent 直接得到的是执行摘要：

```text
Voiceover not generated
artifact_id = generate_voiceover_1789240998.1452188
isError = False
```

这里的 `tool_excute_result` 是空对象。完整的业务结果保存在后端 artifact 中，并没有在这条摘要里重复展开。

完整回送请求：[07-agent-resume-request.json](evidence/generate-voiceover/07-agent-resume-request.json)。

## 7. Agent 收到结果后做了什么

Agent LLM 收到“未生成配音”的 tool result 后，返回：

```json
{
  "role": "assistant",
  "reasoning_content": "",
  "content": "配音已跳过。接下来挑一首轻松的背景音乐。",
  "tool_calls": [
    {
      "id": "call_00_ET_3UMYX3mXWX4vZqFCQ3ui2682",
      "type": "function",
      "function": {
        "name": "storyline_select_BGM",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"轻松、治愈的 Vlog 背景音乐，节奏舒缓。\"}"
      }
    }
  ]
}
```

它下一步尝试选择背景音乐。这里还有一个真实细节：模型第一次返回的工具名是大小写错误的 `storyline_select_BGM`，系统报告工具不存在后，Agent 又重试了正确的 `storyline_select_bgm`。

这也说明 `generate_voiceover` 不会在完成后自动调用背景音乐节点。节点只返回结果，下一步仍由 Agent LLM 通过新的 `tool_calls` 决定。

完整下一步响应：

- [08-agent-next-response.sse.txt](evidence/generate-voiceover/08-agent-next-response.sse.txt)
- [08-agent-next-response.parsed.json](evidence/generate-voiceover/08-agent-next-response.parsed.json)

## 8. 如果使用 `mode: "auto"`，节点会怎样工作

下面是代码中存在的自动生成路径，但本次真实调用没有执行它：

```text
读取 generate_script.group_scripts
    ↓
选择 TTS provider
    ↓
把 user_request 和 provider 参数定义交给内部 LLM
    ↓
内部 LLM 返回语速、音色等 provider 参数
    ↓
逐个读取每个 group 的 raw_text
    ↓
每个 group 请求一次 TTS 服务并生成一个 WAV
    ↓
读取 WAV 时长
    ↓
返回 voiceover 列表
```

如果没有显式指定 provider，默认使用：

```text
minimax
```

当前支持的 provider 是：

```text
bytedance
minimax
302
```

假设自动生成成功，返回结构会是：

```json
{
  "voiceover": [
    {
      "voiceover_id": "voiceover_0001",
      "group_id": "group_0001",
      "path": "生成的 WAV 文件路径",
      "duration": 语音时长毫秒数
    }
  ]
}
```

这四个字段把配音和后续时间线连接起来：

```text
voiceover_id  标识这一段配音
group_id      表示它对应哪个脚本分组
path          告诉后端 WAV 文件在哪里
duration      告诉时间线这段语音持续多久
```

这里的内部 LLM 只负责从用户要求中推断 TTS 参数，不负责生成音频。真正把文字合成为 WAV 的是选定的 TTS 服务。

## 9. 三种 `mode` 的实际行为

tool schema 暴露：

```text
auto
skip
default
```

当前代码的实际行为是：

```text
mode = auto
    → 执行自动配音流程

mode = skip
    → 返回 {"voiceover": []}

mode = default
    → 同样返回 {"voiceover": []}
```

schema 对 `default` 的描述是使用默认配音，但当前节点框架会把所有非 `auto` 模式都交给同一个 `default_process`，因此 `default` 和 `skip` 的实际结果相同。这是 schema 描述与当前实现不一致的地方。

## 10. 用一句话理解整个过程

`storyline_generate_voiceover` 在自动模式下会把 `generate_script` 的每个分组文案交给 TTS 服务，生成带路径和时长的 WAV 配音；但本次 Agent 根据用户的“不要配音”要求传入 `mode: "skip"`，所以节点只保存了 `voiceover: []`，然后由 Agent 继续决定下一项工具调用。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 Agent HTTP trace、`group_clips` 与 `generate_script` artifact、`generate_voiceover` artifact，以及当前 `GenerateVoiceoverNode` 源码。完整 Agent 请求、原始 SSE、解析后响应、依赖 artifact 和节点结果存放在 [evidence/generate-voiceover](evidence/generate-voiceover/) 中。
