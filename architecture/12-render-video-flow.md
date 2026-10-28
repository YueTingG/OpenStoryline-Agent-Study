# `storyline_render_video`：把时间线真正渲染成 MP4

`plan_timeline_pro` 输出的是一份时间线配置，还不是成片。`storyline_render_video` 才真正读取视频、字体和音频文件，把四条轨道合成为一个 MP4：

```text
video track       → 裁剪并拼接三个视频片段
subtitles track   → 用字体文件绘制六条字幕
voiceover track   → 本次为空
bgm track         → 截取并混入背景音乐
transition_rec    → 对整条基础视频应用淡入淡出
```

它是这次剪辑链路中最后一个 MCP 工具。

## 1. 它在本次真实链路中的位置

```text
plan_timeline_pro
    ↓ 生成 11.053 秒的四轨时间线
render_video
    ↓ MoviePy 组合画面、字幕和音频
    ↓ FFmpeg 编码
最终 MP4
```

本次 session 是：

```text
e78297ae266440caac2cac2dc5575c51
```

## 2. Agent 怎样发起渲染

`plan_timeline_pro` 执行完成后，Agent LLM 返回：

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

Agent 真正传给 MCP tool 的参数只有：

```json
{
  "mode": "auto"
}
```

Agent 没有指定画幅、输出尺寸、字幕样式、音量或编码质量，这些字段全部使用 tool schema 的默认值。

完整触发证据：

- [01-agent-trigger-request.json](evidence/render-video/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/render-video/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/render-video/02-agent-trigger-response.parsed.json)

## 3. 后端自动补入了哪些结果

`render_video` 声明依赖：

```text
load_media
plan_timeline
transition_rec
text_rec
```

本次后端找到的是：

```text
load_media     → load_media_1789240960.4566667
plan_timeline  → plan_timeline_pro_1789241019.933336
transition_rec → elementrec_transition_1789240996.9224887
text_rec       → elementrec_text_1789240993.1517227
```

四份结果的分工是：

```text
load_media
    是 render_video 声明的固定依赖，提供原始素材路径和媒体信息

plan_timeline
    提供真正需要渲染的 video、subtitles、voiceover、bgm 四条轨道

transition_rec
    提供整条画面的 500ms fade_in 和 500ms fade_out

text_rec
    提供字幕使用的 font_name = SourceHanSansSC
```

本次 video track 已经在每个 item 的 `source_path` 中保存了可直接读取的文件路径，因此渲染器实际使用的是时间线里的 `source_path`，没有再依靠 `load_media` 查找三个视频文件。

完整来源 artifact：

- [03-load-media-source-artifact.json](evidence/render-video/03-load-media-source-artifact.json)
- [04-plan-timeline-source-artifact.json](evidence/render-video/04-plan-timeline-source-artifact.json)
- [05-transition-source-artifact.json](evidence/render-video/05-transition-source-artifact.json)
- [06-text-source-artifact.json](evidence/render-video/06-text-source-artifact.json)

## 4. 节点最终拿到的渲染参数

Agent 只传入 `mode: "auto"`，其余参数使用默认值：

```json
{
  "aspect_ratio": null,
  "output_max_dimension_px": null,
  "clip_compose_mode": "padding",
  "bg_color": [0, 0, 0],
  "crf": 23,
  "font_color": [255, 255, 255, 255],
  "font_size": 40,
  "margin_bottom": 270,
  "stroke_width": 2,
  "stroke_color": [0, 0, 0, 255],
  "bgm_volume_scale": 0.25,
  "tts_volume_scale": 2.0,
  "include_video_audio": false,
  "video_volume_scale": 1.0
}
```

这些默认值与四份依赖 artifact 合并后，形成完整节点输入。没有省略的重建输入见 [07-node-resolved-input.reconstructed.json](evidence/render-video/07-node-resolved-input.reconstructed.json)。文件名使用 `reconstructed`，表示它由真实参数、真实 artifact 和固定合并规则重建，不是额外捕获到的原始 HTTP 请求。

