package types

import "encoding/json"

// WSPayload is the raw WebSocket frame from the gateway.
type WSPayload struct {
	Op int              `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int             `json:"s,omitempty"`
	T  string           `json:"t,omitempty"`
}

// WSHello is the op=10 payload from the server.
type WSHello struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// WSIdentify is the op=2 payload sent by the client.
type WSIdentify struct {
	Token   string `json:"token"`
	Intents int    `json:"intents"`
	Shard   [2]int `json:"shard"`
}

// WSResume is the op=6 payload sent by the client.
type WSResume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int    `json:"seq"`
}

// DispatchEvent is a parsed op=0 event.
type DispatchEvent struct {
	Name     string          `json:"t"`
	Seq      int             `json:"s"`
	Data     json.RawMessage `json:"d"`
}

// ReadyEvent is the t=READY event data.
type ReadyEvent struct {
	SessionID string `json:"session_id"`
}
