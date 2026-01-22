// Package metrics 提供 Prometheus 指标
//
// 这个包的作用：
// 1. 定义所有的 Prometheus 指标（Gauge, Counter, Histogram）
// 2. 自动注册到 controller-runtime 的 metrics registry
// 3. 提供便捷的记录函数，简化其他包的调用
//
// Prometheus 是什么？
// - 开源的监控和告警系统
// - 通过 HTTP 接口暴露 /metrics 端点
// - Grafana 可以读取这些数据并可视化
package metrics

import (
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics" // ← 改这里，加一个别名

	"github.com/prometheus/client_golang/prometheus"
)

// ============================================================
// 第一部分：定义 Metrics 变量
// ============================================================
// 为什么用 var？
// - 这些是包级别的全局变量，整个程序生命周期存在
// - 不同的 goroutine 都可以安全地记录数据（Prometheus 保证线程安全）

var (
	LLMServiceTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kubeinfer_llmservice_total",  // Metric 名称（必须唯一）
			Help: "Total number of LLMServices", // 描述（会显示在 Prometheus UI）
		},
	)
	/*
		// 用途：记录每个 LLMService 有多少个 Ready 的 Pod
		// 类型选择：GaugeVec，因为：
		//   1. 值会变化（Gauge）
		//   2. 需要区分不同的 LLMService（Vec = Vector = 多个实例）
		//
		// 标签 (Labels) 的作用：
		// - 就像数据库的"索引"，用于区分不同的时间序列
		// - 例子：
		//     llmservice{namespace="default", name="llama2"} = 3
		//     llmservice{namespace="default", name="mistral"} = 2
		//     llmservice{namespace="prod", name="llama2"} = 5
	*/
	LLMServiceReadyReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubeinfer_llmservice_ready_replicas",
			Help: "Number of ready replicas per LLMService",
		},
		[]string{"namespace", "name"}, // 定义标签的 key
	)
	/*
		// 用途：记录每个 LLMService 的 Coordinator 选举了多少次
		// 类型选择：Counter，因为：
		//   - 选举次数只会增加，不会减少
		//   - 我们关心"总共选举了多少次"（累积值）
		//
		// 为什么这个指标重要？
		// - 频繁选举 = 系统不稳定（Pod 经常挂）
		// - 正常情况：启动时选举 1 次，之后很少变化
		// - 异常情况：每分钟选举好几次 → 需要调查 Pod 为什么总是挂掉
	*/
	CoordinatorElections = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeinfer_coordinator_elections_total",
			Help: "Total number of coordinator elections",
		},
		[]string{"namespace", "name"},
	)
	/*
		// ModelDownloadDuration 是一个 HistogramVec (直方图)
		//
		// 用途：记录模型下载耗时的分布
		// 类型选择：Histogram，因为：
		//   - 我们不只关心"平均"耗时
		//   - 还要知道分布：50% 在 1 分钟内？95% 在 5 分钟内？
		//
		// Buckets 是什么？
		// - 把时间分成不同的"桶"
		// - ExponentialBuckets(10, 2, 10) 生成：
		//     10s, 20s, 40s, 80s, 160s, 320s, 640s, 1280s, 2560s, 5120s
		// - Prometheus 会统计有多少次落在每个桶里
		//
		// 为什么用指数增长？
		// - 小模型：几十秒就下完，需要细粒度的桶（10s, 20s, 40s）
		// - 大模型：可能几小时，需要大桶（2560s = 42分钟）
		//
		// 标签 status 的作用：
		// - "success": 下载成功的耗时分布
		// - "failed": 下载失败的耗时分布
		// - 可以单独查看成功/失败的情况
	*/
	ModelDownloadDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubeinfer_model_download_duration_seconds",
			Help:    "Time taken to download models",
			Buckets: prometheus.ExponentialBuckets(10, 2, 10),
		},
		[]string{"model_name", "status"},
	)
	/*
		// ReconcileTotal 是一个 CounterVec
		// 用途：记录 Controller reconcile 的总次数
		//
		// 什么是 Reconcile？
		// - Controller 的核心循环：检查实际状态 vs 期望状态
		// - 每次用户修改 LLMService、Pod 状态变化，都会触发
		//
		// 为什么记录这个？
		// - 可以看出系统负载：每秒 reconcile 多少次？
		// - 异常检测：reconcile 次数突然激增 → 可能有问题
		//
		// 标签 result 的作用：
		// - "success": reconcile 成功
		// - "error": reconcile 出错
		// - 计算错误率：error / (success + error)
	*/
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubeinfer_reconcile_total",
			Help: "Total number of reconciliations",
		},
		[]string{"controller", "result"},
	)
	/*
		// ReconcileDuration 是一个 HistogramVec
		//
		// 用途：记录每次 reconcile 的耗时分布
		//
		// 为什么重要？
		// - Reconcile 太慢 → 系统响应慢，用户等待时间长
		// - 可以设置告警：如果 P95 耗时 > 1s，发送告警
		//
		// Buckets 使用默认值：
		// - prometheus.DefBuckets = [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
		// - 单位是秒，覆盖了 5ms 到 10s
	*/
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{Name: "kubeinfer_reconcile_duration_seconds",
			Help:    "Time spent in reconciliation",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller"},
	)
)

