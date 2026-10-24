# `storyline_split_shots`：完整素材为什么要先变成 clips

这篇笔记解释 `storyline_split_shots` 在 OpenStoryline 中解决的核心问题，以及 LLM、artifact、TransNetV2 和 ffmpeg 如何共同完成这一步。

它的重点不是罗列调用步骤和字段，而是建立一条完整的理解链：

```text
完整素材 media
-> 为镜头检测准备分析帧
-> 预测镜头边界
-> 把预测整理成可执行切点
-> 生成或引用视频片段
-> 形成后续节点统一使用的 clips
```

## 1. 为什么需要 `split_shots`

`load_media` 建立的是素材层记录。一个 `media` 通常对应用户上传的一个完整文件：

```text
media_0001 = 第一个完整视频
media_0002 = 第二个完整视频
media_0003 = 第三个完整视频
```

但后续的视频理解、筛选、排序和时间线规划，需要操作的是视频中的片段，而不只是整个文件。因此，系统需要把“素材”转换成“可编辑单元”：

```text
media_0001
  -> clip_0001：media_0001 的第一个时间区间
  -> clip_0002：media_0001 的第二个时间区间

media_0002
  -> clip_0003：media_0002 的一个时间区间
```

`split_shots` 最重要的作用并不只是“切文件”，而是建立一层新的数据抽象：

```text
media 表示用户提供了什么素材
clip 表示后续剪辑系统可以操作什么片段
```

即使某个视频最终没有被物理切开，它仍会被包装成一个 `clip`。这样，下游节点只需要处理 `clips`，不必区分“完整视频”和“切出来的视频段”。

## 2. 它处在整条链路的什么位置

在这次真实剪辑中，相关顺序是：

```text
load_media
-> split_shots
-> understand_clips
-> filter_clips
-> group_clips
-> generate_script
-> plan_timeline_pro
-> render_video
```

代码也明确声明了这种关系：

```python
require_prior_kind=["load_media"]
default_require_prior_kind=["load_media"]
next_available_node=["understand_clips"]
```

这说明 `split_shots` 是一个边界节点：

```text
上游负责发现和登记原始素材
split_shots 负责形成片段集合
下游开始理解和组织片段内容
```

源码位于：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\src\open_storyline\nodes\core_nodes\split_shots.py
```

## 3. 为什么 LLM 只传 `{"mode":"auto"}`，节点仍知道视频在哪里

这里要区分两条同时存在的链路。

### 3.1 控制链：决定下一步做什么

LLM 看见 `load_media` 已经完成，于是返回：

```json
{
  "name": "storyline_split_shots",
  "arguments": "{\"mode\": \"auto\"}"
}
```

这条 `tool_call` 表达的是控制意图：

```text
下一步执行 split_shots，并采用自动处理方式。
```

它不承担传输全部业务数据的职责，所以其中没有视频路径和媒体列表。

### 3.2 数据链：提供节点真正处理的数据

`load_media` 执行后已经产生结构化结果，其中包含 `media_id`、文件路径和媒体元数据。节点框架把这份上游结果作为 `split_shots` 的输入。

`split_shots` 从这里读取媒体：

```python
def _extract_media(self, inputs):
    return (inputs.get("load_media") or {}).get("media", []) or []
```

所以真实关系是：

```text
LLM tool_call
  -> 决定调用哪个能力

load_media artifact
  -> 提供这个能力真正处理的数据
```

这解释了为什么 `mode:auto` 不是在让 MCP “猜视频”。视频已经通过 session 中的节点状态和上游 artifact 进入执行上下文，`tool_call` 只负责推动工作流前进。

## 4. 从视频到切点，中间发生了什么

`split_shots` 对每个视频执行的是一条“分析后再执行”的流水线：

```text
原视频
-> ffmpeg 解码出分析帧
-> TransNetV2 给出逐帧预测
-> TransNetV2 辅助函数形成场景区间和时间
-> OpenStoryline 调整切点
-> ffmpeg 按最终切点生成片段文件
```

这里最容易混淆的是：TransNetV2 不切文件，ffmpeg 也不决定哪里应该切。两者承担的是不同层次的工作。

### 4.1 ffmpeg 先把视频变成适合模型分析的帧

视频文件不能直接作为这个模型的张量输入。OpenStoryline先让 ffmpeg 解码视频，并输出：

```text
每秒 25 帧
每帧 48 x 27 像素
RGB24 像素格式
```

例如，原视频可能是：

```text
1920 x 1080，60 fps
```

进入模型的分析表示则是：

```text
48 x 27，25 fps
```

这里同时发生了两种不同的缩减：

- 降低帧率：每秒分析更少的画面。
- 降低分辨率：每张画面包含更少的像素。

这个阶段只需要判断整体画面是否发生了足以构成镜头边界的变化，不需要识别人脸细节或读取小字。较低成本的分析帧足以服务镜头边界检测，也显著减少模型计算量。

需要注意，`25 fps` 是镜头分析时间轴的采样率，不代表原视频真的被转换成了 25 fps。原文件在这个阶段没有被改写。

### 4.2 TransNetV2 原始返回的是逐帧预测

OpenStoryline 调用：

```python
single_prediction, _all_prediction = model.predict_raw(frames_tensor)
```

当前实现继续使用 `single_prediction`，而没有使用 `_all_prediction`。整理后，它可以理解成一条与分析帧对应的概率序列：

```text
分析帧序号：  0     1     2     3     4   ...
切换概率：  0.01  0.03  0.92  0.87  0.04 ...
```

这些值不是时间，也不是已经切好的镜头。它们表达的是模型对每个分析位置发生镜头切换的置信程度。

项目使用的阈值是：

```python
DEFAULT_SCENE_DETECTION_THRESHOLD = 0.5
```

因此 `0.95`、`0.94` 和 `0.85` 都高于阈值，都会成为边界判断的候选；`0.49` 则低于阈值。但不能简单地把每一个高于阈值的帧都当成独立切点，因为一次转场可能让连续多帧都出现高概率。

### 4.3 概率如何变成场景区间和时间

OpenStoryline 把概率序列交给 TransNetV2 包提供的转换函数：

```python
scenes = model.predictions_to_scenes_with_data(
    prediction,
    fps=25.0,
    threshold=0.5,
)
```

这个函数承担两件事：

1. 根据阈值和连续预测，把逐帧概率整理成场景边界。
2. 根据分析帧率，把帧位置换算为时间。

最基本的时间关系是：

```text
时间（秒） = 分析帧位置 / 25
```

例如，边界位于第 75 个分析帧附近，对应时间约为：

```text
75 / 25 = 3 秒
```

所以要准确区分两层返回值：

```text
model.predict_raw(...)
-> 原始逐帧预测概率

