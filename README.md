# qqbot-go

QQ 机器人 Go SDK，基于 [tencent-connect/openclaw-qqbot](https://github.com/tencent-connect/openclaw-qqbot) 协实现。

提供扫码绑定 + WebSocket 消息通道，一行代码接入你的 AI 应用。

## 功能

- **扫码绑定** - 终端/平台展示二维码链接，用户手机 QQ 扫码获取凭据，无需手动申请
- **凭据持久化** - 支持文件、数据库等任意存储方式，外部完全控制
- **消息接收** - WebSocket 长连接，支持 C2C 私聊、群聊、频道消息、频道私信
- **消息发送** - 文本、Markdown、媒体消息（图片/视频/语音/文件）
- **流式消息** - C2C 私聊支持流式输出（打字机效果），适合 AI 对话场景
- **媒体上传** - 简单上传 + 分片并发上传（大文件）
- **语音自动转码** - WAV/MP3/OGG/FLAC 自动转换为 SILK 格式（依赖 ffmpeg）
- **附件解析** - 自动提取图片 URL、语音 URL、语音识别文字
- **断线重连** - 指数退避重连（最多100次），快速断线检测，限流自动等待
- **Session 恢复** - 断线后自动 Resume，不丢消息
- **Token 自动管理** - 提前刷新、singleflight 防并发、后台自动续期
- **多账号支持** - 可同时管理多个 Bot 实例

## 安装

```bash
go get github.com/libaibaia/qqbot-go
```

要求 Go >= 1.21

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"

    "github.com/libaibaia/qqbot-go"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    err := qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
        fmt.Printf("[%s] %s: %s\n", msg.Type, msg.Author.Name, msg.Content)
        msg.Reply("收到: " + msg.Content)
    }, &qqbot.QuickStartConfig{
        // 持久化凭据到文件，下次启动自动加载，无需重新扫码
        CredentialFile: "./data/qqbot-credentials.json",

        // 回调二维码链接，由你的平台展示给用户
        OnQrURL: func(url string) {
            fmt.Println("请扫描以下链接对应的二维码:")
            fmt.Println(url)
        },

        // 绑定状态回调
        OnStatus: func(status int, msg string) {
            fmt.Println(msg)
        },
    })

    if err != nil {
        log.Fatal(err)
    }
}
```

**首次启动：** `OnQrURL` 回调链接 -> 用户扫码 -> 凭据自动保存到文件  
**后续启动：** 直接读取文件，跳过扫码，立即连接

---

## 凭据来源优先级

SDK 按以下顺序获取凭据，找到即停：

1. `QuickStartConfig.Credentials` - 直接传入
2. `QuickStartConfig.CredentialFile` - 从文件加载
3. `QQBOT_APP_ID` + `QQBOT_CLIENT_SECRET` 环境变量
4. 扫码绑定

---

## 完整接入示例：平台管理多个 Bot

```go
package main

import (
    "context"
    "log"
    "path/filepath"

    "github.com/libaibaia/qqbot-go"
)

type BotManager struct {
    credentialDir string
}

func (bm *BotManager) StartBot(ctx context.Context, userID string, aiHandler func(string) (string, error)) error {
    credFile := filepath.Join(bm.credentialDir, userID+".json")

    return qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
        reply, err := aiHandler(msg.Content)
        if err != nil {
            msg.Reply("处理出错: " + err.Error())
            return
        }
        if err := msg.Reply(reply); err != nil {
            log.Printf("回复失败: %v", err)
        }
    }, &qqbot.QuickStartConfig{
        CredentialFile: credFile,
        OnQrURL: func(url string) {
            // 推送到你的 Web 前端展示二维码
            pushQrToWebUI(userID, url)
        },
        OnStatus: func(status int, msg string) {
            log.Printf("[bot %s] status=%d: %s", userID, status, msg)
        },
    })
}
```

---

## API 参考

### qqbot.QuickStart

```go
func QuickStart(ctx context.Context, handler func(*Message), cfg *QuickStartConfig) error
```

一站式启动，阻塞直到 `ctx` 取消。内部自动处理：凭据加载 -> 扫码绑定 -> 网关连接 -> 消息分发。

### QuickStartConfig

```go
type QuickStartConfig struct {
    // 直接传入凭据，跳过文件和扫码
    Credentials *Credentials

    // 凭据文件路径（JSON 格式）
    // 文件存在则加载，扫码成功后自动保存
    // 空字符串表示不使用文件持久化
    CredentialFile string

    // 二维码链接回调，你的平台拿到 URL 后展示为二维码
    OnQrURL func(url string)

    // 绑定状态回调
    // status: 0=等待扫码, 1=已扫码等待确认, 2=绑定成功, 3=二维码过期（自动刷新中）
    OnStatus func(status int, msg string)

    // 扫码页面显示的平台名称，留空显示"第三方机器人"
    Source string

    // 日志实例，默认 slog.Default()
    Log *slog.Logger
}
```

### Message

```go
type Message struct {
    ID          string                       // 消息 ID
    Type        string                       // "c2c", "group", "channel", "dm"
    Content     string                       // 消息内容
    Author      Author                       // 发送者
    Target      string                       // openid / group_openid / channel_id
    Timestamp   string                       // 时间戳
    Attachments []types.MessageAttachment    // 附件（图片/语音等）
}

