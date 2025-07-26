package test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhoudm1743/go-cache"
)

// TestRedLock 测试Redlock分布式锁
func TestRedLock(t *testing.T) {
	// 创建多个内存缓存作为模拟Redis实例
	var instances []cache.Cache
	instanceCount := 3

	for i := 0; i < instanceCount; i++ {
		config := cache.NewMemoryConfig()
		config.Prefix = ""
		memCache, err := cache.NewMemoryCache(config, nil)
		if err != nil {
			t.Fatalf("创建内存缓存失败: %v", err)
		}
		defer memCache.Close()
		instances = append(instances, memCache)
	}

	// 创建Redlock实例
	config := &cache.RedLockConfig{
		RetryCount: 3,
		RetryDelay: 100 * time.Millisecond,
		ClockDrift: 0.01,
	}

	redlock := cache.NewRedLock(instances, config, nil)

	// 测试基本锁定和解锁
	testLockKey := "test:lock:1"
	locked, err := redlock.Lock(testLockKey, 5*time.Second)
	if err != nil {
		t.Fatalf("获取锁失败: %v", err)
	}

	if !locked {
		t.Fatal("应该成功获取锁")
	}

	// 再次尝试获取同一个锁，应该失败
	locked, err = redlock.Lock(testLockKey, 5*time.Second)
	if err != nil {
		t.Logf("重复获取锁失败(预期行为): %v", err)
	}

	if locked {
		t.Error("重复获取锁应该失败")
	}

	// 释放锁
	err = redlock.Unlock(testLockKey)
	if err != nil {
		t.Fatalf("释放锁失败: %v", err)
	}

	// 释放后应该可以再次获取锁
	locked, err = redlock.Lock(testLockKey, 5*time.Second)
	if err != nil {
		t.Fatalf("释放后重新获取锁失败: %v", err)
	}

	if !locked {
		t.Error("释放后应该能重新获取锁")
	}

	// 最后释放锁
	redlock.Unlock(testLockKey)
}

// TestRedLockConcurrent 测试并发情况下的Redlock
func TestRedLockConcurrent(t *testing.T) {
	// 创建多个内存缓存作为模拟Redis实例
	var instances []cache.Cache
	instanceCount := 3

	for i := 0; i < instanceCount; i++ {
		config := cache.NewMemoryConfig()
		config.Prefix = ""
		memCache, err := cache.NewMemoryCache(config, nil)
		if err != nil {
			t.Fatalf("创建内存缓存失败: %v", err)
		}
		defer memCache.Close()
		instances = append(instances, memCache)
	}

	// 创建Redlock实例
	redlock := cache.NewRedLock(instances, nil, nil)

	// 共享计数器和锁键
	var counter int64
	lockKey := "test:concurrent:lock"

	// 并发工作线程数
	workerCount := 10
	lockDuration := 500 * time.Millisecond

	// 等待所有工作线程完成
	var wg sync.WaitGroup
	wg.Add(workerCount)

	// 记录成功和失败的锁定尝试
	var lockSuccess int64
	var lockFailure int64

	// 启动多个工作线程竞争锁
	for i := 0; i < workerCount; i++ {
		go func(id int) {
			defer wg.Done()

			// 每个工作线程尝试获取锁多次
			for j := 0; j < 5; j++ {
				// 使用WithLock确保锁的获取和释放是原子的
				err := redlock.WithLock(lockKey, lockDuration, func() error {
					// 成功获取锁
					atomic.AddInt64(&lockSuccess, 1)

					// 模拟临界区操作
					currentValue := atomic.LoadInt64(&counter)

					// 模拟一些处理时间
					time.Sleep(10 * time.Millisecond)

					// 增加计数器
					atomic.StoreInt64(&counter, currentValue+1)

					return nil
				})

				if err != nil {
					atomic.AddInt64(&lockFailure, 1)
					t.Logf("工作线程 %d: 锁定失败: %v", id, err)
				}

				// 等待一段时间再次尝试
				time.Sleep(20 * time.Millisecond)
			}
		}(i)
	}

	// 等待所有工作线程完成
	wg.Wait()

	// 检查结果
	t.Logf("并发测试结果: 成功锁定=%d, 锁定失败=%d, 计数器最终值=%d",
		atomic.LoadInt64(&lockSuccess), atomic.LoadInt64(&lockFailure), atomic.LoadInt64(&counter))

	// 计数器的值应该等于成功锁定的次数
	if atomic.LoadInt64(&counter) != atomic.LoadInt64(&lockSuccess) {
		t.Errorf("计数器值(%d)不等于成功锁定次数(%d)，这表明锁保护失效",
			atomic.LoadInt64(&counter), atomic.LoadInt64(&lockSuccess))
	}
}