## 5. 这一步有没有调用 LLM

没有。

真实执行结构是：

```text
Agent LLM 发出 render_video tool_call
    ↓
后端补入四份 artifact
    ↓
MoviePy 读取和组合媒体
    ↓
Pillow 绘制字幕图层
    ↓
FFmpeg 编码成 MP4
    ↓
tool result 返回 Agent LLM
```

渲染阶段没有新的 LLM request/response。最终画面由时间线和程序规则决定。

## 6. 最终时长为什么是 11.053 秒

渲染器根据 video track 中最大的 `timeline_window.end` 决定成片时长：

```json
[
  {"clip_id": "clip_0001", "timeline_window": {"start": 0, "end": 3065}},
  {"clip_id": "clip_0002", "timeline_window": {"start": 3065, "end": 6780}},
  {"clip_id": "clip_0003", "timeline_window": {"start": 6780, "end": 11053}}
]
```

因此：

```text
final_duration
= max(video.timeline_window.end)
= 11053ms
= 11.053s
```

`render_video` 不会再次调整总时长，也不会读取原始用户的“5 秒”要求。时间线已经是 11.053 秒，渲染结果自然也是 11.053 秒。

## 7. 输出尺寸为什么是 1080×608

Agent 没有传 `aspect_ratio`。渲染器读取三个 video item 的尺寸：

```json
[
  [1280, 720],
  [1280, 720],
  [1280, 720]
]
```

三个素材都是 `16:9`，因此自动画幅选择 `16:9`。

Agent 也没有传 `output_max_dimension_px`，所以最长边使用默认值：

```text
1080px
```

横屏画面的宽度取 1080，高度计算为：

```text
1080 ÷ (16 / 9)
= 607.5
```

编码尺寸必须使用偶数，程序取：

```text
1080 × 608
```

本次 `clip_compose_mode = "padding"`，素材保持原始宽高比，居中放入画布；如果出现空白区域，使用黑色 `bg_color = [0, 0, 0]` 填充。

## 8. 三段视频怎样变成一条基础视频

渲染器按 `timeline_window.start` 排序，然后读取每个 clip 的 `source_window`：

```text
clip_0001：读取源文件 0～3065ms
clip_0002：读取源文件 0～3715ms
clip_0003：读取源文件 0～4273ms
```

三个 `playback_rate` 都是 `1.0`，所以被保留的内容按正常速度播放。渲染器依次拼接：

```text
0～3065ms        clip_0001
3065～6780ms     clip_0002
6780～11053ms    clip_0003
```

本次时间线没有视频空隙，因此不需要插入黑色视频片段。

输出帧率取 video track 中最高的 FPS：

```text
max(25, 30, 25) = 30fps
```

因此最终 MP4 以 30fps 编码。

## 9. 淡入淡出怎样真正作用于画面

`transition_rec` 的原文是：

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

渲染器先把三个视频拼成一条 11.053 秒的基础视频，再应用：

```text
整条基础视频开头：FadeIn 0.5s
整条基础视频结尾：FadeOut 0.5s
```

它仍然不会在 `clip_0001 → clip_0002` 或 `clip_0002 → clip_0003` 之间添加转场。

当前实现先对基础视频应用淡入淡出，再把字幕作为独立图层覆盖上去。因此淡入淡出直接处理的是基础画面，不是整个字幕合成结果。

## 10. 六条字幕怎样真正绘制出来

`text_rec` 提供：

```json
{
  "font_name": "SourceHanSansSC",
  "font_color": [255, 255, 255, 255]
}
```

渲染器使用 `font_name` 回到 `font_info.json` 查找字体文件：

```text
font_name = SourceHanSansSC
    ↓
resource/fonts/SourceHanSansSC/SourceHanSansSC-Bold.otf
```

