package coordinator

import (
	"context"
	"os"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	coordinationv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	"k8s.io/klog/v2"
)

type LeaseManager struct {
	client coordinationv1client.CoordinationV1Interface // K8s client
	leaseName string																		// lease 名称
	namespace string																		// namespace
	identity  string																		// 当前 pod 的唯一标识
	leaseDuration time.Duration													// lease 有效期
	renewDuration time.Duration
	retryPeriod 	time.Duration													// 重试间隔

	mu sync.RWMutex // 读写锁，保护 isLeader 字段
	isLeader bool 	// 当前是否是 leader
}

func NewLeaseManager(clientset *kubernetes.Clientset, namespace string)(*LeaseManager, error){

	podName := os.Getenv("POD_NAME")
	if podName == ""{
		podName = "kubeinfer-operator-local"
	}
	return &LeaseManager{
		client: clientset.CoordinationV1(),
		leaseName: "kubeinfer-coordinator-lease",
		namespace: namespace,
		identity: podName,
		leaseDuration: 15*time.Second,
		renewDuration: 10*time.Second,
		retryPeriod: 	 2*time.Second,
	},nil
}

func (lm *LeaseManager) TryAcquireOrRenew(ctx context.Context)(bool, error){
	leaseClient := lm.client.Leases(lm.namespace)
	lease, err := leaseClient.Get(ctx,lm.leaseName,metav1.GetOptions{})

	// No Lease
	if err != nil {
		klog.Infof("Lease 不存在，尝试创建新的 lease")
		return lm.createLease(ctx)
	}

	// Lease 存在，检查是否由当前 pod 持有, ml.identity
	if lease.Spec.HolderIdentity!=nil && *lease.Spec.HolderIdentity == lm.identity {
		klog.V(4).Infof("当前 pod 是 coordinator,续约 lease")
		return lm.renewLease(ctx,lease)
	}
	// Lease 由其他 pod 持有，检查是否过期
	if lm.isLeaseExpired(lease){
		klog.Infof("检测到 lease 已过期，尝试获取")
		return lm.acquireLease(ctx,lease)
	}
	klog.V(4).Infof("Lease 由其他 pod 持有: %s",*lease.Spec.HolderIdentity)
	return false, nil
}

// createLease 创建新的 lease
func (lm *LeaseManager) createLease(ctx context.Context) (bool, error) {
    // 实现将在下一步添加
    leaseClient := lm.client.Leases(lm.namespace)

    now := metav1.NewMicroTime(time.Now())
    leaseDurationSeconds := int32(lm.leaseDuration.Seconds())  // ✅ 第 75 行
    holderIdentity := lm.identity

    // 构造 Lease 对象
    lease := &coordinationv1.Lease{
        ObjectMeta: metav1.ObjectMeta{
            Name:      lm.leaseName,
            Namespace: lm.namespace,
        },
        Spec: coordinationv1.LeaseSpec{
            HolderIdentity:       &holderIdentity,
            LeaseDurationSeconds: &leaseDurationSeconds,  // ✅ 第 85 行：变量名要一致
            AcquireTime:          &now,
            RenewTime:            &now,
        },
    }

    // 调用 Kubernetes API 创建 Lease
    _, err := leaseClient.Create(ctx, lease, metav1.CreateOptions{})
    if err != nil {
        // 创建失败，可能是其他 pod 同时也在创建（竞争条件）
        klog.Errorf("创建 lease 失败: %v", err)
        return false, err
    }

    klog.Infof("成功创建 lease,成为 coordinator")
    return true, nil
}

// renewLease 续约现有的 lease
func (lm *LeaseManager) renewLease(ctx context.Context, lease *coordinationv1.Lease) (bool, error) {

	leaseClient := lm.client.Leases(lm.namespace)

	now := metav1.NewMicroTime(time.Now())
	lease.Spec.RenewTime = &now
	_, err := leaseClient.Update(ctx,lease,metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("续约 lease 失败: %v", err)
		return false, err
	}
	klog.Infof("成功续约 lease")
  return true, nil
}

// acquireLease 获取过期的 lease
func (lm *LeaseManager) acquireLease(ctx context.Context, lease *coordinationv1.Lease) (bool, error) {
	// 实现将在下一步添加
	leaseClient := lm.client.Leases(lm.namespace)
	// 更新 lease 的持有者为当前 pod
	now := metav1.NewMicroTime(time.Now())
	lease.Spec.HolderIdentity = &lm.identity
	lease.Spec.AcquireTime = &now
	lease.Spec.RenewTime = &now

	// 调用 Kubernetes API 更新 Lease 对象
	// 注意：这里可能会有竞争条件，多个 pod 同时尝试抢占
	// Kubernetes 使用乐观锁（ResourceVersion）来处理这种情况
	_, err := leaseClient.Update(ctx,lease,metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Aquire Lease Failed %v", err)
		return false, err
	}
  return true, nil
}

// isLeaseExpired 检查 lease 是否过期
func (lm *LeaseManager) isLeaseExpired(lease *coordinationv1.Lease) bool {
	if lease.Spec.RenewTime == nil {
		klog.Warningf("检测到异常 Lease (名称: %s)：缺少 RenewTime 字段，可能由其他程序创建", lm.leaseName)
		return true
	}
	expirationTime := lease.Spec.RenewTime.Add(lm.leaseDuration)
	expired := time.Now().After(expirationTime)
	if expired {
		klog.V(4).Infof("Lease 已过期，上次续约时间: %v", lease.Spec.RenewTime)
	}
	return expired
}

func (lm *LeaseManager) IsCoordinator() bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.isLeader
}

func (lm *LeaseManager) updateLeaderStatus(isLeader bool) {
	lm.mu.Lock()						// 加写锁（独占访问）
	defer lm.mu.Unlock()		// 函数结束时解锁
	lm.isLeader = isLeader	// 更新状态
}

// Run 运行选举循环
func (lm *LeaseManager) Run(ctx context.Context, onElected, onLost func()) {
    klog.Info("LeaseManager 开始运行")

    // 创建定时器
    ticker := time.NewTicker(lm.retryPeriod)
    defer ticker.Stop()  // 函数退出时停止定时器

    // 主循环
    for {
        select {
        case <-ticker.C:
            // 定时器触发：尝试获取或续约 lease
            acquired, err := lm.TryAcquireOrRenew(ctx)
            if err != nil {
                klog.Errorf("选举操作失败: %v", err)

                // 更新状态为 follower
                lm.updateLeaderStatus(false)
                continue
            }

            // 检查状态是否发生变化
            wasLeader := lm.IsCoordinator()  // 之前的状态

            if acquired && !wasLeader {
                // 状态变化：follower → coordinator
                klog.Info("角色变化: Follower → Coordinator")
                lm.updateLeaderStatus(true)   // 更新状态
                if onElected != nil {
                    onElected()  // 调用回调函数
                }
            } else if !acquired && wasLeader {
                // 状态变化：coordinator → follower
                klog.Info("角色变化: Coordinator → Follower")
                lm.updateLeaderStatus(false)  // 更新状态
                if onLost != nil {
                    onLost()  // 调用回调函数
                }
            }

        case <-ctx.Done():
            // context 被取消（程序退出）
            klog.Info("收到退出信号,LeaseManager 停止运行")

            // 如果当前是 coordinator，调用 onLost
            if lm.IsCoordinator() {
                klog.Info("清理 Coordinator 角色")
                lm.updateLeaderStatus(false)
                if onLost != nil {
                    onLost()
                }
            }
            return
        }
    }
}