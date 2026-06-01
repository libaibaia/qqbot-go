package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/libaibaia/qqbot-go"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("=== QQ Bot Go SDK Example ===")

	err := qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
		fmt.Printf("[%s] %s: %s\n", msg.Type, msg.Author.Name, msg.Content)
		msg.Reply("收到: " + msg.Content)
	}, &qqbot.QuickStartConfig{
		// 持久化凭据到文件，下次启动无需重新扫码
		CredentialFile: "./data/qqbot-credentials.json",

		// 你的平台拿到这个 URL 后展示为二维码给用户扫
		OnQrURL: func(url string) {
			fmt.Println()
			fmt.Println("请扫描以下链接对应的二维码:")
			fmt.Println(url)
			fmt.Println()
		},

		OnStatus: func(status int, msg string) {
			fmt.Println(msg)
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
