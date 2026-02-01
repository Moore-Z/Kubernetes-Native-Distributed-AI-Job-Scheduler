package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/Moore-Z/kubeinfer/internal/agent/coordinator"
	"github.com/Moore-Z/kubeinfer/internal/agent/follower"
)

// ============================================================================
// Agent 主程序
// ============================================================================
//
// 核心逻辑：
// 1. 启动 LeaseManager，参与 coordinator 选举
// 2. 如果抢到 Lease → 运行 Coordinator 逻辑
// 3. 如果没抢到 → 运行 Follower 逻辑
// 4. 如果角色变化（比如原 coordinator 挂了）→ 自动切换
//
// 这就是 "automatic failover" 的实现！
// ============================================================================

func main() {
	log.Println("🚀 KubeInfer Agent starting...")

	// ========================================
	// Step 1: 读取环境变量
	// ========================================
	podName := os.Getenv("POD_NAME")
	namespace := os.Getenv("POD_NAMESPACE")
	configMapName := os.Getenv("CONFIGMAP_NAME") // 例如 "my-llm-cache"
	modelPath := os.Getenv("MODEL_PATH")

	if podName == "" || namespace == "" || configMapName == "" {
		log.Fatalf("❌ Missing required env: POD_NAME, POD_NAMESPACE, CONFIGMAP_NAME")
	}
	if modelPath == "" {
		modelPath = "/models"
	}

	log.Printf("📋 Pod: %s, Namespace: %s", podName, namespace)

	// ========================================
	// Step 2: 创建 Kubernetes 客户端
	// ========================================
	// rest.InClusterConfig() 在 Pod 内自动获取认证信息
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("❌ Failed to get in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("❌ Failed to create clientset: %v", err)
	}

	// ========================================
	// Step 3: 创建 LeaseManager
	// ========================================
	// Lease 名称 = ConfigMap 名称 + "-lease"
	// 例如：configMapName = "my-llm-cache" → leaseName = "my-llm-cache-lease"
	// 这样每个 LLMService 有自己独立的选举
	leaseName := configMapName + "-lease"

	lm, err := coordinator.NewLeaseManager(clientset, namespace, leaseName)
	if err != nil {
		log.Fatalf("❌ Failed to create LeaseManager: %v", err)
	}

	// ========================================
	// Step 4: 设置 Context 和信号处理
	// ========================================
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("📥 Received signal: %v, shutting down...", sig)
		cancel()
	}()

	// ========================================
	// Step 5: 运行选举循环
	// ========================================
	// LeaseManager.Run() 会：
	// - 每 2 秒尝试获取或续约 Lease
	// - 如果获得 Lease → 调用 onElected
	// - 如果失去 Lease → 调用 onLost
	//
	// 注意：onElected 和 onLost 是回调函数，不能阻塞！
	// 所以我们用 goroutine 来运行 coordinator/follower

	// 用于控制当前运行的角色
	var roleCancel context.CancelFunc

	// 停止当前角色
	stopCurrentRole := func() {
		if roleCancel != nil {
			roleCancel()
			roleCancel = nil
		}
	}

	// 当选为 Coordinator 时的回调
	onElected := func() {
		log.Println("👑 Elected as Coordinator!")
		stopCurrentRole()

		// 创建新的 context 用于 coordinator
		roleCtx, cancel := context.WithCancel(ctx)
		roleCancel = cancel

		// 在 goroutine 中运行（不能阻塞回调）
		go func() {
			coord := coordinator.NewCoordinator(modelPath)
			if err := coord.Run(roleCtx); err != nil {
				if roleCtx.Err() == nil { // 不是被取消的
					log.Printf("❌ Coordinator error: %v", err)
				}
			}
		}()
	}

	// 失去 Coordinator 身份时的回调
	onLost := func() {
		log.Println("📉 Lost coordinator role, becoming Follower...")
		stopCurrentRole()

		// 需要知道新 coordinator 的 IP
		// 从 Lease 的 HolderIdentity 获取 Pod 名称，然后查询 Pod IP
		coordIP, err := getCoordinatorIP(clientset, namespace, leaseName)
		if err != nil {
			log.Printf("⚠️  Failed to get coordinator IP: %v, will retry...", err)
			return
		}

		roleCtx, cancel := context.WithCancel(ctx)
		roleCancel = cancel

		go func() {
			f := follower.NewFollower(coordIP, modelPath)
			if err := f.Run(roleCtx); err != nil {
				if roleCtx.Err() == nil {
					log.Printf("❌ Follower error: %v", err)
				}
			}
		}()
	}

	// 启动选举循环（这个会阻塞直到 ctx 被取消）
	log.Println("🗳️  Starting leader election...")
	lm.Run(ctx, onElected, onLost)

	// 清理
	stopCurrentRole()
	log.Println("👋 Agent shut down gracefully")
}

// getCoordinatorIP 获取当前 Coordinator 的 IP
//
// 流程：
// 1. 读取 Lease，获取 HolderIdentity（Pod 名称）
// 2. 查询该 Pod，获取 PodIP
func getCoordinatorIP(clientset *kubernetes.Clientset, namespace, leaseName string) (string, error) {
	ctx := context.Background()

	// 读取 Lease
	lease, err := clientset.CoordinationV1().Leases(namespace).Get(ctx, leaseName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get lease: %w", err)
	}

	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return "", fmt.Errorf("lease has no holder")
	}

	coordPodName := *lease.Spec.HolderIdentity

	// 查询 Pod
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, coordPodName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get coordinator pod: %w", err)
	}

	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("coordinator pod has no IP")
	}

	return pod.Status.PodIP, nil
}
