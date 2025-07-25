package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zhoudm1743/go-cache"
)

func main() {
	fmt.Println("===== 缓存防护示例 =====")

	// 1. 创建基础缓存
	memoryConfig := cache.NewMemoryConfig()
	memoryCache, err := cache.NewMemoryCache(memoryConfig, nil)
	if err != nil {
		fmt.Println("创建内存缓存失败:", err)
		return
	}
	defer memoryCache.Close()

	// 2. 创建缓存防护配置
	protection := cache.NewCacheProtection()

	// 3. 启用布隆过滤器防护（10000个元素，0.1%误判率）
	protection.EnableBloomFilterProtection(10000, 0.001)

	// 4. 创建带防护功能的缓存
	protectedCache := cache.NewProtectedCache(memoryCache, protection, nil)

	// 5. 准备测试数据
	// 预先添加一些键到缓存中
	existingKeys := []string{"user:1", "user:2", "user:3", "product:1", "product:2"}
	for _, key := range existingKeys {
		protectedCache.Set(key, fmt.Sprintf("Value for %s", key), 10*time.Minute)
	}

	fmt.Println("\n1. 防止缓存穿透示例")
	fmt.Println("布隆过滤器判断有效键：")
	for _, key := range existingKeys {
		fmt.Printf("  键 '%s' 可能存在: %v\n", key, protection.ExistsInBloomFilter(key))
	}

	fmt.Println("\n布隆过滤器判断无效键：")
	invalidKeys := []string{"user:999", "product:999", "nonexistent:key"}
	for _, key := range invalidKeys {
		exists := protection.ExistsInBloomFilter(key)
		fmt.Printf("  键 '%s' 可能存在: %v\n", key, exists)

		// 尝试获取不存在的键，会被布隆过滤器拦截
		_, err := protectedCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 '%s' 失败: %v\n", key, err)
		}
	}

	// 使用GetWithLoader方法，防止缓存穿透
	fmt.Println("\n使用GetWithLoader加载不存在的键，并缓存空值：")

	_, err = protectedCache.GetWithLoader(context.Background(), "user:999", func(ctx context.Context) (interface{}, time.Duration, error) {
		// 模拟数据库查询，没有找到结果
		fmt.Println("  尝试从'数据库'加载user:999")
		return nil, 10 * time.Minute, cache.ErrKeyNotFound
	})

	fmt.Printf("  错误: %v\n", err)

	// 再次尝试获取，这次应该直接返回缓存的"不存在"结果，而不是再次查询数据库
	_, err = protectedCache.GetWithLoader(context.Background(), "user:999", func(ctx context.Context) (interface{}, time.Duration, error) {
		// 这里不应该被执行
		fmt.Println("  这条信息不应该出现，因为缓存了空值，应该直接返回缓存结果")
		return nil, 10 * time.Minute, cache.ErrKeyNotFound
	})

	fmt.Printf("  再次获取错误: %v\n", err)

	fmt.Println("\n2. 防止缓存雪崩示例")
	fmt.Println("过期时间随机化:")

	// 设置一批键，演示过期时间随机化
	expiration := 5 * time.Minute
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("jitter:key:%d", i)
		// 正常设置会自动应用随机因子
		err := protectedCache.Set(key, fmt.Sprintf("Value for %s", key), expiration)
		if err != nil {
			fmt.Printf("  设置键 %s 失败: %v\n", key, err)
		}
	}

	fmt.Println("\n3. 同时处理大量请求场景模拟")
	fmt.Println("模拟100个并发请求，观察布隆过滤器和缓存空值的效果:")

	// 模拟大量并发请求
	var wg sync.WaitGroup
	requestsCount := 100
	hitCount := 0
	missCount := 0
	filteredCount := 0
	nilCount := 0

	var countMutex sync.Mutex

	for i := 0; i < requestsCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 随机生成键，模拟用户请求
			var key string
			if id%5 == 0 {
				// 20%的请求是有效键
				key = existingKeys[id%len(existingKeys)]
			} else {
				// 80%的请求是无效键
				key = fmt.Sprintf("nonexistent:key:%d", id)
			}

			_, err := protectedCache.GetWithLoader(context.Background(), key, func(ctx context.Context) (interface{}, time.Duration, error) {
				// 模拟数据库查询
				time.Sleep(10 * time.Millisecond)

				// 对于有效键，返回数据
				for _, validKey := range existingKeys {
					if key == validKey {
						return fmt.Sprintf("DB value for %s", key), 5 * time.Minute, nil
					}
				}

				// 对于无效键，返回不存在错误
				return nil, 5 * time.Minute, cache.ErrKeyNotFound
			})

			// 统计结果
			countMutex.Lock()
			if err == nil {
				hitCount++
			} else if err == cache.ErrKeyNotFound {
				missCount++
				// 检查是否被布隆过滤器过滤
				if !protection.ExistsInBloomFilter(key) {
					filteredCount++
				} else {
					// 检查是否是缓存的空值
					raw, _ := protectedCache.GetCache().Get(key)
					if raw == protectedCache.GetNilValue() {
						nilCount++
					}
				}
			}
			countMutex.Unlock()
		}(i)
	}

	wg.Wait()

	fmt.Printf("  总请求数: %d\n", requestsCount)
	fmt.Printf("  缓存命中数: %d\n", hitCount)
	fmt.Printf("  缓存未命中数: %d\n", missCount)
	fmt.Printf("    其中布隆过滤器拦截数: %d\n", filteredCount)
	fmt.Printf("    其中空值缓存拦截数: %d\n", nilCount)

	fmt.Println("\n实际应用中，布隆过滤器和缓存空值策略可以有效防止缓存穿透，")
	fmt.Println("而过期时间随机化可以有效防止缓存雪崩。")

	os.Exit(0)
}
