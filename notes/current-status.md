# Current Status

## Environment

- Windows + Docker Desktop + WSL2
- Docker CLI 已可用
- Docker Engine OSType: linux
- OpenStoryline Web 已可访问：`http://127.0.0.1:7860`

## Container

```powershell
docker ps --filter name=openstoryline-test
```

当前已成功启动过：

```text
openstoryline-test
0.0.0.0:7860->7860/tcp
```

## Config

原始本地配置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\config.toml
```

Docker v1.0.1 兼容配置：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\config.docker-v1.0.1.toml
```

该兼容配置来自镜像内部 `/app/config.toml`，只迁移了：

```toml
[llm]
model = "deepseek-v4-flash"
base_url = "https://api.deepseek.com"
api_key = "***"

[vlm]
model = "deepseek-v4-flash-vision-exp"
base_url = "https://api.deepseek.com"
api_key = "***"
```

## Known Issue

不要直接把本地 main 分支的 `config.toml` 挂载到 v1.0.1 镜像里，否则会报：

```text
generate_ai_transition Extra inputs are not permitted
```

原因是镜像代码不认识新版配置字段。

## 2026-09-13 LLM Trace Debug Status

当前为了学习“用户输入如何变成 MCP tool 调用”，已经在本地 OpenStoryline 源码中加入临时 trace：

```text
C:\Users\guoyueting\agent\FireRed-OpenStoryline\agent_fastapi.py
C:\Users\guoyueting\agent\FireRed-OpenStoryline\src\open_storyline\agent.py
```

服务当前通过 Docker v1.0.1 镜像启动，但挂载了本地源码和配置：

```text
http://127.0.0.1:7860
container: openstoryline-test
trace env: OPENSTORYLINE_LLM_TRACE=1
config: config.docker-v1.0.1.toml
```

trace 输出位置：

```text
outputs/<session_id>/debug/llm_raw_trace.jsonl
outputs/<session_id>/debug/llm_trace.jsonl
```

两类 trace 的作用：

```text
llm_raw_trace.jsonl: ChatOpenAI 回调层，记录 chat_model_start / llm_end / llm_error
llm_trace.jsonl: Agent 层补充视角，记录 agent_model_input / agent_ai_message / agent_tool_message
```

已发现并修复：最初 `llm_raw_trace.jsonl` 中有 `llm_end`，但响应序列化没有展开 `AIMessage.tool_calls`。已补充响应序列化逻辑，让后续 `llm_end.response.generations[].message.tool_calls` 可见。

已确认：最新 session `8d6c0d78e2ac42fe8e7c7e7fcfa30fed` 的 raw trace 已经能在下面路径看到完整工具调用：

```text
llm_raw_trace.jsonl
-> event = llm_end
-> payload.response.generations[0][0].message.tool_calls
```

第一条真实响应是：

```text
finish_reason: tool_calls
tool_calls:
- name: storyline_load_media
  args: {"mode": "auto"}
  id: call_00_m8YCIFZvRi6MgrUkGB1k8329
```

这说明 raw trace 层已经不只看到 `finish_reason=tool_calls`，也能直接看到模型选择的 tool name、arguments 和 call id。

注意：为了让本地新版源码能跑在 Docker v1.0.1 镜像上，临时补了 `config.docker-v1.0.1.toml` 的空 `generate_ai_transition` 配置段，并临时把本地 `plan_timeline_pro` 的强依赖从新版能力路径调回基础剪辑路径，避免缺少 `generate_ai_transition` / `speech_rough_cut` 时渲染前失败。
