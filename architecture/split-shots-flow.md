# storyline_split_shots Flow

这份笔记解释 `storyline_split_shots` 的完整过程：它做什么、输入从哪里来、TransNetV2 和 ffmpeg 各自负责什么、输出如何接到后续节点。

## 1. 一句话理解

`storyline_split_shots` 的作用是：

```text
把 load_media 得到的“素材 media 列表”
变成后续剪辑可用的“片段 clip 列表”
```

也就是：

```text
media_0001 = 一个完整视频
media_0002 = 一个完整视频
media_0003 = 一个完整视频

-> split_shots

clip_0001 = media_0001 的某一段
clip_0002 = media_0002 的某一段
clip_0003 = media_0003 的某一段
...
```

后续节点一般不直接处理“上传的完整视频”，而是处理 `clips`。

## 2. 它在工具链里的位置

典型顺序：

```text
load_media
-> split_shots
-> understand_clips
-> filter_clips
-> group_clips
-> generate_script
-> plan_timeline
-> render_video
```

`split_shots` 的上游是：

```text
load_media
```

下游通常是：

```text
understand_clips
```

代码中 `SplitShotsNode` 声明了依赖：

```python
require_prior_kind=["load_media"]
default_require_prior_kind=["load_media"]
next_available_node=["understand_clips"]
```

源码位置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\src\open_storyline\nodes\core_nodes\split_shots.py
```

## 3. 输入从哪里来

LLM 返回的 tool call 通常只是：

```json
{
  "mode": "auto"
}
```

它不会把视频文件路径传给 `split_shots`。

### 3.1 触发 split_shots 的真实 LLM 请求/响应

真实 HTTP trace：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl
```

其中：

```text
第 3 行: http_request
第 4 行: http_response
```

这一组请求/响应就是触发 `storyline_split_shots` 的那一轮。

#### 请求里有什么

第 3 行请求的核心结构是：

```json
{
  "model": "deepseek-v4-flash",
  "stream": true,
  "messages": [
    {
      "role": "system",
      "content": "## 角色\n你是一个短视频剪辑助理..."
    },
    {
      "role": "user",
      "content": "基于我上传的这个素材，做一个 5 秒测试视频。不要联网搜素材，不要配音。现在开始。"
    },
    {
      "role": "assistant",
      "content": "我先说下计划：加载你的素材 → 切分镜头 → 理解内容 → 分组排序 → 生成文案 → 元素推荐（花字/转场）→ 选背景音乐 → 组织时间线 → 渲染成片。因为你说不要配音，所以配音这步我会跳过。\n\n先加载素材，建立索引。",
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
      "role": "tool",
      "tool_call_id": "call_00_omq8yEAaprVmw6risxuU6828",
      "content": "{'summary': {'node_summary': {'INFO_USER': '... Added media_0001: (video)\\n... Added media_0002: (video)\\n... Added media_0003: (video)\\n... Media indexing completed successfully: 3 video(s), 0 image(s)', ...}}, 'isError': False}"
    }
  ],
  "tools": [
    "... 18 个 OpenStoryline tools schema ..."
  ]
}
```

关键点：

```text
这不是第一轮请求。
这是 load_media 已经执行完之后的下一轮请求。
```

所以模型在这一轮已经能看到：

```text
上轮 assistant 要求调用 storyline_load_media
对应 tool 结果说已经加载了 3 个 video
```

这就是它下一步选择 `storyline_split_shots` 的上下文依据。

#### 响应里有什么

第 4 行响应是 SSE stream。拼完后的核心内容是：

```text
content:
素材加载完成，一共 3 个视频。接下来做镜头切分。

tool_calls:
- id: call_00_LIMqSlSnWYl4xNV1Zah18309
  name: storyline_split_shots
  arguments: {"mode": "auto"}

finish_reason:
tool_calls
```

也就是模型对 Agent 说：

```text
我已经读到 load_media 的结果了。
下一步请调用 storyline_split_shots，参数是 {"mode": "auto"}。
```

注意：

```text
tool_calls 里仍然没有视频路径。
视频路径来自 load_media artifact。
```

真正的视频信息来自上一步 `load_media` 的 artifact。

`load_media` artifact 里有类似：

```json
{
  "media": [
    {
      "media_id": "media_0001",
      "path": "...",
      "media_type": "video",
      "metadata": {
        "duration": 5000,
        "fps": 30,
        "width": 1920,
        "height": 1080
      },
      "orig_path": "..."
    }
  ]
}
```

`split_shots` 读取方式：

```python
def _extract_media(self, inputs):
    return (inputs.get("load_media") or {}).get("media", []) or []
```

所以：

```text
load_media artifact
-> inputs["load_media"]["media"]
-> split_shots 遍历每个 media_item
```

## 4. artifact 是什么

在 OpenStoryline 里，artifact 是：

```text
某个节点执行后的结构化结果记录
```

物理上通常是一个 JSON 文件。

