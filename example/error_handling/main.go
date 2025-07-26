package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/zhoudm1743/go-cache" // 导入路径
)

func main() {
	// 创建内存缓存配置
	memoryConfig := cache.NewMemoryConfig()
	memoryConfig.Prefix = "err:"

	// 创建内存缓存
	memoryCache, err := cache.NewMemoryCache(memoryConfig, nil)
	if err != nil {
		fmt.Println("初始化内存缓存失败:", err)
		return
	}
	defer memoryCache.Close()

	// ========== 标准错误处理示例 ==========
	fmt.Println("\n=== 标准错误处理示例 ===")

	// 尝试获取不存在的键
	_, err = memoryCache.Get("non-existent-key")
	if err != nil {
		// 使用errors.Is检查标准错误类型
		if errors.Is(err, cache.ErrKeyNotFound) {
			fmt.Println("错误类型检测成功: 键不存在")
		}

		// 打印带上下文的错误信息
		fmt.Printf("完整错误信息: %v\n", err)

		// 提取根本错误
		rootErr := cache.GetRootError(err)
		fmt.Printf("根本错误: %v\n", rootErr)
	}

	// ========== 不同类型的错误示例 ==========
	fmt.Println("\n=== 不同类型的错误示例 ===")

	// 设置一个键值对
	memoryCache.Set("test-key", "value", time.Minute)

	// 类型不匹配错误
	memoryCache.Set("number-key", 123, time.Minute) // 存储一个数字
	_, err = memoryCache.Get("number-key")          // Get总是返回字符串
	if errors.Is(err, cache.ErrTypeMismatch) {
		fmt.Println("错误检测成功: 类型不匹配")
		fmt.Printf("错误信息: %v\n", err)
	}

	// 连接失败错误(模拟)
	fmt.Println("\n=== 错误转换示例 ===")
	originalErr := fmt.Errorf("connection refused to server")
	convertedErr := cache.ConvertError(originalErr)
	if errors.Is(convertedErr, cache.ErrConnectionFailed) {
		fmt.Println("错误转换成功: 连接失败")
		fmt.Printf("原始错误: %v\n", originalErr)
		fmt.Printf("转换后错误: %v\n", convertedErr)
	}

	// ========== 错误包装示例 ==========
	fmt.Println("\n=== 错误包装示例 ===")

	baseErr := cache.ErrServerInternal
	wrappedErr := cache.WrapError(baseErr, "处理用户请求时")
	doubleWrappedErr := cache.WrapError(wrappedErr, "API调用过程中")

	fmt.Printf("最终错误: %v\n", doubleWrappedErr)

	// 错误链检查
	fmt.Println("错误链检查:")
	fmt.Printf("是服务器内部错误? %v\n", errors.Is(doubleWrappedErr, cache.ErrServerInternal))
	fmt.Printf("是键不存在错误? %v\n", errors.Is(doubleWrappedErr, cache.ErrKeyNotFound))

	// ========== 错误处理最佳实践 ==========
	fmt.Println("\n=== 错误处理最佳实践 ===")

	// 尝试获取可能不存在的键
	value, err := getValueWithRetry(memoryCache, "maybe-key")
	if err != nil {
		if errors.Is(err, cache.ErrKeyNotFound) {
			fmt.Println("键确实不存在，将使用默认值")
		} else {
			fmt.Printf("发生其他错误: %v\n", err)
		}
	} else {
		fmt.Printf("获取到值: %v\n", value)
	}
}

// getValueWithRetry 带重试的获取缓存值
func getValueWithRetry(c cache.Cache, key string) (string, error) {
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		value, err := c.Get(key)

		if err == nil {
			return value, nil
		}

		// 只有连接错误才重试
		if !errors.Is(err, cache.ErrConnectionFailed) && !errors.Is(err, cache.ErrTimeout) {
			return "", err // 其他错误直接返回
		}

		// 连接错误重试
		fmt.Printf("第%d次重试获取键'%s'\n", i+1, key)
		time.Sleep(time.Duration(i*100) * time.Millisecond) // 指数退避
	}

	return "", cache.ErrorWithContext(cache.ErrConnectionFailed, fmt.Sprintf("获取键'%s'失败，已重试%d次", key, maxRetries))
}
