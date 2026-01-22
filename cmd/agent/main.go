package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Moore-Z/kubeinfer/internal/agent/config"
	"github.com/Moore-Z/kubeinfer/internal/agent/coordinator"
)

func main() {
    // 打印启动信息
    log.Println("🚀 KubeInfer Agent starting...")

    // Step 1: 加载配置
    // 这会读取环境变量和 ConfigMap
    cfg, err := config.LoadConfig()
    if err != nil {
        // 如果配置加载失败，直接退出
        // log.Fatalf 会打印错误并调用 os.Exit(1)
        log.Fatalf("❌ Failed to load config: %v", err)
    }

    // Step 2: 打印配置信息（用于调试）
    log.Printf("📋 Pod: %s, Namespace: %s, Role: %s",
		cfg.PodName, cfg.Namespace, cfg.RoleString())

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Step 3: 监听系统信号（SIGINT, SIGTERM）
    sigChan := make(chan os.Signal,1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        sig := <-sigChan
        log.Printf("📥 Received signal: %v", sig)
        cancel()
    }()

    // Step 4: 根据角色运行不同的逻辑
    var runErr error
    if cfg.IsCoordinator {
        coord := coordinator.NewCoordinator(cfg.ModelPath)
        runErr = coord.Run(ctx)
    } else {
        log.Println("⏸️  Follower logic not implemented yet")
        <-ctx.Done()
    }

    if runErr != nil {
		log.Fatalf("❌ Run failed: %v", runErr)
	}

	log.Println("👋 Agent shut down gracefully")
}