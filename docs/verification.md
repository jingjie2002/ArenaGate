# ArenaGate 验证指南

## 基础验证

```powershell
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
$env:GOMODCACHE = Join-Path (Get-Location) ".gomodcache"
go test ./...
go vet ./...
go build -o tmp\arenagate-gateway.exe ./cmd/gateway
```

这三条分别验证：

- 单元测试通过。
- Go 静态检查通过。
- 网关入口可以构建。

## 本地 WebSocket demo

```powershell
python scripts\gate_ws_demo.py
```

这个 demo 会自动做四件事：

1. 启动一个 fake CoreRank HTTP 服务。
2. 构建并启动 ArenaGate。
3. 模拟两个 WebSocket 玩家连接 ArenaGate。
4. 两个玩家发起匹配，并收到 `match_found`。

成功时能看到：

```text
p1 authed
p2 authed
p1 queued ticket_1
p2 match_found room_demo_1 127.0.0.1:7001
p1 match_found room_demo_1 127.0.0.1:7001
ArenaGate WebSocket demo completed
```

## 真实 CoreRank 联调 demo

先确保 Redis 已经启动。可以在 CoreRank 目录执行：

```powershell
cd F:\AI编程\简历\CoreRank
docker compose up -d corerank-redis
```

然后回到 ArenaGate 目录执行：

```powershell
cd F:\AI编程\简历\ArenaGate
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
$env:GOMODCACHE = Join-Path (Get-Location) ".gomodcache"
python scripts\gate_real_corerank_demo.py
```

这个 demo 会自动做这些事：

1. 构建并启动真实 CoreRank Server。
2. 向 CoreRank 注册一个本地 room server 资源。
3. 构建并启动 ArenaGate，让 `CORE_RANK_HTTP` 指向真实 CoreRank。
4. 模拟两个 WebSocket 玩家连接 ArenaGate。
5. 两个玩家通过 ArenaGate 发起匹配。
6. CoreRank 真实生成匹配票据、匹配结果、`room_id` 和 `server_addr`。
7. ArenaGate 向两个玩家推送 `match_found`。

成功时能看到：

```text
CoreRank registered room server arena-demo-room-1234 for arenagate_real_1234_...
p_gate_real_a_... authed through ArenaGate
p_gate_real_b_... authed through ArenaGate
p_gate_real_a_... queued by real CoreRank ticket=ticket_...
real CoreRank match_id=match_...
real CoreRank room_id=room_... server_addr=127.0.0.1:7001
ArenaGate real CoreRank demo completed
```

## 手动健康检查

启动网关后：

```powershell
Invoke-RestMethod http://127.0.0.1:18082/healthz
```

预期：

```json
{
  "active_sessions": 0,
  "status": "ok"
}
```

## 手动指标检查

```powershell
Invoke-WebRequest http://127.0.0.1:18082/metrics
```

应该能看到：

```text
arenagate_active_sessions
arenagate_connections_total
arenagate_messages_total
arenagate_errors_total
arenagate_core_requests_total
arenagate_core_errors_total
arenagate_match_found_total
```

## 最小压测

```powershell
python scripts\gate_ws_benchmark.py
```

默认会启动 fake CoreRank 和 ArenaGate，并模拟 40 个 WebSocket 客户端完成：

1. WebSocket 连接。
2. mock auth。
3. 3 轮 `ping/pong`。
4. `enqueue_match`。
5. 接收 `match_found`。
6. 拉取 `/metrics`。

本轮记录见：[压测与指标记录](benchmark.md)。

成功时能看到类似输出：

```text
ArenaGate benchmark completed
clients=40
auth_success=40
ping_messages=120
match_found=40
metrics_snapshot:
arenagate_active_sessions 40
arenagate_connections_total 40
arenagate_messages_total 200
arenagate_core_errors_total 0
arenagate_match_found_total 40
arenagate_errors_total 0
```

## 当前未验证

- 未做 Linux 服务器部署验证。
- 未做浏览器前端演示。
