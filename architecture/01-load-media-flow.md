# storyline_load_media Flow

这份笔记解释 `storyline_load_media` 的完整过程：真实 LLM 请求/响应是什么、为什么 tool call 里没有文件路径、OpenStoryline 如何通过 session 找到上传素材、以及输出 artifact 如何接到后续节点。

## 1. 一句话理解

`storyline_load_media` 是剪辑流水线的入口。

它的作用是：

```text
把当前 session 已上传的文件
变成结构化 media 列表
```

也就是：

```text
outputs/<session_id>/media/ 下的 3 个视频文件

-> load_media

media_0001
media_0002
media_0003
```

每个 media 会带：

```text
media_id
path
media_type
metadata
orig_path
orig_md5
```

后续 `split_shots`、`render_video` 等节点会基于这些 media 信息继续处理。

## 2. 触发 load_media 的真实 LLM 请求/响应

真实 HTTP trace：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\e78297ae266440caac2cac2dc5575c51\debug\llm_http_trace.jsonl
```

其中：

```text
第 1 行: http_request
第 2 行: http_response
```

这一组请求/响应就是触发 `storyline_load_media` 的第一轮。

### 2.1 请求里有什么

第 1 行请求的核心结构是：

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
    }
  ],
  "tools": [
    "... 18 个 OpenStoryline tools schema ..."
  ]
}
```

关键点：

```text
这是一轮最初请求。
messages 里只有 system + user。
tools 里有 18 个可调用工具，其中包括 storyline_load_media。
```

这一轮请求里不需要把文件路径直接塞给 LLM。LLM 只需要知道：

```text
当前是剪辑任务
可用工具里有 load_media
用户说“基于我上传的素材”
```

### 2.2 响应里有什么

第 2 行响应是 SSE stream。拼完后的核心内容是：

```text
content:
我先说下计划：加载你的素材 → 切分镜头 → 理解内容 → 分组排序 → 生成文案 → 元素推荐（花字/转场）→ 选背景音乐 → 组织时间线 → 渲染成片。因为你说不要配音，所以配音这步我会跳过。

先加载素材，建立索引。

tool_calls:
- id: call_00_omq8yEAaprVmw6risxuU6828
  name: storyline_load_media
  arguments: {"mode": "auto"}

finish_reason:
tool_calls
```

也就是模型对 Agent 说：

```text
下一步请调用 storyline_load_media，参数是 {"mode": "auto"}。
```

注意：

```text
tool_calls 里没有文件路径。
```

这是合理的，因为文件定位不是 LLM 负责，而是后端通过 `session_id` 完成。

## 3. 上传 3 个文件之后发生了什么

上传接口：

```text
POST /api/sessions/{session_id}/media
```

源码位置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\agent_fastapi.py
```

关键逻辑：

```python
sess = await store.get_or_404(session_id)
metas = await sess.add_uploads(files, store_filenames=store_filenames)
```

上传后有两类状态：

```text
1. 物理文件保存到当前 session 的 media_dir
2. session 内存/状态里记录 MediaMeta 和 pending_media_ids
```

## 4. session 如何定位素材目录

每个 session 有自己的素材目录。

代码逻辑：

```python
def resolve_media_dir(cfg_media_dir, session_id):
    root = _abs(cfg_media_dir).rstrip("/\\")
    project_dir = os.path.dirname(root)
    leaf = os.path.basename(root)
    return os.path.join(project_dir, session_id, leaf)
```

所以如果配置里的 media 根目录是：

```text
outputs/media
```

那某个 session 的实际 media 目录就是类似：

```text
outputs/<session_id>/media
```

上传 3 个文件后，它们会在这个 session 专属目录里。

## 5. Agent 调 MCP 时如何带上 session_id

LLM 返回：

```json
storyline_load_media {"mode":"auto"}
```

Agent 调 MCP tool 时，会在 MCP 连接 headers 里带：

```python
"headers": {"X-Storyline-Session-Id": session_id}
```

位置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\src\open_storyline\agent.py
```

所以 MCP Server 收到工具调用时，不是只知道：

```text
mode = auto
```

还知道：