例如真实 `split_shots` artifact：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\split_shots\split_shots_1789240961.7285173.json
```

可以打开它看：

```text
payload.clips
```

里面就是 `split_shots` 产出的 clip 列表。

## 5. 具体怎么切视频

`split_shots` 对视频的处理可以分成四步：

```text
1. 用 ffmpeg 抽取分析用帧
2. 用 TransNetV2 判断哪里是镜头边界
3. 修正切点，避免片段太短或太长
4. 用 ffmpeg 按切点真正切出视频片段
```

### 5.1 ffmpeg 先抽分析用帧

这里的 ffmpeg 不是直接切视频，而是先帮 TransNetV2 准备输入。

OpenStoryline 会从原视频中读取帧，并转换成：

```text
25 fps
48 x 27
RGB24
```

也就是：

```text
原视频可能是 60fps, 1920x1080
分析帧变成 25fps, 48x27
```

注意：

```text
降低帧率 = 每秒取更少的图
降低分辨率 = 每张图变小
```

这两个不是一回事，但这里两个都做了。

原因是 TransNetV2 只需要判断“哪里发生镜头切换”，不需要高清细节。镜头切换主要依赖整体画面变化、颜色变化、构图变化、运动变化，低分辨率也足够。

### 5.2 TransNetV2 负责找切点

TransNetV2 是一个开源的镜头边界检测模型。

它回答的问题是：

```text
这一串视频帧里，哪里发生了镜头切换？
```

它不负责：

```text
切视频文件
生成 clip mp4
理解视频内容
判断镜头好不好看
```

它只负责：

```text
找出疑似镜头边界
```

调用逻辑：

```python
frames_numpy = read_video_frames_as_rgb24(...)
frames_tensor = torch.from_numpy(frames_numpy)
single_prediction, _all_prediction = model.predict_raw(frames_tensor)
scenes = model.predictions_to_scenes_with_data(...)
```

可以理解成：

```text
输入：一串 48x27 的低分辨率帧
输出：镜头边界/场景区间
```

### 5.3 TransNetV2 的来源

FireRed-OpenStoryline 不是自己训练 TransNetV2。

当前项目里：

```text
requirements.txt: transnetv2_pytorch==1.0.5
```

代码里：

```python
from transnetv2_pytorch import TransNetV2
```

权重路径来自配置：

```toml
[split_shots]
transnet_weights = ".storyline/models/transnetv2-pytorch-weights.pth"
transnet_device = "cpu"
```

更准确理解：

```text
TransNetV2 代码 = 模型结构
transnetv2-pytorch-weights.pth = 预训练模型参数
```

这个权重不是训练数据原文，而是别人训练完模型后得到的参数。

对普通使用者来说：

```text
TransNetV2 是开箱即用的镜头切分能力
前提是预训练权重文件已经准备好
不需要自己训练数据
```

### 5.4 切点后处理

TransNetV2 找到的边界不会直接拿来切。

OpenStoryline 会先把它转换成秒级切点：

```python
split_points_seconds = convert_scenes_to_split_points_seconds(scenes)
```

然后根据时长约束修正：

```python
split_points_seconds = enforce_shot_duration_constraints_on_split_points_seconds(...)
```

默认约束大概是：

```text
min_shot_duration = 1000ms
max_shot_duration = 30000ms
```

目的：

```text
避免片段太短
避免片段太长
避免切点太密
```

### 5.5 ffmpeg 真正切视频

得到最终切点后，ffmpeg 才负责真正生成视频片段文件。

调用：

```python
segments = segment_video_stream_copy_with_ffmpeg(
    input_video=input_video_path,
    split_points_seconds=split_points_seconds,
    output_directory=output_directory,
    filename_prefix="clip",
    start_index=starting_clip_index,
)
```

这里 ffmpeg 的作用是：

```text
原视频 + 切点
-> clip_0001.mp4
-> clip_0002.mp4
-> clip_0003.mp4
```

它使用 stream copy，尽量不重新编码：

```text
速度更快
画质损失更小
```

## 6. 如果没有切点怎么办

有几种情况不会实际切出新文件。

### 6.1 视频太短

如果视频时长小于最小镜头时长：

```python
duration_milliseconds < min_shot_duration_milliseconds
```

它直接把整个视频当成一个 clip。

### 6.2 TransNetV2 没发现切点

如果：

```text
split_points_seconds = []
```

并且：

```python
COPY_VIDEO_WHEN_NO_SPLIT = False
```

它也直接把整个原视频当成一个 clip，不再复制/切新文件。

所以测试素材里常见结果是：

```text
media_0001 -> clip_0001
media_0002 -> clip_0002
media_0003 -> clip_0003
```

## 7. 输出长什么样

`split_shots` 最终输出：

```json
{
  "clips": [
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
  ]
}
```

核心字段：

```text
clip_id = 后续节点引用这个片段的编号
path = 片段文件路径，或未切分时的原视频路径
source_ref.media_id = 这个 clip 来自哪个原始 media
source_ref.start/end = 在原始 media 中的时间范围
source_ref.duration = 片段时长
```

## 8. 后续怎么接

`split_shots` artifact 保存后，下一步通常是：

```text
storyline_understand_clips
```

它会读取：

```text
split_shots artifact -> payload.clips
```

然后对每个 clip 做视觉理解，生成 caption/描述。

后续链路：

```text
split_shots clips
-> understand_clips 给每个 clip 增加语义描述
-> filter_clips 根据用户要求筛选
-> group_clips 排序/分组
-> generate_script 生成文案
-> plan_timeline 组织时间线
-> render_video 根据 clips/path/source_ref 渲染
```

## 9. 一句话总结

```text
TransNetV2 负责判断哪里应该切；
ffmpeg 负责真的把视频按切点切成文件；
OpenStoryline 把切出来的文件包装成 clips JSON，并保存成 split_shots artifact；
后续所有理解、筛选、排序、时间线和渲染，都围绕这些 clips 继续。
```
