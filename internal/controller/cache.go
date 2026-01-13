package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1 "github.com/Moore-Z/kubeinfer/api/v1"
)

func (r *LLMServiceReconciler) ensureCacheCoordinator(
	ctx context.Context,
	llm *aiv1.LLMService,
) error {
	log := log.FromContext(ctx)

	if llm.Spec.CacheStrategy != "shared" {
		return nil
	}
	// Configure Map Name (recording model deployment for all pods because pods cannot communicate with each other)
	cmName := llm.Name + "-cache"
	// Get real config map from k8s library
	cm := &corev1.ConfigMap{}

	err := r.Get(ctx, types.NamespacedName{
		Name:      cmName,
		Namespace: llm.Namespace,
	}, cm)

	// Error handling
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("ConfigMap is not found, will create and elect coordinator")
			return r.createCoordinatorConfigMap(ctx, llm)
		}
		return err
	}

	// If Coordinator-prod not exist, we will create one
	coordinationPodName := cm.Data["coordinator-pod"]
	if coordinationPodName == "" {
		log.Info("No coordinator elected, electing now")
		return r.electCoordinator(ctx, llm, cm)
	}

	// Create one pod object to take response
	pod := &corev1.Pod{}

	err = r.Get(ctx, types.NamespacedName{
		Name:      coordinationPodName,
		Namespace: llm.Namespace,
	}, pod)

	// If we got error or pod is not ready
	if err != nil || !r.isPodReady(pod) {
		log.Info("Coordinator pod not found or not ready, re-electing",
			"old-coordinator", coordinationPodName)

		return r.electCoordinator(ctx, llm, cm)
	}
	return r.updateCoordinatorHeartbeat(ctx, cm)
}

func (r *LLMServiceReconciler) createCoordinatorConfigMap(
	ctx context.Context,
	llm *aiv1.LLMService,
) error {
	log := log.FromContext(ctx)
	pods, err := r.getPodsForLLMService(ctx, llm)
	if err != nil {
		return err
	}

	var coordinatorPod *corev1.Pod
	for i := range pods.Items {
		if r.isPodReady(&pods.Items[i]) {
			coordinatorPod = &pods.Items[i]
			break
		}
	}
	// ========================================
	// 步骤3：如果没有Ready的Pod，暂时返回
	// ========================================
	// Go语法：== nil 检查指针是否为空
	if coordinatorPod == nil {
		log.Info("No ready Pod yet, will retry later")
		return fmt.Errorf("no ready pods available for coordinator election")
	}
	// ========================================
	// 步骤4：构造ConfigMap对象
	// ========================================
	// Go语法：&corev1.ConfigMap{...} 创建ConfigMap并返回指针
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      llm.Name + "-cache",
			Namespace: llm.Namespace,
			// Labels 是一个map，用于资源的筛选和组织
			Labels: map[string]string{
				"app":     "llm-inference",
				"llm_cr":  llm.Name,
				"purpose": "cache-coordination",
			},
		},
		// Data 存储ConfigMap的实际数据
		// 这里记录coordinator的信息
		Data: map[string]string{
			"coordinator-pod": coordinatorPod.Name,
			"coordinator-ip":  coordinatorPod.Status.PodIP,
			"model-name":      llm.Spec.Model,
			"cache-strategy":  "shared",
			// time.Now() 获取当前时间
			// Format() 格式化时间为字符串
			// RFC3339 是标准的时间格式: "2006-01-02T15:04:05Z07:00"
			"last-heartbeat": time.Now().Format(time.RFC3339),
		},
	}
	// ========================================
	// 步骤5：记录日志
	// ========================================
	// Info() 可以接受多个参数，成对出现：key, value, key, value...
	log.Info("Creater ConfigMap with coordinator",
		"configmap", cm.Name,
		"coordinator", coordinatorPod.Name,
	)

	// ========================================
	// 步骤6：创建ConfigMap到K8s
	// ========================================
	err = r.Create(ctx, cm)
	if err != nil {
		return err
	}
	// ========================================
	// 步骤7：更新LLMService的status
	// ========================================
	// 记录当前coordinator是谁，方便用户查看
	llm.Status.CacheCoordinator = coordinatorPod.Name
	// r.Status().Update() 专门用于更新资源的status字段
	return r.Status().Update(ctx, llm)
}