然后 Pillow 根据每条 subtitle 的 `timeline_window` 创建透明文字图层：

```text
subtitle_0001：0～3203ms
subtitle_0002：3203～3683ms
subtitle_0003：3683～6566ms
subtitle_0004：6566～7206ms
subtitle_0005：7206～9608ms
subtitle_0006：9608～11049ms
```

渲染参数原始默认值是：

```text
font_size = 40
margin_bottom = 270
stroke_width = 2
font_color = 白色
stroke_color = 黑色
```

但这些尺寸以 1080px 高画布为基准。本次画布高度是 608px，程序按比例缩放：

```text
scale = 608 ÷ 1080 ≈ 0.563
```

得到：

```text
font_size：40 × 0.563 ≈ 22.5
    → 低于代码允许的最小字号 28
    → 最终使用 28px

margin_bottom：270 × 0.563 = 152px

stroke_width：2 × 0.563 ≈ 1.13
    → 最终使用 1px
```

字幕最长宽度限制为画布宽度的 90%；超过时按字符宽度自动换行。最终文字图层水平居中，放在距离底部约 152px 的位置。

当前实现从 `text_rec` 中实际读取的是 `font_name`。字幕颜色仍取 `render_video` 输入里的 `font_color`；本次两边恰好都是白色，所以结果没有冲突。

## 11. 本次音频怎样合成

### 原视频声音

三个原始素材都有音轨，但本次默认参数是：

```json
{
  "include_video_audio": false
}
```

因此三个源视频的原声没有进入最终 MP4。

### 配音

时间线中的配音轨道是：

```json
{
  "voiceover": []
}
```

所以没有旁白音频。

### BGM

时间线要求使用：

```json
{
  "path": "/app/resource/bgms/music_0031.mp3",
  "source_window": {
    "start": 0,
    "end": 11053
  }
}
```

渲染器从 BGM 开头截取约 11.053 秒，并使用：

```text
bgm_volume_scale = 0.25
```

也就是以原始音量的 25% 混入。

本次最终音频关系是：

```text
素材原声：无
配音：无
BGM：music_0031.mp3 的开头约 11.053 秒，音量 25%
```

## 12. MP4 是怎样编码出来的

画面、字幕和音频组合完成后，MoviePy 调用 FFmpeg 写出文件。本次核心编码配置是：

```text
video codec：libx264
audio codec：aac
preset：veryfast
CRF：23
threads：0，由 FFmpeg 自动使用可用线程
fps：30
```

`CRF` 控制 H.264 质量与文件大小。本次 Agent 没有指定，因此使用默认 `23`。

完整渲染计算摘要见 [08-render-calculation.reconstructed.json](evidence/render-video/08-render-calculation.reconstructed.json)。

## 13. 最终生成了什么

渲染 artifact 的完整内容是：

```json
{
  "payload": {
    "output_path": "/app/.storyline/.server_cache/e78297ae266440caac2cac2dc5575c51/render_video_1789241021.2011614/output_f60a792d_1789241021264.mp4",
    "output_basename": "output_f60a792d_1789241021264.mp4",
    "duration_s": 11.053,
    "output_size": {
      "width": 1080,
      "height": 608
    }
  },
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "render_video_1789241021.2011614",
  "node_id": "render_video",
  "create_time": 1789241036.4077234
}
```

这里产生了两个不同但相关的结果：

```text
MP4 文件
    保存真正的视频二进制内容

render_video artifact
    保存 MP4 路径、文件名、时长和尺寸
```

完整 artifact：[09-render-video-artifact.json](evidence/render-video/09-render-video-artifact.json)。

## 14. tool result 怎样返回 Agent LLM

系统没有把 MP4 二进制或完整 artifact 放进 Agent 对话，而是返回摘要：

