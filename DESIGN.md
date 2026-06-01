# QQBot Go SDK - Design Document

## Overview

Go SDK 封装 QQ 机器人开放平台的完整能力，作为消息通道接入 AI 应用。提供：

1. **扫码绑定** - 终端展示二维码，扫码获取 Bot 凭据（appId + appSecret）
2. **消息通道** - WebSocket 长连接收消息 + REST API 发消息
3. **上层抽象** - 一个 callback 接入所有消息，一行代码启动

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   Your AI App                   │
│         (handler func(*qqbot.Message))          │
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│              channel.Channel                     │
│   ScanQR() → Connect() → OnMessage() → Serve()  │
└───┬────────────┬────────────┬───────────────────┘
    │            │            │
┌───▼───┐  ┌────▼────┐  ┌───▼────┐
│connector│ │  auth   │  │gateway │
│ QR码绑定 │ │Token管理│  │ WS网关  │
└───┬───┘  └────┬────┘  └───┬────┘
    │            │            │
┌───▼────────────▼────────────▼───────────────────┐
│                    api.Client                     │
│          REST API (发消息/上传媒体)                │
└──────────────────────────────────────────────────┘
```

## Module Design

### 1. `connector/` - 扫码绑定

复刻 `@tencent-connect/qqbot-connector` 协议。

**协议流程：**

```
POST https://q.qq.com/lite/create_bind_task
  Body: { "key": "<base64(32 random bytes)>" }
  Response: { "retcode": 0, "data": { "task_id": "..." } }

→ 构造二维码 URL:
  https://q.qq.com/qqbot/openclaw/connect.html?task_id={task_id}&source={source}&_wv=2

POST https://q.qq.com/lite/poll_bind_result   (每2秒轮询)
  Body: { "task_id": "..." }
  Response: {
    "retcode": 0,
    "data": {
      "status": 0|1|2|3,          // NONE|PENDING|COMPLETED|EXPIRED
      "bot_appid": "...",
      "bot_encrypt_secret": "..." // AES-256-GCM 加密的 appSecret
    }
  }

→ COMPLETED 时用 AES-256-GCM 解密:
  key = 之前生成的 32 字节随机 key
  ciphertext 格式: [12字节IV][密文][16字节Tag]
  解密得到 appSecret
```

**Status 枚举：**

| Value | Name      | 含义 |
|-------|-----------|------|
| 0     | NONE      | 未扫码 |
| 1     | PENDING   | 已扫码，等待确认 |
| 2     | COMPLETED | 绑定成功 |
| 3     | EXPIRED   | 二维码过期 |

**Go 接口：**

```go
type Credentials struct {
    AppID     string
    AppSecret string
}

type ConnectOptions struct {
    Source string            // 平台标识，空则显示"第三方机器人"
    Signal context.Context   // 取消信号
    OnQrCode func(url string) // 二维码 URL 回调
    OnExpired func()          // 过期回调
    OnStatus  func(status int, msg string)
}

func Connect(ctx context.Context, opts *ConnectOptions) (*Credentials, error)
```

### 2. `auth/` - Token 管理

**Token 获取：**

```
POST https://bots.qq.com/app/getAppAccessToken
  Body: { "appId": "...", "clientSecret": "..." }
  Response: { "access_token": "...", "expires_in": 7200 }
```

**特性：**
- 缓存 + 提前刷新（剩余 1/3 TTL 或 5 分钟）
- singleflight 防并发
- 后台自动续期

**Go 接口：**

```go
type TokenManager struct { ... }

