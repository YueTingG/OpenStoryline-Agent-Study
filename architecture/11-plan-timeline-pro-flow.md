# `storyline_plan_timeline_pro`：把前面各节点的结果编排成时间线

前面的节点分别产生了片段、顺序、字幕、配音和背景音乐，但这些结果还不能直接渲染：

```text
split_shots       知道有哪些视频片段
group_clips       知道片段采用什么顺序
generate_script   知道显示哪些字幕
generate_voiceover 知道有没有配音
select_bgm        知道使用哪首音乐以及节拍位置
```

`storyline_plan_timeline_pro` 的作用，是把这些彼此独立的结果转换成统一时间轴：

```text
什么素材
+ 从素材哪里开始取
+ 在成片什么时间出现
+ 持续多久
+ 使用什么播放速度
```

它生成的是渲染蓝图，不会在这一步剪出新视频，也不会调用 LLM。

## 1. 它在本次真实链路中的位置

```text
select_bgm
    ↓ 选中 music_0031.mp3，并给出重拍时间
plan_timeline_pro
    ↓ 生成 video、subtitles、voiceover、bgm 四条轨道
render_video
    ↓ 按时间线真正裁剪、拼接、绘制和混音
```

本次 session 是：

```text
e78297ae266440caac2cac2dc5575c51
```

## 2. Agent 怎样发起这次调用

`select_bgm` 执行完成后，Agent LLM 返回：

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

真正由 Agent 传给 MCP tool 的参数只有：

```json
{
  "mode": "auto"
}
```

`use_beats` 没有被显式传入，使用 tool schema 的默认值：

```json
{
  "use_beats": true,
  "is_speech_rough_cut": false,
  "is_ai_transition": false,
  "image_duration_ms": 3000
}
```

其中本次真正改变普通视频时间线行为的是：

```text
use_beats = true
```

它要求节点尽量让视频片段边界贴近 BGM 的重拍。

完整触发证据：

- [01-agent-trigger-request.json](evidence/plan-timeline-pro/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/plan-timeline-pro/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/plan-timeline-pro/02-agent-trigger-response.parsed.json)

## 3. 后端自动补入了哪些结果

`plan_timeline_pro` 声明依赖五种节点结果：

```text
split_shots
group_clips
generate_script
tts
music_rec
```

本次后端根据当前 session 找到的对应 artifact 是：

```text
split_shots  → split_shots_1789240961.7285173
group_clips  → group_clips_1789240983.9655333
generate_script → generate_script_1789240988.822574
tts          → generate_voiceover_1789240998.1452188
music_rec    → select_bgm_1789240999.8915014
```

然后把各 artifact 的 `payload` 加入节点输入。它们在本次时间线中的作用分别是：

### `split_shots`：提供片段文件和物理信息

原始结果包含：

```json
{
  "clips": [
    {
      "clip_id": "clip_0001",
      "kind": "video",
      "fps": 25.0,
      "source_ref": {
        "start": 0,
        "end": 5000,
        "duration": 5000,
        "height": 720,
        "width": 1280
      },
      "path": "/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0001.mp4"
    },
    {
      "clip_id": "clip_0002",
      "kind": "video",
      "fps": 30.0,
      "source_ref": {
        "start": 0,
        "end": 5000,
        "duration": 5000,
        "height": 720,
        "width": 1280
      },
      "path": "/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0002.mp4"
    },
    {
      "clip_id": "clip_0003",
      "kind": "video",
      "fps": 25.0,
      "source_ref": {
        "start": 0,
        "end": 5000,
        "duration": 5000,
        "height": 720,
        "width": 1280
      },
      "path": "/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0003.mp4"
    }
  ]
}
```

时间线由此知道每个 clip 的路径、原始时长、类型、帧率和尺寸。

### `group_clips`：提供采用顺序

原始结果包含：

