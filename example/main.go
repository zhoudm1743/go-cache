package main

import (
	"context"
	"fmt"
	"time"

	"github.com/zhoudm1743/go-cache"
)

func main() {
	// ====== 示例1: 使用内存缓存 - 使用默认日志 ======
	memoryConfig := cache.NewMemoryConfig()
	memoryConfig.Prefix = "mem:"

	// 使用默认日志（无需传递logger参数）
	memoryCache, err := cache.NewMemoryCache(memoryConfig, nil)
	if err != nil {
		fmt.Println("初始化内存缓存失败:", err)
		return
	}
	defer memoryCache.Close()

	// 设置缓存
	err = memoryCache.Set("hello", "world", 10*time.Second)
	if err != nil {
		fmt.Println("设置缓存失败:", err)
		return
	}

	// 获取缓存
	val, err := memoryCache.Get("hello")
	if err != nil {
		fmt.Println("获取缓存失败:", err)
		return
	}
	fmt.Println("内存缓存值:", val)

	// ====== 示例2: 使用文件缓存 - 使用默认日志 ======
	fileConfig := cache.NewFileConfig()
	fileConfig.Prefix = "file:"
	fileConfig.FilePath = "./file_cache"

	// 文件缓存需要确保目录存在
	fmt.Println("初始化文件缓存...")
	fileCache, err := cache.NewFileCache(fileConfig, nil)
	if err != nil {
		fmt.Println("初始化文件缓存失败:", err)
		// 继续执行，不中断程序
	} else {
		defer fileCache.Close()

		// 设置缓存
		err = fileCache.Set("file-key", "file value", 1*time.Minute)
		if err != nil {
			fmt.Println("设置文件缓存失败:", err)
		}

		// 获取缓存
		val, err = fileCache.Get("file-key")
		if err != nil {
			fmt.Println("获取文件缓存失败:", err)
		} else {
			fmt.Println("文件缓存值:", val)
		}
	}

	// ====== 示例3: 使用Redis缓存 - 使用默认日志 ======
	// 注意：这部分代码仅作示例，需要有可用的Redis服务才能成功运行
	fmt.Println("Redis缓存示例 (需要Redis服务):")
	redisConfig := cache.NewRedisConfig()
	redisConfig.Prefix = "redis:"
	redisConfig.Host = "localhost"
	redisConfig.Port = 6379
	redisConfig.DB = 0
	redisConfig.Password = "" // 如果需要密码，在这里设置

	fmt.Println("配置Redis: ", redisConfig.Host, redisConfig.Port)
	fmt.Println("如果您没有运行Redis服务，可以忽略连接失败错误")

	redisCache, err := cache.NewRedisCache(redisConfig, nil)
	if err != nil {
		fmt.Println("初始化Redis缓存失败:", err)
	} else {
		defer redisCache.Close()

		// 设置缓存
		err = redisCache.Set("redis-key", "redis value", 1*time.Minute)
		if err != nil {
			fmt.Println("设置Redis缓存失败:", err)
		} else {
			val, err = redisCache.Get("redis-key")
			if err != nil {
				fmt.Println("获取Redis缓存失败:", err)
			} else {
				fmt.Println("Redis缓存值:", val)
			}
		}
	}

	// ====== 示例4: 使用缓存助手 - 使用默认日志 ======
	helper := cache.NewCacheHelper(memoryCache, nil, "helper:")

	// 存储JSON对象
	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	user := User{ID: 1, Name: "张三"}
	err = helper.SetJSON("user:1", user, 30*time.Second)
	if err != nil {
		fmt.Println("存储JSON失败:", err)
	}

	// 获取JSON对象
	var retrievedUser User
	err = helper.GetJSON("user:1", &retrievedUser)
	if err != nil {
		fmt.Println("获取JSON失败:", err)
	}
	fmt.Printf("用户: %+v\n", retrievedUser)

	// 演示记忆模式
	result, err := helper.Remember("cached-data", 1*time.Minute, func() (interface{}, error) {
		// 模拟耗时操作
		time.Sleep(100 * time.Millisecond)
		return "这是一个耗时操作的结果", nil
	})
	if err != nil {
		fmt.Println("记忆模式失败:", err)
	}
	fmt.Println("记忆模式结果:", result)

	// 再次获取，应该直接从缓存中返回，不会执行耗时操作
	start := time.Now()
	result, err = helper.Remember("cached-data", 1*time.Minute, func() (interface{}, error) {
		// 这次不应该执行到这里
		time.Sleep(100 * time.Millisecond)
		return "新的结果", nil
	})
	if err != nil {
		fmt.Println("记忆模式失败:", err)
	}
	fmt.Println("缓存命中结果:", result)
	fmt.Println("耗时:", time.Since(start))

	// ====== 示例5: 自定义日志 ======
	// 如果需要自定义日志，可以设置全局默认日志
	// cache.SetDefaultLogger(自定义Logger)

	// 或者在创建缓存实例时传入
	// customLogger := &MyCustomLogger{}
	// customCache, err := cache.NewMemoryCache(memoryConfig, customLogger)

	// ====== 示例6: 缓存预热 ======
	fmt.Println("\n===== 缓存预热示例 =====")

	// 准备一些预热的数据
	warmupData := map[string]interface{}{
		"product:1": "iPhone 13",
		"product:2": "MacBook Pro",
		"product:3": "iPad Air",
		"product:4": "Apple Watch",
		"product:5": "AirPods Pro",
	}

	// 创建一个基于映射表的数据加载器
	dataLoader := cache.NewMapDataLoader(warmupData, 5*time.Minute)

	// 1. 使用指定的键列表预热
	fmt.Println("1. 使用指定的键列表预热内存缓存:")
	keys := []string{"product:1", "product:2", "product:3"}
	err = memoryCache.WarmupKeys(context.Background(), keys, dataLoader)
	if err != nil {
		fmt.Println("  预热缓存失败:", err)
	}

	// 验证预热数据
	for _, key := range keys {
		val, err := memoryCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, val)
		}
	}

	// 2. 使用键生成器预热Redis缓存（如果可用）
	if redisCache != nil {
		fmt.Println("\n2. 使用简单键生成器预热Redis缓存:")

		// 先设置一些键以便键生成器能够找到它们
		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("warmup:key:%d", i)
			err = redisCache.Set(key, fmt.Sprintf("测试值 %d", i), 5*time.Minute)
			if err != nil {
				fmt.Printf("  设置Redis键失败: %v\n", err)
				continue
			}
		}

		// 创建一个简单的键生成器，匹配特定模式的键
		keyGenerator := cache.NewSimpleKeyGenerator(redisCache, "warmup:key:*")

		// 创建一个函数型数据加载器，模拟从数据库加载数据
		functionLoader := cache.NewFunctionDataLoader(func(ctx context.Context, key string) (interface{}, time.Duration, error) {
			fmt.Printf("  从'数据库'加载键 %s\n", key)
			// 模拟数据库查询延迟
			time.Sleep(100 * time.Millisecond)

			// 返回预热数据、过期时间和错误
			return fmt.Sprintf("预热的 %s 值", key), 5 * time.Minute, nil
		})

		// 执行预热
		err = redisCache.Warmup(context.Background(), functionLoader, keyGenerator)
		if err != nil {
			fmt.Println("  预热Redis缓存失败:", err)
		} else {
			fmt.Println("  Redis缓存预热成功")
		}
	}

	// 3. 演示自定义预热配置
	fmt.Println("\n3. 使用自定义配置预热内存缓存:")

	// 准备自定义键列表
	customKeys := []string{"config:1", "config:2", "config:3"}

	// 自定义数据加载器
	customLoader := cache.NewFunctionDataLoader(func(ctx context.Context, key string) (interface{}, time.Duration, error) {
		// 模拟根据不同键返回不同配置
		switch key {
		case "config:1":
			return `{"server":"api.example.com", "port":443}`, 10 * time.Minute, nil
		case "config:2":
			return `{"max_connections":100, "timeout":30}`, 10 * time.Minute, nil
		case "config:3":
			return `{"feature_flags":{"new_ui":true, "analytics":false}}`, 10 * time.Minute, nil
		default:
			return nil, 0, cache.ErrKeyNotFound
		}
	})

	// 创建自定义预热配置
	config := cache.DefaultWarmupConfig()
	config.Concurrency = 2                   // 限制并发数
	config.Interval = 100 * time.Millisecond // 添加加载间隔

	// 使用自定义配置预热
	err = cache.WarmupCacheWithConfig(context.Background(), memoryCache, customKeys, customLoader, config)
	if err != nil {
		fmt.Println("  预热缓存失败:", err)
	}

	// 验证预热数据
	for _, key := range customKeys {
		val, err := memoryCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, val)
		}
	}
}