```json
[
  {
    "content": "时间线已组织好。最后一步，渲染成片。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_O3EyyrcJJyurjggoZwn61660",
        "function": {
          "name": "storyline_render_video",
          "arguments": "{\"mode\": \"auto\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '[2026-09-12T19:23:56.294036] Video generated successfully, duration: 11.053 seconds, path: /app/.storyline/.server_cache/e78297ae266440caac2cac2dc5575c51/render_video_1789241021.2011614/output_f60a792d_1789241021264.mp4', 'INFO_USER': '[2026-09-12T19:23:56.293610] Video generated successfully, duration: 11.053 seconds, path: /app/.storyline/.server_cache/e78297ae266440caac2cac2dc5575c51/render_video_1789241021.2011614/output_f60a792d_1789241021264.mp4', 'preview_urls': ['/app/.storyline/.server_cache/e78297ae266440caac2cac2dc5575c51/render_video_1789241021.2011614/output_f60a792d_1789241021264.mp4'], 'artifact_id': 'render_video_1789241021.2011614'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_O3EyyrcJJyurjggoZwn61660"
  }
]
```

相同的 tool call ID 把调用和结果配对。Agent 直接得到：

```text
渲染成功
时长 11.053 秒
MP4 路径
artifact ID
```

完整回送请求：[10-agent-resume-request.json](evidence/render-video/10-agent-resume-request.json)。

## 15. Agent 最终怎样回复用户

渲染完成后没有更多工具需要调用。Agent LLM 的最终响应是：

```text
搞定了！测试视频已经渲染出来 ✅

- 素材：3 个片段全部保留，按「早晨咖啡 → 城市漫步 → 日落休闲」顺序排列
- 字幕：用了干净的思源黑体
- 转场：开头淡入、结尾淡出
- 音乐：加了一首轻松治愈的 BGM
- 配音：按你要求跳过了

不过有件事得跟你说清楚：你说想要 5 秒的成片，但最后渲染出来是 11 秒。原因是这段素材本身是 3 个视频片段，每个片段至少要保留一定时长，我手上的工具没办法强制把总时长压缩到 5 秒——除非删掉部分片段或做加速处理。如果你确实想要 5 秒，我可以帮你只保留其中一个片段或加快播放速度，你看要哪种？
```

这里 Agent 根据 tool result 中的 `11.053 seconds` 意识到结果没有满足 5 秒要求，并在最终回复中明确说明。

其中“3 个片段全部保留”只表示三个 `clip_id` 都进入了成片，并不表示每个 5 秒源片段的全部内容都被保留。真实时间线分别只使用了三个片段开头的 `3065ms`、`3715ms` 和 `4273ms`。

完整响应：

- [11-agent-final-response.sse.txt](evidence/render-video/11-agent-final-response.sse.txt)
- [11-agent-final-response.parsed.json](evidence/render-video/11-agent-final-response.parsed.json)

## 16. `mode` 的实际行为

`render_video` 是固定流程节点。当前代码中：

```text
mode = auto
    → 执行渲染

mode = skip
    → default_process 仍然调用同一个渲染流程

mode = default
    → default_process 仍然调用同一个渲染流程
```

因此它不能真正跳过。传入 `skip` 仍然会尝试生成视频。

## 17. 用一句话理解整个过程

`storyline_render_video` 读取已经确定的四轨时间线、字幕字体和首尾淡入淡出配置，用 MoviePy 组合画面与音频、用 Pillow 绘制字幕，再交给 FFmpeg 编码为 1080×608、30fps、11.053 秒的 MP4；它忠实执行时间线，不会自动把 11.053 秒修正成用户最初要求的 5 秒。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 43 至 46 行、四个前置 artifact、`render_video` artifact，以及当前 `RenderVideoNode`、`RenderVideoPipeline`、`PillowSubtitleRenderer` 和 `AudioTrackComposer` 源码。完整请求、原始 SSE、解析响应、依赖 artifact、重建输入和渲染计算摘要存放在 [evidence/render-video](evidence/render-video/) 中。