```json
{
  "groups": [
    {
      "group_id": "group_0001",
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

节点按 `clip_ids` 的顺序取素材：

```text
clip_0001 → clip_0002 → clip_0003
```

这里的 `duration: "15.0s"` 没有直接决定最终时长。它只是分组节点留下的描述；时间线会根据真实 clip 时长和音乐节拍重新计算。

### `generate_script`：提供字幕单元

本次脚本包含六个 `subtitle_units`：

```json
[
  {"unit_id": "subtitle_0001", "text": "早上先来杯 Morning_coffee"},
  {"unit_id": "subtitle_0002", "text": "醒醒神"},
  {"unit_id": "subtitle_0003", "text": "然后 City_walk 出门晃一圈"},
  {"unit_id": "subtitle_0004", "text": "随便走走"},
  {"unit_id": "subtitle_0005", "text": "到了 Sunset_chill"},
  {"unit_id": "subtitle_0006", "text": "就坐着看天慢慢变橙"}
]
```

时间线实际使用这些字幕单元。`raw_text` 和 `title` 不会直接成为单独的时间线元素。

### `tts`：本次没有配音

`generate_voiceover` 的原始结果是：

```json
{
  "voiceover": []
}
```

因此最终 `voiceover` 轨道为空，字幕时间只能根据视频时长计算，不能跟随配音时长。

### `music_rec`：提供音乐和重拍

`select_bgm` 提供：

```json
{
  "bgm": {
    "bgm_id": "3f18ae606a55ad29899bcf5e1f0226fe",
    "path": "resource/bgms/music_0031.mp3",
    "duration": 132310,
    "bpm": 80.74951171875,
    "beats": [
      93,
      3065,
      5294,
      6780,
      9567,
      11053,
      16068
    ]
  }
}
```

上面只展示了与本次 11 秒时间线有关的前几个重拍；完整 `beats` 数组保存在原始 artifact 中。时间线本次实际使用的是 `duration` 和 `beats`，最终输出还保留 `bgm_id` 与 `path`。`bpm` 和能量指标没有直接参与本次编排计算。

完整来源 artifact：

- [03-split-shots-source-artifact.json](evidence/plan-timeline-pro/03-split-shots-source-artifact.json)
- [04-group-clips-source-artifact.json](evidence/plan-timeline-pro/04-group-clips-source-artifact.json)
- [05-generate-script-source-artifact.json](evidence/plan-timeline-pro/05-generate-script-source-artifact.json)
- [06-generate-voiceover-source-artifact.json](evidence/plan-timeline-pro/06-generate-voiceover-source-artifact.json)
- [07-select-bgm-source-artifact.json](evidence/plan-timeline-pro/07-select-bgm-source-artifact.json)

后端合并后的完整节点输入见 [08-node-resolved-input.reconstructed.json](evidence/plan-timeline-pro/08-node-resolved-input.reconstructed.json)。该文件由真实 tool 参数、真实依赖 artifact 和固定合并规则重建，因此文件名明确标记为 `reconstructed`，不是额外捕获到的原始 HTTP 请求。

## 4. 这一步有没有再次调用 LLM

没有。

本次调用结构是：

```text
Agent LLM 返回 plan_timeline_pro tool_call
    ↓
后端读取五个前置 artifact
    ↓
Python 时间线算法计算各轨道
    ↓
直接返回 tool result
```

HTTP trace 中，Agent 的 tool call 与 tool result 之间没有新的 LLM request/response。片段时长、字幕位置和 BGM 区间全部由程序规则计算。

## 5. 三个 5 秒视频为什么变成 11.053 秒

三个源片段原本都是：

```text
clip_0001 = 5000ms
clip_0002 = 5000ms
clip_0003 = 5000ms
总计 = 15000ms
```

因为 `use_beats = true`，节点不会简单保留三个 5 秒片段，而是沿着 BGM 重拍区间依次分配时长。

本次最终采用的边界是：

```text
0ms ───── 3065ms ───── 6780ms ───── 11053ms
      clip_0001       clip_0002       clip_0003
