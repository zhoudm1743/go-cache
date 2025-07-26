package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/zhoudm1743/go-cache"
)

func main() {
	fmt.Println("分布式缓存和Redlock示例")
	fmt.Println("=============================")

	// 创建多个Redis缓存实例，模拟分布式环境
	// 在实际环境中，这些可能是不同机器上的不同Redis实例
	instances := createRedisInstances(3)
	defer closeInstances(instances)

	// 创建分布式缓存
	distributedCache := createDistributedCache(instances)

	// 演示数据结构操作
	demoDataStructures(distributedCache)

	// 演示Redlock分布式锁
	demoRedlock(instances)
}

// 创建多个Redis缓存实例
func createRedisInstances(count int) []cache.Cache {
	instances := make([]cache.Cache, count)

	// 创建Redis配置
	fmt.Println("创建多个Redis缓存实例...")

	// 注意：在实际环境中，这些应该是不同的Redis服务器
	// 这里为了演示，我们使用相同的Redis但不同的数据库编号
	for i := 0; i < count; i++ {
		redisConfig := cache.NewRedisConfig()
		redisConfig.Host = "localhost"
		redisConfig.Port = 6379
		redisConfig.DB = i // 使用不同的数据库编号
		redisConfig.Prefix = fmt.Sprintf("node%d:", i)

		// 创建Redis缓存
		instance, err := cache.NewRedisCache(redisConfig, nil)
		if err != nil {
			fmt.Printf("初始化Redis实例%d失败: %v\n", i, err)
			// 在示例代码中，出错时使用内存缓存代替
			memConfig := cache.NewMemoryConfig()
			instance, _ = cache.NewMemoryCache(memConfig, nil)
			fmt.Printf("已降级使用内存缓存替代Redis实例%d\n", i)
		}

		instances[i] = instance
		fmt.Printf("缓存实例%d已创建\n", i)
	}

	return instances
}

// 关闭所有缓存实例
func closeInstances(instances []cache.Cache) {
	for i, instance := range instances {
		if err := instance.Close(); err != nil {
			fmt.Printf("关闭缓存实例%d失败: %v\n", i, err)
		}
	}
}

// 创建分布式缓存
func createDistributedCache(instances []cache.Cache) *cache.DistributedCache {
	fmt.Println("\n创建分布式缓存...")

	// 创建分布式缓存配置
	config := &cache.DistributedCacheConfig{
		VirtualNodeMultiplier: 100, // 每个物理节点映射到100个虚拟节点
		ReplicaCount:          2,   // 每个键存储2个副本
		DefaultPrefix:         "dist:",
	}

	// 创建分布式缓存
	dc := cache.NewDistributedCache(config, nil)

	// 添加实例
	for i, instance := range instances {
		nodeName := fmt.Sprintf("node-%d", i)
		err := dc.AddNode(nodeName, instance)
		if err != nil {
			fmt.Printf("添加节点%s失败: %v\n", nodeName, err)
		} else {
			fmt.Printf("成功添加节点: %s\n", nodeName)
		}
	}

	fmt.Printf("分布式缓存已创建，包含%d个节点\n", dc.GetNodeCount())
	return dc
}