// TestRedLockWithLock 测试WithLock辅助函数
func TestRedLockWithLock(t *testing.T) {
	// 创建多个内存缓存作为模拟Redis实例
	var instances []cache.Cache
	instanceCount := 3

	for i := 0; i < instanceCount; i++ {
		config := cache.NewMemoryConfig()
		config.Prefix = ""
		memCache, err := cache.NewMemoryCache(config, nil)
		if err != nil {
			t.Fatalf("创建内存缓存失败: %v", err)
		}
		defer memCache.Close()
		instances = append(instances, memCache)
	}

	// 创建Redlock实例
	redlock := cache.NewRedLock(instances, nil, nil)

	// 测试WithLock函数
	var counter int64
	lockKey := "test:withlock"

	// 使用WithLock执行函数
	err := redlock.WithLock(lockKey, 5*time.Second, func() error {
		// 模拟临界区操作
		atomic.AddInt64(&counter, 1)
		return nil
	})

	if err != nil {
		t.Fatalf("WithLock执行失败: %v", err)
	}

	if atomic.LoadInt64(&counter) != 1 {
		t.Errorf("计数器值不正确，期望 1，实际 %d", atomic.LoadInt64(&counter))
	}

	// 测试WithLockCtx函数
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = redlock.WithLockCtx(ctx, lockKey, 5*time.Second, func(c context.Context) error {
		// 检查上下文是否被传递
		if c != ctx {
			t.Error("上下文没有被正确传递")
		}

		// 模拟临界区操作
		atomic.AddInt64(&counter, 1)
		return nil
	})

	if err != nil {
		t.Fatalf("WithLockCtx执行失败: %v", err)
	}

	if atomic.LoadInt64(&counter) != 2 {
		t.Errorf("计数器值不正确，期望 2，实际 %d", atomic.LoadInt64(&counter))
	}
}

// TestRedLockExtend 测试锁扩展功能
func TestRedLockExtend(t *testing.T) {
	// 创建多个内存缓存作为模拟Redis实例
	var instances []cache.Cache
	instanceCount := 3

	for i := 0; i < instanceCount; i++ {
		config := cache.NewMemoryConfig()
		config.Prefix = ""
		memCache, err := cache.NewMemoryCache(config, nil)
		if err != nil {
			t.Fatalf("创建内存缓存失败: %v", err)
		}
		defer memCache.Close()
		instances = append(instances, memCache)
	}

	// 创建Redlock实例
	redlock := cache.NewRedLock(instances, nil, nil)

	// 测试锁扩展
	lockKey := "test:extend"
	shortDuration := 1 * time.Second

	// 获取锁，短暂过期时间
	locked, err := redlock.Lock(lockKey, shortDuration)
	if err != nil || !locked {
		t.Fatalf("获取锁失败: %v", err)
	}

	// 扩展锁的过期时间
	extended, err := redlock.ExtendLock(lockKey, 10*time.Second)
	if err != nil {
		t.Fatalf("扩展锁失败: %v", err)
	}

	if !extended {
		t.Error("锁应该成功扩展")
	}

	// 等待原始过期时间过去
	time.Sleep(shortDuration + 100*time.Millisecond)

	// 锁应该仍然有效，尝试解锁
	err = redlock.Unlock(lockKey)
	if err != nil {
		t.Errorf("解锁扩展的锁失败: %v", err)
	}
}
