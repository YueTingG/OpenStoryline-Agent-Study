# OpenStoryline System Logic

这份文档只讲系统逻辑，不讲 Python 语法。

目标是回答：

- 一条用户消息进入系统后，经过哪些模块？
- Web 页面、后端、Agent、MCP、工具节点、存储分别负责什么？
- 有哪些接口？
- 中间结果如何保存和复用？
- 为什么二次编辑能只重跑一部分节点？

---

## 1. 一句话架构

OpenStoryline 可以理解为：

```text
Web 聊天页面
  -> FastAPI 会话服务
  -> LangChain Agent
  -> MCP 工具服务
  -> 剪辑节点 Nodes
  -> 文件和 artifact 存储
  -> 返回工具进度与最终视频
```

它不是一个单纯聊天机器人。它的核心是：

```text
LLM 负责决策和调度
Nodes 负责实际剪辑能力
ArtifactStore 负责保存每一步结果
Session 负责把多轮对话串起来
```

---

## 2. 架构总览

```mermaid
flowchart LR
    User[用户] --> Web[Web 页面]
    Web --> API[FastAPI 后端]
    API --> Session[Session Store]
    API --> Agent[LangChain Agent]
    Agent --> LLM[LLM 文本模型]
    Agent --> MCPClient[MCP Client]
    MCPClient --> MCP[MCP Server]
    MCP --> Nodes[剪辑工具 Nodes]
    Nodes --> VLM[VLM 视觉模型]
    Nodes --> Store[Artifact Store]
    Nodes --> Files[媒体/输出文件]
    Store --> Agent
    Files --> API
    API --> Web
```

### 各模块职责

| 模块 | 负责什么 | 不负责什么 |
| --- | --- | --- |
| Web 页面 | 展示聊天、上传素材、展示工具卡片、预览视频 | 不直接剪辑视频 |
| FastAPI 后端 | 管理会话、上传文件、WebSocket 流式聊天、预览本地文件 | 不直接决定剪辑策略 |
| LangChain Agent | 读用户意图、决定调用哪个工具、组织多轮工具调用 | 不直接处理视频帧 |
| MCP Client | 把 Agent 的工具调用发给 MCP Server | 不实现具体工具 |
| MCP Server | 注册工具、接收工具调用、创建 NodeState | 不决定调用顺序 |
| Nodes | 实际执行加载、切分、理解、筛选、文案、BGM、时间线、渲染 | 不管理 Web 会话 |
| Artifact Store | 保存每个节点的 JSON 结果，供后续复用 | 不做业务决策 |
| Media/Outputs | 保存上传素材、预览文件、最终视频 | 不知道 Agent 逻辑 |

---

## 3. 一次用户请求的时序图

```mermaid
sequenceDiagram
    participant U as 用户
    participant W as Web 页面
    participant API as FastAPI 后端
    participant S as Session
    participant A as Agent
    participant M as MCP Server
    participant N as Node 工具
    participant ST as Artifact Store
    participant Model as LLM/VLM

    U->>W: 输入剪辑需求
    W->>API: WebSocket chat.send
    API->>S: 找到 session 和历史消息
    API->>A: 传入消息、素材状态、session context
    A->>Model: 判断下一步应调用哪个工具
    A->>M: 调用工具，如 load_media
    M->>N: 执行对应 Node
    N->>ST: 保存节点结果 artifact JSON
    N-->>M: 返回 summary + artifact_id + preview_urls
    M-->>A: 工具结果返回 Agent
    A-->>API: 流式返回工具进度/文字回复
    API-->>W: 页面展示工具卡片和预览
    A->>M: 继续调用下一个工具
    M->>N: 执行后续节点
    N->>ST: 保存新 artifact
    N-->>A: 返回结果
    A-->>API: 最终回复
    API->>S: 保存 session_state.json
    API-->>W: 展示最终视频预览
```

---

## 4. 主流程伪代码

### 4.1 页面发消息

```text
用户在页面输入 prompt
页面通过 WebSocket 发给后端
后端找到 session
后端把 prompt 加入消息历史
后端调用 Agent
Agent 开始思考并选择工具
每次工具调用都会返回进度
后端把进度实时推给页面
Agent 完成后，后端保存 session
页面展示最终回复和预览
```

### 4.2 Agent 调工具

```text
Agent 看到用户目标
Agent 查看已有历史和工具列表

如果没有素材索引：
  调用 load_media

如果素材还没切分：
  调用 split_shots

如果没有画面描述：
  调用 understand_clips

如果用户要求筛选：
  调用 filter_clips

如果需要组织叙事：
  调用 group_clips

如果需要字幕/文案：
  调用 generate_script

如果需要字体/花字：
  调用 elementrec_text

如果需要 BGM：
  调用 select_bgm

如果需要转场：
  调用 elementrec_transition

如果不要配音：
  调用 generate_voiceover(mode=skip)
否则：
  调用 generate_voiceover(mode=auto)

调用 plan_timeline_pro 组织时间线
调用 render_video 输出 mp4
```

