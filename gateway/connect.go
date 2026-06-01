package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/libaibaia/qqbot-go/internal/httputil"
	"github.com/libaibaia/qqbot-go/types"
	"github.com/gorilla/websocket"
)

type wsConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

func (w *wsConn) SendJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("connection closed")
	}
	return w.conn.WriteJSON(v)
}

func (w *wsConn) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.conn.Close()
	}
}

type gatewayURLResponse struct {
	URL string `json:"url"`
}

func (g *Gateway) fetchGatewayURL(ctx context.Context) (string, error) {
	var resp gatewayURLResponse
	err := httputil.DoJSON(ctx, &http.Client{Timeout: 10 * time.Second},
		"GET", httputil.BaseURL+"/gateway",
		map[string]string{"Authorization": "QQBot " + g.token},
		nil, &resp)
	if err != nil {
		return "", err
	}
	if resp.URL == "" {
		return "", fmt.Errorf("empty gateway URL")
	}
	return resp.URL, nil
}

func (g *Gateway) connectWithRetry(ctx context.Context) error {
	delays := []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	attempt := 0
	quickDisconnects := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.done:
			return nil
		default:
		}

		if attempt >= 100 {
			return fmt.Errorf("max reconnect attempts reached")
		}

		err := g.connectOnce(ctx)
		if err == nil {
			// Normal close
			if g.onClose != nil {
				g.onClose(nil)
			}
			return nil
		}

		g.log.Warn("gateway connection lost", "error", err, "attempt", attempt+1)

		// Quick disconnect detection
		// (simplified: we just check attempt count)
		if attempt > 0 && attempt%3 == 0 {
			quickDisconnects++
			if quickDisconnects >= 3 {
				g.log.Warn("quick disconnects detected, waiting 60s")
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-g.done:
					return nil
				case <-time.After(60 * time.Second):
				}
				quickDisconnects = 0
			}
		}

		delayIdx := attempt
		if delayIdx >= len(delays) {
			delayIdx = len(delays) - 1
		}
		delay := delays[delayIdx]

		g.log.Info("reconnecting", "delay", delay, "attempt", attempt+1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.done:
			return nil
		case <-time.After(delay):
		}

		attempt++
	}
}

func (g *Gateway) connectOnce(ctx context.Context) error {
	// Fetch gateway URL
	gwURL, err := g.fetchGatewayURL(ctx)
	if err != nil {
		return fmt.Errorf("fetch gateway: %w", err)
	}

	// Connect WebSocket
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, gwURL, http.Header{
		"User-Agent": []string{httputil.UserAgent},
	})
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}
	g.log.Info("WebSocket 已连接，等待 Hello...")

	ws := &wsConn{conn: conn}
	g.mu.Lock()
	g.ws = ws
	g.mu.Unlock()

	defer func() {
		ws.Close()
		g.mu.Lock()
		g.ws = nil
		g.mu.Unlock()
	}()

	// Read loop
	for {
		select {
		case <-g.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, 1000) {
				return nil // Normal close
			}
			return fmt.Errorf("read message: %w", err)
		}

		var payload types.WSPayload
		if err := json.Unmarshal(message, &payload); err != nil {
			g.log.Warn("failed to parse ws payload", "error", err)
			continue
		}

		if err := g.handlePayload(ctx, ws, &payload); err != nil {
			return err
		}
	}
}

func (g *Gateway) handlePayload(ctx context.Context, ws *wsConn, payload *types.WSPayload) error {
	switch payload.Op {
	case 10: // Hello
		return g.handleHello(ctx, ws, payload)
	case 0: // Dispatch
		return g.handleDispatch(payload)
	case 1: // Heartbeat ACK
		// nothing
	case 7: // Reconnect
		g.log.Info("server requested reconnect")
		return fmt.Errorf("server requested reconnect")
	case 9: // Invalid Session
		var canResume bool
		json.Unmarshal(payload.D, &canResume)
		g.log.Warn("invalid session", "canResume", canResume)
		if !canResume {
			g.sessionStore.Clear(g.appID)
			g.sessionID = ""
			g.lastSeq = 0
		}
		time.Sleep(3 * time.Second)
		return fmt.Errorf("invalid session")
	case 11: // Heartbeat ACK
		// nothing
	}
	return nil
}

func (g *Gateway) handleHello(ctx context.Context, ws *wsConn, payload *types.WSPayload) error {
	var hello types.WSHello
	if err := json.Unmarshal(payload.D, &hello); err != nil {
		return fmt.Errorf("parse hello: %w", err)
	}

	if hello.HeartbeatInterval <= 0 {
		hello.HeartbeatInterval = 41250
	}

	// Try to resume or identify
	saved := g.sessionStore.Load(g.appID)
	if saved != nil && saved.SessionID != "" {
		g.log.Info("resuming session", "sessionID", saved.SessionID)
		resume := types.WSResume{
			Token:     "QQBot " + g.token,
			SessionID: saved.SessionID,
			Seq:       saved.LastSeq,
		}
		if err := ws.SendJSON(map[string]any{"op": 6, "d": resume}); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
		g.sessionID = saved.SessionID
		g.lastSeq = saved.LastSeq
	} else {
		g.log.Info("identifying")
		identify := types.WSIdentify{
			Token:   "QQBot " + g.token,
			Intents: g.intents,
			Shard:   [2]int{0, 1},
		}
		if err := ws.SendJSON(map[string]any{"op": 2, "d": identify}); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
	}

	// Start heartbeat
	go g.heartbeat(ctx, ws, time.Duration(hello.HeartbeatInterval)*time.Millisecond)

	return nil
}

func (g *Gateway) heartbeat(ctx context.Context, ws *wsConn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.done:
			return
		case <-ticker.C:
			seq := g.lastSeq
			if err := ws.SendJSON(map[string]any{"op": 1, "d": seq}); err != nil {
				g.log.Warn("heartbeat failed", "error", err)
				return
			}
		}
	}
}

func (g *Gateway) handleDispatch(payload *types.WSPayload) error {
	if payload.S != nil {
		g.lastSeq = *payload.S
		g.sessionStore.UpdateSeq(g.appID, g.lastSeq)
	}

	switch payload.T {
	case "READY":
		var ready types.ReadyEvent
		if err := json.Unmarshal(payload.D, &ready); err == nil && ready.SessionID != "" {
			g.sessionID = ready.SessionID
			g.sessionStore.Save(g.appID, &SessionState{
				SessionID: ready.SessionID,
				LastSeq:   g.lastSeq,
				AppID:     g.appID,
			})
			g.log.Info("gateway ready", "sessionID", ready.SessionID)
			if g.onReady != nil {
				g.onReady(ready.SessionID)
			}
		}
	case "RESUMED":
		g.log.Info("session resumed")
	default:
		if g.handler != nil {
			event := &types.DispatchEvent{
				Name: payload.T,
				Data: payload.D,
			}
			if payload.S != nil {
				event.Seq = *payload.S
			}
			g.handler(event)
		}
	}
	return nil
}
