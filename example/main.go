package main

import (
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
}
