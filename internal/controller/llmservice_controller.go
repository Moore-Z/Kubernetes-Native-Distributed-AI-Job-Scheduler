/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context" //Go 标准库： 用于传递上下文关系（超时，取消）
	"time"    //Go 标准库： 处理时间相关的操作（计时，延迟）

	// Kubernetes 核心API
	appsv1 "k8s.io/api/apps/v1" //Deployment， StatefulSet 等工作负载类型
	corev1 "k8s.io/api/core/v1" // Pod，Service， ConfigMap 等核心资源类型

	// "k8s.io/apiserver/pkg/endpoints/request"

	// Kubernetes API 辅助库
	"k8s.io/apimachinery/pkg/api/errors"          // error
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // k8s 元数据类型（ObjectMeta， Time等）
	"k8s.io/apimachinery/pkg/runtime"             // k8s 运行时类型系统（schema）
	"k8s.io/apimachinery/pkg/types"               // Namespace type

	// Controller-runtime 库 （KubeBuilder 的底层框架）
	ctrl "sigs.k8s.io/controller-runtime"       // Controller 管理器， Reconciler 接口
	"sigs.k8s.io/controller-runtime/pkg/client" //K8S client 接口（CRUD）
	"sigs.k8s.io/controller-runtime/pkg/log"    // 结构化日志工具

	// 本地代码项目
	aiv1 "github.com/Moore-Z/kubeinfer/api/v1"
	"github.com/Moore-Z/kubeinfer/pkg/metrics" // ← 新增这一行
)

/*
// LLMServiceReconciler 负责调和(reconcile) LLMService 自定义资源,管理 vLLM 模型服务部署的生命周期。
// 它实现了 controller-runtime 的 reconciler 接口,监听 LLMService 对象的变化,
// 并确保集群的实际状态与自定义资源中定义的期望状态一致。
//
// 这个 reconciler 负责处理:
// - 创建和更新 vLLM StatefulSet,使用协调者-跟随者(coordinator-follower)架构
// - 管理模型下载和 pod 之间的缓存协调
// - 配置 Service 以便外部访问 vLLM 推理端点
// - 使用 Kubernetes Lease 对象实现 leader 选举
// - 暴露 Prometheus 指标以便监控
//
// 字段说明:
// - Client: Kubernetes 客户端,用于与 API server 交互,读写资源
// - Scheme: 运行时 scheme,包含这个 reconciler 处理的类型信息,
//           支持类型转换,确保 reconciler 能处理 LLMService CRD
*/
type LLMServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// 下面这几行注释非常重要！它们是 RBAC 权限声明。
// Kubebuilder 会根据这些注释自动生成 ServiceAccount 的权限。
//+kubebuilder:rbac:groups=ai.ruijie.io,resources=llmservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ai.ruijie.io,resources=llmservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ai.ruijie.io,resources=llmservices/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