func (r *LLMServiceReconciler) getPodsForLLMService(
	ctx context.Context, llm *aiv1.LLMService) (*corev1.PodList, error) {
	// ========================================
	// 步骤1：构造Deployment名称
	// ========================================
	// 这个名称必须和desiredDeployment()中设置的一致
	deploymentName := llm.Name + "-deployment"

	// 创建一个空的Deployment对象，准备接收查询结果
	deployment := &appsv1.Deployment{}
	// 查询Deployment
	err := r.Get(ctx, types.NamespacedName{
		Name:      deploymentName,
		Namespace: llm.Namespace,
	}, deployment)
	// 如果Deployment不存在或查询失败，返回错误
	if err != nil {
		// 1: Deployment 还没创建（正常)
		// 2: Deployment 被意外删除（异常）
		// 3: API Server 故障（临时错误）
		if errors.IsNotFound(err) {
			return &corev1.PodList{}, err
		}
		// Other errors like (timeout, network)
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// ========================================
	// 步骤2：构造label selector
	// ========================================
	// 我们在desiredDeployment()中给Pods打了这些labels
	labelSelector := map[string]string{
		"app":    "llm-inference",
		"llm_cr": llm.Name,
	}

	// ========================================
	// 步骤3：查询Pods
	// ========================================
	// 创建空的PodList对象
	podList := &corev1.PodList{}
	// client.InNamespace() 限制在特定namespace
	// client.MatchingLabels() 按label筛选
	err = r.List(
		ctx, podList,
		client.InNamespace(llm.Namespace),
		client.MatchingLabels(labelSelector),
	)
	return podList, err
}

func (r *LLMServiceReconciler) electCoordinator(
	ctx context.Context,
	llm *aiv1.LLMService,
	cm *corev1.ConfigMap) error {
	log := log.FromContext(ctx)
	// ========================================
	// 步骤1：获取所有Pods
	// ========================================
	pods, err := r.getPodsForLLMService(ctx, llm)
	if err != nil {
		return err
	}
	// ========================================
	// 步骤2：选择最早创建的Ready Pod作为新coordinator
	// ========================================
	// 为什么选最早的？因为它最可能已经下载好了模型

	var coordinator *corev1.Pod
	for i := range pods.Items {
		if r.isPodReady(&pods.Items[i]) {
			// Go语法：if coordinator == nil 表示"如果这是第一个Ready的Pod"
			// || 是"或"运算符
			// Before() 比较时间戳，判断哪个Pod更早创建
			if coordinator == nil || pods.Items[i].CreationTimestamp.Before(&coordinator.CreationTimestamp) {
				coordinator = &pods.Items[i]
			}
		}
	}
	if coordinator == nil {
		log.Info("No ready pods available for coordinator election")
		return nil
	}
	// ========================================
	// 步骤3：更新ConfigMap的Data字段
	// ========================================
	// 注意：这里不是创建新ConfigMap，而是修改existing ConfigMap
	cm.Data["coordinator-pod"] = coordinator.Name
	cm.Data["coordinator-ip"] = coordinator.Status.PodIP
	cm.Data["last-heartbeat"] = time.Now().Format(time.RFC3339)

	log.Info("Elected new coordinator",
		"coordinator", coordinator.Name,
		"ip", coordinator.Status.PodIP)

	// ========================================
	// 步骤4：更新ConfigMap到K8s
	// ========================================
	err = r.Update(ctx, cm)
	if err != nil {
		return err
	}

	// ========================================
	// 步骤5：更新LLMService status
	// ========================================
	llm.Status.CacheCoordinator = coordinator.Name
	return r.Status().Update(ctx, llm)
}

// ========================================
// 函数：isPodReady
// ========================================
// 功能：检查一个Pod是否处于Ready状态
//
// K8s知识：Pod有多种Condition（状态条件）
//   - PodScheduled: Pod已被调度到节点
//   - ContainersReady: 所有容器都Ready
//   - Ready: Pod可以接收流量
//
// 我们关心的是Ready状态
func (r *LLMServiceReconciler) isPodReady(pod *corev1.Pod) bool {
	// ========================================
	// 遍历Pod的所有Conditions
	// ========================================
	// Go语法：for _, condition := range ...
	// _ 表示忽略索引，我们只关心condition本身

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady &&
			condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *LLMServiceReconciler) updateCoordinatorHeartbeat(
	ctx context.Context,
	cm *corev1.ConfigMap) error {
	cm.Data["last-heartbeat"] = time.Now().Format(time.RFC3339)
	return r.Update(ctx, cm)
}