type Author struct {
    ID     string
    Name   string
    OpenID string
    IsBot  bool
}
```

### 消息回复方法

```go
// 纯文本回复
msg.Reply("你好")

// Markdown 回复
msg.ReplyMarkdown("**加粗** 消息")

// 媒体回复（需要先上传获取 file_info）
msg.ReplyMedia(fileInfo)

// 流式回复（仅 C2C 私聊）
stream, _ := msg.StartStream()
stream.Send(ctx, "正在思考...", false)  // 中间内容
stream.Send(ctx, "回答如下：...", true) // 最终内容，done=true
```

### 接收媒体附件

```go
// 检查是否有附件
if msg.HasAttachments() {
    // 处理图片
    for _, url := range msg.Images() {
        fmt.Println("图片:", url)
    }

    // 处理语音
    for _, url := range msg.Voices() {
        fmt.Println("语音:", url)
    }

    // QQ 内置语音识别结果
    if text := msg.VoiceText(); text != "" {
        fmt.Println("语音识别:", text)
    }
}
```

### 上传并发送媒体

```go
// 发送图片（从 URL）
msg.UploadAndReplyImage("https://example.com/image.png", nil)

// 发送图片（从本地文件）
data, _ := os.ReadFile("photo.jpg")
msg.UploadAndReplyImage("", data)

// 发送语音（自动转码为 SILK，依赖 ffmpeg）
audioData, _ := os.ReadFile("recording.mp3")
msg.UploadAndReplyVoice("", audioData)

// 发送文件
fileData, _ := os.ReadFile("document.pdf")
msg.UploadAndReplyFile("", fileData, "report.pdf")
```

---

## 语音转码

SDK 内置语音自动转码功能，发送语音消息时自动将常见音频格式转换为 QQ 要求的 SILK v3 格式。

### 支持的输入格式

| 格式 | 扩展名 | 说明 |
|------|--------|------|
| WAV | .wav | PCM 无压缩音频 |
| MP3 | .mp3 | MPEG 音频 |
| OGG | .ogg | Ogg Vorbis |
| FLAC | .flac | 无损压缩音频 |
| AAC/M4A | .aac/.m4a | AAC 编码 |

### 前置条件

需要安装 [ffmpeg](https://ffmpeg.org/download.html)，SDK 启动时会自动检测：

```go
path, err := qqbot.CheckFFmpeg()
if err != nil {
    // ffmpeg 未安装，语音转换不可用
    // 图片和文本消息不受影响
}
```

### 手动转码

```go
// 任意格式 → SILK v3
silkData, err := qqbot.ToSilk(audioData)
if err != nil {
    // 处理错误
}
```

---

## 低级 API

### Channel（直接连接，不使用 QuickStart）

```go
import "github.com/libaibaia/qqbot-go/channel"

ch := channel.New(channel.Config{
    AppID:     "102146862",
    AppSecret: "your-secret",
    Handler: func(msg *channel.Message) {
        msg.Reply("hello")
    },
})
defer ch.Close()

err := ch.Connect(ctx)
```

### Connector（单独扫码绑定）

```go
import "github.com/libaibaia/qqbot-go/connector"

// 扫码绑定
creds, err := connector.Connect(&connector.ConnectOptions{
    Source: "my-platform",  // 扫码页面显示的平台名
    Ctx:   ctx,
    OnQrURL: func(url string) {
        // url 格式: https://q.qq.com/qqbot/openclaw/connect.html?task_id=xxx&source=xxx&_wv=2
        // 你的平台将此 URL 生成二维码展示
    },
    OnStatus: func(status connector.BindStatus, msg string) {
        // connector.StatusNone      = 0  // 等待扫码
        // connector.StatusPending   = 1  // 已扫码，等待确认
        // connector.StatusCompleted = 2  // 绑定成功
        // connector.StatusExpired   = 3  // 二维码过期，正在刷新
    },
})

// 扫码 + 持久化一步完成
store := connector.NewFileStore("/path/to/credentials.json")
creds, err := connector.LoadOrConnect(opts, store)
```

### CredentialStore（自定义持久化）

```go
// 内置文件实现
store := connector.NewFileStore("./data/credentials.json")

// 自定义接口
type CredentialStore interface {
    Load() (*Credentials, error)
    Save(creds *Credentials) error
}
```

自定义数据库实现示例：

```go
type DBStore struct {
    db *sql.DB
}

func (s *DBStore) Load() (*connector.Credentials, error) {
    var creds connector.Credentials
    err := s.db.QueryRow("SELECT app_id, app_secret FROM qqbot LIMIT 1").
        Scan(&creds.AppID, &creds.AppSecret)
    if err == sql.ErrNoRows {
        return nil, nil  // 未找到返回 nil, nil
    }
    return &creds, err
}

