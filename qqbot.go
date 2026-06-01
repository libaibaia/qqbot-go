// Package qqbot provides a Go SDK for the QQ Bot platform.
//
// QuickStart is the simplest way to get a bot running:
//
//	qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
//	    msg.Reply("收到: " + msg.Content)
//	})
package qqbot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/libaibaia/qqbot-go/channel"
	"github.com/libaibaia/qqbot-go/connector"
)

// Message re-exports channel.Message for convenience.
type Message = channel.Message

// Author re-exports channel.Author for convenience.
type Author = channel.Author

// Credentials holds bot credentials (re-exported from connector).
type Credentials = connector.Credentials

// CredentialStore re-exports for convenience.
type CredentialStore = connector.CredentialStore

// QuickStartConfig configures the QuickStart flow.
type QuickStartConfig struct {
	// Credentials to use directly. If nil, loads from CredentialFile.
	Credentials *Credentials

	// CredentialFile is the path to a JSON file for persisting credentials.
	// If the file exists, it's loaded. After QR binding, credentials are saved here.
	// Default: "" (no file persistence, requires QR scan every time).
	CredentialFile string

	// OnQrURL is called when a QR code URL is ready.
	// Your platform should display this URL as a QR code for the user to scan.
	// Only called when credentials are not available.
	OnQrURL func(url string)

	// OnStatus is called with binding status updates.
	OnStatus func(status int, msg string)

	// Source is the platform identifier for the QR page.
	Source string

	// Log is the logger. Default: slog.Default().
	Log *slog.Logger
}

// QuickStart performs the full bot startup flow:
// 1. Load credentials from file (if CredentialFile is set)
// 2. If no credentials, perform QR binding
// 3. Save credentials to file (if CredentialFile is set)
// 4. Connect to the gateway
// 5. Call handler for each incoming message
//
// Blocks until ctx is cancelled.
func QuickStart(ctx context.Context, handler func(*Message), cfg *QuickStartConfig) error {
	if cfg == nil {
		cfg = &QuickStartConfig{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}

	var creds *Credentials

	// 1. Use direct credentials if provided
	if cfg.Credentials != nil {
		creds = cfg.Credentials
	}

	// 2. Try credential file
	if creds == nil && cfg.CredentialFile != "" {
		store := connector.NewFileStore(cfg.CredentialFile)
		var err error
		creds, err = store.Load()
		if err != nil {
			return fmt.Errorf("load credentials from %s: %w", cfg.CredentialFile, err)
		}
		if creds != nil {
			cfg.Log.Info("从文件加载凭据", "file", cfg.CredentialFile)
		}
	}

	// 3. QR binding if still no credentials
	if creds == nil {
		qrOpts := &connector.ConnectOptions{
			Source: cfg.Source,
			Ctx:   ctx,
			OnQrURL: func(url string) {
				if cfg.OnQrURL != nil {
					cfg.OnQrURL(url)
				}
			},
			OnStatus: func(status connector.BindStatus, msg string) {
				if cfg.OnStatus != nil {
					cfg.OnStatus(int(status), msg)
				}
			},
		}

		if cfg.CredentialFile != "" {
			// LoadOrConnect: try file first, then QR, then save
			store := connector.NewFileStore(cfg.CredentialFile)
			var err error
			creds, err = connector.LoadOrConnect(qrOpts, store)
			if err != nil {
				return fmt.Errorf("QR connect failed: %w", err)
			}
		} else {
			var err error
			creds, err = connector.Connect(qrOpts)
			if err != nil {
				return fmt.Errorf("QR connect failed: %w", err)
			}
		}
	}

	cfg.Log.Info("正在连接...", "appID", creds.AppID)

	ch := channel.New(channel.Config{
		AppID:     creds.AppID,
		AppSecret: creds.AppSecret,
		Handler:   handler,
		Log:       cfg.Log,
	})
	defer ch.Close()

	return ch.Connect(ctx)
}
