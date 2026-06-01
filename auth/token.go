package auth

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/libaibaia/qqbot-go/internal/httputil"
)

// Credentials holds the bot's application credentials.
type Credentials struct {
	AppID     string
	AppSecret string
}

type tokenEntry struct {
	token     string
	expiresAt time.Time
}

// TokenManager handles access token acquisition, caching, and refresh.
type TokenManager struct {
	creds    Credentials
	mu       sync.RWMutex
	cache    tokenEntry
	fetching chan struct{} // singleflight gate
	fetchMu  sync.Mutex
	client   *http.Client

	cancelAuto context.CancelFunc
}

// NewTokenManager creates a new token manager.
func NewTokenManager(creds Credentials) *TokenManager {
	return &TokenManager{
		creds:    creds,
		fetching: make(chan struct{}, 1),
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// GetToken returns a valid access token, refreshing if necessary.
func (tm *TokenManager) GetToken(ctx context.Context) (string, error) {
	// Fast path: check cache with read lock
	tm.mu.RLock()
	if tm.cache.token != "" && time.Now().Before(tm.cache.expiresAt) {
		t := tm.cache.token
		tm.mu.RUnlock()
		return t, nil
	}
	tm.mu.RUnlock()

	// Slow path: need to refresh (singleflight)
	tm.fetchMu.Lock()
	defer tm.fetchMu.Unlock()

	// Double-check after acquiring lock
	tm.mu.RLock()
	if tm.cache.token != "" && time.Now().Before(tm.cache.expiresAt) {
		t := tm.cache.token
		tm.mu.RUnlock()
		return t, nil
	}
	tm.mu.RUnlock()

	return tm.fetchToken(ctx)
}

// Invalidate clears the cached token, forcing a refresh on next GetToken.
func (tm *TokenManager) Invalidate() {
	tm.mu.Lock()
	tm.cache = tokenEntry{}
	tm.mu.Unlock()
}

// StartAutoRefresh starts a background goroutine that refreshes the token before expiry.
func (tm *TokenManager) StartAutoRefresh(ctx context.Context) {
	if tm.cancelAuto != nil {
		tm.cancelAuto()
	}
	ctx, tm.cancelAuto = context.WithCancel(ctx)

	go func() {
		for {
			// Get token to trigger initial fetch
			token, err := tm.GetToken(ctx)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			// Calculate refresh time: 2/3 of TTL minus random jitter
			tm.mu.RLock()
			ttl := time.Until(tm.cache.expiresAt)
			tm.mu.RUnlock()

			refreshAhead := ttl / 3
			if refreshAhead > 5*time.Minute {
				refreshAhead = 5 * time.Minute
			}
			jitter := time.Duration(rand.Intn(30)) * time.Second
			wait := ttl - refreshAhead - jitter
			if wait < 60*time.Second {
				wait = 60 * time.Second
			}

			_ = token

			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
				tm.Invalidate()
			}
		}
	}()
}

// StopAutoRefresh stops the background token refresh.
func (tm *TokenManager) StopAutoRefresh() {
	if tm.cancelAuto != nil {
		tm.cancelAuto()
		tm.cancelAuto = nil
	}
}

func (tm *TokenManager) fetchToken(ctx context.Context) (string, error) {
	reqBody := map[string]string{
		"appId":       tm.creds.AppID,
		"clientSecret": tm.creds.AppSecret,
	}

	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   any    `json:"expires_in"` // 可能是 int 或 string
	}

	err := httputil.DoJSON(ctx, tm.client, "POST", httputil.TokenEndpoint, nil, reqBody, &resp)
	if err != nil {
		return "", fmt.Errorf("fetch access token: %w", err)
	}

	if resp.AccessToken == "" {
		return "", fmt.Errorf("fetch access token: empty token in response")
	}

	expiresIn := 7200
	switch v := resp.ExpiresIn.(type) {
	case float64:
		expiresIn = int(v)
	case string:
		fmt.Sscanf(v, "%d", &expiresIn)
	}

	tm.mu.Lock()
	tm.cache = tokenEntry{
		token:     resp.AccessToken,
		expiresAt: time.Now().Add(time.Duration(expiresIn) * time.Second),
	}
	tm.mu.Unlock()

	return resp.AccessToken, nil
}