func (s *DBStore) Save(creds *connector.Credentials) error {
    _, err := s.db.Exec(
        "INSERT OR REPLACE INTO qqbot (app_id, app_secret) VALUES (?, ?)",
        creds.AppID, creds.AppSecret)
    return err
}
```

---

## 凭据文件格式

```json
{
  "app_id": "102146862",
  "app_secret": "xxxxxxxxxxxx"
}
```

注意：SDK 不会自动创建父目录，调用方需确保路径存在且可写。

---

## Token 管理

SDK 内部自动管理 access_token：

- 获取后缓存，2小时有效期
- 剩余 1/3 TTL 或不足 5 分钟时自动刷新
- singleflight 保证同一 appId 只有一个并发请求
- 后台 goroutine 定时续期，不需要外部干预

---

## 断线重连策略

| 场景 | 处理方式 |
|------|---------|
| 普通断线 | 指数退避: 1s, 2s, 5s, 10s, 30s, 60s |
| 快速断线 (<5s) | 连续3次后等待60s再重试 |
| Token 失效 (4004) | 刷新 Token 后重连 |
| Session 失效 (4006/4007/4009) | 清除 Session，重新 Identify |
| 限流 (4008) | 等待60s后重连 |
| Bot 离线/封禁 (4914/4915) | 不重连，返回错误 |
| 最大重试 | 100次 |

---

## 支持的消息事件

| 事件 | 说明 |
|------|------|
| `C2C_MESSAGE_CREATE` | C2C 私聊消息 |
| `GROUP_AT_MESSAGE_CREATE` | 群聊 @机器人 消息 |
| `GROUP_MESSAGE_CREATE` | 群聊所有消息 |
| `AT_MESSAGE_CREATE` | 频道 @机器人 消息 |
| `DIRECT_MESSAGE_CREATE` | 频道私信 |
| `INTERACTION_CREATE` | 按钮交互回调 |
| `GROUP_ADD_ROBOT` | 机器人被加入群 |
| `GROUP_DEL_ROBOT` | 机器人被移出群 |
| `GROUP_MSG_REJECT` | 群拒绝主动消息 |
| `GROUP_MSG_RECEIVE` | 群接受主动消息 |

---

## 发送消息类型

| msg_type | 说明 |
|----------|------|
| 0 | 纯文本 |
| 2 | Markdown |
| 6 | 正在输入通知 |
| 7 | 媒体消息（图片/视频/语音/文件） |

### 媒体文件类型

| 类型 | 值 |
|------|---|
| IMAGE | 1 |
| VIDEO | 2 |
| VOICE | 3 |
| FILE  | 4 |

---

## 项目结构

```
qqbot-go/
├── qqbot.go               # 顶层入口 (QuickStart)
├── connector/
│   ├── connector.go       # 扫码绑定 + CredentialStore
│   └── crypto.go          # AES-256-GCM 解密
├── channel/
│   └── channel.go         # 消息通道抽象 (Message/Handler)
├── auth/
│   └── token.go           # Token 管理 (获取/缓存/自动刷新)
├── gateway/
│   ├── gateway.go         # WebSocket 网关 (Session 持久化)
│   └── connect.go         # 连接管理 (心跳/重连/Dispatch 分发)
├── api/
│   ├── client.go          # REST API (发消息/交互回复)
│   ├── media.go           # 媒体上传 (简单上传 + 分片并发上传)
│   └── stream.go          # 流式消息协议 (C2C 打字机效果)
├── internal/
│   ├── httputil/client.go # 共享 HTTP 客户端
│   ├── qrterm/qrterm.go  # 终端二维码渲染
│   └── audio/convert.go   # 语音转码 (SILK v3 编码器)
├── types/
│   ├── const.go           # 常量 (Intents/媒体类型/消息类型)
│   ├── events.go          # 事件结构体定义
│   └── ws.go              # WebSocket 帧结构
├── cmd/
│   └── example/
│       └── main.go        # 示例 Echo Bot
├── DESIGN.md              # 协议设计文档
└── README.md              # 本文档
```

---

## 错误处理

SDK 不会吞掉任何错误，所有错误均返回给调用方，包括：

- 凭据文件读写失败
- 扫码绑定失败
- WebSocket 连接失败
- 消息发送失败
- Token 刷新失败
- 网络错误

---

## 依赖

| 依赖 | 用途 |
|------|------|
| `github.com/gorilla/websocket` | WebSocket 客户端 |
| `ffmpeg` (系统依赖) | 语音格式转换（发送语音消息时需要） |

---

## 免责声明

本项目代码由 AI 辅助生成，协议细节基于[tencent-connect/openclaw-qqbot](https://github.com/tencent-connect/openclaw-qqbot) 而来。

- 本项目与腾讯公司无任何关联，非官方 SDK
- 使用本项目所产生的一切后果由使用者自行承担
- 本项目不保证协议接口的稳定性和兼容性，腾讯可能随时变更接口
- 请遵守 QQ 机器人开放平台的使用条款和服务协议
- 不得将本项目用于任何违反法律法规的用途

---

## 参考项目

- [tencent-connect/openclaw-qqbot](https://github.com/tencent-connect/openclaw-qqbot) - 协议参考，TypeScript 原版实现
- [QQ 机器人开放平台](https://q.qq.com) - 官方文档

## 协议

MIT
