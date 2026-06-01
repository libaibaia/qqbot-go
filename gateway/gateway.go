package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/libaibaia/qqbot-go/types"
)

const sessionExpireTime = 5 * time.Minute

// SessionState holds the gateway session state for resumption.
type SessionState struct {
	SessionID string    `json:"session_id"`
	LastSeq   int       `json:"last_seq"`
	ExpiresAt time.Time `json:"expires_at"`
	AppID     string    `json:"app_id"`
}

// SessionStore manages session persistence.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState // key: appID
}

// NewSessionStore creates a new in-memory session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*SessionState),
	}
}

// Save persists the session state.
func (s *SessionStore) Save(appID string, state *SessionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.ExpiresAt = time.Now().Add(sessionExpireTime)
	s.sessions[appID] = state
}

// Load retrieves the session state if valid.
func (s *SessionStore) Load(appID string) *SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.sessions[appID]
	if !ok {
		return nil
	}
	if time.Now().After(st.ExpiresAt) {
		return nil
	}
	return st
}

// Clear removes the session state.
func (s *SessionStore) Clear(appID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, appID)
}

// UpdateSeq updates the last sequence number.
func (s *SessionStore) UpdateSeq(appID string, seq int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.sessions[appID]; ok {
		st.LastSeq = seq
	}
}

// Gateway implements the QQ Bot WebSocket gateway client.
type Gateway struct {
	token     string
	appID     string
	intents   int
	handler   DispatchHandler
	onReady   func(sessionID string)
	onClose   func(err error)
	log       *slog.Logger

	sessionStore *SessionStore
	sessionID    string
	lastSeq      int

	ws     *wsConn
	mu     sync.Mutex
	done   chan struct{}
	closed bool
}

// DispatchHandler is called for each dispatch event.
type DispatchHandler func(event *types.DispatchEvent)

// Config holds gateway configuration.
type Config struct {
	Token    string
	AppID    string
	Intents  int
	Handler  DispatchHandler
	OnReady  func(sessionID string)
	OnClose  func(err error)
	Log      *slog.Logger
}

// New creates a new Gateway.
func New(cfg Config) *Gateway {
	if cfg.Intents == 0 {
		cfg.Intents = types.DefaultIntents
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Gateway{
		token:        cfg.Token,
		appID:        cfg.AppID,
		intents:      cfg.Intents,
		handler:      cfg.Handler,
		onReady:      cfg.OnReady,
		onClose:      cfg.OnClose,
		log:          cfg.Log,
		sessionStore: NewSessionStore(),
		done:         make(chan struct{}),
	}
}

// Connect establishes a WebSocket connection and starts the event loop.
func (g *Gateway) Connect(ctx context.Context) error {
	return g.connectWithRetry(ctx)
}

// Close gracefully shuts down the gateway.
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	close(g.done)
	if g.ws != nil {
		g.ws.Close()
	}
}
