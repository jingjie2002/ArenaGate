# ArenaGate 项目方案书

## 项目定位

ArenaGate 是一个基于 Go 实现的游戏网关与 WebSocket 长连接接入层。

它不是完整游戏服务器，也不是战斗服。它负责玩家进入游戏后的第一道后端入口：连接接入、mock 鉴权、session 管理、心跳、协议校验、限流、转发匹配请求，并把 CoreRank 的匹配结果推送回在线玩家。

## 为什么需要 ArenaGate

CoreRank 已经负责排行榜、匹配票据、匹配结果和房间资源分配，但玩家客户端不能直接把所有实时连接都压到 CoreRank 上。

ArenaGate 补齐的是玩家接入层：

```text
玩家客户端
  -> ArenaGate：WebSocket 长连接、session、心跳、协议校验、限流
  -> CoreRank：匹配、排行榜、房间资源分配
  -> roomserver / battle server：入房、准备、战斗状态
```

一句话：CoreRank 决定“谁和谁打、去哪个房间”，ArenaGate 负责“玩家怎么连进来、怎么收到这个结果”。

## v1 目标

ArenaGate v1 的目标是做成一个简历可讲、可运行、可验证的网关项目，而不是生产级完整网关。

v1 要打通：

```text
WebSocket 玩家
  -> ArenaGate
  -> CoreRank RESTful API
  -> ArenaGate 推送 match_found
```

## v1 范围

已纳入 v1：

- `/ws` WebSocket 长连接入口。
- `/healthz` 健康检查。
- `/metrics` Prometheus 文本指标。
- mock token 鉴权：`dev-token:{player_id}`。
- 进程内 session 管理。
- 心跳 `ping/pong`。
- 消息大小限制。
- 每 session 简单限流。
- JSON 消息协议。
- CoreRank RESTful client。
- `enqueue_match`、`cancel_match`、`match_status`、`match_found`。
- fake CoreRank demo。
- 真实 CoreRank + Redis 联调 demo。
- WebSocket handler 集成测试。
- GitHub Actions CI。
- 本机小规模 WebSocket 压测与 metrics 记录。

## v1 明确不做

- 不做完整战斗服。
- 不做角色移动、技能、伤害、帧同步。
- 不做房间服替代品。
- 不重写 CoreRank 的匹配和排行榜。
- 不做真实账号系统。
- 不做 JWT 完整权限体系。
- 不做 WSS/TLS/证书管理。
- 不做 Redis 分布式 session。
- 不做多实例网关集群。
- 不做 Kubernetes 部署。
- 不声明生产级容量。

## 核心模块

```text
cmd/gateway/          网关启动入口
internal/config/      环境变量配置
internal/coreclient/  CoreRank RESTful 客户端
internal/gateway/     WebSocket、session、限流、metrics 和消息处理
internal/protocol/    JSON 消息协议
scripts/              demo 和压测脚本
docs/                 协议、概念、验证和压测文档
```

## 验收标准

v1 收口以以下结果为准：

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `go build ./cmd/gateway` 通过。
- fake CoreRank WebSocket demo 通过。
- 真实 CoreRank + Redis 联调 demo 通过。
- 小规模 WebSocket 压测通过。
- `/metrics` 可以看到连接、消息、CoreRank 请求、匹配成功和错误指标。
- GitHub Actions CI 通过。

## 后续演进

V2 可以考虑：

- JWT 鉴权。
- 更完整错误码。
- CoreRank 调用超时、重试和熔断。
- Docker Compose 一键启动 ArenaGate + CoreRank + Redis。
- 更完整压测报告。

V3 可以考虑：

- 多实例网关。
- Redis/NATS/Kafka 跨节点消息路由。
- 分布式 session。
- WSS/TLS。
- Kubernetes 部署。
- 更大规模连接压测。

V2/V3 是演进方向，不属于当前 v1 已完成成果。
