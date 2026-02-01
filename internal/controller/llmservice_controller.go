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

func (r *LLMServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.RecordReconcile("LLMService", "completed", duration)
	}()

	// 1. 从 K8s 集群获取 LLMService 对象
	//
	// 为什么要 Get？
	// - controller-runtime 只告诉我们"某个对象变化了"
	// - 但不会直接给我们对象的完整数据
	// - 我们需要主动去 API server 读取最新数据
	llmService := &aiv1.LLMService{}

	// 去k8s 查一下llmservice 这个资源
	err := r.Get(ctx, req.NamespacedName, llmService)

	if err != nil {
		// 意思是：用户已经把 CR (LLMService) 给删了。
		// 既然老板把订单都撕了，那我们就没必要干活了。
		// 直接收工 (return nil)，也不需要报错。
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// 定义我们想要什么deployment的format
	deployment := r.desiredDeployment(llmService)

	// 3. 检查集群中是否已经存在这个 Deployment
	// - 如果不存在 → 创建
	// - 如果存在 → 可能需要更新（这里简化了，没做更新）
	found := &appsv1.Deployment{}
	err = r.Get(
		ctx,
		types.NamespacedName{
			Name:      deployment.Name,
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
		llmService.Namespace).Set(float64(found.Status.ReadyReplicas))

	// 注意：Coordinator 选举现在由 Agent 通过 Lease 自己完成
	// 不再需要 Controller 调用 ensureCacheCoordinator()

	// 6. 把 Status 的更新保存到 K8s API server
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

// desiredDeployment 生成期望的 Deployment
//
// 关键点：
// 1. 运行真正的 agent（不是 mock_server.py）
// 2. 添加必要的环境变量（POD_NAME, POD_NAMESPACE, CONFIGMAP_NAME, MODEL_PATH, MODEL_REPO）
// 3. 挂载模型存储卷
func (r *LLMServiceReconciler) desiredDeployment(llm *aiv1.LLMService) *appsv1.Deployment {
	replicas := llm.Spec.Replicas

	labels := map[string]string{
		"app":    "llm-inference",
		"llm_cr": llm.Name,
	}

	// ConfigMap 名称（和 cache_coordinator.go 保持一致）
	configMapName := llm.Name + "-cache"

	return &appsv1.Deployment{
		// Meta data “data about data” 数据用来管理数据
		ObjectMeta: metav1.ObjectMeta{
			Name:      llm.Name + "-deployment",
			Namespace: llm.Namespace,
		},
		// Pod 的“Desired State”， k8s 会给一个status 目前状态
		// 外层spec deployment 的部署说明书
		Spec: appsv1.DeploymentSpec{
			// 管几个pod
			Replicas: &replicas,
			// “标识识别器” 通过label 找到归它管的pod
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			// Template 每个pod 的模版 （每个pod 长什么样子）
			Template: corev1.PodTemplateSpec{
				// Object Metadata
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				// 单个Pod 部署说明书
				Spec: corev1.PodSpec{
					// Container 容器列表
					Containers: []corev1.Container{{
						Name:            "agent",
						Image:           llm.Spec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,

						// ========================================
						// 环境变量配置
						// ========================================
						// Agent 需要这些环境变量来：
						// 1. 知道自己是谁（POD_NAME）
						// 2. 知道在哪个 namespace（POD_NAMESPACE）
						// 3. 知道去哪里找角色信息（CONFIGMAP_NAME）
						// 4. 知道模型存哪里（MODEL_PATH）
						// 5. 知道下载什么模型（MODEL_REPO）
						Env: []corev1.EnvVar{
							{
								// POD_NAME: 通过 Downward API 获取 Pod 名称
								Name: "POD_NAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.name",
									},
								},
							},
							{
								// POD_NAMESPACE: 通过 Downward API 获取 namespace
								Name: "POD_NAMESPACE",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.namespace",
									},
								},
							},
							{
								// CONFIGMAP_NAME: Agent 读取这个 ConfigMap 来判断角色
								Name:  "CONFIGMAP_NAME",
								Value: configMapName,
							},
							{
								// MODEL_PATH: 模型存储路径
								Name:  "MODEL_PATH",
								Value: "/models",
							},
							{
								// MODEL_REPO: HuggingFace 模型 ID
								// Coordinator 用这个来下载模型
								Name:  "MODEL_REPO",
								Value: llm.Spec.Model,
							},
						},

						//端口设置
						Ports: []corev1.ContainerPort{
							{
								// vLLM 推理服务端口
								Name:          "vllm",
								ContainerPort: 8000,
							},
							{
								// 模型分发 HTTP 服务端口（Coordinator 用）
								Name:          "model-server",
								ContainerPort: 8080,
							},
						},

						// 数据的（Persistence & Decoupling）， 我们的volume 该插在哪里
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "model-storage",
								MountPath: "/models",
							},
						},
					}},

					// Declare volume 外挂 模型存储， 目前是EmptyDir（空硬盘）
					Volumes: []corev1.Volume{
						{
							// EmptyDir: Pod 生命周期内的临时存储
							// 生产环境应该用 PVC （pesistent volumn claim） 永久硬盘
							// Dev 环境可以用零时存储 （Pod 重启后数据会丢失）
							Name: "model-storage",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},

					// ========================================
					// ServiceAccount
					// ========================================
					// Agent 需要权限读取 ConfigMap 和 Pod 信息
					ServiceAccountName: "kubeinfer-agent",
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
