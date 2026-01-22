## Start Everything

```bash
# Terminal 1
cd ~/kubeinfer
go run ./cmd/main.go

# Terminal 2
kubectl apply -f config/samples/llmservice-mock.yaml
kubectl get pods -l llm_cr=test-cache-llm -w
```

## Stop Everything

```bash
# Terminal 1: Ctrl+C to stop operator

# Terminal 2:
kubectl delete llmservice test-cache-llm
kubectl delete configmap test-cache-llm-cache
```

## Rebuild Mock Image

```bash
cd ~/kubeinfer/test-vllm-mock
docker build -t vllm-mock:latest .
kind load docker-image vllm-mock:latest --name kubeinfer-dev
```

```bash
# 查看 Operator 日志（如果在后台运行）
kubectl logs -f deployment/kubeinfer-controller-manager -n kubeinfer-system

# 查看 Pod 日志
kubectl logs test-cache-llm-deployment-xxxxx

# 查看 Pod 详情
kubectl describe pod test-cache-llm-deployment-xxxxx

# 查看 LLMService 详情
kubectl describe llmservice test-cache-llm

# 查看所有资源
kubectl get all -l llm_cr=test-cache-llm

# 重新生成 CRD（修改 types.go 后）
make manifests
make install
```

```bash
# 清理所有测试资源
kubectl delete llmservice test-cache-llm
kubectl delete configmap test-cache-llm-cache

# 卸载 CRD
make uninstall

# 删除 Kind 集群
kind delete cluster --name kubeinfer-dev

# 删除 Docker 镜像
docker rmi vllm-mock:latest
```

---

## 📁 重要文件路径

```
~/kubeinfer/
├── api/v1/llmservice_types.go          # CRD 定义
├── internal/controller/
│   ├── llmservice_controller.go        # 主 Controller
│   └── cache.go                        # Coordinator 逻辑
├── config/samples/
│   └── llmservice-mock.yaml            # 测试用 LLMService
├── test-vllm-mock/
│   ├── Dockerfile                      # Mock 镜像定义
│   └── mock_server.py                  # Mock 服务代码
├── cmd/main.go                         # Operator 入口
├── go.mod                              # Go 依赖
└── Makefile                            # 构建工具
```