model.predictions_to_scenes_with_data(...)
-> 整理后的场景区间，其中带有 start_time 和 end_time
```

OpenStoryline随后读取场景的 `end_time`，将除了视频最终结束时间之外的场景结束时间作为候选切点：

```text
scenes
-> 每个 scene 的 end_time
-> 去掉最后一个 end_time（通常是整段视频的结尾）
-> split_points_seconds
```

最后一个结束时间不能作为内部切点，因为它只表示“视频到这里结束”，并不表示结尾之后还要生成另一个片段。

### 4.4 为什么模型给出的边界还不能直接使用

镜头检测回答的是“画面语义上可能在哪里切换”，但剪辑系统还要考虑片段是否实用。

项目默认要求：

```text
最短片段：1000 ms
最长片段：30000 ms
```

OpenStoryline用 `enforce_shot_duration_constraints_on_split_points_seconds(...)` 调整候选切点：

- 某段短于最短时长时，删除相关切点，把它与相邻片段合并。
- 某段长于最长时长时，在该段内部补充均匀切点。

这意味着最终切点不完全等同于模型预测。它是两种判断共同作用的结果：

```text
TransNetV2 提供视觉上的镜头边界
+
OpenStoryline 施加工程上的片段时长约束
=
最终可执行切点
```

因此，最长时长产生的切点不一定是自然镜头边界。它存在的目的，是避免后续节点面对过长且难以处理的片段。

### 4.5 ffmpeg 最后才真正切视频

最终切点确定后，OpenStoryline再次调用 ffmpeg：

```python
segments = segment_video_stream_copy_with_ffmpeg(
    input_video=input_video_path,
    split_points_seconds=split_points_seconds,
    ...
)
```

这一次 ffmpeg 不再为模型准备输入，而是执行文件层面的分段：

```text
原视频 + 最终切点
-> clip_0001.mp4
-> clip_0002.mp4
-> clip_0003.mp4
```

代码采用 stream copy，也就是尽量复制原有编码数据，而不是完整解码后重新编码。这样通常更快，也避免不必要的重复压缩。

所以 ffmpeg 在这里有两个完全不同的角色：

```text
第一次：解码并缩放分析帧，服务 TransNetV2
第二次：按照已经确定的时间切点，生成实际片段文件
```

## 5. TransNetV2、权重和 OpenStoryline 是什么关系

TransNetV2 是外部的镜头边界检测模型，不是 FireRed-OpenStoryline 自己定义的模型结构。项目依赖：

```text
transnetv2_pytorch==1.0.5
```

并加载配置指定的权重文件：

```toml
[split_shots]
transnet_weights = ".storyline/models/transnetv2-pytorch-weights.pth"
transnet_device = "cpu"
```

两者的关系是：

```text
TransNetV2 代码
-> 定义模型由哪些计算层组成，以及输入输出如何流动

transnetv2-pytorch-weights.pth
-> 保存训练完成后模型内部学到的参数
```

权重不是训练视频的集合，也不是每次运行时继续训练所需的数据。OpenStoryline加载模型结构和预训练权重后，执行的是推理。因此普通使用这项能力时，不需要自己收集数据重新训练。

OpenStoryline在这之上负责输入准备、阈值配置、时间约束、文件切分以及结果包装：

```text
TransNetV2 提供“怎么看出镜头边界”的能力
OpenStoryline 决定“怎样把边界变成工作流中的 clips”
```

## 6. 没有检测到切点时，为什么仍会得到 clip

如果视频短于最短片段时长，或者最终没有留下可用切点，系统不会认为执行失败。

当前代码配置是：

```python
COPY_VIDEO_WHEN_NO_SPLIT = False
```

这时不会额外复制文件，而是直接让一个 `clip` 引用原视频路径：

```text
media_0001 的完整时间范围
-> clip_0001
-> path 仍然指向原视频
```

这体现了 `clip` 的本质：它首先是一个带来源和时间范围的逻辑片段，不必一定对应一个新生成的文件。

图片也会进入统一的 `clips` 输出，不过图片不需要镜头检测。系统为图片创建 `kind: image` 的 clip，使后续节点可以通过同一个片段集合处理视频和图片。

## 7. `clip` 为什么还要保留 `source_ref`

一个典型视频 clip 形如：

```json
{
  "clip_id": "clip_0001",
  "kind": "video",
  "path": "...",
  "fps": 30,
  "source_ref": {
    "media_id": "media_0001",
    "start": 0,
    "end": 5000,
    "duration": 5000,
    "height": 1080,
    "width": 1920
  }
}
```

这些信息同时表达了两件事：

- `path` 告诉后续程序从哪里读取这个片段。
- `source_ref` 保留它在原始素材中的身份和时间位置。

如果只保存切出来的文件路径，系统会失去“这个片段来自哪个素材、对应原素材的哪一段”这一层关系。保留 `source_ref` 后，后续节点可以理解、筛选和排序 clip，而时间线与渲染阶段仍能追溯其素材来源。

## 8. artifact 在这里承担什么作用

`split_shots` 完成后，会把 `clips` 作为节点的结构化产出保存下来。artifact 可以理解为工作流节点之间的数据交接记录：

```text
节点执行过程
-> 产生结构化结果
-> 保存为 artifact
-> 后续节点读取该结果
```

这次真实执行中的 `split_shots` artifact 位于：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\split_shots\split_shots_1789240961.7285173.json
```

