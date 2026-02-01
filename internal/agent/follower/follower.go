package follower

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Moore-Z/kubeinfer/internal/agent/vllm"
)

// Coordinator HTTP 服务器的端口（和 model_server.go 里定义的一样）
const CoordinatorPort = 8080

// Follower 结构体
// Follower 是"跟随者" Pod，它的任务是：
// 1. 从 Coordinator 的 HTTP 服务器获取模型文件列表
// 2. 下载每个模型文件到本地
// 3. 下载完成后，等待退出信号
type Follower struct {
	coordinatorIP string // Coordinator 的 IP 地址，例如 "10.0.0.5"
	modelPath     string // 模型文件存放路径，例如 "/models"
}

// NewFollower 创建一个新的 Follower 实例
//
// 参数：
//   - coordinatorIP: 从 config.LoadConfig().CoordinatorIP 获得
//   - modelPath: 从 config.LoadConfig().ModelPath 获得
func NewFollower(coordinatorIP, modelPath string) *Follower {
	return &Follower{
		coordinatorIP: coordinatorIP,
		modelPath:     modelPath,
	}
}

// Run 是 Follower 的主函数
//
// 执行流程：
//  1. 调用 getFileList() 获取文件列表
//  2. 循环调用 downloadFile() 下载每个文件
//  3. 全部下载完成后，等待 ctx.Done()
func (f *Follower) Run(ctx context.Context) error {
	log.Println("🚀 Running as Follower")
	log.Printf("📡 Coordinator IP: %s", f.coordinatorIP)

	// Step 1: 获取文件列表
	files, err := f.getFileList()
	if err != nil {
		return fmt.Errorf("failed to get file list: %w", err)
	}

	// Step 2: 下载每个文件
	for _, filename := range files {
		err := f.downloadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to download file: %s, %w", filename, err)
		}
	}
	// 启动 vLLM
	vllmConfig := vllm.LoadConfigFromEnv(f.modelPath)
	vllmServer := vllm.NewServer(vllmConfig)
	if err := vllmServer.Start(); err != nil {
		return fmt.Errorf("failed to start vLLM: %w", err)
	}

	// Step 3: 等待退出信号
	log.Println("✅ All files downloaded, waiting for shutdown signal...")
	<-ctx.Done()
	vllmServer.Stop()

	return nil
}

// getFileList 从 Coordinator 获取模型文件列表
//
// 调用 Coordinator 的 GET /models 接口
// 返回值示例：["config.json", "tokenizer.json", "model.safetensors"]
func (f *Follower) getFileList() ([]string, error) {

	// 构造 URL， 记得我们的coordination class 里面有个model_server 里面有的http， 通过接口调别的pod info
	url := fmt.Sprintf("http://%s:%d/models", f.coordinatorIP, CoordinatorPort)
	log.Printf("📋 Fetching file list from %s", url)

	// Step 2: 发送 HTTP GET 请求
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file list: %w", err)
	}
	defer resp.Body.Close()

	// Step 3: 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Step 4: 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Step 5: 按行分割，返回文件列表
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	return lines, nil
}

// downloadFile 从 Coordinator 下载单个文件
//
// 调用 Coordinator 的 GET /models/{filename} 接口
// 参数：
//   - filename: 文件名，比如 "config.json"
func (f *Follower) downloadFile(filename string) error {
	// Step 1: 构造 URL
	url := fmt.Sprintf("http://%s:%d/models/%s", f.coordinatorIP, CoordinatorPort, filename)
	log.Printf("📥 Downloading %s", filename)

	// Step 2: 发送 HTTP GET 请求
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	// Step 3: 检查状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status: %d", filename, resp.StatusCode)
	}

	// Step 4: 创建本地文件
	localPath := filepath.Join(f.modelPath, filename)
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %s, error: %w", filename, err)
	}
	defer file.Close()

	// Step 5: 把 HTTP 响应写入文件
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write http response: %w", err)
	}
	log.Printf("✅ Downloaded %s (%d bytes)", filename, written)

	return nil
}
