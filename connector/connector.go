package connector

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/libaibaia/qqbot-go/internal/httputil"
)

const (
	apiHost        = "https://q.qq.com"
	qrBaseURL      = "https://q.qq.com/qqbot/openclaw/connect.html"
	pollInterval   = 2 * time.Second
	requestTimeout = 10 * time.Second
)

// BindStatus represents the QR scan status.
type BindStatus int

const (
	StatusNone      BindStatus = 0
	StatusPending   BindStatus = 1
	StatusCompleted BindStatus = 2
	StatusExpired   BindStatus = 3
)

// Credentials holds the bot credentials obtained via QR binding.
type Credentials struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// CredentialStore is the interface for persisting credentials.
type CredentialStore interface {
	Load() (*Credentials, error)
	Save(creds *Credentials) error
}

// FileStore persists credentials as JSON to a local file.
type FileStore struct {
	Path string
}

// NewFileStore creates a file-based credential store.
func NewFileStore(path string) *FileStore {
	return &FileStore{Path: path}
}

func (fs *FileStore) Load() (*Credentials, error) {
	data, err := os.ReadFile(fs.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.AppID == "" || creds.AppSecret == "" {
		return nil, nil
	}
	return &creds, nil
}

func (fs *FileStore) Save(creds *Credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fs.Path, data, 0o600)
}

// ConnectOptions configures the QR connect flow.
type ConnectOptions struct {
	Source   string              // Platform identifier (empty = "第三方机器人")
	Ctx     context.Context     // Context for cancellation
	OnQrURL func(url string)    // Called with QR code URL - your platform renders it
	OnStatus func(status BindStatus, msg string)
}

// Connect performs the full QR binding flow.
func Connect(opts *ConnectOptions) (*Credentials, error) {
	if opts == nil {
		opts = &ConnectOptions{}
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	client := &http.Client{Timeout: requestTimeout}

	for {
		keyBytes := make([]byte, 32)
		if _, err := rand.Read(keyBytes); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		keyBase64 := base64.StdEncoding.EncodeToString(keyBytes)

		taskID, err := createBindTask(ctx, client, keyBase64)
		if err != nil {
			return nil, fmt.Errorf("create bind task: %w", err)
		}

		qrURL := fmt.Sprintf("%s?task_id=%s&source=%s&_wv=2", qrBaseURL, taskID, opts.Source)
		if opts.OnQrURL != nil {
			opts.OnQrURL(qrURL)
		}

		creds, expired, err := pollForResult(ctx, client, taskID, keyBase64, opts.OnStatus)
		if err != nil {
			return nil, err
		}
		if expired {
			if opts.OnStatus != nil {
				opts.OnStatus(StatusExpired, "二维码已过期，正在刷新...")
			}
			continue
		}

		return creds, nil
	}
}

// LoadOrConnect loads credentials from store, or performs QR binding and saves.
func LoadOrConnect(opts *ConnectOptions, store CredentialStore) (*Credentials, error) {
	// Try loading from store
	creds, err := store.Load()
	if err == nil && creds != nil {
		return creds, nil
	}

	// Need to scan
	creds, err = Connect(opts)
	if err != nil {
		return nil, err
	}

	if err := store.Save(creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}

	return creds, nil
}

func createBindTask(ctx context.Context, client *http.Client, keyBase64 string) (string, error) {
	body := map[string]string{"key": keyBase64}

	var resp struct {
		RetCode int    `json:"retcode"`
		Msg     string `json:"msg"`
		Data    struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}

	err := httputil.DoJSON(ctx, client, "POST", apiHost+"/lite/create_bind_task",
		nil, body, &resp)
	if err != nil {
		return "", err
	}
	if resp.RetCode != 0 {
		return "", fmt.Errorf("create_bind_task failed: retcode=%d msg=%s", resp.RetCode, resp.Msg)
	}
	if resp.Data.TaskID == "" {
		return "", fmt.Errorf("create_bind_task: empty task_id")
	}
	return resp.Data.TaskID, nil
}

func pollForResult(ctx context.Context, client *http.Client, taskID, keyBase64 string,
	onStatus func(BindStatus, string)) (*Credentials, bool, error) {

	body := map[string]string{"task_id": taskID}

	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}

		var resp struct {
			RetCode int    `json:"retcode"`
			Msg     string `json:"msg"`
			Data    struct {
				Status           BindStatus `json:"status"`
				BotAppID         string     `json:"bot_appid"`
				BotEncryptSecret string     `json:"bot_encrypt_secret"`
			} `json:"data"`
		}

		err := httputil.DoJSON(ctx, client, "POST", apiHost+"/lite/poll_bind_result",
			nil, body, &resp)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(pollInterval):
			}
			continue
		}

		if resp.RetCode != 0 {
			return nil, false, fmt.Errorf("poll_bind_result failed: retcode=%d msg=%s", resp.RetCode, resp.Msg)
		}

		switch resp.Data.Status {
		case StatusNone:
			if onStatus != nil {
				onStatus(StatusNone, "等待扫码...")
			}
		case StatusPending:
			if onStatus != nil {
				onStatus(StatusPending, "已扫码，等待确认...")
			}
		case StatusCompleted:
			appSecret, err := DecryptSecret(keyBase64, resp.Data.BotEncryptSecret)
			if err != nil {
				return nil, false, fmt.Errorf("decrypt secret: %w", err)
			}
			if onStatus != nil {
				onStatus(StatusCompleted, "绑定成功！")
			}
			return &Credentials{
				AppID:     resp.Data.BotAppID,
				AppSecret: appSecret,
			}, false, nil

		case StatusExpired:
			return nil, true, nil
		}

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
