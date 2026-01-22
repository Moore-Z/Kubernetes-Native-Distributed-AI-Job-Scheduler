package coordinator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
)

type Coordinator struct{
	modelPath string
	modelServer *ModelServer
}

// NewCoordinator 创建新的 Coordinator
func NewCoordinator(modelPath string) *Coordinator{
	return &Coordinator{
		modelPath: modelPath,
		modelServer: NewModelServer(modelPath),
	}
}

// Run 运行 Coordinator 的主逻辑
// 这是 Coordinator 的入口函数，会：
// 1. 下载模型（如果不存在）
// 2. 启动 HTTP 服务器
// 3. 等待关闭信号
func (c *Coordinator) Run(ctx context.Context) error {
	log.Println("🚀 Running as Coordinator")
	if err := c.ensureModel(); err != nil {
		return fmt.Errorf("failed to ensure model: %w", err)
	}
	// Step 2: 启动 HTTP 服务器（在 goroutine 中运行，不阻塞）
	go func ()  {
		if err := c.modelServer.Start(); err != nil {
			log.Fatalf("❌ Model server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("🛑 Coordinator shutting down")
	return nil
}



// ensureModel 确保模型存在
// 如果模型已存在，跳过下载；否则下载
func (c *Coordinator)ensureModel() error{
	if c.modelExists(c.modelPath){
		log.Println("✅ Model already exists, skipping download")
		return nil
	}
	// 模型不存在，需要下载
	log.Println("📥 Model not found, starting download...")
	return c.downloadModel()
}

// modelExists 检查模型目录是否有文件
// os : read path
func (c *Coordinator) modelExists(modelPath string) bool{
	files, err := os.ReadDir(modelPath)
	if err != nil{
		return false
	}
	return len(files) > 0
}

// downloadModel 从 HuggingFace 下载模型
func (c *Coordinator) downloadModel() error{
	// 从环境变量获取模型仓库名称
	modelRepo := os.Getenv("MODEL_REPO")

	if modelRepo == ""{
		return fmt.Errorf("MODEL_REPO environment variable not set")
	}

	log.Printf("📦 Downloading model: %s to %s", modelRepo, c.modelPath)

	if err := os.MkdirAll(c.modelPath, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	// 调用 huggingface-cli 下载模型
	// 命令格式：huggingface-cli download <repo> --local-dir <path>
	cmd := exec.Command(
		"huggingface-cli",
		"download",
		modelRepo,
		"--local-dir", c.modelPath,
		"--local-dir-use-symlinks", "False", // 不使用符号链接，直接复制文件
	)
	// 将命令的输出连接到标准输出/错误，这样可以看到下载进度
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	log.Println("✅ Model download completed")
	return nil
}