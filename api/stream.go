package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync/atomic"

	"github.com/libaibaia/qqbot-go/internal/httputil"
	"github.com/libaibaia/qqbot-go/types"
)

func encodeBase64Std(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// StreamSession manages a streaming message session (C2C only).
type StreamSession struct {
	client     *Client
	openID     string
	streamID   string
	msgSeq     int
	index      atomic.Int64
	eventID    string
}

// NewStreamSession creates a new streaming session for a C2C chat.
func (c *Client) NewStreamSession(openID, eventID string) *StreamSession {
	return &StreamSession{
		client:  c,
		openID:  openID,
		msgSeq:  genMsgSeq(),
		eventID: eventID,
	}
}

// Send sends a streaming message chunk. Set done=true for the final chunk.
func (ss *StreamSession) Send(ctx context.Context, content string, done bool) error {
	auth, err := ss.client.authHeader(ctx)
	if err != nil {
		return err
	}

	state := types.StreamStateGenerating
	if done {
		state = types.StreamStateDone
	}

	idx := int(ss.index.Add(1) - 1)

	body := map[string]any{
		"input_mode":    "replace",
		"input_state":   state,
		"content_type":  "markdown",
		"content_raw":   content,
		"event_id":      ss.eventID,
		"msg_id":        ss.eventID,
		"msg_seq":       ss.msgSeq,
		"index":         idx,
	}
	if ss.streamID != "" {
		body["stream_msg_id"] = ss.streamID
	}

	path := fmt.Sprintf("/v2/users/%s/stream_messages", ss.openID)

	var resp struct {
		ID string `json:"id"`
	}
	err = httputil.DoJSON(ctx, ss.client.client, "POST",
		httputil.BaseURL+path,
		map[string]string{"Authorization": auth},
		body, &resp)
	if err != nil {
		return err
	}

	if ss.streamID == "" && resp.ID != "" {
		ss.streamID = resp.ID
	}

	return nil
}
