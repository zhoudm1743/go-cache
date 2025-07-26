package test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhoudm1743/go-cache"
)

// BenchmarkMemoryCacheParallel 内存缓存(全局锁)并发基准测试
func BenchmarkMemoryCacheParallel(b *testing.B) {
	// 创建内存缓存
	config := cache.NewMemoryConfig()
	memCache, err := cache.NewMemoryCache(config, nil)
	if err != nil {
		b.Fatalf("创建内存缓存失败: %v", err)
	}
	defer memCache.Close()

	// 预热缓存
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("warmup:%d", i)
		memCache.Set(key, fmt.Sprintf("value:%d", i), 10*time.Minute)
	}

	// 运行并发基准测试
	b.Run("Read_80_Write_20", func(b *testing.B) {
		benchmarkCacheParallel(b, memCache, 0.8, 0.2)
	})

	b.Run("Read_50_Write_50", func(b *testing.B) {
		benchmarkCacheParallel(b, memCache, 0.5, 0.5)
	})

	b.Run("Read_20_Write_80", func(b *testing.B) {
		benchmarkCacheParallel(b, memCache, 0.2, 0.8)
	})
}

// BenchmarkShardedMemoryCacheParallel 分段锁内存缓存并发基准测试
func BenchmarkShardedMemoryCacheParallel(b *testing.B) {
	// 创建分段锁内存缓存
	config := cache.NewShardedMemoryConfig()
	config.ShardCount = 32
	shardedCache, err := cache.NewShardedMemoryCache(config, nil)
	if err != nil {
		b.Fatalf("创建分段锁内存缓存失败: %v", err)
	}
	defer shardedCache.Close()

	// 预热缓存
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("warmup:%d", i)
		shardedCache.Set(key, fmt.Sprintf("value:%d", i), 10*time.Minute)
	}

	// 运行并发基准测试
	b.Run("Read_80_Write_20", func(b *testing.B) {
		benchmarkCacheParallel(b, shardedCache, 0.8, 0.2)
	})

	b.Run("Read_50_Write_50", func(b *testing.B) {
		benchmarkCacheParallel(b, shardedCache, 0.5, 0.5)
	})

	b.Run("Read_20_Write_80", func(b *testing.B) {
		benchmarkCacheParallel(b, shardedCache, 0.2, 0.8)
	})
}

// benchmarkCacheParallel 缓存并发基准测试通用函数
func benchmarkCacheParallel(b *testing.B, c cache.Cache, readRatio, writeRatio float64) {
	b.Helper()

	// 重置计时器，不包括设置阶段
	b.ResetTimer()

	// 并发执行
	b.RunParallel(func(pb *testing.PB) {
		// 每个goroutine有自己的计数器
		counter := 0

		for pb.Next() {
			key := fmt.Sprintf("key:%d", counter%1000)

			if float64(counter%100)/100 < readRatio {
				// 读操作
				_, _ = c.Get(key)
			} else {
				// 写操作
				_ = c.Set(key, fmt.Sprintf("value:%d", counter), 5*time.Minute)
			}

			counter++
		}
	})
}

// TestCachePerformanceComparison 缓存性能比较测试
func TestCachePerformanceComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过性能比较测试")
	}

	// 创建内存缓存
	memConfig := cache.NewMemoryConfig()
	memCache, err := cache.NewMemoryCache(memConfig, nil)
	if err != nil {
		t.Fatalf("创建内存缓存失败: %v", err)
	}
	defer memCache.Close()

	// 创建分段锁内存缓存
	shardedConfig := cache.NewShardedMemoryConfig()
	shardedConfig.ShardCount = 32
	shardedCache, err := cache.NewShardedMemoryCache(shardedConfig, nil)
	if err != nil {
		t.Fatalf("创建分段锁内存缓存失败: %v", err)
	}
	defer shardedCache.Close()

	// 测试场景
	scenarios := []struct {
		name       string
		readRatio  float64
		writeRatio float64
		operations int
		workers    int
	}{
		{"读密集(90%读,10%写)", 0.9, 0.1, 100000, 8},
		{"读写均衡(50%读,50%写)", 0.5, 0.5, 100000, 8},
		{"写密集(10%读,90%写)", 0.1, 0.9, 100000, 8},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// 测试传统内存缓存
			memDuration := measureCachePerformance(t, memCache, scenario.readRatio,
				scenario.writeRatio, scenario.operations, scenario.workers)

			// 测试分段锁内存缓存
			shardedDuration := measureCachePerformance(t, shardedCache, scenario.readRatio,
				scenario.writeRatio, scenario.operations, scenario.workers)

			// 计算性能提升
			speedup := float64(memDuration) / float64(shardedDuration)
			t.Logf("传统内存缓存: %v, 分段锁内存缓存: %v, 性能提升: %.2f倍",
				memDuration, shardedDuration, speedup)
		})
	}
}

// measureCachePerformance 测量缓存性能
func measureCachePerformance(t *testing.T, c cache.Cache, readRatio, writeRatio float64,
	totalOperations, workers int) time.Duration {
	t.Helper()

	// 预热缓存
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("warmup:%d", i)
		c.Set(key, fmt.Sprintf("value:%d", i), 10*time.Minute)
	}

	// 等待所有工作线程完成
	var wg sync.WaitGroup
	wg.Add(workers)

	// 计算每个工作线程的操作数
	opsPerWorker := totalOperations / workers

	// 记录开始时间
	start := time.Now()

	// 启动工作线程
	for w := 0; w < workers; w++ {
		go func(id int) {
			defer wg.Done()

			// 每个工作线程独立的键空间
			prefix := fmt.Sprintf("worker-%d-", id)

			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("%skey-%d", prefix, i%100)

				// 根据读写比例确定操作
				if float64(i%100)/100 < readRatio {
					// 读操作
					_, _ = c.Get(key)
				} else {
					// 写操作
					_ = c.Set(key, fmt.Sprintf("value-%d", i), 10*time.Minute)
				}
			}
		}(w)
	}

	// 等待所有工作线程完成
	wg.Wait()

	// 返回执行时间
	return time.Since(start)
}