```

所以三个新时长是：

```text
clip_0001: 3065 - 0    = 3065ms
clip_0002: 6780 - 3065 = 3715ms
clip_0003: 11053 - 6780 = 4273ms
```

节点还会为每个 clip 计算最低可接受时长。本次没有配音，因此它根据当前组的全部字幕字符估算文字所需时间：

```text
200ms × 字符数
最高不超过 5000ms
```

本组文字估算结果达到上限 `5000ms`，再平均分配给三个 clip，并结合 `min_clip_duration = 1000ms`：

```text
5000 × 0.33 = 1650ms
```

因此每个 clip 至少需要约 `1650ms`。节点在满足最低时长的前提下，沿 BGM 重拍寻找不超过源片段 5000ms 的边界，最终得到 `3065、3715、4273`。

完整计算摘要：[09-timeline-calculation.reconstructed.json](evidence/plan-timeline-pro/09-timeline-calculation.reconstructed.json)。

## 6. 这是裁剪还是变速

本次是**裁剪尾部，不是变速**。

前一节的节拍算法已经把每个 clip 的目标时长限制在：

```text
最低约 1650ms
最高不超过源片段的 5000ms
```

因此本次计算出的目标时长都不可能超过原始 5000ms：

```text
clip_0001：3065ms < 5000ms
clip_0002：3715ms < 5000ms
clip_0003：4273ms < 5000ms
```

节点随后从每个源 clip 的开头截取目标长度：

```text
clip_0001 取 0～3065ms，丢弃 3065～5000ms
clip_0002 取 0～3715ms，丢弃 3715～5000ms
clip_0003 取 0～4273ms，丢弃 4273～5000ms
```

截取出来的内容按正常速度播放，所以三个 `playback_rate` 都是：

```json
[1.0, 1.0, 1.0]
```

这里的因果顺序是：

```text
节拍算法先得到小于 5000ms 的目标时长
    ↓
节点从源 clip 开头截取对应长度
    ↓
截取部分保持正常速度
    ↓
playback_rate = 1.0
```

`playback_rate = 1.0` 本身只表示正常速度，单独看这个字段不能判断有没有裁剪。证明本次发生裁剪的是：

```text
源 clip 原始时长 = 5000ms
source_window.duration < 5000ms
```

对应输出原文是：

```json
{
  "clip_id": "clip_0001",
  "source_window": {
    "start": 0,
    "end": 3065,
    "duration": 3065
  },
  "timeline_window": {
    "start": 0,
    "end": 3065,
    "duration": 3065
  },
  "playback_rate": 1.0
}
```

这段原文同时表达了两件事：

```text
source_window.duration = 3065
    → 原始 5000ms 素材只使用前 3065ms，发生了裁剪

playback_rate = 1.0
    → 被保留的 3065ms 按正常速度播放
```

两个 window 的含义不同：

```text
source_window
    从源文件内部取哪一段

timeline_window
    这一段放在最终成片的什么位置
```

例如 `clip_0002` 的原文是：

```json
{
  "source_window": {
    "start": 0,
    "end": 3715,
    "duration": 3715
  },
  "timeline_window": {
    "start": 3065,
    "end": 6780,
    "duration": 3715
  }
}
```

它从自己的源文件第 0 毫秒开始取 3715 毫秒，但在成片中接在 `clip_0001` 后面，从 3065 毫秒开始播放。

当前代码也包含“目标时长大于源时长时降速拉长视频”的通用分支，例如配音比素材更长时可能使用；但本次没有配音，并且节拍计算结果都小于 5000ms，所以该分支没有执行。本次也没有采用“保留完整 5000ms，再加速压缩到目标时长”的方案。

## 7. 六条字幕怎样分布到 11.053 秒

因为没有配音，节点先把整个 group 对应到三个视频片段的总时长：

```text
group_0001 = 11053ms
```

然后按照每条字幕的字符数占总字符数的比例分配显示时长。最终得到：

```text
subtitle_0001   0 ～ 3203ms
subtitle_0002   3203 ～ 3683ms
subtitle_0003   3683 ～ 6566ms
subtitle_0004   6566 ～ 7206ms
subtitle_0005   7206 ～ 9608ms
subtitle_0006   9608 ～ 11049ms
```

这意味着字幕并不是由 LLM 再次判断应该对应哪个画面，也不是逐条绑定到某个 clip。当前无配音分支使用的是：

```text
一组字幕共享该 group 的总时间
    ↓
