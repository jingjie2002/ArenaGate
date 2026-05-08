# ArenaGate 技术报告

## 摘要

ArenaGate 是一个 Go 游戏网关项目，用于承接玩家 WebSocket 长连接，并与 CoreRank 匹配中台联动。

当前 v1 已完成玩家接入、mock 鉴权、session 管理、心跳、限流、协议处理、CoreRank RESTful 调用、匹配结果推送、metrics、demo、集成测试、GitHub Actions CI 和本机小规模压测。

## 架构

```text
WebSocket Client
    |
    | auth / ping / enqueue_match / cancel_match / match_status
    v
ArenaGate
    |
    | POST /api/match/tickets
    | GET  /api/match/tickets/{ticket_id}
    | DELETE /api/match/tickets/{ticket_id}
    | GET  /api/match/results/{match_id}
    v
CoreRank
    |
    | match_id / room_id / server_addr
    v
ArenaGate
    |
    | match_found
    v
WebSocket Client
```

CoreRank 负责匹配和房间资源分配，ArenaGate 负责玩家长连接和结果推送。二者职责分离，避免把匹配中台扩成连接网关，也避免把网关扩成完整战斗服。

## 关键设计

### WebSocket 接入

ArenaGate 暴露 `/ws` 作为玩家长连接入口。v1 使用 Go 标准库实现最小 WebSocket 握手和文本帧读写，没有引入第三方 WebSocket 依赖。

### Session 管理

每条连接对应一个进程内 session，记录：

- 连接来源。
- 鉴权状态。
- player_id。
- pending ticket。
- 最后活跃时间。
- 每秒消息限流状态。

v1 使用进程内 session，是为了聚焦长连接生命周期和网关边界；分布式 session 属于 V3 范围。

### 鉴权

v1 使用 mock token：

```text
token = dev-token:{player_id}
```

这样可以保留网关鉴权流程，又不把项目扩成账号系统。

### 协议

客户端请求：

- `auth`
- `ping`
- `enqueue_match`
- `cancel_match`
- `match_status`

服务端响应：

- `authed`
- `pong`
- `match_queued`
- `match_cancelled`
- `match_status`
- `match_found`
- `error`

协议详情见 `docs/protocol.md`。

### CoreRank 联动

ArenaGate 通过 RESTful API 调用 CoreRank：

- 创建匹配票据。
- 查询票据状态。
- 取消匹配票据。
- 查询匹配结果。

当 CoreRank 返回匹配成功后，ArenaGate 会向在线玩家推送 `match_found`，其中包含 `match_id`、`room_id`、`server_id`、`server_addr` 和玩家列表。

### 指标

ArenaGate 暴露 `/metrics`，当前包含：

- `arenagate_active_sessions`
- `arenagate_connections_total`
- `arenagate_messages_total`
- `arenagate_errors_total`
- `arenagate_core_requests_total`
- `arenagate_core_errors_total`
- `arenagate_match_found_total`

这些指标足够支撑 v1 的连接、消息处理、CoreRank 调用和匹配推送观测。

## 验证结果

### 本地基础验证

```powershell
go test ./...
go vet ./...
go build -o tmp\arenagate-gateway.exe ./cmd/gateway
```

验证内容：

- 单元测试。
- WebSocket handler 集成测试。
- Go 静态检查。
- gateway 构建。

### fake CoreRank demo

```powershell
python scripts\gate_ws_demo.py
```

验证内容：

- fake CoreRank 启动。
- ArenaGate 启动。
- 两个 WebSocket 玩家完成 auth。
- 两个玩家发起匹配。
- 两个玩家收到 `match_found`。

### 真实 CoreRank 联调

```powershell
python scripts\gate_real_corerank_demo.py
```

验证内容：

- 启动真实 CoreRank Server。
- 连接 Redis。
- 注册本地 room server 资源。
- ArenaGate 指向真实 CoreRank。
- 两个玩家通过 ArenaGate 创建匹配。
- CoreRank 真实生成票据、匹配结果、`room_id` 和 `server_addr`。
- ArenaGate 推送真实 `match_found`。

### 小规模压测

```powershell
python scripts\gate_ws_benchmark.py
```

本轮结果：

```text
clients=40
auth_success=40
ping_messages=120
match_found=40
queued_before_found=20
auth_ms_avg=0.69
ping_ms_avg=0.63
ping_ms_p95=1.16
match_ms_avg=28.85
match_ms_p95=56.27
arenagate_active_sessions 40
arenagate_connections_total 40
arenagate_messages_total 200
arenagate_core_requests_total 100
arenagate_core_errors_total 0
arenagate_match_found_total 40
arenagate_errors_total 0
```

结论：ArenaGate v1 在本机小规模压测下可以稳定完成连接、鉴权、心跳、匹配请求、结果推送和指标采集。

## CI

GitHub Actions 覆盖：

- `go test ./...`
- `go vet ./...`
- `go build ./cmd/gateway`

远端仓库：

```text
https://github.com/jingjie2002/ArenaGate
```

## 当前边界

ArenaGate v1 不声明生产级网关能力，当前仍有以下边界：

- 单进程内 session。
- mock 鉴权。
- 未启用 WSS/TLS。
- 未做多实例网关。
- 未做 Kubernetes 部署。
- 未做真实公网环境压测。
- 未做完整战斗服。

这些是后续 V2/V3 的演进方向，不影响 v1 作为简历可讲项目收口。
