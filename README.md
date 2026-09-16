# CQU NetProbe

`cqu-netprobe` 是 CQU 校园网状态拨测系统的探针。程序启动后从 Gateway
拉取一次拨测计划，随后自行维护测量循环，并把每轮最新结果推送到 Gateway。

当前实现支持：

- Windows 与 Linux（包括 Docker）；
- ICMP 和 HTTP 拨测；
- 同一轮内所有 Target/Probe Type 并发执行；
- Gateway 配置拉取失败时从 1 秒开始指数退避，最大间隔 1 分钟；
- Push 失败后不补传历史数据，下一轮继续；
- JSON 结构化日志，且不记录 Token。

DNS 拨测暂未启用。Gateway 如果意外下发 `dns` 类型，探针会忽略该类型。

## 配置

复制 [`cqu-netprobe.example.json`](cqu-netprobe.example.json) 并填写 Gateway 地址。
推荐把 Token 单独保存在文件中：

```json
{
  "gateway_url": "https://netprobe.example.com",
  "token_file": "cqu-netprobe.token",
  "log_level": "info"
}
```

也可以在配置文件中使用 `"token": "..."`。环境变量会覆盖配置文件：

| 环境变量 | 含义 |
|---|---|
| `CQU_NETPROBE_GATEWAY_URL` | Gateway 根地址 |
| `CQU_NETPROBE_TOKEN` | Bearer Token |
| `CQU_NETPROBE_TOKEN_FILE` | Token 文件路径 |
| `CQU_NETPROBE_LOG_LEVEL` | `debug`、`info`、`warn`、`error` 或 `off` |

生产环境的 Gateway 必须使用 HTTPS。为方便本地开发，仅回环地址允许使用 HTTP。

## 运行

```console
go run ./cmd/cqu-netprobe --config.file cqu-netprobe.json
```

命令行覆盖项：

```text
--gateway.url
--gateway.token-file
--log.level
--version
```

程序收到 `SIGINT` 或 `SIGTERM` 后会停止当前工作并退出。成功拉取的拨测计划在
本次进程生命周期中保持不变；Gateway 修改计划后需要重启探针。

## 构建

```console
go build -trimpath -o cqu-netprobe ./cmd/cqu-netprobe
```

发布版本可通过链接参数注入：

```console
go build -trimpath -ldflags="-X main.version=0.1.0" -o cqu-netprobe ./cmd/cqu-netprobe
```

## Docker

```console
docker build --build-arg VERSION=0.1.0 -t cqu-netprobe .
docker run --rm --cap-add NET_RAW \
  -v "$PWD/cqu-netprobe.json:/etc/cqu-netprobe/config.json:ro" \
  -v "$PWD/cqu-netprobe.token:/etc/cqu-netprobe/cqu-netprobe.token:ro" \
  cqu-netprobe --config.file /etc/cqu-netprobe/config.json
```

Linux 实现会优先使用内核的非特权 ping socket；系统未允许该方式时会回退到
raw socket，因此容器示例添加了 `NET_RAW` capability。Windows 实现使用系统
IP Helper ICMP API，无需启动外部 `ping` 进程。

## 调度语义

启动并成功拉取配置后立即执行第一轮。相邻轮次按 Gateway 下发的
`config.interval_ms` 对齐，轮次不会重叠；如果一轮耗时过长，已经错过的调度点
会被跳过。每轮结束后只 Push 本轮结果。

HTTP 拨测直连目标，不读取系统代理环境变量。耗时包含重定向和响应体读取
（最多读取 1 MiB）；即使读取响应体失败，`success` 仍遵循 v1 的状态码定义，
错误仅记录到日志。因此它表示 HTTP 状态可达性，不保证完整内容下载成功。
ICMP 域名目标优先选择 IPv4，无 IPv4 地址时使用 IPv6；jitter 按发送序号排列的
成功样本计算。Windows RTT 使用单调时钟测量 API 调用耗时，保留亚毫秒精度，
但包含系统 API 调用开销。Windows 同步 ICMP 调用退出时可能等待当前请求超时。

回归测试：`go test ./...`；真实 IPv4/IPv6 回环测试：
`go test -tags=integration ./internal/probe`。Linux 集成测试还验证 raw socket，
需要 root 或 `CAP_NET_RAW`。这些测试不依赖公网或校园网目标。

接口与 Payload 定义见 [`CQU NetProbe Protocol v1.md`](CQU%20NetProbe%20Protocol%20v1.md)
和 [`openapi.json`](openapi.json)。
