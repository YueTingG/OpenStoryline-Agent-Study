# 个人理解

Agent 给出音乐要求，例如：【轻松、治愈的 Vlog 背景音乐，节奏舒缓。】

mcp 本地有一个向量库，里面存放了所有候选背景音乐（都是描述）

用要求去检索向量库，返回 Top32。

之后再让 llm 从 32 首中挑一首

# `storyline_select_bgm`：从本地曲库选出背景音乐并分析节拍

`storyline_select_bgm` 不生成音乐，也不从互联网搜索音乐。它处理的是项目已经准备好的本地 BGM 曲库：

```text
Agent 给出音乐要求
    ↓
程序从本地曲库召回候选
    ↓
内部 LLM 从候选中选择一首
    ↓
程序读取选中的 MP3，分析时长、BPM、重拍和能量
    ↓
结果交给后面的时间线
```

本次最终选择的是：

```text
resource/bgms/music_0031.mp3
```

## 1. 它在本次真实链路中的位置

```text
generate_voiceover
    ↓ 本次按用户要求跳过配音
select_bgm
    ↓ 选中 music_0031.mp3，并分析音乐节拍
plan_timeline_pro
    ↓ 根据视频、字幕、配音和 BGM 组织时间线
render_video
    ↓ 把 BGM 混入最终视频
```

本次 session 是：

```text
e78297ae266440caac2cac2dc5575c51
```

## 2. Agent 第一次调用为什么失败

`generate_voiceover` 返回“未生成配音”后，Agent LLM 第一次给出的响应是：

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

但系统真正注册的工具名是：

```text
storyline_select_bgm
```

第一次响应使用了大写的 `BGM`。工具名需要精确匹配，因此系统没有执行选曲节点，而是把错误作为一条 `tool` message 返回给 Agent：

```json
[
  {
    "content": "配音已跳过。接下来挑一首轻松的背景音乐。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_ET_3UMYX3mXWX4vZqFCQ3ui2682",
        "function": {
          "name": "storyline_select_BGM",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"轻松、治愈的 Vlog 背景音乐，节奏舒缓。\"}"
        }
      }
    ]
  },
  {
    "content": "Error: storyline_select_BGM is not a valid tool, try one of [storyline_load_media, storyline_search_media, storyline_split_shots, storyline_understand_clips, storyline_filter_clips, storyline_group_clips, storyline_generate_script, storyline_script_template_rec, storyline_generate_voiceover, storyline_select_bgm, storyline_elementrec_transition, storyline_elementrec_text, storyline_plan_timeline_pro, storyline_render_video, storyline_read_node_history, storyline_write_skills, subtitle_imitation_skill, create_profile_style_skill].",
    "role": "tool",
    "tool_call_id": "call_00_ET_3UMYX3mXWX4vZqFCQ3ui2682"
  }
]
```

这里没有“模糊匹配”或自动改名。错误消息明确列出了有效工具名，Agent LLM 收到错误后重新判断并重试。

完整证据：

- [01-agent-first-attempt-request.json](evidence/select-bgm/01-agent-first-attempt-request.json)
- [02-agent-first-attempt-response.sse.txt](evidence/select-bgm/02-agent-first-attempt-response.sse.txt)
- [02-agent-first-attempt-response.parsed.json](evidence/select-bgm/02-agent-first-attempt-response.parsed.json)
- [03-agent-retry-request.json](evidence/select-bgm/03-agent-retry-request.json)

## 3. Agent 怎样发起正确调用

Agent LLM 第二次返回：

```json
{
  "role": "assistant",
  "reasoning_content": "Correct tool name is storyline_select_bgm.",
  "content": null,
  "tool_calls": [
    {
      "id": "call_00_18L4EUcfarynJpL7OG9S9883",
      "type": "function",
      "function": {
        "name": "storyline_select_bgm",
        "arguments": "{\"mode\": \"auto\", \"user_request\": \"轻松、治愈的 Vlog 背景音乐，节奏舒缓。\"}"
      }
    }
  ]
}
```

这次真正传给 MCP tool 的参数是：

```json
{
  "mode": "auto",
  "user_request": "轻松、治愈的 Vlog 背景音乐，节奏舒缓。"
}
```

