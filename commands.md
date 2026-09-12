# Commands

## Check Container

```powershell
docker ps --filter name=openstoryline-test
```

## View Logs

```powershell
docker logs -f openstoryline-test
```

## Stop Container

```powershell
docker stop openstoryline-test
```

## Remove Container

```powershell
docker rm openstoryline-test
```

## Start Container

```powershell
cd C:\Users\guoyueting\agent\FireRed-OpenStoryline

docker run -d --name openstoryline-test `
  -v ${PWD}\config.docker-v1.0.1.toml:/app/config.toml `
  -v ${PWD}\outputs:/app/outputs `
  -p 7860:7860 `
  crpi-6knxem4w8ggpdnsn.cn-shanghai.personal.cr.aliyuncs.com/openstoryline/openstoryline:v1.0.1
```

## Open Web

```text
http://127.0.0.1:7860
```

## If Container Name Already Exists

```powershell
docker stop openstoryline-test
docker rm openstoryline-test
```

Then start again.
