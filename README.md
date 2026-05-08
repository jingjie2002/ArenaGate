# ArenaGate

ArenaGate 是一个 Go 游戏网关与长连接接入层示例项目。

它的目标不是做完整游戏服务器，而是把“玩家进入游戏后的第一道门”讲清楚、跑起来、测得出。

## 它在游戏里控制什么

一局在线游戏通常可以拆成几层：

```text
玩家客户端
  -> ArenaGate：玩家连接入口、会话、心跳、协议校验、限流
  -> CoreRank：排行榜、匹配、房间资源分配
  -> roomserver / battle server：入房、准备、战斗开始、战斗帧或状态同步
```

ArenaGate 控制的是：

- 玩家能不能连进来。
- 这条连接属于哪个玩家。
- 玩家连接是否还活着。
- 玩家发来的消息是否合法。
- 玩家上线后是否收到运营公告、赛季提示或维护状态。
- 玩家发起匹配时，应该把请求转给哪个后端服务。
- 匹配成功后，如何把 `room_id` / `server_addr` 推回给在线玩家。

ArenaGate 不控制的是：

- 角色移动。
- 技能命中。
- 伤害结算。
- 战斗帧同步。
- 房间内准备状态。
- 排行榜分数如何存储。
- 匹配算法如何挑选对手。

这些分别由客户端、战斗服、房间服和 CoreRank 负责。

## 它和 CoreRank 的关系

CoreRank 是“匹配和排行榜中台”：

- 记录排行榜分数。
- 创建匹配票据。
- 找到能匹配到一起的玩家。
- 分配 `room_id` 和 `server_addr`。

ArenaGate 是“玩家接入网关”：

- 玩家先连 ArenaGate。
- ArenaGate 维护 WebSocket 长连接。
- 玩家通过 ArenaGate 发起匹配。
- ArenaGate 调用 CoreRank。
- CoreRank 匹配成功后，ArenaGate 把结果推给玩家。

一句话：

```text
CoreRank 负责决定“谁和谁打、去哪个房间”；ArenaGate 负责让玩家保持在线，并把这个结果送回玩家。
```

## 当前 v1 能力

- `/ws`：WebSocket 长连接入口。
- `/healthz`：健康检查。
- `/metrics`：Prometheus 文本指标。
- mock 鉴权：`token` 必须等于 `dev-token:{player_id}`。
- session 管理：记录玩家连接、鉴权状态和 pending ticket。
- 心跳：客户端发 `ping`，服务端回 `pong`。
- 运营通知：鉴权后可推送 `server_notice`。
- 维护态：可通过环境变量开启 `maintenance_state`，并拒绝新的匹配入场。
- 简单限流：默认每个 session 每秒最多 20 条消息。
- 消息大小限制：默认 32KB。
- CoreRank RESTful 联动：
  - 创建匹配票据。
  - 查询匹配状态。
  - 取消匹配。
  - 匹配成功后查询匹配结果。
- 本地 demo：使用 fake CoreRank 演示两个玩家收到 `match_found`。
- 真实联调 demo：启动真实 CoreRank Server，经真实 CoreRank RESTful API 完成匹配并推送 `match_found`。
- 最小压测：批量 WebSocket 连接、心跳、匹配请求和 `/metrics` 记录。

## 项目文档

- [概念说明](docs/concepts.md)
- [协议文档](docs/protocol.md)
- [验证指南](docs/verification.md)
- [压测与指标记录](docs/benchmark.md)
- [项目方案书](ArenaGate_Proposal.md)
- [技术报告](ArenaGate_Technical_Report.md)

## 当前明确不做

- 不做完整战斗服。
- 不做房间服替代品。
- 不重写 CoreRank 的匹配和排行榜。
- 不做真实账号系统。
- 不做反作弊。
- 不做 Redis 分布式 session。
- 不做 Kubernetes 或生产级网关集群。

## 快速验证

```powershell
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
$env:GOMODCACHE = Join-Path (Get-Location) ".gomodcache"
go test ./...
go vet ./...
go build -o tmp\arenagate-gateway.exe ./cmd/gateway
python scripts\gate_ws_demo.py
python scripts\gate_real_corerank_demo.py
python scripts\gate_ws_benchmark.py
```

fake demo 成功时能看到类似输出：

```text
p1 authed
p1 notice SS25 season is live; ranked queue is open
p2 authed
p2 notice SS25 season is live; ranked queue is open
p1 queued ticket_1
p2 match_found room_demo_1 127.0.0.1:7001
p1 match_found room_demo_1 127.0.0.1:7001
ArenaGate WebSocket demo completed
```

真实 CoreRank demo 成功时能看到类似输出：

```text
CoreRank registered room server arena-demo-room-1234 for arenagate_real_1234_...
p_gate_real_a_... authed through ArenaGate
p_gate_real_b_... authed through ArenaGate
p_gate_real_a_... queued by real CoreRank ticket=ticket_...
real CoreRank match_id=match_...
real CoreRank room_id=room_... server_addr=127.0.0.1:7001
ArenaGate real CoreRank demo completed
```

压测成功时能看到类似输出：

```text
ArenaGate benchmark completed
clients=40
auth_success=40
ping_messages=120
match_found=40
metrics_snapshot:
arenagate_connections_total 40
arenagate_messages_total 200
arenagate_match_found_total 40
```

## 手动启动

```powershell
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
$env:GOMODCACHE = Join-Path (Get-Location) ".gomodcache"
$env:GATEWAY_ADDR = "127.0.0.1:18082"
$env:CORE_RANK_HTTP = "http://127.0.0.1:8081"
$env:SERVER_NOTICE = "SS25 season is live; ranked queue is open"
$env:MAINTENANCE_ENABLED = "false"
go run ./cmd/gateway
```

然后客户端连接：

```text
ws://127.0.0.1:18082/ws
```

## 目录结构

```text
cmd/gateway/          ArenaGate 启动入口
internal/config/      环境变量配置
internal/coreclient/  CoreRank RESTful 客户端
internal/gateway/     WebSocket、session、限流、metrics 和消息处理
internal/protocol/    ArenaGate JSON 消息协议
docs/                 面向理解和验收的文档
scripts/              本地演示脚本
```
