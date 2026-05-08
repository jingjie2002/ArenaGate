# ArenaGate 压测与指标记录

## 目标

这份记录用于证明 ArenaGate v1 不只可以跑通单次 demo，也可以在本机完成一批 WebSocket 连接、心跳消息、匹配请求和 metrics 采集。

该压测是 v1 简历项目的最小工程证据，不等同于生产级容量评估。

## 压测范围

本轮压测覆盖：

- 批量建立 WebSocket 连接。
- 每个连接完成 mock auth。
- 每个连接发送多轮 `ping` 并收到 `pong`。
- 每个连接发送 `enqueue_match`。
- fake CoreRank 完成两两匹配。
- ArenaGate 向所有连接推送 `match_found`。
- 拉取 `/metrics` 记录连接数、消息数、CoreRank 请求数和匹配成功数。

本轮压测不覆盖：

- 真实公网网络延迟。
- WSS/TLS。
- 多实例网关。
- Redis 分布式 session。
- 完整战斗服或房间内状态同步。
- 上万连接级别生产压测。

## 运行命令

```powershell
cd F:\AI编程\简历\ArenaGate
$env:GOCACHE = Join-Path (Get-Location) ".gocache"
$env:GOMODCACHE = Join-Path (Get-Location) ".gomodcache"
python scripts\gate_ws_benchmark.py
```

默认参数：

```text
clients=40
ping_rounds=3
workers=10
```

也可以手动调整：

```powershell
python scripts\gate_ws_benchmark.py --clients 80 --ping-rounds 3 --workers 10
```

`--clients` 必须是正偶数，因为 fake CoreRank 会按双人匹配。

## 本轮结果

运行时间：2026-05-08

```text
ArenaGate benchmark completed
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
metrics_snapshot:
arenagate_active_sessions 40
arenagate_connections_total 40
arenagate_messages_total 200
arenagate_core_requests_total 100
arenagate_core_errors_total 0
arenagate_match_found_total 40
arenagate_errors_total 0
```

## 结果解读

- `auth_success=40`：40 个 WebSocket 客户端均完成 mock 鉴权。
- `ping_messages=120`：40 个连接各完成 3 次 `ping/pong`。
- `match_found=40`：所有客户端都收到了匹配成功推送。
- `arenagate_messages_total=200`：网关处理了 40 次 auth、120 次 ping、40 次 enqueue。
- `arenagate_core_requests_total=100`：网关向 CoreRank 发起了创建票据、轮询票据和查询匹配结果等请求。
- `arenagate_core_errors_total=0`：压测期间 CoreRank 调用没有失败。
- `arenagate_errors_total=0`：压测期间没有协议或处理错误返回给客户端。

## 当前结论

ArenaGate v1 已具备可展示的本机小规模长连接压测证据：连接、鉴权、心跳、匹配请求、匹配结果推送和 metrics 采集链路都可以闭环。

该结果只用于证明 v1 工程链路成立，不声明生产容量上限。