func NewTokenManager(appID, appSecret string) *TokenManager
func (tm *TokenManager) GetToken(ctx context.Context) (string, error)
func (tm *TokenManager) StartAutoRefresh(ctx context.Context)
func (tm *TokenManager) Invalidate()
```

### 3. `gateway/` - WebSocket 网关

**连接生命周期：**

```
GET /gateway → { "url": "wss://..." }
→ WebSocket 连接 (带 User-Agent header)
→ 收到 Hello (op=10): { "d": { "heartbeat_interval": 41250 } }
→ 发送 Identify (op=2) 或 Resume (op=6)
→ 收到 Dispatch (op=0): t=READY → 保存 session_id
→ 心跳保活 (op=1, 每 heartbeat_interval ms)
→ 消息分发到 handler
```

**Opcodes：**

| Op | Name          | 方向 |
|----|---------------|------|
| 0  | Dispatch      | S→C  |
| 1  | Heartbeat     | C→S  |
| 2  | Identify      | C→S  |
| 6  | Resume        | C→S  |
| 7  | Reconnect     | S→C  |
| 9  | InvalidSession| S→C  |
| 10 | Hello         | S→C  |
| 11 | Heartbeat ACK | S→C  |

**Intents：**

```go
const (
    IntentGuildMessages       = 1 << 30  // 频道消息
    IntentDirectMessage       = 1 << 12  // 频道私信
    IntentGroupAndC2C         = 1 << 25  // 群聊 + C2C
    IntentInteraction         = 1 << 26  // 按钮交互
)
// 默认: IntentGuildMessages | IntentDirectMessage | IntentGroupAndC2C | IntentInteraction
// 值: 1174405120
```

**Identify 载荷：**

```json
{
  "op": 2,
  "d": {
    "token": "QQBot <access_token>",
    "intents": 1174405120,
    "shard": [0, 1]
  }
}
```

**Resume 载荷：**

```json
{
  "op": 6,
  "d": {
    "token": "QQBot <access_token>",
    "session_id": "...",
    "seq": 12345
  }
}
```

**断线重连策略：**

```
重连延迟: [1s, 2s, 5s, 10s, 30s, 60s] (渐进退避)
最大重试: 100 次
快速断线: 连接 < 5s 内断开，连续 3 次后等 60s
限流 (4008): 等 60s
无效 Token (4004): 刷新 token 后重连
```

**Close Codes：**

| Code | 处理 |
|------|------|
| 1000 | 正常关闭，不重连 |
| 4004 | 刷新 token，重连 |
| 4006/4007/4009 | 清除 session，刷新 token，重连 |
| 4008 | 等 60s，重连 |
| 4900-4913 | 清除 session，刷新 token，重连 |
| 4914/4915 | Bot 离线/被封，不重连，返回错误 |

**Go 接口：**

```go
type Gateway struct { ... }

type GatewayConfig struct {
    Token    string
    Intents  int
    Handler  func(event *DispatchEvent)
    OnReady  func(sessionID string)
    OnClose  func(err error)
}

func NewGateway(cfg *GatewayConfig) *Gateway
func (g *Gateway) Connect(ctx context.Context) error
func (g *Gateway) Close() error
```

### 4. `api/` - REST API 客户端

**Base URL:** `https://api.sgroup.qq.com`

**Authorization:** `QQBot <access_token>`

**主要接口：**

```go
type Client struct { ... }

func NewClient(tokenMgr *auth.TokenManager) *Client

// 消息
func (c *Client) SendC2CMessage(ctx, openid string, msg *Message) (*MessageResponse, error)
func (c *Client) SendGroupMessage(ctx, groupOpenid string, msg *Message) (*MessageResponse, error)
func (c *Client) SendChannelMessage(ctx, channelID string, msg *Message) (*MessageResponse, error)
func (c *Client) SendGuildDM(ctx, guildID string, msg *Message) (*MessageResponse, error)

// 流式消息 (C2C only)
func (c *Client) SendStreamMessage(ctx, openid string, msg *StreamMessage) (*MessageResponse, error)

// 媒体上传
func (c *Client) UploadMedia(ctx, scope, targetID string, file *MediaFile) (*MediaInfo, error)

// 交互
func (c *Client) AckInteraction(ctx, interactionID string, code int) error
```

**消息类型：**

| msg_type | 说明 |
|----------|------|
| 0        | 普通文本 |
| 2        | Markdown |
| 6        | 正在输入通知 |
| 7        | 媒体消息 |

**媒体文件类型：**

| Type  | Value |
|-------|-------|
| IMAGE | 1     |
| VIDEO | 2     |
| VOICE | 3     |
| FILE  | 4     |

### 5. `channel/` - 上层抽象

一站式接入：扫码 → 连接 → 收发消息。