其中的核心数据是：

```text
payload.clips
```

artifact 让节点之间不必把所有结构化数据塞回 LLM 消息。LLM主要负责决定工作流方向，节点则通过 artifact 传递视频处理所需的完整数据。

## 9. clips 如何接到后续剪辑链路

`split_shots` 解决的是“片段边界”，但它并不知道片段中有什么内容，也不判断片段是否符合用户要求。

这些职责被拆给后续节点：

```text
split_shots
-> 确定有哪些 clips，以及每个 clip 的路径和时间范围

understand_clips
-> 理解每个 clip 的画面内容，形成语义描述

filter_clips
-> 根据用户目标决定哪些 clip 应保留

group_clips
-> 对保留的 clip 分组和排序

plan_timeline_pro
-> 把 clip 安排到最终时间线

render_video
-> 按时间线读取和渲染实际媒体
```

这种拆分把三个不同问题分开：

```text
视频在哪里发生画面边界？     -> split_shots
这些片段分别是什么内容？     -> understand_clips
哪些片段应该进入最终成片？   -> filter/group/plan
```

TransNetV2 只需要解决第一个问题，因此它不必理解用户要求，也不必判断镜头“好不好”。

## 10. `split_shots` 前后的完整 LLM JSON

真实 HTTP trace：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl
```

这一段的顺序是：

```text
第 3 行 request：load_media 已执行完，请 LLM 决定下一步
第 4 行 response：LLM 要求调用 storyline_split_shots
Agent 执行 storyline_split_shots
第 5 行 request：携带 split_shots 的执行结果，再次请求 LLM
第 6 行 response：LLM 要求调用 storyline_understand_clips
```

### 10.1 第 4 行 response 的完整解析结果

第 4 行原始数据由 37 个 SSE JSON chunks 和一个 `data: [DONE]` 组成。下面的 JSON 是把全部 chunks 按 `delta` 顺序合并后的完整逻辑响应。它不保留“某几个字符来自第几个 chunk”这种传输边界，但没有省略任何响应内容、tool call、结束原因或 usage 字段。

```json
{
  "id": "5849cc98-84a5-4f89-9499-4e7d5ce8c921",
  "object": "chat.completion.chunk",
  "created": 1789240960,
  "model": "deepseek-flash",
  "system_fingerprint": "aeb56401ca74e127821c4f9126dcb669",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "reasoning_content": "3 videos loaded. Now split shots.",
        "content": "素材加载完成，一共 3 个视频。接下来做镜头切分。",
        "tool_calls": [
          {
            "index": 0,
            "id": "call_00_LIMqSlSnWYl4xNV1Zah18309",
            "type": "function",
            "function": {
              "name": "storyline_split_shots",
              "arguments": "{\"mode\": \"auto\"}"
            }
          }
        ]
      },
      "logprobs": null,
      "finish_reason": "tool_calls"
    }
  ],
  "usage": {
    "prompt_tokens": 5985,
    "completion_tokens": 65,
    "total_tokens": 6050,
    "prompt_tokens_details": {
      "cached_tokens": 5760
    },
    "completion_tokens_details": {
      "reasoning_tokens": 8
    },
    "prompt_cache_hit_tokens": 5760,
    "prompt_cache_miss_tokens": 225
  }
}
```

这份 JSON 表明，LLM 同时返回了：

```text
reasoning_content：模型的推理文本
content：给用户显示的文本
tool_calls：要求 Agent 调用 storyline_split_shots
finish_reason = tool_calls：本轮以工具调用结束
usage：本轮 token 使用信息
```

### 10.2 第 5 行 request 的完整 body

下面是 Agent执行完 `storyline_split_shots` 后，下一次实际发送给 LLM 的 HTTP request body。它包含完整 system prompt、完整 6 条 messages 和完整 18 个 tools schema，没有使用占位符，也没有缩写字段。

```json
{
  "messages": [
    {
      "content": "## 角色\n你是一个短视频剪辑助理。\n\n## 任务\n- 你需要理解用户提出的需求，执行合适的工具完成剪辑，并避免向用户回复过于专业的剪辑术语。你会拿到一个关于剪辑的工具函数的描述列表。\n- 如果用户是第一次提出“帮我剪辑/处理素材”等类似需求，请先用自然语言列出你计划如何使用给定的剪辑工具及理由，待用户确认。\n- 注意你只能使用你可用的剪辑工具进行剪辑，如果工具能力范围超出了用户需求，请明确告诉用户你做不到。\n- 当用户指定一个剪辑风格时，优先查看工具里面是否有满足的技能（描述以【SKILL】为开头的工具），如果有匹配的技能，优先使用。\n- 整个剪辑流程中，有些节点是固定的，你无法改动；你计划的范围仅限于可以改动的节点。除非用户明确想要跳过某个步骤，否则在列出计划时，**尽可能使用多的工具以丰富视频内容**，除非用户明确指出不要某个元素。\n- 注意，有些节点依赖前面节点的结果，具体的依赖关系你可以在工具描述中看到，请在工具调用前检查依赖。工具会自己寻找依赖的结果，你不需要将前面节点的结果输入到工具参数中。如果工具需要输入参数，会在工具描述中另加说明，请填入合适的参数。\n- 重要：**每次只调用一个工具，不允许并行工具调用**。如果需要连续调用工具，每次调用完工具后，向用户简单总结本次工具调用的结果和下一步的意图，增强互动感，然后再进行下一次工具调用。\n\n## 流程参考\n常规剪辑流程如下，这里每一步都对应一个或多个工具供你使用：\n- 搜索素材 \"search_media\"（可跳过）。如果你发现用户并没有上传素材，可以提示用户你可以上网搜索素材。搜索素材后需要运行load_media工具才可以真正加载到素材。\n- 素材加载 \"load_media\"（固定）。用于获取输入素材的路径、长宽等基础信息。\n- 镜头切分 \"split_shots\"（可跳过）。将素材按镜头切分成片段。\n- 内容理解 \"understand_clips\"（可跳过）。 为每个片段(clips)生成一段描述（captions）\n- 镜头筛选 \"filter_clips\"（可跳过）。根据用户要求，筛选出符合要求的片段(clips)\n- 片段分组 \"group_clips\"（可跳过，但应默认运行）。根据用户要求，对片段进行排序和分组，组织合理的叙事逻辑，并辅助后续文案生成。\n- 文案生成 \"generate_script\"（可跳过）。根据用户要求，生成视频文案。\n- 元素推荐 （可跳过，但应默认运行）。根据用户要求，推荐花字、标题、特效、转场、配音音色等元素。\n- 配音生成 \"generate_voiceover\"（可跳过）。根据文案生成对应的配音。\n- 背景音乐选取 \"select_BGM\"（可跳过）。选择合适的背景音乐。\n- 组织时间线 \"plan_timeline\"（固定）。根据前面的视频片段、文案、语音和BGM，组织成合理的时间线。\n- 渲染成片。\"render_video\"（固定）。根据时间线渲染成片。\n此外，虽然你在工具调用后只能看到summary，但你有一个工具可以读取任意中间节点的输出。你可以用它完成更复杂的任务。\n\n## 风格要求\n- 用简洁、口语化的语言\n\n## 语言\n- 根据用户使用的语言进行回答\n- 如用户要求用英语、日语等回答，则使用相应语言\n\n\n## 示例\n示例 1：列出计划\n[用户]：\n我想要你帮我把我的素材剪辑成旅行vlog，\n[助手]：\n我将要使用如下流程进行剪辑：  \n- 素材加载：需要先对输入素材建立索引，提取元信息。\n- 镜头切分：将素材按镜头切分成片段。\n- 内容理解：对每个镜头生成文字描述。\n- 镜头筛选：筛选出适合放在旅行vlog中的镜头。\n- 片段分组：组织一个合理的素材顺序。\n- 文案生成：配一段符合主题的文案。\n- 花字检索：添加花字元素，增强视觉效果。\n- 转场检索：添加合适的转场效果，让镜头切换更自然。\n- 配音生成：根据文案生成配音。\n- 音乐推荐：选择轻松愉快的vlog背景音乐。\n- 组织时间线：这是剪辑的固定流程。\n- 渲染成片：这也是剪辑的固定流程。\n\n示例 2：不需要工具时，直接回答\n用户：\n什么是“镜头切分”？\n助手：\n镜头切分是指把原始视频按照画面内容或语义边界切分成若干个独立的镜头片段，通常会结合画面变化、音频变化等特征来判断切分点，用于后续剪辑、检索或分析。\n\n示例3：取消配音\n用户：\n之前你给我剪的视频有配音，我现在不想要视频的配音了。\n\n此时助手需要执行generate_voiceover工具，参数mode选择skip。\n\n示例4：取消筛选\n用户：\n你怎么把我的素材丢掉了那么多，我要使用全部素材。\n\n此时助手需要执行filter_clips工具，参数mode选择skip。\n\n【User media upload status】The following fields are used to determine the nature of the media provided by the user: \n{\"Number of media carried in this message sent by the user\": 3, \"Total number of media sent by the user in all conversations\": 3, \"Total number of media in user's media library\": {\"image number in user's media library\": 0, \"video number in user's media library\": 3}}",
      "role": "system"
    },
    {
      "content": "基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。",
      "role": "user"
    },
    {
      "content": "我先说下计划：加载你的素材 → 切分镜头 → 理解内容 → 分组排序 → 生成文案 → 元素推荐（花字/转场）→ 选背景音乐 → 组织时间线 → 渲染成片。因为你说不要配音，所以配音这步我会跳过。\n\n先加载素材，建立索引。",
      "role": "assistant",
      "tool_calls": [
        {
          "type": "function",
          "id": "call_00_omq8yEAaprVmw6risxuU6828",
          "function": {
            "name": "storyline_load_media",
            "arguments": "{\"mode\": \"auto\"}"
          }
        }
      ]
    },
    {
      "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:22:40.602268] Added media_0001: (video)\\n[2026-09-12T19:22:40.669416] Added media_0002: (video)\\n[2026-09-12T19:22:40.733529] Added media_0003: (video)\\n[2026-09-12T19:22:40.734001] [Node load_media] Media indexing completed successfully: 3 video(s), 0 image(s)', 'preview_urls': [], 'artifact_id': 'load_media_1789240960.4566667'}, 'tool_excute_result': {}}, 'isError': False}",
      "role": "tool",
      "tool_call_id": "call_00_omq8yEAaprVmw6risxuU6828"
    },
    {
      "content": "素材加载完成，一共 3 个视频。接下来做镜头切分。",
      "role": "assistant",
      "tool_calls": [
        {
          "type": "function",
          "id": "call_00_LIMqSlSnWYl4xNV1Zah18309",
          "function": {
            "name": "storyline_split_shots",
            "arguments": "{\"mode\": \"auto\"}"
          }
        }
      ]
    },
    {
      "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:22:42.475031] clip_0001 split successfully\\n[2026-09-12T19:22:43.052775] clip_0002 split successfully\\n[2026-09-12T19:22:43.589515] clip_0003 split successfully\\n[2026-09-12T19:22:43.589790] split_shots executed successfully, output clips count: 3', 'preview_urls': ['/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0001.mp4', '/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0002.mp4', '/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0003.mp4'], 'artifact_id': 'split_shots_1789240961.7285173'}, 'tool_excute_result': {}}, 'isError': False}",
      "role": "tool",
      "tool_call_id": "call_00_LIMqSlSnWYl4xNV1Zah18309"
    }
  ],
  "model": "deepseek-v4-flash",
  "stream": true,
  "temperature": 0.1,
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "storyline_load_media",
        "description": "Loads and indexes input media. Entry point with no dependencies; required by all downstream operations",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Automatic mode; skip: Skip mode; default: Default mode",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_search_media",
        "description": "search",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Automatically search media from pexels; skip: skip search; default: skip search",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "photo_number": {
              "default": 0,
              "description": "The number of images the user wants to obtain",
              "type": "integer"
            },
            "video_number": {
              "default": 5,
              "description": "The number of videos the user wants to obtain",
              "type": "integer"
            },
            "search_keyword": {
              "default": "scenery",
              "description": "Keyword of the media the user wants to obtain. Only one keyword is allowed; multiple keywords are not permitted.",
              "type": "string"
            },
            "orientation": {
              "default": "landscape",
              "description": "landscape: The screen is wider horizontally and narrower vertically, making it suitable for computer screens, landscape images, etc;portrait: The screen is higher vertically and narrower horizontally, making it suitable for mobile browsing and close-up shots of people.",
              "enum": [
                "landscape",
                "portrait"
              ],
              "type": "string"
            },
            "min_video_duration": {
              "default": 1,
              "description": "The shortest duration of footage requested by the user in seconds.",
              "type": "integer"
            },
            "max_video_duration": {
              "default": 30,
              "description": "The longest duration of footage requested by the user in seconds.",
              "type": "integer"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_split_shots",
        "description": "Segment input video based on shot boundary detection",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Automatically segment shots based on scene changes, treat images as single shots; skip: Do not segment shots; default: Use default segmentation method",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "min_shot_duration": {
              "default": 1000,
              "description": "Segmented shots must not be shorter than this duration (unit: milliseconds)",
              "type": "integer"
            },
            "max_shot_duration": {
              "default": 10000,
              "description": "If a single shot exceeds this duration, force segmentation (unit: milliseconds)",
              "type": "integer"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_understand_clips",
        "description": "Analyze clips and generate descriptions for each. Requires `load_media` and `split_shots` output",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Generate descriptions based on media content; skip: Do not generate descriptions; default: Use default description generation method",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_filter_clips",
        "description": "Filter clips based on their descriptions according to user requirements. Depends on the results from the understand_clips tool",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Filter clips based on user requirements; skip: Skip filtering; default: Use default filtering method",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for clip filtering; if none provided, formulate one based on media materials and other editing requirements.",
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_group_clips",
        "description": "Group clips based on their descriptions according to user requirements. Depends on the filter_clips tool output",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Organize clips in a logical order based on narrative flow of media content and user's sequencing requirements; skip: Skip sorting; default: Use default ordering method",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for media organization order; if none provided, arrange in a logical narrative sequence following standard conventions.",
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_generate_script",
        "description": "Generate video script/copy that can be used to synthesize voice-over or be directly applied as video subtitlesSupport lyrical, humorous, and casual styles, and consider using the `subtitle_imitation_skill` for special styles.",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Generate appropriate script based on media content and user's script requirements; skip: Skip, do not add subtitles; default: Use default script",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for the script.",
              "type": "string"
            },
            "custom_script": {
              "additionalProperties": true,
              "default": {},
              "description": "If user has specific character-level editing requirements for script/title, pass the edited custom script and title through this parameter. Format should be based on the original script generation output format but with the subtitle_units field removed. In this case, mode must use `auto`, other modes are prohibited",
              "type": "object"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_script_template_rec",
        "description": "Select an script template (script style) for generation",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Select an appropriate copywriting template based on the material content and user's requirements for voiceover style; skip: Skip;",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's specific requirements for the script style.",
              "type": "string"
            },
            "filter_include": {
              "additionalProperties": {
                "items": {
                  "type": "string"
                },
                "type": "array"
              },
              "default": {},
              "description": "Positive filter conditions. Multiple dimensions are combined with AND, multiple values within the same dimension are combined with OR.\nSupported dimensions:\n- tags: category, one or more of [Life, Food, Beauty, Entertainment, Travel, Tech, Business, Vehicle, Health, Family, Pets, Knowledge]",
              "type": "object"
            },
            "filter_exclude": {
              "additionalProperties": {
                "items": {
                  "type": "string"
                },
                "type": "array"
              },
              "default": {},
              "description": "Negative filter conditions. Items matching these conditions will be excluded. The semantics are the same as filter_include. Supported dimensions: tags, id.",
              "type": "object"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_generate_voiceover",
        "description": "Generate voice-over based on the script",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Generate appropriate voiceover based on media content and user's voice requirements; skip: Skip voiceover; default: Use default voiceover",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for voiceover.",
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_select_bgm",
        "description": "Select appropriate BGM based on user requirements",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Select appropriate music based on media content and user's music requirements; skip: Do not use music; default: Use default music",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for background music.",
              "type": "string"
            },
            "filter_include": {
              "additionalProperties": {
                "items": {
                  "anyOf": [
                    {
                      "type": "string"
                    },
                    {
                      "type": "integer"
                    }
                  ]
                },
                "type": "array"
              },
              "default": {},
              "description": "Positive filter conditions. Multiple dimensions are combined with AND, multiple values within the same dimension are combined with OR.\nSupported dimensions:\n- mood: music emotion, one or more of [Dynamic, Chill, Happy, Sorrow, Romantic, Calm, Excited, Healing, Inspirational]\n- scene: usage scene, one or more of [Vlog, Travel, Relaxing, Emotion, Transition, Outdoor, Cafe, Evening, Scenery, Food, Date, Club]\n- genre: music genre, one or more of [Pop, BGM, Electronic, R&B/Soul, Hip Hop/Rap, Rock, Jazz, Folk, Classical, Chinese Style]\n- lang: lyric language, one or more of [bgm, en, zh, ko, ja]\n- id: specific music ids (int)",
              "type": "object"
            },
            "filter_exclude": {
              "additionalProperties": {
                "items": {
                  "anyOf": [
                    {
                      "type": "string"
                    },
                    {
                      "type": "integer"
                    }
                  ]
                },
                "type": "array"
              },
              "default": {},
              "description": "Negative filter conditions. Items matching these conditions will be excluded. The semantics are the same as filter_include. Supported dimensions: mood, scene, genre, lang, id.",
              "type": "object"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_elementrec_transition",
        "description": "Recommend transition effects according to user needs and segment count, ensuring transition list length equals group count",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: add fade in and fade out transitions at beginning and end; skip: Do not use transitions; default: Use default transitions",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "duration": {
              "default": 1000,
              "description": "Duration of the transition in milliseconds",
              "type": "integer"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_elementrec_text",
        "description": "Recommend text effects according to user needs",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Select appropriate font style and color based on user's subtitle font style requirements; default: Use default font",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "user_request": {
              "default": "",
              "description": "User's requirements for font style",
              "type": "string"
            },
            "filter_include": {
              "additionalProperties": {
                "items": {
                  "anyOf": [
                    {
                      "type": "string"
                    },
                    {
                      "type": "integer"
                    }
                  ]
                },
                "type": "array"
              },
              "default": {},
              "description": "Positive filter conditions. Multiple dimensions are combined with AND, multiple values within the same dimension are combined with OR.\nSupported dimensions:\n- class: Font type, one or more[Creative, Handwriting, Calligraphy, Basic]\n",
              "type": "object"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_plan_timeline_pro",
        "description": "Create a coherent timeline by arranging video clips, subtitles, voice-over, and background music. ",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Automatic mode; skip: Skip mode; default: Default mode",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "use_beats": {
              "default": true,
              "description": "Whether clip transitions should sync with BGM beats",
              "type": "boolean"
            },
            "is_speech_rough_cut": {
              "default": false,
              "description": "Whether the input clips are from speech rough cut, which affects the default timeline strategy selection",
              "type": "boolean"
            },
            "is_ai_transition": {
              "default": false,
              "description": "Whether to build a minimal AI transition timeline by concatenating clips in order without subtitles, voiceover, cutting, or speed changes",
              "type": "boolean"
            },
            "image_duration_ms": {
              "default": 3000,
              "description": "Default duration for image clips in milliseconds when using the AI transition timeline branch",
              "type": "integer"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_render_video",
        "description": "Render final video from the timeline",
        "parameters": {
          "properties": {
            "mode": {
              "default": "auto",
              "description": "auto: Automatic mode; skip: Skip mode; default: Default mode",
              "enum": [
                "auto",
                "skip",
                "default"
              ],
              "type": "string"
            },
            "aspect_ratio": {
              "anyOf": [
                {
                  "type": "string"
                },
                {
                  "type": "null"
                }
              ],
              "default": null,
              "description": "When explicitly specified, forces the canvas to one of 16:9, 4:3, 1:1, 3:4, 9:16. If unset, the system automatically infers the most suitable aspect ratio."
            },
            "output_max_dimension_px": {
              "anyOf": [
                {
                  "type": "integer"
                },
                {
                  "type": "null"
                }
              ],
              "default": null,
              "description": "Maximum output size in pixels (longest side); defaults to 1080 and works with the aspect ratio."
            },
            "clip_compose_mode": {
              "default": "padding",
              "description": "How to fit media into the canvas: 'padding' keeps aspect ratio and fills empty areas with a solid color; 'crop' center-crops media to match the canvas aspect ratio.",
              "type": "string"
            },
            "bg_color": {
              "anyOf": [
                {
                  "maxItems": 1,
                  "minItems": 1,
                  "prefixItems": [
                    {
                      "type": "integer"
                    }
                  ],
                  "type": "array"
                },
                {
                  "items": {
                    "type": "integer"
                  },
                  "type": "array"
                },
                {
                  "type": "null"
                }
              ],
              "default": [
                0,
                0,
                0
              ],
              "description": "Background color for canvas padding, specified as an (R, G, B) tuple (no alpha channel)."
            },
            "crf": {
              "default": 23,
              "description": "CRF value (10–30), lower = better quality, larger file",
              "type": "integer"
            },
            "font_color": {
              "default": [
                255,
                255,
                255,
                255
              ],
              "description": "Font color, RGBA format (R, G, B, A), values range 0-255",
              "maxItems": 4,
              "minItems": 4,
              "prefixItems": [
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                }
              ],
              "type": "array"
            },
            "font_size": {
              "default": 40,
              "description": "Font size in pixels. Recommended range: 28–120.",
              "type": "integer"
            },
            "margin_bottom": {
              "default": 270,
              "description": "Bottom margin for subtitles in pixels. Defaults to 80; valid range: 40–1040.",
              "type": "integer"
            },
            "stroke_width": {
              "default": 2,
              "description": "Text stroke width (px), typically 0–8",
              "type": "integer"
            },
            "stroke_color": {
              "default": [
                0,
                0,
                0,
                255
              ],
              "description": "Text stroke color in RGBA format",
              "maxItems": 4,
              "minItems": 4,
              "prefixItems": [
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                },
                {
                  "type": "integer"
                }
              ],
              "type": "array"
            },
            "bgm_volume_scale": {
              "default": 0.25,
              "description": "Background music volume multiplier, range 0.0–3.0 (1.0 = default volume)",
              "type": "number"
            },
            "tts_volume_scale": {
              "default": 2,
              "description": "TTS volume multiplier, range 0.0–3.0 (1.0 = default volume)",
              "type": "number"
            },
            "include_video_audio": {
              "default": false,
              "description": "Whether to include the original video audio track",
              "type": "boolean"
            },
            "video_volume_scale": {
              "default": 1,
              "description": "Original video audio volume multiplier, range 0.0–3.0 (1.0 = default volume)",
              "type": "number"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_read_node_history",
        "description": "Retrieve the execution result of any node using its artifact_id",
        "parameters": {
          "properties": {
            "query_artifact_id": {
              "description": "The artifact_id used to retrieve the corresponding JSON",
              "type": "string"
            }
          },
          "required": [
            "query_artifact_id"
          ],
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "storyline_write_skills",
        "description": "Save the generated Agent Skill (Markdown format) to the file system and return the absolute file path on success.",
        "parameters": {
          "properties": {
            "skill_name": {
              "description": "Skill file name, e.g., 'fast_paced_vlog', without extension",
              "type": "string"
            },
            "skill_dir": {
              "default": ".storyline/skills/",
              "description": "Skill storage directory, defaults to '.storyline/skills/'",
              "type": "string"
            },
            "skill_content": {
              "default": "",
              "description": "Skill content in Markdown format",
              "type": "string"
            }
          },
          "required": [
            "skill_name"
          ],
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "subtitle_imitation_skill",
        "description": "【SKILL】基于用户提供的参考文案样本，对视频素材内容进行深度文风仿写，生成风格化脚本。",
        "parameters": {
          "properties": {
            "arguments": {
              "default": "",
              "description": "Arguments to pass to the skill",
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "create_profile_style_skill",
        "description": "【SKILL】分析当前剪辑逻辑与风格，总结并生成一个新的可复用 Skill 文件，存入剪辑技能库。",
        "parameters": {
          "properties": {
            "arguments": {
              "default": "",
              "description": "Arguments to pass to the skill",
              "type": "string"
            }
          },
          "type": "object"
        }
      }
    }
  ]
}
```

### 10.3 怎样理解这份 request

这份 request 不是“只把 split_shots 的结果发给 LLM”。它同时发送了四类信息：

```text
system     -> 告诉模型应该怎样工作
user       -> 告诉模型用户最终想完成什么
messages   -> 告诉模型此前发生了什么、当前进行到哪里
tools      -> 告诉模型这一轮仍然可以选择哪些能力
```

可以先把整个 request 理解成一句话：

```text
你是一个按指定剪辑流程工作的助手。
用户要把上传素材做成 5 秒测试视频，而且不要搜索素材、不要配音。
目前 load_media 和 split_shots 都已经执行成功。
这里有 18 个可用工具，请根据流程和当前进度决定下一步做什么。
```

下面按照 JSON 的真实层级逐块解释。

#### 第一块：`model`、`stream` 和 `temperature`

```json
{
  "model": "deepseek-v4-flash",
  "stream": true,
  "temperature": 0.1
}
```

这三个字段控制“怎样调用模型”：

| 字段 | 作用 | 在这次请求中的含义 |
|---|---|---|
| `model` | 指定使用哪个模型 | 使用 `deepseek-v4-flash` |
| `stream` | 指定响应是否流式返回 | `true` 表示响应会拆成 SSE chunks 返回 |
| `temperature` | 控制输出随机性 | `0.1` 较低，让工具选择和参数更稳定 |

这部分不描述剪辑业务，也不告诉模型当前进度。它只设置模型调用方式。

#### 第二块：`messages` 是模型这一次能看到的上下文

```text
messages[0] system
messages[1] user
messages[2] assistant：要求调用 load_media
messages[3] tool：返回 load_media 结果
messages[4] assistant：要求调用 split_shots
messages[5] tool：返回 split_shots 结果
```

这些消息按时间顺序排列。模型通过它们重新看到整段过程，而不是只看到最后一条 `tool` 消息。

模型接口本身不会因为上一次请求结束就自动记住此前发生的事情。Agent需要在下一次请求中重新携带相关历史，这就是为什么 `load_media` 和 `split_shots` 的调用与结果会再次出现在 request 里。

#### `messages[0]`：system message 定义工作规则

```text
role = system
```

它的 `content` 是完整 system prompt，主要包含：

- 模型的角色是短视频剪辑助理。
- 需要根据用户需求选择工具。
- 每次只能调用一个工具。
- 某些节点存在依赖，工具会自行寻找上游结果。
- 常规剪辑顺序是 `load_media -> split_shots -> understand_clips -> ...`。
- 当前会话上传了 3 个视频。

system message 提供的是长期规则和工作流知识。它不会直接证明 `split_shots` 已经执行成功，因为执行进度来自后面的历史消息。

#### `messages[1]`：user message 保留用户的原始目标

```json
{
  "role": "user",
  "content": "基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。"
}
```

这条消息定义最终目标和限制条件：

```text
目标：生成 5 秒测试视频
素材来源：用户上传的素材
限制：不要联网搜索素材
限制：不要配音
```

后续每一次工具选择都应该服务这个目标。例如模型不应调用搜索素材工具，后面也应跳过配音。

#### `messages[2]`：assistant message 要求调用 `load_media`

这条消息是此前某次 LLM response 被保存到对话历史后的结果。它同时包含两部分：

```text
content
-> 给用户看的计划和当前说明

tool_calls
-> 给 Agent 的结构化执行要求
```

```json
{
    "content": "我先说下计划：加载你的素材 → 切分镜头 → 理解内容 → 分组排序 → 生成文案 → 元素推荐（花字/转场）→ 选背景音乐 → 组织时间线 → 渲染成片。因为你说不要配音，所以配音这步我会跳过。\n\n先加载素材，建立索引。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_omq8yEAaprVmw6risxuU6828",
        "function": {
          "name": "storyline_load_media",
          "arguments": "{\"mode\": \"auto\"}"
        }
      }
    ]
  }
```

#### `messages[3]`：tool message 返回 `load_media` 的执行结果

```text
role = tool
tool_call_id = call_00_omq8yEAaprVmw6risxuU6828
```

这个 `tool_call_id` 与 `messages[2].tool_calls[0].id` 相同：

```json
{
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:22:40.602268] Added media_0001: (video)\\n[2026-09-12T19:22:40.669416] Added media_0002: (video)\\n[2026-09-12T19:22:40.733529] Added media_0003: (video)\\n[2026-09-12T19:22:40.734001] [Node load_media] Media indexing completed successfully: 3 video(s), 0 image(s)', 'preview_urls': [], 'artifact_id': 'load_media_1789240960.4566667'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_omq8yEAaprVmw6risxuU6828"
  }