// 演示数据结构操作
func demoDataStructures(dc *cache.DistributedCache) {
	fmt.Println("\n演示分布式缓存的数据结构操作")
	fmt.Println("----------------------------------")

	// 1. 字符串操作
	fmt.Println("1. 字符串操作:")
	err := dc.Set("string-key", "这是分布式缓存中的值", 5*time.Minute)
	if err != nil {
		fmt.Printf("设置失败: %v\n", err)
	}

	value, err := dc.Get("string-key")
	if err != nil {
		fmt.Printf("获取失败: %v\n", err)
	} else {
		fmt.Printf("获取到的值: %s\n", value)
	}

	// 2. 哈希表操作
	fmt.Println("\n2. 哈希表操作:")
	dc.HSet("user:100",
		"name", "张三",
		"age", "30",
		"city", "北京")

	name, err := dc.HGet("user:100", "name")
	if err != nil {
		fmt.Printf("获取哈希字段失败: %v\n", err)
	} else {
		fmt.Printf("用户名: %s\n", name)
	}

	fields, err := dc.HGetAll("user:100")
	if err != nil {
		fmt.Printf("获取哈希所有字段失败: %v\n", err)
	} else {
		fmt.Println("用户信息:")
		for k, v := range fields {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}

	// 3. 列表操作
	fmt.Println("\n3. 列表操作:")
	dc.LPush("tasks", "任务1", "任务2", "任务3")
	dc.RPush("tasks", "任务4")

	length, err := dc.LLen("tasks")
	if err != nil {
		fmt.Printf("获取列表长度失败: %v\n", err)
	} else {
		fmt.Printf("任务数量: %d\n", length)
	}

	tasks, err := dc.LRange("tasks", 0, -1)
	if err != nil {
		fmt.Printf("获取列表失败: %v\n", err)
	} else {
		fmt.Println("所有任务:")
		for i, task := range tasks {
			fmt.Printf("  %d. %s\n", i+1, task)
		}
	}

	// 4. 集合操作
	fmt.Println("\n4. 集合操作:")
	dc.SAdd("tags", "go", "redis", "分布式", "缓存")

	tags, err := dc.SMembers("tags")
	if err != nil {
		fmt.Printf("获取集合成员失败: %v\n", err)
	} else {
		fmt.Println("所有标签:")
		for _, tag := range tags {
			fmt.Printf("  - %s\n", tag)
		}
	}

	exists, err := dc.SIsMember("tags", "go")
	if err != nil {
		fmt.Printf("检查集合成员失败: %v\n", err)
	} else {
		fmt.Printf("标签'go'是否存在: %v\n", exists)
	}

	// 5. 有序集合操作
	fmt.Println("\n5. 有序集合操作:")
	dc.ZAdd("scores",
		cache.Z{Score: 90.5, Member: "张三"},
		cache.Z{Score: 87.0, Member: "李四"},
		cache.Z{Score: 95.5, Member: "王五"})

	members, err := dc.ZRange("scores", 0, -1)
	if err != nil {
		fmt.Printf("获取有序集合成员失败: %v\n", err)
	} else {
		fmt.Println("所有学生(从低分到高分):")
		for _, m := range members {
			score, _ := dc.ZScore("scores", m)
			fmt.Printf("  %s: %.1f\n", m, score)
		}
	}

	topScores, err := dc.ZRangeWithScores("scores", -1, -1)
	if err != nil {
		fmt.Printf("获取最高分失败: %v\n", err)
	} else {
		fmt.Println("最高分:")
		for _, z := range topScores {
			fmt.Printf("  %v: %.1f\n", z.Member, z.Score)
		}
	}
}

// 演示Redlock分布式锁
func demoRedlock(instances []cache.Cache) {
	fmt.Println("\n演示Redlock分布式锁")
	fmt.Println("----------------------------------")

	// 创建Redlock配置
	redlockConfig := &cache.RedLockConfig{
		RetryCount: 3,
		RetryDelay: 200 * time.Millisecond,
		ClockDrift: 0.01, // 1%
	}

	// 创建Redlock
	redlock := cache.NewRedLock(instances, redlockConfig, nil)

	// 使用分布式锁保护共享资源
	var counter int
	var wg sync.WaitGroup
	var mu sync.Mutex // 本地互斥锁，用于保护counter变量

	// 定义任务函数
	task := func(id int) {
		defer wg.Done()

		// 获取分布式锁
		resourceName := "shared-resource"
		locked, err := redlock.Lock(resourceName, 10*time.Second)
		if err != nil {
			fmt.Printf("工作者%d获取锁失败: %v\n", id, err)
			return
		}

		if !locked {
			fmt.Printf("工作者%d无法获取锁\n", id)
			return
		}

		// 成功获取锁
		fmt.Printf("工作者%d获取锁成功，开始处理共享资源\n", id)

		// 模拟处理共享资源
		time.Sleep(500 * time.Millisecond)

		// 更新计数器
		mu.Lock()
		counter++
		localCounter := counter
		mu.Unlock()

		fmt.Printf("工作者%d更新计数器: %d\n", id, localCounter)

		// 释放锁
		if err := redlock.Unlock(resourceName); err != nil {
			fmt.Printf("工作者%d释放锁失败: %v\n", id, err)
		} else {
			fmt.Printf("工作者%d释放锁成功\n", id)
		}
	}

	// 启动多个工作者
	workers := 5
	fmt.Printf("启动%d个工作者...\n", workers)
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go task(i)
		// 稍微错开启动时间
		time.Sleep(100 * time.Millisecond)
	}

	// 等待所有工作者完成
	wg.Wait()
	fmt.Printf("\n所有工作者已完成，最终计数器值: %d\n", counter)

	// 使用WithLock辅助函数
	fmt.Println("\n使用WithLock函数执行受保护的代码")
	err := redlock.WithLock("protected-operation", 5*time.Second, func() error {
		fmt.Println("执行受保护的操作...")
		time.Sleep(1 * time.Second)
		fmt.Println("操作完成")
		return nil
	})

	if err != nil {
		fmt.Printf("执行失败: %v\n", err)
	}

	// 测试锁的延长
	fmt.Println("\n测试锁的延长功能")
	resourceName := "long-operation"
	locked, err := redlock.Lock(resourceName, 2*time.Second)
	if err != nil || !locked {
		fmt.Printf("获取锁失败: %v\n", err)
		return
	}

	fmt.Println("获取锁成功，执行长时间操作...")

	// 启动一个协程定期延长锁
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 延长锁
				extended, err := redlock.ExtendLock(resourceName, 2*time.Second)
				if err != nil {
					fmt.Printf("延长锁失败: %v\n", err)
					return
				}
				if extended {
					fmt.Println("成功延长锁")
				} else {
					fmt.Println("无法延长锁")
					return
				}
			case <-done:
				return
			}
		}
	}()

	// 模拟长时间操作
	time.Sleep(3 * time.Second)

	// 通知延长锁的协程停止
	done <- true

	// 释放锁
	if err := redlock.Unlock(resourceName); err != nil {
		fmt.Printf("释放锁失败: %v\n", err)
	} else {
		fmt.Println("释放锁成功")
	}

	fmt.Println("\n分布式缓存和Redlock演示完成")
}
