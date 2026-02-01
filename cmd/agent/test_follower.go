//go:build ignore

// 这是一个手动测试文件，用于本地测试 Follower
// 运行方法：go run test_follower.go
//
// 测试步骤：
// 1. 先启动一个 HTTP 服务器模拟 Coordinator:
//    mkdir -p /tmp/coordinator-models
//    echo "hello config" > /tmp/coordinator-models/config.json
//    echo "hello tokenizer" > /tmp/coordinator-models/tokenizer.json
//    cd /tmp/coordinator-models && python3 -m http.server 8080
//
// 2. 然后运行这个测试:
//    cd /Users/henry/Projects/kubeinfer/cmd/agent
//    go run test_follower.go

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Moore-Z/kubeinfer/internal/agent/follower"
)

func main() {
	log.Println("🧪 Testing Follower...")

	// 配置（硬编码用于测试）
	coordinatorIP := "127.0.0.1" // localhost
	modelPath := "/tmp/follower-models"

	// 创建目标目录
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		log.Fatalf("Failed to create model path: %v", err)
	}

	// 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听 Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("📥 Received shutdown signal")
		cancel()
	}()

	// 运行 Follower
	f := follower.NewFollower(coordinatorIP, modelPath)
	if err := f.Run(ctx); err != nil {
		log.Fatalf("❌ Follower failed: %v", err)
	}

	log.Println("✅ Test completed!")
}