```

#### `messages[4]`：assistant message 要求调用 `split_shots`

这条消息就是第 4 行 response 拼接完成后，保存进对话历史的结果：

```json
{
    "content": "素材加载完成，一共 3 个视频。接下来做镜头切分。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_LIMqSlSnWYl4xNV1Zah18309",
        "function": {
          "name": "storyline_split_shots",
          "arguments": "{\"mode\": \"auto\"}"
        }
      }
    ]
  }
```

这条消息表达的是：

```text
LLM已经根据 system 流程、用户目标和 load_media 结果作出决定：
下一步请 Agent 执行 storyline_split_shots。
```

它仍然只是调用要求，不是执行结果。

#### `messages[5]`：tool message 返回 `split_shots` 的执行结果

这是 Agent实际执行 `storyline_split_shots` 后新增的消息，也是本次 request 中最新的状态：

```json
{
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:22:42.475031] clip_0001 split successfully\\n[2026-09-12T19:22:43.052775] clip_0002 split successfully\\n[2026-09-12T19:22:43.589515] clip_0003 split successfully\\n[2026-09-12T19:22:43.589790] split_shots executed successfully, output clips count: 3', 'preview_urls': ['/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0001.mp4', '/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0002.mp4', '/app/outputs/e78297ae266440caac2cac2dc5575c51/media/media_0003.mp4'], 'artifact_id': 'split_shots_1789240961.7285173'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_LIMqSlSnWYl4xNV1Zah18309"
  }
