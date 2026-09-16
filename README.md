# CQU NetProbe

`cqu-netprobe` 是 CQU 校园网状态拨测系统的探针，支持 Windows、Linux 和 Docker。它从 Gateway 获取 ICMP/HTTP 拨测配置，自行按周期执行测量并上报结果。

## 快速开始

复制示例配置：

```bash
cp config.example.json config.json
```

编辑 `config.json`：

```json
{
  "gateway_url": "GATEWAY_URL",
  "token": "YOUR_GATEWAY_TOKEN",
  "log_level": "info"
}
```

启动时，程序默认读取当前工作目录的 `config.json`。按 `Ctrl+C` 或发送 `SIGTERM` 可停止程序。

## Docker

```bash
docker run -d \
  --cap-add NET_RAW \
  --name cqu-netprobe \
  -e CQU_NETPROBE_GATEWAY_URL=GATEWAY_URL \
  -e CQU_NETPROBE_TOKEN=YOUR_GATEWAY_TOKEN \
  -e CQU_NETPROBE_LOG_LEVEL=info \
  --restart unless-stopped \
  tano26/cqu-netprobe:latest
```

Linux 优先使用非特权 ping socket，不可用时回退到 raw socket，因此容器需要 `NET_RAW` capability。Windows 使用系统 IP Helper ICMP API，不启动外部 `ping`。

## 配置

覆盖优先级为：**命令行 > 环境变量 > 配置文件 > 默认值**。默认读取当前工作目录的 `config.json`，也可以完全通过环境变量或命令行启动。

| 配置项         | 命令行           | 环境变量                    |
| -------------- | ---------------- | --------------------------- |
|                | `--config-file`  | `CQU_NETPROBE_CONFIG_FILE`  |
| `gateway_url`  | `--gateway-url`  | `CQU_NETPROBE_GATEWAY_URL`  |
| `token`        | `--token`        | `CQU_NETPROBE_TOKEN`        |
| `log_level`    | `--log-level`    | `CQU_NETPROBE_LOG_LEVEL`    |
| `local_input`  | `--local-input`  | `CQU_NETPROBE_LOCAL_INPUT`  |
| `local_output` | `--local-output` | `CQU_NETPROBE_LOCAL_OUTPUT` |

## 运行行为

- 配置获取
  - 启动时拉取配置；失败后从 1 秒开始指数退避，最大间隔 1 分钟。
  - 没有可上报目标时，每个测量周期主动检查配置。
- 配置更新
  - 每轮携带配置的 `config_id`。收到 `409 config_stale` 后立即拉取新配置，并从下一轮开始使用。
  - 配置刷新失败时继续使用旧配置，下一次收到 `409` 后再次尝试。
- 调度与上报
  - 拉取成功后立即执行第一轮，同一轮的 Target 和拨测类型并发执行。
  - 轮次不会重叠；耗时过长时跳过已经错过的调度点。
  - Push 失败不补传历史数据，下一轮只上报最新结果。
- 拨测
  - HTTP 拨测直连目标，不读取系统代理；耗时包含重定向和最多 8 MiB 的响应体读取。
  - ICMP 域名目标优先使用 IPv4，无 IPv4 地址时使用 IPv6。

## 开发

直接运行：

```bash
go run ./cmd/cqu-netprobe
```

构建：

```bash
go build -trimpath -o cqu-netprobe ./cmd/cqu-netprobe
```

发布构建应注入版本号：

```bash
go build -trimpath -ldflags="-s -w -X main.version=0.1.0" -o cqu-netprobe ./cmd/cqu-netprobe
```

测试：

```bash
go test ./...
go vet ./...
go test -tags=integration ./internal/probe
```
