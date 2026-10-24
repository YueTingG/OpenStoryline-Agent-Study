# `storyline_elementrec_text`：从字体候选库中选择字幕字体

`storyline_generate_script` 解决的是：**字幕写什么？**

`storyline_elementrec_text` 解决的是：**这些字幕用什么字体呈现？**

```text
generate_script 的输出                    elementrec_text 的输出
字幕文案 group_scripts              ->     font_name + 字体风格元数据 + font_color
```

这一步不会修改文案内容，也不会立刻把文字绘制到视频上。它从项目已有字体库中选择一个字体配置，供后面的时间线与渲染阶段使用。

## 1. 它在真实链路中的位置

```text
generate_script
    ↓ 提供 raw_text 和 subtitle_units
elementrec_text
    ↓ 选择字体名称与颜色
plan_timeline
    ↓ 把字幕安排到时间线上
render_video
    ↓ 根据 font_name 找到字体文件并绘制字幕
```

本次调用包含三个决策层次：

```text
Agent LLM
  └─ 提出字体风格要求：极简、干净、适合 Vlog

节点代码
  └─ 从本地 font_info.json 过滤出允许选择的候选字体

Font LLM
  └─ 结合字幕内容和风格要求，从候选中选择一个 font_name
```

所以字体不是 Agent 直接指定的，也不是内部 LLM 凭空创造的。

## 2. Agent 怎样发起 `elementrec_text`

`generate_script` 执行完成后，Agent LLM 的真实响应是：

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

真正传给 MCP 工具的参数是：

```json
{
  "mode": "auto",
  "user_request": "极简、干净的字体风格，适合 Vlog 标题。"
}
```

原始用户没有直接提出字体要求。这条 `user_request` 是 Agent 根据当前视频的极简 Vlog 风格，为字体推荐能力整理出的局部要求。

Agent 在这一层只描述审美方向，没有指定具体字体文件或 `font_name`。

完整触发证据：

- [01-agent-trigger-request.json](evidence/elementrec-text/01-agent-trigger-request.json)
- [02-agent-trigger-response.sse.txt](evidence/elementrec-text/02-agent-trigger-response.sse.txt)
- [02-agent-trigger-response.parsed.json](evidence/elementrec-text/02-agent-trigger-response.parsed.json)

## 3. 字体候选从哪里来

字体候选不是 LLM 自己生成的，而是来自项目准备好的一份字体库。这里要区分两件事：代码定义字体资源如何取得，`resource.zip` 提供字体文件本身。

### 3.1 字体如何进入 Docker 镜像

`Dockerfile` 在构建镜像时执行：

```dockerfile
COPY download.sh .
RUN ./download.sh
```

`download.sh` 的相关原文是：

```bash
wget "https://image-url-2-feature-1251524319.cos.ap-shanghai.myqcloud.com/openstoryline/resource.zip" \
  -O .storyline/resource.zip

unzip -o .storyline/resource.zip -d resource
```

因此，字体文件的实际内容来自腾讯云 COS 上的 `resource.zip`。`Dockerfile` 和 `download.sh` 只定义下载与解压规则。下载发生在镜像构建阶段，解压结果被保存在镜像的 `/app/resource/` 中；以后用这个镜像创建容器时，容器直接继承这些文件，不会在每次启动时重新下载。

本次容器中已经确认存在：

```text
/app/resource/fonts/font_info.json
/app/resource/fonts/<各字体目录>/<字体文件.ttf 或 .otf>
```

### 3.2 运行时如何找到字体库

`config.toml` 的原文是：

```toml
[recommend_text]
font_info_path = "resource/fonts/font_info.json"
```

这是相对于进程工作目录的路径。本次容器的工作目录是 `/app`，所以运行时实际读取：

```text
/app/resource/fonts/font_info.json
```

`font_info.json` 中的每个条目把“供模型选择的字体名称和风格”与“供渲染程序读取的真实文件路径”连接起来。本次选中字体对应的原文是：

