# OpenStoryline Agent Study

这个项目用于记录学习 FireRed-OpenStoryline / OpenStoryline Agent 的过程。

目标不是一上来改源码，而是先建立清晰的理解：

1. 它作为 Agent 能做什么。
2. 一次完整剪辑任务会经过哪些工具节点。
3. 当前 Docker v1.0.1 镜像和本地 main 源码有哪些版本差异。
4. 如何通过实验验证能力，再回到源码阅读。

## 当前状态

- 原项目路径：`C:\Users\guoyueting\agent\FireRed-OpenStoryline`
- 学习项目路径：`C:\Users\guoyueting\agent\OpenStoryline-Agent-Study`
- 运行方式：Docker Desktop + WSL2
- 镜像：`crpi-6knxem4w8ggpdnsn.cn-shanghai.personal.cr.aliyuncs.com/openstoryline/openstoryline:v1.0.1`
- 当前容器：`openstoryline-test`
- Web 地址：`http://127.0.0.1:7860`
- 兼容配置文件：`C:\Users\guoyueting\agent\FireRed-OpenStoryline\config.docker-v1.0.1.toml`
- 模型服务：DeepSeek

## 重要结论

当前官方推荐 Docker 镜像 `v1.0.1` 和本地仓库 main 分支的 `config.toml` 不完全兼容。

根因是：

```text
v1.0.1 镜像中的 OpenStoryline 代码较旧
本地 main 分支的 config.toml 较新，包含 generate_ai_transition 等新配置
启动 Docker 时挂载本地 config.toml 会导致旧代码读取新配置失败
```

当前采用的稳妥方案：

```text
使用 v1.0.1 镜像自带 config.toml 作为模板
只迁移 llm/vlm 的 DeepSeek 配置
用 config.docker-v1.0.1.toml 启动容器
```

## 文件说明

- `notes/current-status.md`：当前安装、启动、配置状态。
- `notes/capability-map.md`：Agent 能力地图。
- `experiments/experiment-plan.md`：推荐的分阶段实验路线。
- `experiments/run-log.md`：每次实验记录。
- `code-reading/reading-path.md`：源码阅读顺序。
- `commands.md`：常用命令。