```text
X-Storyline-Session-Id = 当前 session_id
```

这就是后端能定位素材的关键。

## 6. load_media 的输入是怎么生成的

`load_media` 节点本身不是直接去问 LLM 要文件路径。

在 MCP tool 执行前，有一个 node interceptor 会检查：

```python
if node_id == 'load_media':
```

然后扫描当前 session 的：

```python
context.media_dir
```

核心逻辑：

```python
media_dir = Path(context.media_dir)
for file_name in os.listdir(media_dir):
    path = media_dir / file_name
    if path.is_dir():
        continue
    input_data["inputs"].append({
        "path": rel_or_abs,
        "orig_path": rel_or_abs,
        "orig_md5": None,
    })
```

也就是说：

```text
session_id
-> context.media_dir
-> 扫描目录里的文件
-> 生成 load_media 的 inputs
```

这也是为什么：

```text
mode:auto 本身不包含视频路径
```

因为路径来自 session 目录扫描。

## 7. load_media 节点做了什么

源码位置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\src\open_storyline\nodes\core_nodes\load_media.py
```

它读取：

```python
input_media = inputs.get("inputs", [])
```

然后遍历每个文件：

```python
for enc_media in input_media:
    path = Path(enc_media["path"])
    suffix = path.suffix.lower()
```

根据后缀判断类型：

```text
视频 -> video
图片 -> image
其他 -> 跳过
```

对视频会读取 metadata，例如：

```text
duration
fps
width
height
has_audio
audio_sample_rate_hz
```

最后生成：

```json
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
  "orig_path": "...",
  "orig_md5": null
}
```

如果有 3 个视频，就会生成：

```text
media_0001
media_0002
media_0003
```

## 8. load_media artifact 长什么样

`load_media` 返回：

```json
{
  "media": [
    {
      "media_id": "media_0001",
      "path": "...",
      "media_type": "video",
      "metadata": {...},
      "orig_path": "...",
      "orig_md5": null
    }
  ]
}
```

这个结果会被保存成 `load_media` artifact。

真实文件可以找：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\outputs\<session_id>\load_media\load_media_*.json
```

后续节点通过 artifact 读取它。

## 9. 为什么每次会加载全部文件

当前逻辑是扫描当前 session 的整个 `media_dir`：

```text
os.listdir(context.media_dir)
```

所以：

```text
如果 session 里有 3 个文件，load_media 会看到 3 个。
如果后来又上传第 4 个文件，再调用 load_media，会看到 4 个。
```

也就是说，`load_media` 不是只看“本轮新上传文件”，而是看：

```text
当前 session 素材目录里的全部文件
```

`pending_media_ids` 更多是前端/会话层的“待发送素材”状态；真正传给 `load_media` 的输入，是拦截器扫描 `media_dir` 得来的。

## 10. 后续怎么接到 split_shots

`load_media` 执行完之后，下一轮请求会把 tool result 放回 LLM 上下文。

下一轮请求里会出现：

```json
{
  "role": "assistant",
  "tool_calls": [
    {
      "id": "call_00_omq8yEAaprVmw6risxuU6828",
      "function": {
        "name": "storyline_load_media",
        "arguments": "{\"mode\": \"auto\"}"
      }
    }
  ]
}
```

紧接着：

```json
{
  "role": "tool",
  "tool_call_id": "call_00_omq8yEAaprVmw6risxuU6828",
  "content": "... Media indexing completed successfully: 3 video(s), 0 image(s) ..."
}
```

LLM 看到这个结果后，下一步通常返回：

```json
storyline_split_shots {"mode":"auto"}
```

然后 `split_shots` 不再扫描 session 目录，而是读取 `load_media` artifact 里的：

```text
payload.media
```

## 11. 一句话总结

```text
LLM 只决定“调用 load_media，mode=auto”；
session_id 通过 MCP header 传给后端；
后端根据 session_id 找到 media_dir；
load_media 前置拦截器扫描该目录里的全部文件；
load_media 节点把文件变成 media_0001/media_0002/media_0003；
结果保存为 load_media artifact；
下一步 split_shots 读取这个 artifact 继续切镜头。
```
