package api

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/libaibaia/qqbot-go/auth"
	"github.com/libaibaia/qqbot-go/internal/httputil"
	"github.com/libaibaia/qqbot-go/types"
)

// Message holds fields for sending a message.
type Message struct {
	Content      string // Plain text content
	Markdown     string // Markdown content (msg_type=2)
	MsgType      int    // 0=text, 2=markdown, 7=media
	MsgID        string // Reply-to message ID
	MsgSeq       int    // Sequence number
	Media        *MediaRef
	Keyboard     *Keyboard
	StreamMsgID  string // For streaming
	StreamState  int    // 1=generating, 10=done
	StreamIndex  int
}

// MediaRef is a reference to uploaded media.
type MediaRef struct {
	FileInfo string `json:"file_info"`
}

// Keyboard is an inline keyboard.
type Keyboard struct {
	Rows []KeyboardRow `json:"rows"`
}

type KeyboardRow struct {
	Buttons []KeyboardButton `json:"buttons"`
}

type KeyboardButton struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// MessageResponse is the API response for message sending.
type MessageResponse struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
}

// MediaInfo is the result of a media upload.
type MediaInfo struct {
	FileUUID string `json:"file_uuid"`
	FileInfo string `json:"file_info"`
	TTL      int    `json:"ttl"`
}

// Client is the QQ Bot REST API client.
type Client struct {
	tokenMgr *auth.TokenManager
	client   *http.Client
}

// NewClient creates a new API client.
func NewClient(tokenMgr *auth.TokenManager) *Client {
	return &Client{
		tokenMgr: tokenMgr,
		client:   &http.Client{Timeout: httputil.DefaultTimeout},
	}
}

func (c *Client) authHeader(ctx context.Context) (string, error) {
	token, err := c.tokenMgr.GetToken(ctx)
	if err != nil {
		return "", err
	}
	return "QQBot " + token, nil
}

func genMsgSeq() int {
	return int(time.Now().UnixMilli()%100000000) ^ rand.Intn(65536) % 65536
}

// SendC2CMessage sends a message in a C2C (private) chat.
func (c *Client) SendC2CMessage(ctx context.Context, openid string, msg *Message) (*MessageResponse, error) {
	path := fmt.Sprintf("/v2/users/%s/messages", openid)
	return c.sendV2Message(ctx, path, msg)
}

// SendGroupMessage sends a message in a group chat.
func (c *Client) SendGroupMessage(ctx context.Context, groupOpenid string, msg *Message) (*MessageResponse, error) {
	path := fmt.Sprintf("/v2/groups/%s/messages", groupOpenid)
	return c.sendV2Message(ctx, path, msg)
}

// SendChannelMessage sends a message in a guild channel.
func (c *Client) SendChannelMessage(ctx context.Context, channelID string, content string, replyMsgID string) (*MessageResponse, error) {
	path := fmt.Sprintf("/channels/%s/messages", channelID)
	auth, err := c.authHeader(ctx)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"content": content,
	}
	if replyMsgID != "" {
		body["msg_id"] = replyMsgID
	}

	var resp MessageResponse
	err = httputil.DoJSON(ctx, c.client, "POST",
		httputil.BaseURL+path,
		map[string]string{"Authorization": auth},
		body, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendGuildDM sends a direct message in a guild.
func (c *Client) SendGuildDM(ctx context.Context, guildID string, content string, replyMsgID string) (*MessageResponse, error) {
	path := fmt.Sprintf("/dms/%s/messages", guildID)
	auth, err := c.authHeader(ctx)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"content": content,
	}
	if replyMsgID != "" {
		body["msg_id"] = replyMsgID
	}

	var resp MessageResponse
	err = httputil.DoJSON(ctx, c.client, "POST",
		httputil.BaseURL+path,
		map[string]string{"Authorization": auth},
		body, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) sendV2Message(ctx context.Context, path string, msg *Message) (*MessageResponse, error) {
	auth, err := c.authHeader(ctx)
	if err != nil {
		return nil, err
	}

	body := make(map[string]any)

	if msg.MsgType == types.MsgTypeMedia && msg.Media != nil {
		body["msg_type"] = types.MsgTypeMedia
		body["media"] = map[string]string{"file_info": msg.Media.FileInfo}
	} else if msg.Markdown != "" {
		body["msg_type"] = types.MsgTypeMD
		body["markdown"] = map[string]string{"content": msg.Markdown}
	} else {
		body["msg_type"] = types.MsgTypeText
		body["content"] = msg.Content
	}

	if msg.MsgID != "" {
		body["msg_id"] = msg.MsgID
	}

	seq := msg.MsgSeq
	if seq == 0 {
		seq = genMsgSeq()
	}
	body["msg_seq"] = seq

	if msg.Keyboard != nil {
		body["keyboard"] = msg.Keyboard
	}

	var resp MessageResponse
	err = httputil.DoJSON(ctx, c.client, "POST",
		httputil.BaseURL+path,
		map[string]string{"Authorization": auth},
		body, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// AckInteraction acknowledges a button interaction.
func (c *Client) AckInteraction(ctx context.Context, interactionID string, code int) error {
	auth, err := c.authHeader(ctx)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/interactions/%s", interactionID)
	body := map[string]any{"code": code}

	return httputil.DoJSON(ctx, c.client, "PUT",
		httputil.BaseURL+path,
		map[string]string{"Authorization": auth},
		body, nil)
}