原始用户只要求制作测试视频，没有直接描述背景音乐风格。这里的 `user_request` 是 Agent LLM 根据当前 Vlog 内容和整体剪辑目标，为选曲节点整理出的局部要求。

`select_bgm` 没有声明前置节点依赖，因此后端不会像 `generate_voiceover` 那样自动补入 `generate_script`、`group_clips` 或视频理解结果。这个节点作出选择时直接使用的是：

```text
Agent 提供的 user_request
+
本地音乐库的候选元数据
```

它没有直接看到视频画面、字幕文案或前面节点的完整 artifact。

完整正确响应：[04-agent-retry-response.parsed.json](evidence/select-bgm/04-agent-retry-response.parsed.json)。

## 4. 本地曲库从哪里来

项目配置指定：

```toml
[project]
bgm_dir = "./resource/bgms"
```

这个目录中包含两类内容：

```text
resource/bgms/meta.json
resource/bgms/music_XXXX.mp3
```

两者的关系是：

```text
meta.json
    保存每首音乐的 scene、genre、mood、lang、description、path 和 id

music_XXXX.mp3
    保存真正可以播放和渲染的音频文件
```

这些资源不是运行选曲节点时临时联网搜索得到的。`download.sh` 在构建运行环境时下载项目提供的 `resource.zip`，并解压到 `resource/`：

```bash
wget "https://image-url-2-feature-1251524319.cos.ap-shanghai.myqcloud.com/openstoryline/resource.zip" \
  -O .storyline/resource.zip

unzip -o .storyline/resource.zip -d resource
```

因此完整来源关系是：

```text
项目提供的 resource.zip
    ↓ 构建环境时下载并解压
resource/bgms/meta.json + MP3 文件
    ↓
select_bgm 在运行时读取本地曲库
```

本次选中的 `music_0031.mp3` 已经存在于该曲库中。节点没有创作它，也没有从网络音乐平台下载它。

## 5. 程序怎样从曲库得到 32 个候选

节点启动时读取 `resource/bgms/meta.json`，取每首音乐的 `description`，使用 embedding 模型把描述转成向量，再建立 FAISS 向量索引。

当前默认使用：

```text
all-MiniLM-L6-v2 embedding 模型
+
FAISS 向量索引
```

当本次请求到来时，程序用下面这句话查询索引：

```text
轻松、治愈的 Vlog 背景音乐，节奏舒缓。
```

向量检索返回语义上最接近的 32 首音乐：

```text
完整曲库
    ↓ 按 description 与 user_request 的语义相似度检索
Top 32 候选
```

这一步不是让内部 LLM面对整个曲库，而是先用本地检索缩小范围。

随后程序应用两个可选的结构化过滤条件：

```text
filter_include
filter_exclude
```

本次 Agent 没有传入这两个字段，因此两者都使用空对象，没有进一步排除候选。最终送给内部 LLM 的仍然是 32 首音乐。

## 6. 内部 BGM LLM 收到了什么

这不是 Agent LLM 的完整历史对话，而是选曲能力自己的独立请求。完整请求由两条 message 构成：

```json
{
  "messages": [
    {
      "content": "你是一个音乐选择器，根据我提供的音乐条目（List[Dict]格式），返回最适合用户要求的**有且只有一条**json。",
      "role": "system"
    },
    {
      "content": "候选音乐条目：\n[这里是本次检索出的 32 个完整音乐条目]\n用户要求：\n轻松、治愈的 Vlog 背景音乐，节奏舒缓。",
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

上面为了说明两块内容的关系，将 32 个候选显示为说明文字；没有省略的完整原始请求保存在 [05-bgm-llm-request.json](evidence/select-bgm/05-bgm-llm-request.json)。

请求中两条 message 的分工是：

```text
system message
    规定任务：只能返回一条最合适的音乐 JSON

user message
    提供选择范围：32 个候选音乐条目
    提供选择依据：轻松、治愈、Vlog、节奏舒缓
