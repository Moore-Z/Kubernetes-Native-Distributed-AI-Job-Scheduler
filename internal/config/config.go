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


type AgentConfig struct{
	// Pod 的基本信息（从环境变量获取）
	PodName       string  // 例如："vllm-deployment-0"
  Namespace     string  // 例如："default"
  ConfigMapName string  // 例如："vllm-config"

  // 模型存储路径
  ModelPath     string  // 例如："/models"

	// 角色判断结果
	IsCoordinator bool   // true = 我是 Coordinator，false = 我是 Follower
	CoordinatorIP string // 如果我是 Follower，这里存储 Coordinator 的 IP 地址
}

func LoadConfig()(*AgentConfig, error){

	config := &AgentConfig{
		PodName: os.Getenv("POD_NAME"),
		Namespace: os.Getenv("POD_NAMESPACE"),
		ConfigMapName: os.Getenv("CONFIGMAP_NAME"),
		ModelPath: getModelPath(),
	}

	if config.PodName == "" || config.Namespace== "" {
		return nil, fmt.Errorf("POD_NAME and POD_NAMESPACE must be set")
	}

	if config.ConfigMapName != ""{
		if err := config.loadRoleFronConfigMap(); err!=nil{
			return nil, err
		}
	} else {
		config.IsCoordinator = true
		log.Println("⚠️  Running in test mode (no ConfigMap), assuming Coordinator role")
	}

	return config, nil
}

// func createKubernetesClient()(*kubernetes.Clientset, error){
// 	config, err := rest.InClusterConfig()
// 	if err != nil {
// 		homedir, _ := os.UserHomeDir()
// 		kuberConfig := homedir + "/.kuber/config"
// 		config, err = clientcmd.BuildConfigFromFlags("", kuberConfig)
// 		if err != nil {
//         return nil, fmt.Errorf("failed to build config: %w", err)
//     }
// 	}
// 	clientset, err := kubernetes.NewForConfig(config)
// 	if err != nil {
//       return nil, fmt.Errorf("failed to create clientset: %w", err)
//   }
// 	return clientset, nil
// }

func (c *AgentConfig)loadRoleFronConfigMap()(error){
	config, err := rest.InClusterConfig()

	// Error Handling
	if err !=nil{
		return fmt.Errorf("Failed to load in cluster Config %w",err)
	}

	// 创建 Kubernetes API 客户端
  // 用这个客户端可以调用 Kubernetes API
	clientSet, err := kubernetes.NewForConfig(config)
	// Error Handling
	if err !=nil{
		return fmt.Errorf("Failed to create clientset %w",err)
	}
	// Step 2: 读取 ConfigMap
	ctx := context.Background()

	cm, err := clientSet.CoreV1().ConfigMaps(c.Namespace).Get(
		ctx,
		c.ConfigMapName,
		metav1.GetOptions{},
	)
	if err !=nil{
		return fmt.Errorf("Failed to get configMap %w",err)
	}
	// Step 3: 从 ConfigMap 中读取 coordinator 字段
    // ConfigMap 的格式如下：
    // apiVersion: v1
    // kind: ConfigMap
    // metadata:
    //   name: vllm-config
    // data:
    //   coordinator: "vllm-deployment-0"  ← 这里指定了谁是 Coordinator
	coordinator , exists := cm.Data["coordinator"]
	if !exists {
		return fmt.Errorf("No coordinate in ConfigMap %w",err)
	}

	c.IsCoordinator = (coordinator == c.PodName)
	// Step 5: 如果我是 Follower，需要获取 Coordinator 的 IP 地址
  // 为什么？因为 Follower 需要通过 HTTP 从 Coordinator 拉取模型
	if !c.IsCoordinator {

		coordinatorPod, err := clientSet.CoreV1().Pods(c.Namespace).Get(
			ctx,
			coordinator,
			metav1.GetOptions{},
		)

		if err !=nil{
			return fmt.Errorf("Failed to get Coordinate Pod: %w",err)
		}

		c.CoordinatorIP = coordinatorPod.Status.PodIP

		if c.CoordinatorIP == ""{
			return fmt.Errorf("Failed to get Coordinate IP: %w",err)
		}
	}
	return nil
}

func (c *AgentConfig) RoleString() string {
	if c.IsCoordinator {
		return "Coordinator"
	}
	return "Follower"
}

func getModelPath()string{
	if path := os.Getenv("MODEL_PATH"); path!=""{
		return path
	}

	return "/models"
}