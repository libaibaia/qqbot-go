package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/libaibaia/qqbot-go/api"
	"github.com/libaibaia/qqbot-go/auth"
	"github.com/libaibaia/qqbot-go/gateway"
	"github.com/libaibaia/qqbot-go/internal/audio"
	"github.com/libaibaia/qqbot-go/types"
)

// Message is the unified message type passed to the user's handler.
type Message struct {
	ID        string
	Type      string // "c2c", "group", "channel", "dm"
	Content   string
	Author    Author
	Target    string // openid / group_openid / channel_id
	Timestamp string

	// Raw attachments
	Attachments []types.MessageAttachment

	// Internal fields for reply
	client      *api.Client
	msgID       string // original message ID for reply
	channelType string // "c2c" or "group"
}

// Author holds message sender info.
type Author struct {
	ID       string
	Name     string
	OpenID   string
	IsBot    bool
}

// Reply sends a plain text reply to this message.
func (m *Message) Reply(content string) error {
	msg := &api.Message{Content: content, MsgID: m.msgID}
	return m.sendMsg(msg)
}

// ReplyMarkdown sends a markdown reply.
func (m *Message) ReplyMarkdown(content string) error {
	msg := &api.Message{Markdown: content, MsgID: m.msgID}
	return m.sendMsg(msg)
}

// ReplyMedia sends a media reply.
func (m *Message) ReplyMedia(fileInfo string) error {
	msg := &api.Message{
		MsgType: types.MsgTypeMedia,
		Media:   &api.MediaRef{FileInfo: fileInfo},
		MsgID:   m.msgID,
	}
	return m.sendMsg(msg)
}

// HasAttachments returns true if the message has any attachments.
func (m *Message) HasAttachments() bool {
	return len(m.Attachments) > 0
}

// Images returns all image attachments (content_type starts with "image/").
func (m *Message) Images() []string {
	var urls []string
	for _, a := range m.Attachments {
		if strings.HasPrefix(a.ContentType, "image/") {
			urls = append(urls, a.URL)
		}
	}
	return urls
}

// Voices returns all voice attachments (content_type starts with "audio/").
// Each entry is the WAV direct link if available, otherwise the original URL.
func (m *Message) Voices() []string {
	var urls []string
	for _, a := range m.Attachments {
		if strings.HasPrefix(a.ContentType, "audio/") {
			if a.VoiceWavURL != "" {
				urls = append(urls, a.VoiceWavURL)
			} else {
				urls = append(urls, a.URL)
			}
		}
	}
	return urls
}

// VoiceText returns the ASR (speech-to-text) result from QQ's built-in recognition.
// Returns empty string if no ASR result is available.
func (m *Message) VoiceText() string {
	for _, a := range m.Attachments {
		if a.ASRText != "" {
			return a.ASRText
		}
	}
	return ""
}

// UploadAndReplyImage uploads an image and replies with it.
// url: remote image URL; data: raw image bytes (use one of them).
func (m *Message) UploadAndReplyImage(url string, data []byte) error {
	return m.uploadAndReply(api.MediaTypeImage, url, data, "")
}

// UploadAndReplyVoice uploads a voice file and replies with it.
// Automatically converts WAV/MP3/OGG/FLAC to SILK format.
// Requires ffmpeg to be installed for non-SILK input formats.
func (m *Message) UploadAndReplyVoice(url string, data []byte) error {
	// If data provided, auto-convert to SILK
	if len(data) > 0 {
		silk, err := audio.ToSilk(data)
		if err != nil {
			return fmt.Errorf("convert to silk: %w", err)
		}
		data = silk
		url = "" // use converted data
	}
	return m.uploadAndReply(api.MediaTypeVoice, url, data, "")
}

// UploadAndReplyFile uploads a file and replies with it.
func (m *Message) UploadAndReplyFile(url string, data []byte, filename string) error {
	return m.uploadAndReply(api.MediaTypeFile, url, data, filename)
}

func (m *Message) uploadAndReply(fileType api.MediaFileType, url string, data []byte, filename string) error {
	if m.client == nil {
		return fmt.Errorf("api client not available")
	}

	ctx := context.Background()

	// 根据消息类型选择上传 scope
	scope := "users"
	target := m.Target
	switch m.channelType {
	case "group":
		scope = "groups"
	case "channel", "dm":
		// 频道/频道私信走 guild 上传，目前用 c2c 降级处理
		// QQ 开放平台对频道媒体上传接口不同，后续可扩展
		scope = "users"
	}

	media, err := m.client.UploadMedia(ctx, scope, target, &api.MediaFile{
		FileType: fileType,
		URL:      url,
		Data:     data,
		FileName: filename,
	})
	if err != nil {
		return fmt.Errorf("upload media: %w", err)
	}

	return m.ReplyMedia(media.FileInfo)
}

// StartStream starts a streaming message session (C2C only).
func (m *Message) StartStream() (*api.StreamSession, error) {
	if m.Type != "c2c" {
		return nil, fmt.Errorf("streaming is only supported for C2C messages")
	}
	return m.client.NewStreamSession(m.Target, m.msgID), nil
}

func (m *Message) sendMsg(msg *api.Message) error {
	ctx := context.Background()
	switch m.channelType {
	case "c2c":
		_, err := m.client.SendC2CMessage(ctx, m.Target, msg)
		return err
	case "group":
		_, err := m.client.SendGroupMessage(ctx, m.Target, msg)
		return err
	default:
		return fmt.Errorf("reply not supported for channel type: %s", m.channelType)
	}
}

