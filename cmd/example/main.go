package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/libaibaia/qqbot-go"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// 确保凭据目录存在
	os.MkdirAll("./data", 0o700)

	// 启动时检查 ffmpeg
	ffmpegPath, err := qqbot.CheckFFmpeg()
	if err != nil {
		fmt.Println("[警告] ffmpeg 未安装，语音转换功能不可用:", err)
		fmt.Println("       图片和文本消息不受影响")
	} else {
		fmt.Println("[OK] ffmpeg:", ffmpegPath)
	}

	fmt.Println("=== QQ Bot Go SDK Test ===")
	fmt.Println("等待消息中... Ctrl+C 退出")

	err = qqbot.QuickStart(ctx, func(msg *qqbot.Message) {
		ts := time.Now().Format("15:04:05")
		fmt.Printf("[%s] [%s] %s: %s\n", ts, msg.Type, msg.Author.Name, msg.Content)

		// 处理图片
		if images := msg.Images(); len(images) > 0 {
			fmt.Printf("[%s] 收到 %d 张图片: %v\n", ts, len(images), images)
			msg.Reply(fmt.Sprintf("收到 %d 张图片", len(images)))
			return
		}

		// 处理语音
		if voices := msg.Voices(); len(voices) > 0 {
			fmt.Printf("[%s] 收到语音: %v\n", ts, voices)
			if text := msg.VoiceText(); text != "" {
				fmt.Printf("[%s] 语音识别: %s\n", ts, text)
				msg.Reply("语音识别结果: " + text)
			} else {
				msg.Reply("收到语音消息")
			}
			return
		}

		// 普通文本：回复固定内容
		reply := fmt.Sprintf("[机器人] 已收到你的消息，类型=%s，内容长度=%d", msg.Type, len(msg.Content))
		if err := msg.Reply(reply); err != nil {
			fmt.Printf("[%s] 回复失败: %v\n", ts, err)
		} else {
			fmt.Printf("[%s] 回复成功\n", ts)
		}
	}, &qqbot.QuickStartConfig{
		CredentialFile: "./data/qqbot-credentials.json",

		OnQrURL: func(url string) {
			fmt.Println()
			fmt.Println("========================================")
			fmt.Println("请扫描以下链接对应的二维码:")
			fmt.Println(url)
			fmt.Println("========================================")
			fmt.Println()
		},

		OnStatus: func(status int, msg string) {
			fmt.Printf("[绑定状态] status=%d: %s\n", status, msg)
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
