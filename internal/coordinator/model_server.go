package coordinator

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)
const ServerPort = 8080

type ModelServer struct {
	modelPath string
}

// NewModelServer 创建新的模型服务器
func NewModelServer(modelpath string)*ModelServer{
	return &ModelServer{
		modelPath: modelpath,
	}
}

func (m *ModelServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health",m.handleHealth)					// Check health
	mux.HandleFunc("/models",m.handleListModels)			// List all model files
	mux.HandleFunc("/models/",m.handleDownloadModel)	// Download specific model

	// 启动服务器
	addr := fmt.Sprintf(":%d",ServerPort)
	fmt.Printf("🌐 Starting model server on %s", addr)
	return http.ListenAndServe(addr,mux)
}




func (m *ModelServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	// handleHealth 处理健康检查请求
	// GET /health → 返回 "OK"
	if r.Method != http.MethodGet {
		http.Error(w, "Method is not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w,"OK\n")
}



// handleListModels 处理文件列表请求
// GET /models → 返回模型目录中的所有文件名（每行一个）
func (m *ModelServer) handleListModels(w http.ResponseWriter, r *http.Request) {
	// 只允许 GET 方法
	if r.Method != http.MethodGet {
		http.Error(w, "Method is not allowed", http.StatusMethodNotAllowed)
	}

	// 读取模型目录
	files, err := os.ReadDir(m.modelPath)
	if err != nil {
		log.Printf("❌ Error reading model directory: %v", err)
		http.Error(w, "Failed to list models", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type","text/plain")

	for _, file := range files {
		fmt.Fprintf(w, "%s\n",file.Name())
	}
	log.Printf("📋 Listed %d model files", len(files))
	return
}



// handleDownloadModel 处理文件下载请求
// GET /models/config.json → 返回 config.json 文件内容
// GET /models/subfolder/model.bin → 返回 subfolder/model.bin 文件内容
func (ms *ModelServer) handleDownloadModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 从 URL 提取文件路径
	// 例如：/models/config.json → config.json
	//       /models/subfolder/model.bin → subfolder/model.bin

	relativePath := strings.TrimPrefix(r.URL.Path,"/models/")
	if relativePath == "" {
		http.Error(w, "File path required", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(ms.modelPath , relativePath)

	if !strings.HasPrefix(fullPath, ms.modelPath) {
		log.Printf("⚠️  Blocked path traversal attempt: %s", relativePath)
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	file, err := os.Open(fullPath)
	if err != nil {
		log.Printf("❌ File not found: %s, error: %v", fullPath, err)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Printf("❌ Error getting file info: %v", err)
		http.Error(w, "Failed to stat file", http.StatusInternalServerError)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/octet-stream")                       // 二进制流
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))             // 文件大小
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s",     // 下载文件名
		filepath.Base(fullPath)))
	// 流式传输文件内容
	// io.Copy 会自动处理大文件，边读边写，不会占用大量内存
	log.Printf("📤 Serving file: %s (size: %d bytes)", relativePath, fileInfo.Size())
	written,err := io.Copy(w,file)
	if err != nil {
		fmt.Printf("Error Stream file %v", err)
		return
	}
	log.Printf("✅ Sent %d bytes", written)
}