// Handler is the user's message callback.
type Handler func(msg *Message)

// Config configures a Channel.
type Config struct {
	AppID     string
	AppSecret string
	Intents   int
	Handler   Handler
	Log       *slog.Logger
}

// Channel is the unified message channel.
type Channel struct {
	cfg       Config
	tokenMgr  *auth.TokenManager
	apiClient *api.Client
	gw        *gateway.Gateway
	log       *slog.Logger
}

// New creates a new Channel.
func New(cfg Config) *Channel {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Channel{cfg: cfg, log: cfg.Log}
}

// Connect starts the channel: authenticates and connects to the gateway.
func (ch *Channel) Connect(ctx context.Context) error {
	ch.tokenMgr = auth.NewTokenManager(auth.Credentials{
		AppID:     ch.cfg.AppID,
		AppSecret: ch.cfg.AppSecret,
	})
	ch.tokenMgr.StartAutoRefresh(ctx)

	ch.apiClient = api.NewClient(ch.tokenMgr)

	// Get initial token for gateway
	token, err := ch.tokenMgr.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	ch.log.Info("token 获取成功，正在连接网关...")

	ch.gw = gateway.New(gateway.Config{
		Token:   token,
		AppID:   ch.cfg.AppID,
		Intents: ch.cfg.Intents,
		Handler: ch.makeDispatchHandler(),
		OnReady: func(sessionID string) {
			ch.log.Info("channel connected", "sessionID", sessionID)
		},
		Log: ch.log,
	})

	return ch.gw.Connect(ctx)
}

// Close shuts down the channel.
func (ch *Channel) Close() {
	if ch.gw != nil {
		ch.gw.Close()
	}
	if ch.tokenMgr != nil {
		ch.tokenMgr.StopAutoRefresh()
	}
}

func (ch *Channel) makeDispatchHandler() gateway.DispatchHandler {
	return func(event *types.DispatchEvent) {
		if ch.cfg.Handler == nil {
			return
		}

		switch event.Name {
		case "C2C_MESSAGE_CREATE":
			var evt types.C2CMessageEvent
			if err := json.Unmarshal(event.Data, &evt); err != nil {
				ch.log.Warn("parse C2C message", "error", err)
				return
			}
			msg := &Message{
				ID:        evt.ID,
				Type:      "c2c",
				Content:   evt.Content,
				Target:    evt.Author.UserOpenID,
				Timestamp: evt.Timestamp,
				Author: Author{
					ID:     evt.Author.ID,
					OpenID: evt.Author.UserOpenID,
				},
				Attachments: evt.Attachments,
				client:      ch.apiClient,
				msgID:       evt.ID,
				channelType: "c2c",
			}
			ch.cfg.Handler(msg)

		case "GROUP_AT_MESSAGE_CREATE", "GROUP_MESSAGE_CREATE":
			var evt types.GroupMessageEvent
			if err := json.Unmarshal(event.Data, &evt); err != nil {
				ch.log.Warn("parse group message", "error", err)
				return
			}
			name := evt.Author.Username
			if name == "" {
				name = evt.Author.MemberOpenID
			}
			msg := &Message{
				ID:        evt.ID,
				Type:      "group",
				Content:   evt.Content,
				Target:    evt.GroupOpenID,
				Timestamp: evt.Timestamp,
				Author: Author{
					ID:     evt.Author.ID,
					Name:   name,
					OpenID: evt.Author.MemberOpenID,
					IsBot:  evt.Author.Bot,
				},
				Attachments: evt.Attachments,
				client:      ch.apiClient,
				msgID:       evt.ID,
				channelType: "group",
			}
			ch.cfg.Handler(msg)

		case "AT_MESSAGE_CREATE":
			var evt types.GuildMessageEvent
			if err := json.Unmarshal(event.Data, &evt); err != nil {
				ch.log.Warn("parse guild message", "error", err)
				return
			}
			msg := &Message{
				ID:        evt.ID,
				Type:      "channel",
				Content:   evt.Content,
				Target:    evt.ChannelID,
				Timestamp: evt.Timestamp,
				Author: Author{
					ID:     evt.Author.ID,
					Name:   evt.Author.Username,
					IsBot:  evt.Author.Bot,
				},
				Attachments: evt.Attachments,
				client:      ch.apiClient,
				msgID:       evt.ID,
				channelType: "channel",
			}
			ch.cfg.Handler(msg)

		case "DIRECT_MESSAGE_CREATE":
			var evt types.GuildMessageEvent
			if err := json.Unmarshal(event.Data, &evt); err != nil {
				ch.log.Warn("parse DM message", "error", err)
				return
			}
			msg := &Message{
				ID:        evt.ID,
				Type:      "dm",
				Content:   evt.Content,
				Target:    evt.GuildID,
				Timestamp: evt.Timestamp,
				Author: Author{
					ID:     evt.Author.ID,
					Name:   evt.Author.Username,
					IsBot:  evt.Author.Bot,
				},
				Attachments: evt.Attachments,
				client:      ch.apiClient,
				msgID:       evt.ID,
				channelType: "dm",
			}
			ch.cfg.Handler(msg)
		}
	}
}