```json
{
  "style": "现代、加粗",
  "class": "黑体",
  "lang": ["zh", "en"],
  "font_name": "SourceHanSansSC",
  "font_path": "resource/fonts/SourceHanSansSC/SourceHanSansSC-Bold.otf"
}
```

### 3.3 字体库如何变成候选列表

`ElementFilter` 读取 `font_info.json`，再根据 tool 参数中的 `filter_include` 过滤条目。

本次 `storyline_elementrec_text` 没有传入 `filter_include`，所以没有排除任何字体，文件里的 17 条记录全部成为候选。如果过滤结果为空，程序会从完整字体库中随机取最多 10 条作为后备候选。

因此，“字体候选从哪里来”的完整关系是：

```text
resource.zip 提供字体数据
    ↓
docker build 将它解压进镜像的 /app/resource/
    ↓
config.toml 指向 resource/fonts/font_info.json
    ↓
ElementFilter 读取并过滤其中的条目
    ↓
本次得到 17 个候选字体
```

## 4. 为什么不把 `font_path` 发给 LLM

节点在构造 prompt 前，会从候选条目中移除 `font_path`。因此模型看到的是：

```text
style + class + lang + font_name
```

而不是机器上的实际文件路径。

这层设计关系是：

```text
Font LLM 只负责选择 font_name
              ↓
后续 render_video 根据 font_name
回到 font_info.json 查找 font_path
```

模型不需要理解本地路径，也不应该生成任意字体文件位置。

## 5. 节点内部的 LLM 请求是什么

下面是本次字体推荐能力的完整请求。它不包含 Agent 的完整历史对话和 tools 列表，只包含字体推荐专用 prompt、已生成字幕、候选字体和本次审美要求：