```

因此第二组调用与结果是：

```text
messages[4]：请执行 split_shots
messages[5]：这是那次 split_shots 的执行结果
```

`messages[5].content` 告诉模型：

```text
clip_0001 切分成功
clip_0002 切分成功
clip_0003 切分成功
split_shots 执行成功
输出 clip 数量为 3
artifact_id = split_shots_1789240961.7285173
isError = False
```

注意，工具结果没有把三个 clips 的完整结构放进消息。它只给 LLM 提供执行摘要和 `artifact_id`。完整 clips 仍保存在工作流状态和 artifact 中，后续节点会在那里获取。

#### 第三块：`tools` 定义这一轮允许模型选择的能力

`tools` 数组包含 18 个工具定义。它不是“即将依次执行的 18 个工具”，而是这一轮交给模型的可选能力列表。

每个工具都采用相同的外层结构：

```json
{
  "type": "function",
  "function": {
    "name": "工具名称",
    "description": "工具做什么、依赖什么",
    "parameters": {
      "type": "object",
      "properties": {}
    }
  }
}
```

#### 第四块：`messages` 与 `tools` 如何共同产生下一步

`messages` 和 `tools` 解决的是两个不同问题：

```text
messages
-> 当前任务是什么
-> 前面做过什么
-> 哪些调用成功了
-> 工作流现在走到哪里

