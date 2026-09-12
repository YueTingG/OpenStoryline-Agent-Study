# Capability Map

## Agent 角色

OpenStoryline 是一个对话式短视频剪辑 Agent。用户用自然语言描述目标，Agent 负责规划并调用剪辑工具节点。

## 当前 v1.0.1 镜像优先验证的能力

### 1. 素材处理

- 加载上传素材
- 读取素材基础信息
- 自动镜头切分

对应代码方向：

```text
load_media.py
split_shots.py
```

### 2. 内容理解

- 对视频片段抽帧
- 调用 VLM 理解画面
- 给片段生成文字描述

对应代码方向：

```text
understand_clips.py
```

### 3. 片段筛选与组织

- 根据用户主题筛选片段
- 把片段按叙事逻辑分组

对应代码方向：

```text
filter_clips.py
group_clips.py
```

### 4. 文案与风格

- 根据素材和主题生成视频文案
- 支持风格模仿和脚本模板推荐

对应代码方向：

```text
generate_script.py
script_template_rec.py
```

### 5. 音乐和视觉元素

- 选择 BGM
- 推荐字幕、字体、转场、文字样式

对应代码方向：

```text
select_bgm.py
recommend_effects.py
```

### 6. 时间线和渲染

- 组织视频、字幕、BGM、转场
- 导出 mp4

对应代码方向：

```text
plan_timeline_pro.py
render_video.py
```

## 暂缓验证的能力

这些能力需要额外 key，或当前 v1.0.1 镜像未必完整支持：

- 在线搜索素材：需要 Pexels API key
- 配音生成：需要 TTS provider key
- AI 转场：本地 main 有相关配置，v1.0.1 镜像不匹配
- ASR 粗剪：本地 main 有相关节点，v1.0.1 镜像不一定可用
- 保存 Editing Skill：适合在基础闭环跑通后验证
