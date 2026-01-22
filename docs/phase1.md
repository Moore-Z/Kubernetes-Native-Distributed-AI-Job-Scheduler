kubeinfer/
├── cmd/
│ └── agent/
│ └── main.go # Agent 入口 (运行在 Pod 中)
├── internal/
│ ├── coordinator/
│ │ ├── coordinator.go # Coordinator 逻辑
│ │ └── model_server.go # HTTP 模型服务
│ ├── follower/
│ │ ├── follower.go # Follower 逻辑
│ │ └── model_fetcher.go # 模型拉取器
│ └── config/
│ └── config.go # 配置读取
└── api/v1alpha1/
└── vlldeploy_types.go # CRD 定义
