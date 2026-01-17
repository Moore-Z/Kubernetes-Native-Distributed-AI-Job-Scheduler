package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Moore-Z/kubeinfer/internal/coordinator"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()

	// ========== 创建 Kubernetes Client ==========

  // 1. 获取 kubeconfig 文件路径（默认 ~/.kube/config）
	kuberconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	// 2. 从 kubeconfig 构建连接配置
	config, err := clientcmd.BuildConfigFromFlags("",kuberconfig)
	if err != nil {
        klog.Fatalf("无法加载 kubeconfig: %v", err)
    }
	// 3. 创建 Kubernetes clientset（用于调用 K8s API）
	clientSet, err:= kubernetes.NewForConfig(config)
	if err != nil {
			klog.Fatalf("无法创建 Kubernetes client: %v", err)
	}

	// ========== 创建 LeaseManager ==========

	// 创建选举管理器
	// clientset.CoordinationV1(): 获取 Coordination API 的 client
	// "default": 在 default namespace 中创建 lease
	leaseMgr, err := coordinator.NewLeaseManager(
		clientSet,
		"default",
	)
	if err != nil {
        klog.Fatalf("无法创建 LeaseManager: %v", err)
    }

	// ========== 创建 Context（用于控制 goroutine 生命周期）==========

	// context.Background: 创建根 context
	// context.WithCancel: 创建可取消的 context
	// 当调用 cancel() 时，ctx.Done() channel 会关闭，通知所有监听者停止
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 程序退出时自动调用 cancel()

	// ========== 定义回调函数 ==========

	// onElected: 当这个 pod 成为 coordinator 时会被调用
	onElected := func() {
		klog.Infof("========== 我现在是 Coordinator ==========")
		// TODO: 后续实现这些功能
		// 1. 启动 HTTP 文件服务器，让 follower 可以下载模型
		// go startFileServer()

		// 2. 开始下载模型到本地
		// go downloadModels()

		// 3. 创建/更新 ConfigMap，告诉 follower 模型准备好了
		// go updateConfigMap()
	}

	// onLost: 当这个 pod 失去 coordinator 角色时会被调用
	onLost := func() {
			klog.Info("========== 我不再是 Coordinator ==========")

			// TODO: 后续实现这些功能
			// 1. 停止 HTTP 文件服务器
			// stopFileServer()

			// 2. 停止下载任务
			// cancelDownload()

			// 3. 切换到 follower 模式，从 coordinator 下载模型
			// switchToFollowerMode()
	}

	// ========== 启动选举循环 ==========

	// go: 在新的 goroutine（轻量级线程）中运行
	// leaseMgr.Run: 启动选举循环，会不断尝试获取/续约 lease
	// 这个函数会一直运行，直到 ctx 被取消
	klog.Info("开始 Leader Election 测试...")
	go leaseMgr.Run(ctx,onElected,onLost)

	// ========== 创建定时器（用于定期打印状态）==========

	// time.NewTicker: 创建一个定时器，每 5 秒触发一次
	// ticker.C 是一个 channel，每 5 秒会收到一个时间值
	ticker := time.NewTicker(5*time.Second)

	// defer ticker.Stop: 程序退出前停止定时器
  // 如果不停止，ticker 的 goroutine 会泄漏（一直运行但没人用）
	defer ticker.Stop()

	// ========== 设置信号监听（处理 Ctrl+C）==========

	// make(chan os.Signal, 1): 创建一个可以接收 1 个信号的 channel
	// 容量为 1 是为了防止信号丢失
	sigCh := make(chan os.Signal,1)

	// signal.Notify: 告诉 Go 运行时
  // "当收到 SIGINT（Ctrl+C）或 SIGTERM（kill 命令）时，发送到 sigCh"
	signal.Notify(sigCh, syscall.SIGINT,syscall.SIGTERM)

	// ========== 主循环（程序会阻塞在这里）==========
	klog.Info("Program is running, press Control + C exist.....")
	for {
		select{
		case <- ticker.C:
			klog.Infof("心跳检查 - 程序正在运行...我是coordinator:%v", leaseMgr.IsCoordinator())
		case sig := <-sigCh:
			klog.Infof("收到退出信号: %v", sig)
			klog.Info("Closing the Program")

			cancel()

			time.Sleep(2*time.Second)

			klog.Info("Program has been stopped")
			return
		}
	}

}