```json
{
  "messages": [
    {
      "content": "你是一个字体推荐器，根据我提供视频字幕与字体条目（List[Dict]格式），返回最适合用户要求的**有且只有一条**json。",
      "role": "system"
    },
    {
      "content": "字幕：\n[{'group_id': 'group_0001', 'raw_text': '早上先来杯 Morning_coffee，醒醒神。然后 City_walk 出门晃一圈，随便走走。到了 Sunset_chill，就坐着看天慢慢变橙。', 'subtitle_units': [{'unit_id': 'subtitle_0001', 'index_in_group': 0, 'text': '早上先来杯 Morning_coffee'}, {'unit_id': 'subtitle_0002', 'index_in_group': 1, 'text': '醒醒神'}, {'unit_id': 'subtitle_0003', 'index_in_group': 2, 'text': '然后 City_walk 出门晃一圈'}, {'unit_id': 'subtitle_0004', 'index_in_group': 3, 'text': '随便走走'}, {'unit_id': 'subtitle_0005', 'index_in_group': 4, 'text': '到了 Sunset_chill'}, {'unit_id': 'subtitle_0006', 'index_in_group': 5, 'text': '就坐着看天慢慢变橙'}]}]\n\n候选字体条目：\n[{'style': '力量', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'BiaoXiaoZhiBiaoTiHei'}, {'style': '力量', 'class': '黑体', 'lang': ['zh', 'en', 'jp'], 'font_name': 'CP_Revenge'}, {'style': '手写、青春', 'class': '创意字', 'lang': ['zh', 'en'], 'font_name': 'ChenYuLuoYanTi'}, {'style': '力量', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'DeyiHei'}, {'style': '手写、青春', 'class': '创意字', 'lang': ['zh', 'en'], 'font_name': 'HanChanShouZhuoTi'}, {'style': '手写', 'class': '楷体', 'lang': ['zh', 'en'], 'font_name': 'JiangXiZuoKaiTi'}, {'style': '现代', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'LeetfontMengHeiTi'}, {'style': '现代', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'MaoKenShiJinHei'}, {'style': '现代', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'PangMenZhengDaoBiaoTiTi'}, {'style': '手写', 'class': '楷体', 'lang': ['zh', 'en'], 'font_name': 'PingFangXingChenTi'}, {'style': '趣味、手写', 'class': '创意字', 'lang': ['zh', 'en'], 'font_name': 'QianTuXueHuaTi'}, {'style': '优雅、复古', 'class': '宋体', 'lang': ['zh', 'en'], 'font_name': 'ShiWeiB2JiaTangSongTi'}, {'style': '基础', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'SiYuanHeiTi'}, {'style': '现代、加粗', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'SourceHanSansSC'}, {'style': '趣味', 'class': '创意字', 'lang': ['zh', 'en'], 'font_name': 'UnifontDianZhengHei'}, {'style': '手写、青春', 'class': '创意字', 'lang': ['zh', 'en'], 'font_name': 'ZhangKuQinKeHuangYouTi'}, {'style': '力量', 'class': '黑体', 'lang': ['zh', 'en'], 'font_name': 'ZiZhiQuXiMaiTi'}]\n\n用户要求：\n极简、干净的字体风格，适合 Vlog 标题。",
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

### System prompt：限定任务

system prompt 非常短，只要求模型：

```text
根据视频字幕、候选字体条目和用户要求，返回有且只有一条 JSON
```

### User prompt：提供选择依据

user prompt 由三块组成：

```text
generate_script 的 group_scripts
+
本地字体库过滤后的候选条目
+
Agent 整理出的字体风格要求
```

模型没有看到视频画面。它根据字幕语言、文字内容、候选字体标签和风格要求进行选择。

## 6. Font LLM 返回什么

原始 SSE 拼接后，`content` 中的完整业务 JSON 是：

```json
{
  "style": "现代、加粗",
  "class": "黑体",
  "lang": [
    "zh",
    "en"
  ],
  "font_name": "SourceHanSansSC"
}
```

模型选择了：

```text
font_name = SourceHanSansSC
style     = 现代、加粗
class     = 黑体
lang      = zh、en
```

这个选择与本次输入的关系是：

- 字幕同时包含中文和英文，因此候选字体需要覆盖 `zh` 和 `en`。
- Agent 要求“极简、干净”，现代黑体比手写、复古或趣味字体更匹配。
- `SourceHanSansSC` 确实存在于提供给模型的候选列表中。

完整响应：

- [04-font-llm-response.sse.txt](evidence/elementrec-text/04-font-llm-response.sse.txt)
- [04-font-llm-response.parsed.json](evidence/elementrec-text/04-font-llm-response.parsed.json)

## 7. `font_color` 从哪里来

Font LLM 的真实响应只选择了字体条目，没有返回 `font_color`。节点解析响应后，由程序追加：

```json
{
  "font_color": [255, 255, 255, 255]
}
```

这个数组采用 RGBA：

```text
R = 255
G = 255
B = 255
A = 255
```

也就是完全不透明的白色。

本次 tool schema 没有向 Agent 暴露 `font_color` 参数，因此调用中没有提供颜色，节点使用代码中的默认白色。

所以本次最终结果是两部分合成的：

```text
Font LLM 返回字体选择
          +
程序追加默认 font_color
          ↓