注意：实际不是硬编码成这段 if-else，而是由 LLM Agent 根据工具描述、历史结果和用户请求来决定。上面只是逻辑等价的伪代码。

---

## 5. 接口地图

这些接口来自后端 FastAPI。它们是页面和后端沟通的主要入口。

### 5.1 页面和静态资源

| 接口 | 作用 |
| --- | --- |
| `GET /` | 返回 Web 主页面 |
| `GET /node-map` | 返回节点地图页面 |
| `/static/*` | 前端 JS/CSS/图片资源 |
| `/node_map/*` | 节点地图静态资源 |

### 5.2 元信息接口

| 接口 | 作用 |
| --- | --- |
| `GET /api/meta/tts` | 返回 TTS 配置 UI schema |
| `GET /api/meta/ai_transition` | 返回 AI 转场配置 UI schema。注意 v1.0.1 镜像未必支持新版配置 |

### 5.3 会话接口

| 接口 | 作用 |
| --- | --- |
| `POST /api/sessions` | 创建一个新剪辑会话，返回 `session_id` |
| `GET /api/sessions/{session_id}` | 获取会话状态、历史、素材等信息 |
| `POST /api/sessions/{session_id}/clear` | 清空当前会话聊天历史 |
| `POST /api/sessions/{session_id}/cancel` | 中断当前正在执行的回复/工具调用 |

### 5.4 素材上传接口

| 接口 | 作用 |
| --- | --- |
| `POST /api/sessions/{session_id}/media` | 普通上传文件 |
| `POST /api/sessions/{session_id}/media/init` | 初始化分片上传 |
| `POST /api/sessions/{session_id}/media/{upload_id}/chunk` | 上传一个分片 |
| `POST /api/sessions/{session_id}/media/{upload_id}/complete` | 完成分片上传 |
| `POST /api/sessions/{session_id}/media/{upload_id}/cancel` | 取消分片上传 |
| `GET /api/sessions/{session_id}/media/pending` | 查看待发送素材 |
| `DELETE /api/sessions/{session_id}/media/pending/{media_id}` | 删除一个待发送素材 |
| `GET /api/sessions/{session_id}/media/{media_id}/thumb` | 获取素材缩略图 |
| `GET /api/sessions/{session_id}/media/{media_id}/file` | 获取上传素材原文件 |

### 5.5 预览接口

| 接口 | 作用 |
| --- | --- |
| `GET /api/sessions/{session_id}/preview?path=...` | 把工具返回的本地路径转换成浏览器可访问的预览 URL |

它允许预览的路径范围主要包括：

```text
session media_dir
outputs_dir
bgm_dir
.server_cache
```

所以工具返回 `/app/.../output.mp4` 后，页面通过这个接口播放视频。

### 5.6 聊天 WebSocket

| 接口 | 作用 |
| --- | --- |
| `WS /ws/sessions/{session_id}/chat` | 页面和 Agent 的主通信通道，负责发送用户消息、接收流式回复和工具进度 |

---

## 6. 存储地图

OpenStoryline 的存储可以分成四类。

### 6.1 上传素材

逻辑路径来自配置：

```text
project.media_dir = ./outputs/media
```

实际运行时会按 session 隔离：

```text
outputs/media/{session_id}/media_0001.mp4
outputs/media/{session_id}/media_0002.mp4
...
```

页面上传后，素材先成为 pending media；当用户发出聊天请求时，Agent 可以通过 `load_media` 建立索引。

### 6.2 会话状态

每个 session 会保存状态文件：

```text
outputs/{session_id}/session_state.json
```

里面保存：

```text
聊天历史
语言设置
待上传素材元信息
模型/服务配置摘要
前端恢复会话所需状态
```

### 6.3 节点结果 artifact

每个工具节点执行完，会保存 JSON 结果。

观察日志可见：

```text
/app/outputs/{session_id}/load_media/load_media_*.json
/app/outputs/{session_id}/split_shots/split_shots_*.json
/app/outputs/{session_id}/generate_script/generate_script_*.json
...
```

每次工具返回一个：

```text
artifact_id
```

后续工具或 Agent 可以通过：

```text
read_node_history(query_artifact_id)
```

读取之前节点结果。

### 6.4 渲染视频缓存

最终视频常出现在 server cache：

```text
/app/.storyline/.server_cache/{session_id}/render_video_xxx/output_xxx.mp4
```

工具结果会把这个路径放进：

```text
preview_urls
```

页面再通过 `/api/sessions/{session_id}/preview?path=...` 播放它。

---

## 7. 能力模块地图

