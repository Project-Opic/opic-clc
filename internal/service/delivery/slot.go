package delivery

import (
	"context"
	"sync"
)

// Slots 全局执行槽位(REF-14 缺省 2;R-18:占满 ⇒ 新任务排队 QUEUED 而不是失败)。
type Slots struct {
	ch chan struct{}
}

// NewSlots 容量 n(<=0 视为 1)。
func NewSlots(n int) *Slots {
	if n <= 0 {
		n = 1
	}
	return &Slots{ch: make(chan struct{}, n)}
}

// TryAcquire 非抢占尝试(失败 ⇒ 保持 QUEUED 排队)。
func (s *Slots) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Acquire 阻塞至取得槽位(ctx 取消则失败)。
func (s *Slots) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 释放槽位(未持有时安全空转)。
func (s *Slots) Release() {
	select {
	case <-s.ch:
	default:
	}
}

// ClusterLock 同集群互斥(INV-48:同一集群至多一个运行中交付任务;进程内快速道,
// 提交时的 DB 强校验兜底见服务层)。
type ClusterLock struct {
	mu sync.Mutex
	m  map[string]string
}

// NewClusterLock 构造。
func NewClusterLock() *ClusterLock {
	return &ClusterLock{m: map[string]string{}}
}

// TryLock 取得集群执行权;失败返回当前占用任务号。
func (l *ClusterLock) TryLock(clusterNo, taskNo string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, ok := l.m[clusterNo]; ok && cur != taskNo {
		return false, cur
	}
	l.m[clusterNo] = taskNo
	return true, ""
}

// Unlock 释放(仅持有者可释放)。
func (l *ClusterLock) Unlock(clusterNo, taskNo string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, ok := l.m[clusterNo]; ok && cur == taskNo {
		delete(l.m, clusterNo)
	}
}

// Holder 当前占用任务号(对账/诊断用)。
func (l *ClusterLock) Holder(clusterNo string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.m[clusterNo]
	return cur, ok
}
