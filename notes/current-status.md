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