/*
// ============================================================
// 第二部分：注册 Metrics
// ============================================================
// init() 函数会在包被导入时自动执行
//
// 为什么在 init() 中注册？
// - 保证在任何代码使用 metrics 之前，它们已经注册好了
// - 只会执行一次，避免重复注册导致 panic
*/
func init() {

	ctrlmetrics.Registry.MustRegister(
		LLMServiceTotal,
		LLMServiceReadyReplicas,
		CoordinatorElections,
		ModelDownloadDuration,
		ReconcileTotal,
		ReconcileDuration,
	)
}

/*
// ============================================================
// 第三部分：便捷函数
// ============================================================
// 为什么要提供这些函数？
// - 简化调用：不需要在业务代码中写 .WithLabelValues(...).Inc()
// - 统一接口：如果将来要改 metric 实现，只需要改这里
// - 更好的代码可读性

// RecordReconcile 记录一次 reconcile 操作
//
// 参数：
//   - controller: 哪个 controller（例如 "LLMService"）
//   - result: 结果（"success" 或 "error"）
//   - duration: 耗时（秒）
//
// 这个函数做了什么？
// 1. 增加 reconcile 的计数（Counter）
// 2. 记录耗时到直方图（Histogram）
//
// 使用例子：
//   startTime := time.Now()
//   // ... 执行 reconcile 逻辑 ...
//   duration := time.Since(startTime).Seconds()
//   metrics.RecordReconcile("LLMService", "success", duration)
*/

func RecordReconcile(controller, result string, duration float64) {
	ReconcileTotal.WithLabelValues(controller, result).Inc()
	ReconcileDuration.WithLabelValues(controller).Observe(duration)
}

/*
// RecordModelDownload 记录模型下载事件
//
// 参数：
//   - modelName: 模型名称（例如 "meta-llama/Llama-2-7b"）
//   - status: "success" 或 "failed"
//   - duration: 下载耗时（秒）
//
// 为什么单独记录下载？
// - 模型下载是最耗时的操作，需要重点关注
// - 可以分析：哪些模型下载最慢？失败率多高？
*/

func RecordModelDownload(controller, status string, duration float64) {
	ModelDownloadDuration.WithLabelValues(controller, status).Observe(duration)
}

/*
// RecordCoordinatorElection 记录 Coordinator 选举事件
//
// 参数：
//   - namespace: 资源所在的 namespace
//   - name: LLMService 的名称
//
// 什么时候调用？
// - 每次选出新的 Coordinator 时
//
// 为什么重要？
// - 选举频繁 = Pod 不稳定
// - 可以设置告警：1 小时内选举超过 3 次 → 告警
*/
func RecordCoordinatorElection(controller, name string) {
	CoordinatorElections.WithLabelValues(controller, name).Inc()
}