tools
-> 当前有哪些能力可选
-> 每项能力做什么
-> 每项能力需要哪些参数和依赖
```

模型进行下一步选择时，逻辑可以理解为：

```text
1. system prompt 说明常规顺序中 split_shots 后面是 understand_clips。
2. user message 说明最终仍要完成一个 5 秒测试视频。
3. messages[5] 说明 split_shots 已经成功，得到了 3 个 clips。
4. tools 中存在 storyline_understand_clips。
5. understand_clips 的 description 说明它依赖 load_media 和 split_shots，而这两个依赖都已满足。
6. 因此模型在下一次 response 中返回 storyline_understand_clips tool_call。
```

这里不存在 `split_shots` 函数直接调用 `understand_clips`。真正发生的是：

```text
split_shots 执行完成
-> Agent把执行结果加入 messages[5]
-> Agent携带完整 messages 和 tools 再请求 LLM
-> LLM结合流程、历史和工具描述选择 understand_clips
-> Agent再执行新的 tool_call
```

#### 用一句话读懂整个 request

这份 request 的完整意思是：

```text
请继续处理用户的 5 秒视频任务。
你必须遵守 system prompt 中的剪辑流程和单工具调用规则。
此前你要求加载素材，Agent已经成功加载了 3 个视频；
随后你要求切分镜头，Agent也已成功得到 3 个 clips。
现在请从提供的 18 个工具中选择下一项合适的能力，或者在不需要工具时直接回答。
```

## 11. 最终心智模型

可以把整个过程压缩成三层：

```text
决策层
LLM 看到 load_media 完成
-> 返回 storyline_split_shots tool_call

分析与执行层
ffmpeg 生成分析帧
-> TransNetV2 返回逐帧预测
-> 转换函数生成场景时间
-> OpenStoryline修正切点
-> ffmpeg 物理切分视频

工作流数据层
media 转换为 clips
-> clips 保存到 split_shots artifact
-> understand_clips 等后续节点继续处理
```

最关键的区分是：

```text
LLM 决定调用 split_shots
TransNetV2 预测哪里可能发生镜头切换
OpenStoryline 把预测变成满足工程约束的切点
ffmpeg 准备模型输入并执行实际文件切分
artifact 把 clips 交给后续工作流
```

因此，`storyline_split_shots` 不是一个单纯的“视频切割函数”。它是从原始媒体世界进入片段化剪辑世界的转换节点。
