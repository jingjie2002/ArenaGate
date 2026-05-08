# ArenaGate 协议文档

ArenaGate v1 使用 WebSocket 长连接，消息格式为 JSON。

连接地址：

```text
ws://127.0.0.1:18082/ws
```

## mock 鉴权

v1 没有真实账号系统。客户端必须先发送：

```json
{"type":"auth","request_id":"1","player_id":"p1","token":"dev-token:p1"}
```

成功返回：

```json
{"type":"authed","request_id":"1","player_id":"p1"}
```

规则：

```text
token = AUTH_TOKEN_PREFIX + player_id
```

默认 `AUTH_TOKEN_PREFIX` 是：

```text
dev-token:
```

## 心跳

请求：

```json
{"type":"ping","request_id":"2"}
```

响应：

```json
{"type":"pong","request_id":"2"}
```

## 发起匹配

请求：

```json
{
  "type": "enqueue_match",
  "request_id": "3",
  "mmr_score": 1200,
  "match_mode": "duel",
  "max_wait_ms": 30000
}
```

返回排队中：

```json
{
  "type": "match_queued",
  "request_id": "3",
  "player_id": "p1",
  "ticket_id": "ticket_1",
  "status": "queued"
}
```

匹配成功后，ArenaGate 会主动推送：

```json
{
  "type": "match_found",
  "request_id": "3",
  "ticket_id": "ticket_1",
  "status": "matched",
  "match_id": "match_1",
  "room_id": "room_demo_1",
  "server_id": "demo-room-1",
  "server_addr": "127.0.0.1:7001",
  "players": ["p1", "p2"]
}
```

## 查询匹配状态

请求：

```json
{"type":"match_status","request_id":"4","ticket_id":"ticket_1"}
```

如果不传 `ticket_id`，ArenaGate 会使用当前 session 记录的 pending ticket。

## 取消匹配

请求：

```json
{"type":"cancel_match","request_id":"5","ticket_id":"ticket_1"}
```

响应：

```json
{
  "type": "match_cancelled",
  "request_id": "5",
  "ticket_id": "ticket_1",
  "status": "cancelled"
}
```

## 错误响应

```json
{"type":"error","request_id":"3","message":"auth is required before enqueue_match"}
```

## v1 边界

- 当前协议只覆盖接入、心跳和匹配。
- 当前不包含房间内 ready、移动、技能、战斗帧。
- 当前不包含真实账号登录。
- 当前不保证分布式多网关下的 session 迁移。
