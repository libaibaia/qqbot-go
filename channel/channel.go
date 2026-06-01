package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/libaibaia/qqbot-go/api"
	"github.com/libaibaia/qqbot-go/auth"
	"github.com/libaibaia/qqbot-go/gateway"
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
	return &Channel{cfg: cfg}
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