```go
type Message struct {
    ID        string
    Type      string // "c2c", "group", "guild", "dm"
    Content   string
    Author    Author
    Target    string // openid / group_openid / channel_id
    Timestamp string
    Attachments []Attachment
    Reply     func(content string) error  // 快捷回复
    ReplyMarkdown func(content string) error
    ReplyMedia func(file *MediaFile) error
    Stream    func(onChunk func(content string, done bool)) error  // 流式回复
}

type Author struct {
    ID       string
    Name     string
    OpenID   string
    IsBot    bool
}

type Handler func(msg *Message)

type Channel struct { ... }

type ChannelConfig struct {
    AppID     string           // 为空则触发扫码
    AppSecret string           // 为空则触发扫码
    Source    string           // 扫码平台标识
    Handler   Handler          // 消息回调
    Intents   int              // 默认全量 intents
    Log       *slog.Logger     // 日志
}

func NewChannel(cfg *ChannelConfig) *Channel
func (ch *Channel) Connect(ctx context.Context) error
func (ch *Channel) Close() error

// 便捷方法
func QuickStart(ctx context.Context, handler Handler) error  // 扫码 + 连接
```

### 6. `types/` - 类型定义

所有事件结构体、消息体、常量。

**事件类型：**

| 事件名 | 说明 |
|--------|------|
| READY | 连接就绪 |
| RESUMED | 会话恢复 |
| C2C_MESSAGE_CREATE | C2C 私聊消息 |
| GROUP_AT_MESSAGE_CREATE | 群 @消息 |
| GROUP_MESSAGE_CREATE | 群所有消息 |
| AT_MESSAGE_CREATE | 频道 @消息 |
| DIRECT_MESSAGE_CREATE | 频道私信 |
| GROUP_ADD_ROBOT | 机器人入群 |
| GROUP_DEL_ROBOT | 机器人退群 |
| GROUP_MSG_REJECT | 群拒绝主动消息 |
| GROUP_MSG_RECEIVE | 群接受主动消息 |
| INTERACTION_CREATE | 按钮交互 |

## Project Structure

```
qqbot-go/
├── DESIGN.md           # 本文档
├── go.mod
├── go.sum
├── cmd/
│   └── example/
│       └── main.go     # 示例：扫码 + 启动
├── qqbot.go            # 顶层 API (QuickStart)
├── connector/
│   ├── connector.go    # 扫码绑定主逻辑
│   └── crypto.go       # AES-256-GCM 解密
├── auth/
│   └── token.go        # Token 获取/缓存/刷新
├── gateway/
│   ├── gateway.go      # WebSocket 连接管理
│   ├── reconnect.go    # 断线重连逻辑
│   └── session.go      # Session 持久化
├── api/
│   ├── client.go       # REST API 基础客户端
│   ├── message.go      # 消息发送
│   ├── media.go        # 媒体上传（简单 + 分片）
│   └── stream.go       # 流式消息
├── channel/
│   └── channel.go      # 上层消息通道抽象
├── types/
│   ├── events.go       # 事件结构体
│   ├── message.go      # 消息类型
│   └── ws.go           # WebSocket 帧结构
└── internal/
    ├── qrterm/         # 终端二维码渲染
    │   └── qrterm.go
    └── httputil/       # HTTP 工具函数
        └── client.go
```

## Implementation Phases

### Phase 1: 核心基础 (先做)
1. `types/` - 所有类型定义
2. `auth/` - Token 管理
3. `gateway/` - WebSocket 网关（核心）
4. `api/client.go` + `api/message.go` - REST 基础 + 发消息
5. `cmd/example/main.go` - 最小可用示例

### Phase 2: 扫码绑定
6. `connector/` - 扫码协议 + 解密
7. `internal/qrterm/` - 终端二维码渲染
8. `channel/` - 上层整合（扫码 + 连接）

### Phase 3: 高级功能
9. `api/media.go` - 媒体上传
10. `api/stream.go` - 流式消息
11. `gateway/session.go` - Session 持久化

## Dependencies

```go
// go.mod
module github.com/yourname/qqbot-go

go 1.21

require (
    github.com/gorilla/websocket v1.5.3   // WebSocket
    github.com/skip2/go-qrcode v0.0.0-... // 二维码生成
)
```

## Usage Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"

    "github.com/yourname/qqbot-go"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    err := qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
        fmt.Printf("[%s] %s: %s\n", msg.Type, msg.Author.Name, msg.Content)
        
        // 简单 echo
        msg.Reply("收到: " + msg.Content)
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

## Security Notes

- Token 不落盘，仅内存缓存
- appSecret 通过 AES-256-GCM 加密传输，SDK 在内存中解密
- 连接使用 TLS (wss://)
- 支持 context.Context 取消所有阻塞操作
