# Code Reading Path

建议在跑通过一次完整实验后再读源码。

## 1. Web 和会话层

```text
agent_fastapi.py
```

关注：

- FastAPI app 启动
- session 创建和保存
- 文件上传
- WebSocket 聊天
- tool trace 如何返回前端

## 2. Agent 构建层

```text
src/open_storyline/agent.py
```

关注：

- LLM/VLM 如何初始化
- MCP client 如何连接
- LangChain Agent 如何创建
- tool interceptors 如何注入媒体内容和保存结果

## 3. MCP 工具注册层

```text
src/open_storyline/mcp/server.py
src/open_storyline/mcp/register_tools.py
```

关注：

- FastMCP server 如何启动
- Node 如何包装成 MCP tool
- session_id 如何从 header 传入

## 4. 节点管理

```text
src/open_storyline/nodes/node_manager.py
src/open_storyline/nodes/node_schema.py
src/open_storyline/nodes/node_state.py
src/open_storyline/nodes/node_summary.py
```

关注：

- node_id / node_kind / priority
- 节点依赖关系
- 节点结果如何被复用

## 5. 核心工作流节点

按这条线读：

```text
load_media.py
split_shots.py
understand_clips.py
filter_clips.py
group_clips.py
generate_script.py
select_bgm.py
recommend_effects.py
plan_timeline_pro.py
render_video.py
```

读代码时的问题：

1. 这个节点输入是什么？
2. 输出 artifact 是什么？
3. 是否调用 LLM/VLM？
4. 是否读写文件？
5. 下游节点依赖它的哪些字段？

## 6. 前端

```text
web/static/app.js
web/static/style.css
web/index.html
```

关注：

- 会话列表
- 文件上传
- WebSocket 消息
- 工具调用卡片
- 结果预览
