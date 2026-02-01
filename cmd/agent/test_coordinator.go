//go:build ignore

// 这是一个手动测试文件，用于本地测试 Coordinator
// 运行方法：go run test_coordinator.go
//
// 测试步骤：
// 1. 先创建一些测试文件:
//    mkdir -p /tmp/coordinator-models
//    echo "hello config" > /tmp/coordinator-models/config.json
//    echo "hello tokenizer" > /tmp/coordinator-models/tokenizer.json
//
// 2. 运行 Coordinator:
//    cd /Users/henry/Projects/kubeinfer/cmd/agent
//    go run test_coordinator.go
//
// 3. 测试 HTTP 接口:
//    curl http://localhost:8080/health
//    curl http://localhost:8080/models
//    curl http://localhost:8080/models/config.json

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Moore-Z/kubeinfer/internal/agent/coordinator"
)

func main() {
	log.Println("🧪 Testing Coordinator...")

	// 配置（硬编码用于测试）
	modelPath := "/tmp/coordinator-models"

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

	// 运行 Coordinator（跳过下载，直接启动 HTTP 服务器）
	log.Printf("📂 Model path: %s", modelPath)
	log.Println("🌐 Starting HTTP server on :8080")

	coord := coordinator.NewCoordinator(modelPath)
	if err := coord.Run(ctx); err != nil {
		log.Fatalf("❌ Coordinator failed: %v", err)
	}

	log.Println("✅ Test completed!")
}
