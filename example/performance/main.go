package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhoudm1743/go-cache"
)

const (
	// 测试参数
	numWorkers          = 20   // 并发工作线程数
	operationsPerWorker = 5000 // 每个工作线程的操作次数
	readWriteRatio      = 4    // 读写比例，4表示4次读1次写
)

func main() {
	fmt.Printf("CPU核心数: %d\n", runtime.NumCPU())
	fmt.Printf("并发线程数: %d\n", numWorkers)
	fmt.Printf("每线程操作数: %d\n", operationsPerWorker)
	fmt.Printf("总操作数: %d\n", numWorkers*operationsPerWorker)
	fmt.Printf("读写比例: %d:1\n", readWriteRatio)
	fmt.Println("==============================")

	// 测试传统内存缓存
	fmt.Println("测试传统内存缓存(全局锁)...")
	traditionalConfig := cache.NewMemoryConfig()
	traditionalCache, _ := cache.NewMemoryCache(traditionalConfig, nil)
	defer traditionalCache.Close()

	traditionalDuration := benchmarkCache("传统内存缓存", traditionalCache)

	// 测试分段锁缓存
	fmt.Println("\n测试分段锁缓存...")
	shardedConfig := cache.NewShardedMemoryConfig()
	shardedConfig.ShardCount = 32 // 32个分段
	shardedCache, _ := cache.NewShardedMemoryCache(shardedConfig, nil)
	defer shardedCache.Close()

	shardedDuration := benchmarkCache("分段锁缓存", shardedCache)

	// 计算性能提升
	improvement := float64(traditionalDuration) / float64(shardedDuration)
	fmt.Printf("\n性能对比:\n")
	fmt.Printf("传统内存缓存: %v\n", traditionalDuration)
	fmt.Printf("分段锁缓存: %v\n", shardedDuration)
	fmt.Printf("性能提升: %.2f倍\n", improvement)
}

// benchmarkCache 对缓存进行基准测试
func benchmarkCache(name string, c cache.Cache) time.Duration {
	// 预热缓存
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("warmup-key-%d", i)
		c.Set(key, fmt.Sprintf("value-%d", i), 0)
	}

	// 启动工作线程
	var wg sync.WaitGroup
	var ops int64
	var errs int64

	start := time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerId int) {
			defer wg.Done()

			// 每个工作线程独立的键空间
			prefix := fmt.Sprintf("worker-%d-", workerId)

			for i := 0; i < operationsPerWorker; i++ {
				// 确定是读还是写操作
				isRead := i%(readWriteRatio+1) != 0
				key := fmt.Sprintf("%skey-%d", prefix, i%100) // 使用有限的键空间

				if isRead {
					// 读操作
					_, err := c.Get(key)
					if err != nil && err != cache.ErrKeyNotFound {
						atomic.AddInt64(&errs, 1)
					}
				} else {
					// 写操作
					err := c.Set(key, fmt.Sprintf("value-%d", i), time.Hour)
					if err != nil {
						atomic.AddInt64(&errs, 1)
					}
				}

				atomic.AddInt64(&ops, 1)
			}
		}(w)
	}

	// 定期打印进度
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				completed := atomic.LoadInt64(&ops)
				total := int64(numWorkers * operationsPerWorker)
				percent := float64(completed) / float64(total) * 100
				fmt.Printf("\r%s 进度: %.1f%% (%d/%d)", name, percent, completed, total)
			case <-done:
				return
			}
		}
	}()

	// 等待所有工作线程完成
	wg.Wait()
	close(done)

	duration := time.Since(start)
	opsPerSecond := float64(ops) / duration.Seconds()

	// 打印结果
	fmt.Printf("\r%s 完成: %d 操作, %d 错误, 耗时: %v, %.0f ops/sec\n",
		name, ops, errs, duration, opsPerSecond)

	return duration
}