| 能力 | 工具/节点 | 输入 | 输出 |
| --- | --- | --- | --- |
| 素材加载 | `load_media` | 上传素材路径 | 媒体索引、素材清单 |
| 镜头切分 | `split_shots` | 已加载视频 | clip 列表、起止时间 |
| 内容理解 | `understand_clips` | clip + 抽帧 | 每个 clip 的文字描述、整体描述 |
| 镜头筛选 | `filter_clips` | clip 描述 + 用户要求 | 选中的 clip 列表 |
| 片段分组 | `group_clips` | 选中 clip + 排序要求 | 叙事分组 |
| 文案生成 | `generate_script` | 分组 + 风格要求 | 标题、字幕文本 |
| 文案模板推荐 | `script_template_rec` | 风格要求 | 参考模板/风格线索 |
| 字幕样式 | `elementrec_text` | 审美要求 | 字体、颜色等文本样式 |
| 背景音乐 | `select_bgm` | 情绪/节奏要求 | BGM 文件 |
| 转场推荐 | `elementrec_transition` | 转场要求 | 转场配置 |
| 配音 | `generate_voiceover` | 字幕文案 | 音频，或 skip 结果 |
| 时间线 | `plan_timeline_pro` | 素材、文案、BGM、转场、配音 | 时间线 JSON |
| 渲染 | `render_video` | 时间线 | mp4 文件 |
| 历史读取 | `read_node_history` | artifact_id | 指定节点历史结果 |
| Skill 写入 | `write_skills` | skill 名称和内容 | 本地 Skill 文件 |

---

## 8. 为什么二次编辑能复用结果

核心原因是：每个工具节点的输出都会被保存成 artifact。

第一次完整剪辑时产生：

```text
load_media artifact
split_shots artifact
understand_clips artifact
filter_clips artifact
group_clips artifact
generate_script artifact
elementrec_text artifact
select_bgm artifact
elementrec_transition artifact
plan_timeline_pro artifact
render_video artifact
```

第二次用户说：

```text
字幕更小红书，音乐更活力，节奏更快
```

Agent 判断：

```text
素材没变 -> 不需要 load_media
镜头没变 -> 不需要 split_shots
画面理解没变 -> 不需要 understand_clips
筛选和分组大体可复用 -> 不需要 filter/group
文案风格变了 -> 重跑 generate_script
字幕风格变了 -> 重跑 elementrec_text
音乐要求变了 -> 重跑 select_bgm
转场/节奏变了 -> 重跑 transition/timeline/render
```

第三次用户说：

```text
只把字幕文案改正式，音乐、转场和节奏不要变
```

Agent 判断：

```text
只需要新文案
需要把新文案放回时间线
需要重新渲染
不需要重新选择音乐/转场/素材
```

实际日志验证：

```text
generate_script
read_node_history
plan_timeline_pro
read_node_history
render_video
```

这说明复用能力不是凭空发生的，而是基于：

```text
session_id
artifact_id
read_node_history
节点输出文件
Agent 对用户意图的判断
```

---

## 9. 逻辑层级伪代码

```text
function handleUserMessage(session_id, user_message):
    session = loadSession(session_id)
    media_state = scanSessionMedia(session_id)
    history = session.chat_history

    agent_input = {
        user_message,
        history,
        media_state,
        available_tools,
        previous_artifacts
    }

    while agent_not_done:
        decision = LLM.decide_next_action(agent_input)

        if decision.type == "reply":
            streamReplyToWeb(decision.text)
            saveSession(session)
            return

        if decision.type == "tool_call":
            tool_result = MCP.call_tool(
                name = decision.tool_name,
                args = decision.args,
                session_id = session_id
            )

            saveArtifact(tool_result)
            streamToolCardToWeb(tool_result)
            agent_input.history.append(tool_result)

    saveSession(session)
```

```text
function runNode(session_id, node_name, args):
    previous_outputs = loadArtifacts(session_id)
    node_state = {
        session_id,
        artifact_id,
        language,
        llm_or_vlm_access,
        previous_outputs,
        file_paths
    }

    result = node.execute(node_state, args)
    artifact_id = saveResult(session_id, node_name, result)

    return {
        artifact_id,
        user_summary,
        llm_summary,
        preview_urls
    }
```

---

## 10. 当前已验证的系统行为

通过实验已验证：

```text
1. Web 上传素材成功
2. Agent 能调用 load_media / split_shots / understand_clips 等工具
3. VLM 能识别测试素材性质
4. Agent 能完整生成视频
5. Agent 能在同一 session 中二次编辑
6. Agent 能根据局部修改请求只重跑必要节点
7. 页面能通过 preview 接口播放生成视频
```

还未验证：

```text
1. 真实生活素材上的剪辑质量
2. Pexels 在线素材搜索
3. TTS 配音能力
4. 保存 Editing Skill
5. 自定义 BGM / 字体 / 文案模板
6. 新版 main 分支中的 AI 转场和 ASR 粗剪
```