func (r *LLMServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request)(ctrl.Result, error){
	l := log.FromContext(ctx)
	startTime := time.Now()

	defer func(){
		duration := time.Since(startTime).Seconds()

		metrics.RecordReconcile("LLMService","completed",duration)
	}()

	// 1. 从 K8s 集群获取 LLMService 对象
	//
	// 为什么要 Get？
	// - controller-runtime 只告诉我们"某个对象变化了"
	// - 但不会直接给我们对象的完整数据
	// - 我们需要主动去 API server 读取最新数据
	llmService := &aiv1.LLMService{}   // 创建空对象用于接收数据
	err := r.Get(ctx, req.NamespacedName, llmService)
	if err != nil {
		// 如果对象已被删除，返回 nil 表示成功
		// controller-runtime 会自动停止对这个对象的 wat
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// 其他错误（网络问题、权限问题等）返回 error
		// controller-runtime 会自动重试
		return ctrl.Result{}, err
	}
		// 2. 根据 LLMService 的配置，生成期望的 Deployment 定义
	//
	// desiredDeployment() 是你写的函数，它会：
	// - 读取 llmService.Spec（用户期望）
	// - 生成对应的 Deployment YAML
	// - 包括：镜像、副本数、环境变量等
	deployment := r.desiredDeployment(llmService)

	// 3. 检查集群中是否已经存在这个 Deployment
	//
	// 为什么要检查？
	// - 如果不存在 → 创建
	// - 如果存在 → 可能需要更新（这里简化了，没做更新）
	found := &appsv1.Deployment{}
	err = r.Get(
		ctx,
		types.NamespacedName{
			Name : deployment.Name,
			Namespace: deployment.Namespace,
		},
		found)

		// Error handling
	if err != nil && errors.IsNotFound(err) {
		// 情况 1：Deployment 不存在 → 创建新的

		l.Info("Creating a new Deployment",
			"Deployment.Namespace", deployment.Namespace,
			"Deployment.Name", deployment.Name)

		// 调用 K8s API 创建 Deployment
		err = r.Create(ctx, deployment)
		if err != nil {
			l.Error(err, "Failed to create new Deployment")
			return ctrl.Result{}, err
		}

		// 返回 Requeue: true
		// 告诉 controller-runtime：创建成功，但立即再调用一次 Reconcile
		// 为什么？因为创建后需要检查 Pod 是否 Ready
		return ctrl.Result{Requeue: true}, nil

	} else if err != nil {
		// 情况 2：查询出错（不是 NotFound，而是网络错误等）
		l.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	/*
	// 情况 3：Deployment 已存在，found 对象包含了它的最新状态

	// 5. 更新 LLMService 的 Status 字段
	//
	// Status vs Spec：
	// - Spec: 用户期望的状态（用户填写的）
	// - Status: 实际的运行状态（controller 更新的）
	//
	// ReadyReplicas：有多少个 Pod 处于 Ready 状态
	// 用户可以通过 kubectl get llmservice 看到这个数字
	*/
	llmService.Status.AvailableReplicas = found.Status.ReadyReplicas

	metrics.LLMServiceReadyReplicas.WithLabelValues(
		llmService.Name,
		llmService.Namespace,).Set(float64(found.Status.ReadyReplicas))

	// 6. 确保 Cache Coordinator 被选举和维护
	//
	// ensureCacheCoordinator() 的职责：
	// - 检查是否有 coordinator
	// - 如果没有或者挂了，重新选举
	// - 更新 ConfigMap 记录 coordinator 信息
	if err := r.ensureCacheCoordinator(ctx,llmService); err != nil {
		l.Error(err, "Failed to Ensure cache coordinator")

		// 特殊错误处理：
		// - NotFound: ConfigMap 还不存在（第一次运行）
		// - "no ready pods": 还没有 Ready 的 Pod
		//
		// 这两种情况是正常的，只需要等待 2 秒后重试
		if errors.IsNotFound(err) || err.Error() == "no ready pods available for coordinator election" {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}

		// 其他错误直接返回
		return ctrl.Result{}, err
	}
	// 7. 把 Status 的更新保存到 K8s API server
	//
	// 为什么单独调用 Status().Update()？
	// - K8s 把 Spec 和 Status 分开管理
	// - 普通用户只能改 Spec，不能改 Status
	// - Controller 通过 Status().Update() 更新 Status
	// - 这样可以防止用户手动改 Status 造成混乱
	if err := r.Status().Update(ctx, llmService); err != nil {
		l.Error(err, "Failed to update LLMService status")
		return ctrl.Result{}, err
	}
	// 8. 全部成功，返回空结果
	//
	// ctrl.Result{}: 表示这次 reconcile 成功完成
	// - 如果对象有新的变化，controller-runtime 会自动再调用
	// - 不需要我们主动 requeue
	return ctrl.Result{}, nil
}

// 如果 * 右边是 大写字母开头的类型 (e.g., *Deployment) -> 它是 “指针类型” (名词)。
// 如果 * 右边是 小写字母的变量名 (e.g., *llm) -> 它是 “取值/解引用” (动词)。
// 这就是写了一个deployment的 metadata
func (r *LLMServiceReconciler) desiredDeployment(llm *aiv1.LLMService) *appsv1.Deployment {

	replicas := llm.Spec.Replicas

	// 语法点 2: map[key类型]value类型 {...}
	// 定义一组标签，这是 K8s 里 Pod 和 Service 互相识别的“暗号”
	labels := map[string]string{
		"app":    "llm-inference",
		"llm_cr": llm.Name, // 标记这个 Pod 属于哪个 LLMService
	}

	// 语法点 3: &appsv1.Deployment{...}
	// 相当于 Java: return new Deployment().setMetadata(...).setSpec(...)
	// 这里直接返回这个对象的“地址”（指针）
	return &appsv1.Deployment{

		// --- 元数据 (Metadata) ---
		ObjectMeta: metav1.ObjectMeta{
			// 名字不能冲突，所以通常用 "CR名-后缀" 的格式
			Name:      llm.Name + "-deployment",
			Namespace: llm.Namespace,
		},

		// --- 规格 (Spec) ---
		Spec: appsv1.DeploymentSpec{
			// 语法点 4: 取地址符 &
			// K8s 要求 Replicas 必须传指针 (*int32)，而不是值。
			// 为什么？因为指针可以是 nil（表示没填，用默认值），但 int 只能是 0。
			Replicas: &replicas,

			// 选择器：告诉 Deployment "你要管理哪些 Pod"
			// 这里必须和下面的 Template.Labels 完全一致
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},

			// --- Pod 模板 (Template) ---
			// 以后扩容出来的每一个 Pod 都长这样
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels, // 给 Pod 打标签
				},
				Spec: corev1.PodSpec{
					// 容器数组 (Containers list)
					Containers: []corev1.Container{{
						Name:  "vllm",
						Image: llm.Spec.Image,
						ImagePullPolicy:  corev1.PullIfNotPresent,

						// Env variable
						Env : []corev1.EnvVar{
							{
								Name: "POD_NAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.name",
									},
								},
							},
						},

						Command: []string{
							"python", "mock_server.py",
						},

						// 启动命令：把用户的 Model ID 注入进去
						// 相当于: python3 -m vllm... --model deepseek-ai/deepseek-r1
						// Command: []string{
						// 	"python3", "-m", "vllm.entrypoints.openai.api_server",
						// 	"--model", llm.Spec.Model,
						// },

						// 端口暴露
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8000,
							Name:          "http",
						}},

						// 资源限制 (Resources)
						// Resources: corev1.ResourceRequirements{
						// 	Limits: corev1.ResourceList{
						// 		// 内存限制：暂时写死 2Gi，后面我们会改成动态的
						// 		corev1.ResourceMemory: resource.MustParse("2Gi"),
						// 	},
						// },
					}},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *LLMServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1.LLMService{}).
		Owns(&appsv1.Deployment{}). // 监听 Deployment，如果 Deployment 被误删，Controller 会自动感知
		Complete(r)
}