```

候选条目已经带有 `scene`、`genre`、`mood`、`description` 和 `path`。内部 LLM 根据这些文字元数据选择音乐，并没有听取 32 个 MP3 的声音。

## 7. 内部 LLM 选择了什么

内部 LLM 返回的业务内容是：

```json
{
  "scene": [
    "Vlog",
    "Relaxing",
    "Cafe"
  ],
  "genre": [
    "BGM",
    "Electronic"
  ],
  "mood": [
    "Chill",
    "Calm",
    "Healing"
  ],
  "lang": [
    "bgm"
  ],
  "description": "这是一首轻柔舒缓的电子背景音乐，以钢琴和合成器为主，适合用于Vlog、咖啡馆或放松场景，营造宁静治愈的氛围。",
  "path": "./resource/bgms/music_0031.mp3",
  "id": "3f18ae606a55ad29899bcf5e1f0226fe"
}
```

它与本次要求的对应关系是：

```text
Vlog       → scene 包含 Vlog
轻松       → mood 包含 Chill
节奏舒缓   → mood 包含 Calm，description 写明“轻柔舒缓”
治愈       → mood 包含 Healing，description 写明“宁静治愈”
```

因此模型选择了候选列表中已经存在的 `music_0031.mp3`，没有创造新的音乐路径。

完整响应：

- [06-bgm-llm-response.sse.txt](evidence/select-bgm/06-bgm-llm-response.sse.txt)
- [06-bgm-llm-response.parsed.json](evidence/select-bgm/06-bgm-llm-response.parsed.json)

## 8. 选中 MP3 后为什么还要分析音乐

内部 LLM 返回的只是音乐条目元数据。后续时间线不仅需要知道“用哪首音乐”，还需要知道音乐有多长、节奏在哪里。

因此程序实际打开 `music_0031.mp3`，将音频读取为单声道波形，并分析：

```text
duration          总时长，单位毫秒
sample_rate       分析时使用的采样率
bpm               估算的每分钟节拍数
beats             选出的重拍时间点，单位毫秒
energy_mean       平均音频能量
energy_mean_db    以 dB 表示的平均能量
dynamic_range_db  音量动态范围
```

本次配置使用：

```text
sample_rate = 22050
hop_length  = 2048
frame_length = 2048
```

这里的 `beats` 不是 MP3 中的声音内容，也不是所有采样点。程序先估算节拍，再根据节拍位置的起音强度、局部峰值和相邻间隔筛出更明显的重拍，最后把它们换算成毫秒时间戳。

它们的用途是让后续时间线有机会把镜头切换或字幕位置贴近音乐节奏。

## 9. 最终 artifact 保存了什么

本次节点最终保存：

```json
{
  "payload": {
    "bgm": {
      "bgm_id": "3f18ae606a55ad29899bcf5e1f0226fe",
      "path": "resource/bgms/music_0031.mp3",
      "duration": 132310,
      "sample_rate": 22050,
      "bpm": 80.74951171875,
      "beats": [
        93,
        3065,
        5294,
        6780,
        9567,
        11053,
        16068,
        17554,
        19040,
        22570,
        24056,
        25542,
        27028,
        28514,
        32322,
        33808,
        36037,
        40310,
        45511,
        49041,
        50527,
        52013,
        55078,
        57307,
        59536,
        61022,
        63065,
        66038,
        67524,
        70589,
        72075,
        73561,
        75047,
        78576,
        83035,
        85078,
        86564,
        88050,
        90279,
        93066,
        94552,
        96038,
        98267,
        101053,
        102539,
        104025,
        106255,
        107555,
        109041,
        110527,
        115078,
        117307,
        118793,
        121022,
        122508
      ],
      "energy_mean": 0.11375369131565094,
      "energy_mean_db": -22.273874282836914,
      "dynamic_range_db": 19.28468894958496
    }
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "select_bgm_1789240999.8915014",
  "node_id": "select_bgm",
  "create_time": 1789241017.7781591
}
```

这说明选曲节点的输出不只是一个文件路径，而是一份可以被时间线使用的音乐分析结果。

完整 artifact：[07-select-bgm-artifact.json](evidence/select-bgm/07-select-bgm-artifact.json)。

## 10. tool result 怎样返回 Agent LLM

完整 artifact 保存在后端。回送 Agent LLM 的是摘要：

```json
[
  {
    "content": null,
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_18L4EUcfarynJpL7OG9S9883",
        "function": {
          "name": "storyline_select_bgm",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"轻松、治愈的 Vlog 背景音乐，节奏舒缓。\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:37.769268] Successfully choose music', 'preview_urls': ['resource/bgms/music_0031.mp3'], 'artifact_id': 'select_bgm_1789240999.8915014'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_18L4EUcfarynJpL7OG9S9883"
  }
]
```

相同 ID 将调用和结果配对：

```text
assistant.tool_calls[0].id
    = call_00_18L4EUcfarynJpL7OG9S9883

tool.tool_call_id
    = call_00_18L4EUcfarynJpL7OG9S9883
```

Agent 直接知道的是：

```text
选曲成功
预览文件是 resource/bgms/music_0031.mp3
完整结果的 artifact_id
```

它没有在 tool result 中收到完整的 BPM、beats 和能量数组。完整数据保存在 artifact 中，后端会在后续执行 `plan_timeline_pro` 时按依赖自动读取。

完整回送请求：[08-agent-resume-request.json](evidence/select-bgm/08-agent-resume-request.json)。

## 11. Agent 下一步做了什么

Agent 收到选曲成功的摘要后返回：

```json
{
  "role": "assistant",
  "reasoning_content": "Now plan_timeline. 5 second video. The timeline tool will arrange. Let me call plan_timeline_pro with mode auto. \n\nShould I try to make it 5 seconds? The tool doesn't have a duration param except image_duration_ms for AI transition. These are videos. There's no total duration control. Let me just run auto mode.\n\nActually, maybe I should read the script node to check. Not necessary.\n\nLet me call plan_timeline_pro.",
  "content": "背景音乐选好了。接下来组织时间线。",
  "tool_calls": [
    {
      "id": "call_00_j9Sc6YToszK0KXnmGFly8642",
      "type": "function",
      "function": {
        "name": "storyline_plan_timeline_pro",
        "arguments": "{\"mode\": \"auto\"}"
      }
    }
  ]
}
```

因此下一步是 `storyline_plan_timeline_pro`。`select_bgm` 本身不会自动组织时间线；它只产出 BGM artifact，下一步仍由 Agent LLM 发起。

完整响应：

- [09-agent-next-response.sse.txt](evidence/select-bgm/09-agent-next-response.sse.txt)
- [09-agent-next-response.parsed.json](evidence/select-bgm/09-agent-next-response.parsed.json)

## 12. 三种 `mode` 的实际行为

tool schema 暴露：

```text
auto
skip
default
```

当前代码的实际行为是：

```text
mode = auto
    → 检索候选、调用内部 LLM、分析选中的 MP3

mode = skip
    → 返回 {"bgm": {}}

mode = default
    → 同样返回 {"bgm": {}}
```

schema 对 `default` 的描述是使用默认音乐，但当前节点的所有非 `auto` 模式都会进入同一个 `default_process`，所以 `default` 和 `skip` 都不会选择音乐。这是 schema 描述与当前实现不一致的地方。

## 13. 当前实现的边界

选曲过程有三层约束：

```text
向量检索限制候选范围
    ↓
include / exclude 过滤候选标签
    ↓
prompt 要求内部 LLM 从候选中返回一条 JSON
```

但当前代码只检查 LLM 响应能否解析成字典以及是否包含 `path`，没有严格验证返回的 `id` 和 `path` 一定属于候选列表。

如果响应无法解析，或者没有 `path`，程序会降级选择候选列表的第一首。如果模型返回了一个不存在的路径，后面的音频读取会报错，而不是自动改选另一首。

因此，“从候选中选择”主要由候选上下文和 prompt 约束，并不是完全由程序强校验保证。

## 14. 用一句话理解整个过程

`storyline_select_bgm` 先根据 Agent 整理出的音乐要求，从项目自带的本地曲库中检索 32 个候选，再让独立选曲 LLM 挑出 `music_0031.mp3`，最后由程序实际读取该 MP3 并计算时长、BPM、重拍和能量数据，供下一步时间线编排使用。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 35 至 42 行、`select_bgm` artifact、当前 `SelectBGMNode`、`StorylineRecall`、`ElementFilter` 源码，以及项目资源下载与路径配置。完整 Agent 请求、原始 SSE、解析后响应、内部 BGM LLM 请求/响应和 artifact 存放在 [evidence/select-bgm](evidence/select-bgm/) 中。
