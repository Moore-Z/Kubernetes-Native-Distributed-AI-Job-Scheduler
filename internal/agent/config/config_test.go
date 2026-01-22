package config

import (
	"os"
	"testing"
)

// TestGetModelPath 测试模型路径的获取逻辑
func TestGetModelPath(t *testing.T) {
    // 定义测试用例
    // 每个测试用例包含：名称、环境变量值、期望结果
    tests := []struct {
        name     string  // 测试用例的名称
        envValue string  // 设置的环境变量值
        expected string  // 期望的返回值
    }{
        {
            name:     "使用默认路径",
            envValue: "",              // 不设置环境变量
            expected: "/models",       // 应该返回默认值
        },
        {
            name:     "使用自定义路径",
            envValue: "/custom/models", // 设置自定义路径
            expected: "/custom/models", // 应该返回自定义值
        },
    }

    // 运行每个测试用例
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 设置环境变量
            if tt.envValue != "" {
                os.Setenv("MODEL_PATH", tt.envValue)
                // defer 确保测试结束后清理环境变量
                defer os.Unsetenv("MODEL_PATH")
            } else {
                // 确保环境变量未设置
                os.Unsetenv("MODEL_PATH")
            }

            // 调用函数
            result := getModelPath()

            // 验证结果
            if result != tt.expected {
                t.Errorf("got %s, want %s", result, tt.expected)
            }
        })
    }
}

// TestLoadConfig_MissingEnvVars 测试缺少必需环境变量时的错误处理
func TestLoadConfig_MissingEnvVars(t *testing.T) {
    // 清空所有相关环境变量
    os.Unsetenv("POD_NAME")
    os.Unsetenv("POD_NAMESPACE")
    os.Unsetenv("CONFIGMAP_NAME")

    // 尝试加载配置
    _, err := LoadConfig()

    // 应该返回错误
    if err == nil {
        t.Error("Expected error when POD_NAME and POD_NAMESPACE are missing")
    }
}

// TestLoadConfig_WithEnvVars 测试正确设置环境变量时的配置加载
func TestLoadConfig_WithEnvVars(t *testing.T) {
    // 设置测试用的环境变量
    os.Setenv("POD_NAME", "test-pod-0")
    os.Setenv("POD_NAMESPACE", "default")
    os.Setenv("MODEL_PATH", "/test/models")

    // 确保测试结束后清理
    defer func() {
        os.Unsetenv("POD_NAME")
        os.Unsetenv("POD_NAMESPACE")
        os.Unsetenv("MODEL_PATH")
    }()

    // 加载配置（不设置 CONFIGMAP_NAME，避免尝试连接 Kubernetes）
    cfg, err := LoadConfig()
    if err != nil {
        t.Fatalf("LoadConfig failed: %v", err)
    }

    // 验证各个字段
    if cfg.PodName != "test-pod-0" {
        t.Errorf("got PodName=%s, want test-pod-0", cfg.PodName)
    }
    if cfg.Namespace != "default" {
        t.Errorf("got Namespace=%s, want default", cfg.Namespace)
    }
    if cfg.ModelPath != "/test/models" {
        t.Errorf("got ModelPath=%s, want /test/models", cfg.ModelPath)
    }
}

// TestAgentConfig_RoleString 测试角色字符串的输出
func TestAgentConfig_RoleString(t *testing.T) {
    tests := []struct {
        name          string
        isCoordinator bool
        expected      string
    }{
        {"Coordinator", true, "Coordinator"},
        {"Follower", false, "Follower"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := &AgentConfig{
                IsCoordinator: tt.isCoordinator,
            }

            result := cfg.RoleString()
            if result != tt.expected {
                t.Errorf("got %s, want %s", result, tt.expected)
            }
        })
    }
}