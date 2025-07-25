package main

import (
	"fmt"
	"time"

	"github.com/zhoudm1743/go-cache"
)

func main() {
	fmt.Println("===== 分布式缓存示例 =====")

	// 创建分布式缓存配置
	distConfig := &cache.DistributedCacheConfig{
		VirtualNodeMultiplier: 100, // 每个实际节点映射为100个虚拟节点
		ReplicaCount:          2,   // 每个键保存2个副本
		DefaultPrefix:         "dist:",
	}

	// 创建分布式缓存
	distCache := cache.NewDistributedCache(distConfig, nil)

	// 创建3个Redis节点（这里使用内存缓存模拟多个Redis节点）
	for i := 1; i <= 3; i++ {
		// 创建内存缓存配置
		memConfig := cache.NewMemoryConfig()
		memConfig.Prefix = fmt.Sprintf("node%d:", i)

		// 创建内存缓存实例
		memCache, err := cache.NewMemoryCache(memConfig, nil)
		if err != nil {
			fmt.Printf("创建内存缓存节点%d失败: %v\n", i, err)
			return
		}

		// 添加到分布式缓存
		nodeName := fmt.Sprintf("node%d", i)
		if err := distCache.AddNode(nodeName, memCache); err != nil {
			fmt.Printf("添加节点%s失败: %v\n", nodeName, err)
			return
		}

		fmt.Printf("添加节点: %s\n", nodeName)
	}

	// 查看所有节点
	fmt.Printf("\n节点总数: %d\n", distCache.GetNodeCount())
	fmt.Printf("节点列表: %v\n", distCache.GetNodeNames())

	// 存储一些数据
	keys := []string{
		"user:1", "user:2", "user:3", "user:4",
		"product:1", "product:2", "product:3",
		"order:1", "order:2", "order:3",
	}

	fmt.Println("\n写入测试数据:")
	for _, key := range keys {
		err := distCache.Set(key, fmt.Sprintf("Value for %s", key), 10*time.Minute)
		if err != nil {
			fmt.Printf("  设置键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  设置键: %s\n", key)
		}
	}

	// 读取数据测试
	fmt.Println("\n读取测试数据:")
	for _, key := range keys {
		value, err := distCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, value)
		}
	}

	// 模拟节点故障
	fmt.Println("\n模拟节点故障场景:")
	fmt.Println("移除节点 node2...")
	distCache.RemoveNode("node2")

	fmt.Println("\n节点故障后读取数据:")
	for _, key := range keys {
		value, err := distCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, value)
		}
	}

	// 添加一个新节点
	fmt.Println("\n添加新节点测试:")
	memConfig := cache.NewMemoryConfig()
	memConfig.Prefix = "node4:"
	memCache, err := cache.NewMemoryCache(memConfig, nil)
	if err != nil {
		fmt.Printf("创建内存缓存节点4失败: %v\n", err)
		return
	}
	distCache.AddNode("node4", memCache)
	fmt.Println("添加节点: node4")

	// 重新分布一些键
	fmt.Println("\n向新节点重新分布部分键:")
	for _, key := range []string{"user:1", "product:1", "order:1"} {
		err := distCache.Set(key, fmt.Sprintf("Updated value for %s", key), 10*time.Minute)
		if err != nil {
			fmt.Printf("  更新键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  更新键: %s\n", key)
		}
	}

	// 读取更新后的数据
	fmt.Println("\n读取重新分布后的数据:")
	for _, key := range []string{"user:1", "product:1", "order:1"} {
		value, err := distCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, value)
		}
	}

	// 批量删除
	deleteKeys := []string{"user:1", "product:1"}
	fmt.Printf("\n批量删除键: %v\n", deleteKeys)
	count, err := distCache.Del(deleteKeys...)
	if err != nil {
		fmt.Printf("删除失败: %v\n", err)
	} else {
		fmt.Printf("成功删除 %d 个键\n", count)
	}

	// 读取删除后的数据
	fmt.Println("\n读取删除后的数据:")
	for _, key := range deleteKeys {
		value, err := distCache.Get(key)
		if err != nil {
			fmt.Printf("  获取键 %s 失败: %v\n", key, err)
		} else {
			fmt.Printf("  键 %s = %s\n", key, value)
		}
	}

	// 关闭分布式缓存
	fmt.Println("\n关闭分布式缓存...")
	if err := distCache.Close(); err != nil {
		fmt.Printf("关闭分布式缓存失败: %v\n", err)
	}

	fmt.Println("\n示例执行完毕。")
}
