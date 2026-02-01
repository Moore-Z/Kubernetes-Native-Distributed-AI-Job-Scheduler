package vllm

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type Config struct {
	// 模型文件path
	ModelPath string
	// building地址 → 监听地址， 默认0.0。0.0
	Host string
	// 部门地址 → 监听端口
	Port int

	// GPU 并行的数量 --tensor-parallel-size
	TensorParallelSize int
	// 显卡利用率（0-1.0） --gpu-memory-utilization
	GPUMemoryUtilization float64
	// 最大上下文长度， 0 就是默认； --max-model-len
	MaxModelLen int
	// data type，
	Dtype string
	// 兜底函数，用于传递任意其他参数
	ExtraArgs []string
}

// 初始化config， 填写default值
func DefaultConfig(modelPath string) *Config {
	return &Config{
		ModelPath:            modelPath,
		Host:                 "0.0.0.0",
		Port:                 8000,
		TensorParallelSize:   1,
		GPUMemoryUtilization: 0.9,
		Dtype:                "auto",
	}
}

// 很简单就是往 vLLM config 里面填写data 的
func LoadConfigFromEnv(modelPath string) *Config {
	config := DefaultConfig(modelPath)

	if v := os.Getenv("VLLM_HOST"); v != "" {
		config.Host = v
	}
	if v := os.Getenv("VLLM_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			config.Port = port
		}
	}
	if v := os.Getenv("VLLM_TENSOR_PARALLEL_SIZE"); v != "" {
		if tp, err := strconv.Atoi(v); err == nil {
			config.TensorParallelSize = tp
		}
	}
	if v := os.Getenv("VLLM_GPU_MEMORY_UTILIZATION"); v != "" {
		if gpu, err := strconv.ParseFloat(v, 64); err == nil {
			config.GPUMemoryUtilization = gpu
		}
	}
	if v := os.Getenv("VLLM_MAX_MODEL_LEN"); v != "" {
		if maxLen, err := strconv.Atoi(v); err == nil {
			config.MaxModelLen = maxLen
		}
	}
	if v := os.Getenv("VLLM_DTYPE"); v != "" {
		config.Dtype = v
	}
	if v := os.Getenv("VLLM_EXTRA_ARGS"); v != "" {
		config.ExtraArgs = strings.Fields(v)
	}

	return config
}

type Server struct {
	config *Config
	cmd    *exec.Cmd
}

// 这里用config 就可以initalized Server model
func NewServer(config *Config) *Server {
	return &Server{config: config}
}

// 把 config 里面的东西转化成 cmd 给vllm
func (s *Server) buildArgs() []string {
	args := []string{
		"-m", "vllm.entrypoints.openai.api_server",
		"--model", s.config.ModelPath,
		"--host", s.config.Host,
		"--port", strconv.Itoa(s.config.Port),
		"--tensor-parallel-size", strconv.Itoa(s.config.TensorParallelSize),
		"--gpu-memory-utilization", fmt.Sprintf("%.2f", s.config.GPUMemoryUtilization),
		"--dtype", s.config.Dtype,
	}

	if s.config.MaxModelLen > 0 {
		args = append(args, "--max-model-len", strconv.Itoa(s.config.MaxModelLen))
	}
	if len(s.config.ExtraArgs) > 0 {
		args = append(args, s.config.ExtraArgs...)
	}

	return args
}

// 整体逻辑，给vllm server 的cmd 补全
func (s *Server) Start() error {
	args := s.buildArgs()
	log.Printf("🚀 Starting vLLM: python %s", strings.Join(args, " "))

	s.cmd = exec.Command("python", args...)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("Failed to start vLLM: %w", err)
	}
	log.Printf("✅ vLLM started with PID %d", s.cmd.Process.Pid)
	return nil
}

// 两个辅助函数，一个停止，一个补全
func (s *Server) Wait() error {
	if s.cmd == nil {
		return fmt.Errorf("vLLM not started")
	}
	return s.cmd.Wait()
}
func (s *Server) Stop() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Signal(syscall.SIGTERM)
}