elementrec_text artifact
```

## 8. 最终 artifact 保存了什么

本次 artifact 的完整内容是：

```json
{
  "payload": [
    {
      "style": "现代、加粗",
      "class": "黑体",
      "lang": [
        "zh",
        "en"
      ],
      "font_name": "SourceHanSansSC",
      "font_color": [
        255,
        255,
        255,
        255
      ]
    }
  ],
  "session_id": "e78297ae266440caac2cac2dc5575c51",
  "artifact_id": "elementrec_text_1789240993.1517227",
  "node_id": "elementrec_text",
  "create_time": 1789240995.997949
}
```

artifact 的 `payload` 是一个列表，因为节点允许以列表形式返回文字样式配置。本次列表只有一个字体配置，它会被后续时间线和渲染阶段复用。

artifact 不包含 `font_path`。渲染阶段会再次读取配置项 `recommend_text.font_info_path` 指向的 `font_info.json`，通过 `font_name` 找到实际字体文件；在本次 Docker 运行环境中，该文件对应 `/app/resource/fonts/font_info.json`。

完整 artifact：[05-elementrec-text-artifact.json](evidence/elementrec-text/05-elementrec-text-artifact.json)。

## 9. 模型返回是否受到程序校验

当前节点会检查响应能否解析成 JSON，但没有进一步验证：

- `font_name` 是否一定来自候选列表。
- `style`、`class` 和 `lang` 是否与字体库原始条目完全一致。
- 响应是否严格只包含允许字段。

本次模型返回的 `SourceHanSansSC` 确实是有效候选，所以结果可以被后续使用。但从实现边界看，“必须从候选中选择”主要依赖 prompt 约束，而不是代码强校验。

如果响应无法解析成 JSON，节点会记录错误并返回 `None`，不会自动改用默认字体。只有 `mode` 不是 `auto` 时，才会进入 `default_process` 并返回 `SiYuanHeiTi`。

## 10. 字体结果怎样回到 Agent LLM

工具执行结束后，系统把 tool call 和 tool result 放回 Agent 对话。真实 request 最末尾两条消息是：

```json
[
  {
    "content": "文案生成好了。接下来推荐字幕字体样式。",
    "role": "assistant",
    "tool_calls": [
      {
        "type": "function",
        "id": "call_00_7EQkvw0GBuvG8MmqUMns6619",
        "function": {
          "name": "storyline_elementrec_text",
          "arguments": "{\"mode\": \"auto\", \"user_request\": \"极简、干净的字体风格，适合 Vlog 标题。\"}"
        }
      }
    ]
  },
  {
    "content": "{'summary': {'node_summary': {'ERROR': '', 'WARNING': '', 'INFO_LLM': '', 'INFO_USER': '[2026-09-12T19:23:15.992400] [elementrec_text] Use font `SourceHanSansSC`', 'preview_urls': [], 'artifact_id': 'elementrec_text_1789240993.1517227'}, 'tool_excute_result': {}}, 'isError': False}",
    "role": "tool",
    "tool_call_id": "call_00_7EQkvw0GBuvG8MmqUMns6619"
  }
]
```

两条消息通过相同 ID 配对：

```text
assistant.tool_calls[0].id
    = call_00_7EQkvw0GBuvG8MmqUMns6619

tool.tool_call_id
    = call_00_7EQkvw0GBuvG8MmqUMns6619
```

tool result 的摘要告诉 Agent 使用了哪个 `font_name`，并附带 artifact ID。完整回送请求见 [06-agent-resume-request.json](evidence/elementrec-text/06-agent-resume-request.json)。

## 11. Agent 下一步做了什么

Agent 收到字体推荐结果后，真实响应是：

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

下一步是 `storyline_elementrec_transition`，参数指定 500 毫秒转场。字体推荐节点本身不会自动推荐转场；这是 Agent LLM 在收到字体结果后发起的下一次独立 tool call。

完整下一步响应：

- [07-agent-next-response.sse.txt](evidence/elementrec-text/07-agent-next-response.sse.txt)
- [07-agent-next-response.parsed.json](evidence/elementrec-text/07-agent-next-response.parsed.json)

## 12. 用一句话理解整个过程

`storyline_elementrec_text` 先从项目本地字体库中形成候选集合，再把候选元数据、已经生成的字幕和 Agent 给出的审美方向交给独立文本 LLM选择 `font_name`，最后由程序补上默认颜色；它只生成字体配置，不在这一阶段把字幕绘制到视频上。

## 证据说明

本文基于会话 `e78297ae266440caac2cac2dc5575c51` 的真实 HTTP trace 第 27 至 32 行、`elementrec_text` artifact、字体库配置和当前节点源码。正文保留理解关系所需的完整可读原文；完整 Agent 请求、原始 SSE 与解析后响应存放在 [evidence/elementrec-text](evidence/elementrec-text/) 中。
