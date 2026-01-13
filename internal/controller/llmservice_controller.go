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
	"context"
	"time"

	// "fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1 "github.com/Moore-Z/kubeinfer/api/v1" // 确保这里和你的 go.mod 模块名一致
)

// LLMServiceReconciler reconciles a LLMService object
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

	// 1. 获取 LLMService 取出一个object 从 LLMService 我们的class 里面
	llmService := &aiv1.LLMService{}
	// NamespacedName 是我们想要的 expected nameSpaced
	err := r.Get(ctx, req.NamespacedName, llmService)
	if err != nil {
		if errors.IsNotFound(err) {
			// 对象被删除了，我们可以忽略
			return ctrl.Result{}, nil
		}
		// 读取错误，重试
		return ctrl.Result{}, err
	}

	// 2. 定义我们期望的 Deployment (vLLM)， 因为这里没有err handling， 所以assume
	deployment := r.desiredDeployment(llmService)

	// 3. 检查 Deployment 是否存在
	found := &appsv1.Deployment{}
	err = r.Get(ctx,
		types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace},
		found)

	if err != nil && errors.IsNotFound(err) {
		// A. 如果没找到 -> 创建新 Deployment
		l.Info("Creating a new Deployment", "Deployment.Namespace", deployment.Namespace, "Deployment.Name", deployment.Name)
		err = r.Create(ctx, deployment)
		if err != nil {
			l.Error(err, "Failed to create new Deployment")
			return ctrl.Result{}, err
		}
		// 创建成功，稍后重新排队以更新 Status
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		l.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// 4. (简版) 如果存在，暂时只打印日志，后续我们在这里做更新逻辑
	// 比如：如果 found.Spec.Replicas != dep.Spec.Replicas，则 Update

	// 5. 更新 Status (告诉用户现在有几个副本准备好了)
	llmService.Status.AvailableReplicas = found.Status.ReadyReplicas

	// 6. 确保Cache Coordinator（如果使用shared策略）
	if err := r.ensureCacheCoordinator(ctx, llmService); err != nil {
		l.Error(err, "Failed to Ensure cache coordinator")
		if errors.IsNotFound(err) || err.Error() == "no ready pods available for coordinator election" {
			return ctrl.Result{RequeueAfter: 2 * time.Second},nil
		}
		return ctrl.Result{}, err
	}

	if err := r.Status().Update(ctx, llmService); err != nil {
		l.Error(err, "Failed to update LLMService status")
		return ctrl.Result{}, err
	}

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
