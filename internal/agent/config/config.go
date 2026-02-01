package config

import (
	"context"
	"fmt"
	"os"

	// Kubernetes API 相关的包
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type AgentConfig struct {
	// Pod 的基本信息（从环境变量获取）
	PodName       string // 例如："vllm-deployment-0"
	Namespace     string // 例如："default"
	ConfigMapName string // 例如："vllm-config"

	// 模型存储路径
	ModelPath string // 例如："/models"

	// 角色判断结果
	IsCoordinator bool   // true = 我是 Coordinator，false = 我是 Follower
	CoordinatorIP string // 如果我是 Follower，这里存储 Coordinator 的 IP 地址
}

func LoadConfig() (*AgentConfig, error) {

	config := &AgentConfig{
		PodName:       os.Getenv("POD_NAME"),
		Namespace:     os.Getenv("POD_NAMESPACE"),
		ConfigMapName: os.Getenv("CONFIGMAP_NAME"),
		ModelPath:     getModelPath(),
	}

	if config.PodName == "" || config.Namespace == "" {
		return nil, fmt.Errorf("POD_NAME and POD_NAMESPACE must be set")
	}

	if config.ConfigMapName != "" {
		if err := config.loadRoleFromConfigMap(); err != nil {
			return nil, err
		}
	} else {
		config.IsCoordinator = true
		log.Println("⚠️  Running in test mode (no ConfigMap), assuming Coordinator role")
	}

	return config, nil
}

// loadRoleFromConfigMap 从 ConfigMap 中读取角色信息，判断当前 Pod 是 Coordinator 还是 Follower
//
// 这个函数是 Agent 启动时的核心逻辑，它回答一个关键问题：
// "我是应该下载模型的那个 Pod (Coordinator)，还是应该从别人那里拿模型的 Pod (Follower)？"
//
// 整体流程：
//
//	┌─────────────────────────────────────────────────────────────┐
//	│  ConfigMap (由 Controller 创建和维护)                        │
//	│                                                             │
//	│  data:                                                      │
//	│    coordinator-pod: "vllm-deployment-0"   ← 谁是老大        │
//	│    coordinator-ip: "10.0.0.5"             ← 老大的 IP       │
//	└─────────────────────────────────────────────────────────────┘
//	                          │
//	                          │ 读取
//	                          ▼
//	┌─────────────────────────────────────────────────────────────┐
//	│  当前 Pod (比如 vllm-deployment-1)                           │
//	│                                                             │
//	│  比较: "vllm-deployment-0" == "vllm-deployment-1" ?         │
//	│        false → 我是 Follower                                │
//	│                                                             │
//	│  结果: IsCoordinator = false                                │
//	│        CoordinatorIP = "10.0.0.5"                           │
//	└─────────────────────────────────────────────────────────────┘
func (c *AgentConfig) loadRoleFromConfigMap() error {
	// ========================================
	// Step 1: 创建 Kubernetes API 客户端
	// ========================================
	//
	// rest.InClusterConfig() 的作用：
	// - 当代码在 Pod 内运行时，自动读取 Pod 的 ServiceAccount 凭证
	// - 凭证位置：/var/run/secrets/kubernetes.io/serviceaccount/token
	// - 这样我们就能调用 Kubernetes API 了
	//
	// 注意：这个只能在 Pod 内使用，本地开发时会报错
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("Failed to load in cluster Config %w", err)
	}

	// 用 config 创建 clientSet
	// clientSet 就像一个"遥控器"，可以操作 Kubernetes 的各种资源
	// - clientSet.CoreV1().Pods()       → 操作 Pod
	// - clientSet.CoreV1().ConfigMaps() → 操作 ConfigMap
	// - clientSet.AppsV1().Deployments()→ 操作 Deployment
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("Failed to create clientset %w", err)
	}

	// ========================================
	// Step 2: 从 Kubernetes 读取 ConfigMap
	// ========================================
	//
	// ConfigMap 是 Controller 创建的，格式如下：
	//
	//   apiVersion: v1
	//   kind: ConfigMap
	//   metadata:
	//     name: my-llm-cache        ← c.ConfigMapName
	//     namespace: default        ← c.Namespace
	//   data:
	//     coordinator-pod: "vllm-deployment-0"   ← 我们要读的字段
	//     coordinator-ip: "10.0.0.5"
	//     last-heartbeat: "2024-01-20T10:00:00Z"
	//
	ctx := context.Background()

	cm, err := clientSet.CoreV1().ConfigMaps(c.Namespace).Get(
		ctx,
		c.ConfigMapName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("Failed to get configMap %w", err)
	}

	// ========================================
	// Step 3: 读取 coordinator-pod 字段
	// ========================================
	//
	// cm.Data 是一个 map[string]string，存储 ConfigMap 的所有键值对
	// 我们要找的是 "coordinator-pod" 这个 key
	coordinator, exists := cm.Data["coordinator-pod"]
	if !exists {
		return fmt.Errorf("No coordinator-pod in ConfigMap %w", err)
	}

	// ========================================
	// Step 4: 判断"我是不是 Coordinator"
	// ========================================
	//
	// 逻辑很简单：比较 ConfigMap 里的 coordinator 名字和我自己的名字
	//
	// 例子 1：我是 vllm-deployment-0，ConfigMap 写的也是 vllm-deployment-0
	//         → "vllm-deployment-0" == "vllm-deployment-0" → true
	//         → 我是 Coordinator！
	//
	// 例子 2：我是 vllm-deployment-1，ConfigMap 写的是 vllm-deployment-0
	//         → "vllm-deployment-0" == "vllm-deployment-1" → false
	//         → 我是 Follower
	c.IsCoordinator = (coordinator == c.PodName)

	// ========================================
	// Step 5: 如果我是 Follower，获取 Coordinator 的 IP
	// ========================================
	//
	// 为什么 Follower 需要知道 Coordinator 的 IP？
	// → 因为 Follower 要通过 HTTP 从 Coordinator 下载模型文件
	// → HTTP 请求需要 IP 地址：http://{CoordinatorIP}:8080/models
	//
	// 为什么不直接从 ConfigMap 读 coordinator-ip？
	// → 也可以，但这里选择直接查询 Pod 获取最新的 IP（更可靠）
	if !c.IsCoordinator {
		// 调用 Kubernetes API 查询 Coordinator Pod 的详细信息
		coordinatorPod, err := clientSet.CoreV1().Pods(c.Namespace).Get(
			ctx,
			coordinator, // Pod 名字，比如 "vllm-deployment-0"
			metav1.GetOptions{},
		)
		if err != nil {
			return fmt.Errorf("Failed to get Coordinator Pod: %w", err)
		}

		// Pod.Status.PodIP 是 Kubernetes 分配给这个 Pod 的集群内部 IP
		// 例如："10.244.0.15"
		c.CoordinatorIP = coordinatorPod.Status.PodIP

		if c.CoordinatorIP == "" {
			// 如果 IP 为空，说明 Coordinator Pod 可能还没完全启动
			return fmt.Errorf("Coordinator Pod has no IP yet (Pod may still be starting)")
		}
	}

	// 到这里，AgentConfig 已经填充完毕：
	// - 如果是 Coordinator: IsCoordinator=true, CoordinatorIP=""
	// - 如果是 Follower:    IsCoordinator=false, CoordinatorIP="10.x.x.x"
	return nil
}

func (c *AgentConfig) RoleString() string {
	if c.IsCoordinator {
		return "Coordinator"
	}
	return "Follower"
}

func getModelPath() string {
	if path := os.Getenv("MODEL_PATH"); path != "" {
		return path
	}

	return "/models"
}