按字符长度比例依次分配
```

由于每段时长转换成整数时会向下取整，六条字幕合计到 `11049ms`，比视频结尾 `11053ms` 少 4 毫秒。

## 8. 配音和 BGM 轨道怎样形成

### 配音轨道

因为输入是：

```json
{
  "voiceover": []
}
```

所以输出也是：

```json
{
  "voiceover": []
}
```

### BGM 轨道

选中的音乐长 `132310ms`，而视频时间线只有 `11053ms`，因此不需要循环音乐，只取开头 11.053 秒：

```json
{
  "bgm_id": "3f18ae606a55ad29899bcf5e1f0226fe",
  "source_window": {
    "start": 0,
    "end": 11053
  },
  "path": "/app/resource/bgms/music_0031.mp3"
}
```

时间线只描述使用哪一段 BGM。音量缩放和实际混音发生在后面的 `render_video`。

## 9. 最终 artifact 是四条轨道

节点最终输出的结构是：

```text
tracks
├─ video       3 个片段
├─ subtitles   6 条字幕
├─ voiceover   0 段配音
└─ bgm         1 段背景音乐
```

完整 artifact 中的核心结果是：

```json
{
  "payload": {
    "tracks": {
      "video": [
        {
          "clip_id": "clip_0001",
          "timeline_window": {"start": 0, "end": 3065, "duration": 3065},
          "playback_rate": 1.0
        },
        {
          "clip_id": "clip_0002",
          "timeline_window": {"start": 3065, "end": 6780, "duration": 3715},
          "playback_rate": 1.0
        },
        {
          "clip_id": "clip_0003",
          "timeline_window": {"start": 6780, "end": 11053, "duration": 4273},
          "playback_rate": 1.0
        }
      ],
      "subtitles": [
        {"unit_id": "subtitle_0001", "text": "早上先来杯 Morning_coffee", "timeline_window": {"start": 0, "end": 3203}},
        {"unit_id": "subtitle_0002", "text": "醒醒神", "timeline_window": {"start": 3203, "end": 3683}},
        {"unit_id": "subtitle_0003", "text": "然后 City_walk 出门晃一圈", "timeline_window": {"start": 3683, "end": 6566}},
        {"unit_id": "subtitle_0004", "text": "随便走走", "timeline_window": {"start": 6566, "end": 7206}},
        {"unit_id": "subtitle_0005", "text": "到了 Sunset_chill", "timeline_window": {"start": 7206, "end": 9608}},
        {"unit_id": "subtitle_0006", "text": "就坐着看天慢慢变橙", "timeline_window": {"start": 9608, "end": 11049}}
      ],
      "voiceover": [],
      "bgm": [
        {
          "bgm_id": "3f18ae606a55ad29899bcf5e1f0226fe",
          "source_window": {"start": 0, "end": 11053},
          "path": "/app/resource/bgms/music_0031.mp3"
        }
      ]
    }
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "plan_timeline_pro_1789241019.933336",
  "node_id": "plan_timeline_pro"
}
```

上面为了突出时间关系，省去了视频轨道中已经在前文解释的 `fps`、`size`、`source_path` 和完整 `source_window`；没有省略的 artifact 原文见 [10-plan-timeline-pro-artifact.json](evidence/plan-timeline-pro/10-plan-timeline-pro-artifact.json)。

## 10. 为什么用户要求 5 秒，结果却是 11.053 秒

原始用户要求是制作 5 秒测试视频，但 `plan_timeline_pro` 的 tool schema 没有“目标总时长”参数。

Agent LLM 在 `reasoning_content` 中也注意到了这个问题：

```text
The tool doesn't have a duration param ... There's no total duration control.
```

但它仍然只调用：

```json
{
  "mode": "auto"
}
```

节点收到的三个源片段总长 15 秒，然后根据 BGM 重拍裁成 11.053 秒。它不知道必须继续压缩到 5 秒，也没有可以接收该要求的参数。

所以本次结果是：

```text
用户目标：5000ms
实际时间线：11053ms
差值：6053ms
```

这不是渲染阶段把视频变长了，而是时间线阶段已经生成了 11.053 秒的 video track。后面的 `render_video` 只是按这份时间线执行。

## 11. tool result 怎样返回 Agent LLM

完整四轨时间线保存在 artifact 中。下一次 Agent 请求末尾收到的是摘要：

```json
[
  {
    "content": "背景音乐选好了。接下来组织时间线。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_j9Sc6YToszK0KXnmGFly8642",
        "function": {
          "name": "storyline_plan_timeline_pro",
          "arguments": "{\"mode\": \"auto\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '[2026-09-12T19:23:40.003165] [W/O. Beats Rate] 0.00', 'INFO_USER': '', 'preview_urls': [], 'artifact_id': 'plan_timeline_pro_1789241019.933336'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_j9Sc6YToszK0KXnmGFly8642"
  }
]
```

`[W/O. Beats Rate] 0.00` 表示：

```text
没有任何 clip 因时长限制而无法按重拍方案处理
```

它不是 BPM，也不是“没有检测到节拍”。本次三个 clip 都成功使用了重拍边界，所以比例是 `0.00`。

Agent 没有收到完整 tracks，只知道节点成功、节拍未对齐比例为 0，以及 artifact ID。完整回送请求见 [11-agent-resume-request.json](evidence/plan-timeline-pro/11-agent-resume-request.json)。

## 12. Agent 下一步做了什么

Agent 收到时间线结果后返回：

```json
{
  "role": "assistant",
  "reasoning_content": "Now render video. Let me render. aspect ratio? Leave auto. Output dimension maybe 1080.\n\nLet me render.",
  "content": "时间线已组织好。最后一步，渲染成片。",
  "tool_calls": [
    {
      "id": "call_00_O3EyyrcJJyurjggoZwn61660",
      "type": "function",
      "function": {
        "name": "storyline_render_video",
        "arguments": "{\"mode\": \"auto\"}"
      }
    }
  ]
}
```

下一步是 `storyline_render_video`。`plan_timeline_pro` 不会自动开始渲染；它返回时间线后，仍由 Agent LLM 发起下一次 tool call。

完整响应：

- [12-agent-next-response.sse.txt](evidence/plan-timeline-pro/12-agent-next-response.sse.txt)
- [12-agent-next-response.parsed.json](evidence/plan-timeline-pro/12-agent-next-response.parsed.json)

## 13. `mode` 的实际行为

`plan_timeline_pro` 是固定流程节点。当前实现中：

```text
mode = auto
    → 执行时间线计算

mode = skip
    → default_process 再次调用同一个时间线计算

mode = default
    → default_process 再次调用同一个时间线计算
```

因此它不能像配音或 BGM 一样真正跳过。即使传入 `skip`，当前代码仍会生成时间线。

## 14. 用一句话理解整个过程

`storyline_plan_timeline_pro` 从当前 session 读取片段、顺序、字幕、配音和音乐结果，用确定性算法把它们转换成 video、subtitles、voiceover、bgm 四条轨道；本次它按 BGM 重拍裁短了三个视频并生成 11.053 秒时间线，但没有实现用户提出的 5 秒总时长。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 41 至 44 行、五个前置 artifact、`plan_timeline_pro` artifact，以及当前 `PlanTimelineProNode` 和 `TimeLine` 源码。完整请求、原始 SSE、解析后响应、依赖 artifact、重建输入和最终时间线存放在 [evidence/plan-timeline-pro](evidence/plan-timeline-pro/) 中。
