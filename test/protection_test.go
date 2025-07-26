package test

/*
// 注意：以下测试需要实现缓存保护功能后才能启用

import (
	"fmt"
	"testing"
	"time"

	"github.com/zhoudm1743/go-cache"
)

// TestBloomFilter 测试布隆过滤器
func TestBloomFilter(t *testing.T) {
	// 创建布隆过滤器
	bf := cache.NewBloomFilter(10000, 0.01)

	// 添加元素
	testKeys := []string{
		"key1", "key2", "key3", "key4", "key5",
		"test-key", "user:1001", "product:2001",
	}

	for _, key := range testKeys {
		bf.Add(key)
	}

	// 测试已添加的键
	for _, key := range testKeys {
		if !bf.Contains(key) {
			t.Errorf("布隆过滤器应该包含键 '%s'", key)
		}
	}

	// 测试未添加的键
	nonExistKeys := []string{
		"non-exist-key", "user:9999", "product:9999",
	}

	falsePositives := 0
	for _, key := range nonExistKeys {
		if bf.Contains(key) {
			// 布隆过滤器可能会有误报
			falsePositives++
		}
	}

	// 统计误报率
	falsePositiveRate := float64(falsePositives) / float64(len(nonExistKeys))
	t.Logf("布隆过滤器误报率: %.2f%%", falsePositiveRate*100)

	// 误报率应该在可接受范围内
	if falsePositiveRate > 0.05 {
		t.Errorf("布隆过滤器误报率过高: %.2f%%", falsePositiveRate*100)
	}
}

// TestCachePenetrationProtection 测试缓存穿透保护
func TestCachePenetrationProtection(t *testing.T) {
	// 设置缓存
	memConfig := cache.NewMemoryConfig()
	memCache, err := cache.NewMemoryCache(memConfig, nil)
	if err != nil {
		t.Fatalf("创建内存缓存失败: %v", err)
	}
	defer memCache.Close()

	// 创建缓存穿透保护
	protectionConfig := &cache.ProtectionConfig{
		BloomFilterSize:      10000,
		BloomFilterErrorRate: 0.01,
		CacheNilValues:       true,
		NilValueExpiration:   5 * time.Minute,
	}

	protection := cache.NewCacheProtection(memCache, protectionConfig)

	// 1. 测试布隆过滤器保护
	// 添加一些键到布隆过滤器
	existingKeys := []string{"user:1", "user:2", "user:3"}
	for _, key := range existingKeys {
		protection.RegisterKey(key)
	}

	// 测试已注册的键
	for _, key := range existingKeys {
		if !protection.MightExist(key) {
			t.Errorf("已注册的键 '%s' 应该被识别为可能存在", key)
		}
	}

	// 测试未注册的键
	if protection.MightExist("user:999") {
		// 这可能是误报，但我们记录一下
		t.Logf("未注册的键被误报为可能存在")
	}

	// 2. 测试缓存空值保护
	testKey := "test:nonexistent"

	// 确认键不存在
	_, err = protection.Get(testKey)
	if err == nil {
		t.Fatalf("获取不存在的键应该返回错误")
	}

	// 检查是否缓存了空值
	val, err := memCache.Get(testKey)
	if err != nil {
		t.Logf("空值可能没有被缓存: %v", err)
	} else {
		// 如果缓存了空值，值应该是 NIL_VALUE
		if val != cache.NilValue {
			t.Errorf("缓存的空值不正确，期望 '%s'，实际 '%s'", cache.NilValue, val)
		} else {
			t.Logf("成功缓存了空值: %s", val)
		}
	}

	// 再次获取同一个键，应该直接返回缓存的空值
	_, err = protection.Get(testKey)
	if err == nil {
		t.Errorf("即使缓存了空值，获取不存在的键仍然应该返回错误")
	} else {
		t.Logf("正确处理了缓存的空值: %v", err)
	}
}

// TestCacheAvalancheProtection 测试缓存雪崩保护
func TestCacheAvalancheProtection(t *testing.T) {
	// 设置缓存
	memConfig := cache.NewMemoryConfig()
	memCache, err := cache.NewMemoryCache(memConfig, nil)
	if err != nil {
		t.Fatalf("创建内存缓存失败: %v", err)
	}
	defer memCache.Close()

	// 创建缓存雪崩保护
	protectionConfig := &cache.ProtectionConfig{
		JitterEnabled: true,
		JitterFactor:  0.2, // 20%的随机波动
	}

	protection := cache.NewCacheProtection(memCache, protectionConfig)

	// 设置一批键，检查它们的过期时间是否有波动
	baseExpiration := 60 * time.Second
	keyCount := 100

	expirations := make([]time.Duration, keyCount)

	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("test:jitter:%d", i)

		// 设置值
		err := protection.SetWithProtection(key, fmt.Sprintf("value-%d", i), baseExpiration)
		if err != nil {
			t.Fatalf("设置值失败: %v", err)
		}

		// 获取TTL
		ttl, err := memCache.TTL(key)
		if err != nil {
			t.Fatalf("获取TTL失败: %v", err)
		}

		expirations[i] = ttl
	}

	// 分析过期时间波动情况
	min, max := expirations[0], expirations[0]
	sum := expirations[0]

	for i := 1; i < keyCount; i++ {
		if expirations[i] < min {
			min = expirations[i]
		}
		if expirations[i] > max {
			max = expirations[i]
		}
		sum += expirations[i]
	}

	avg := sum / time.Duration(keyCount)

	// 检查最小值和最大值
	minExpected := baseExpiration * (1 - protectionConfig.JitterFactor)
	maxExpected := baseExpiration * (1 + protectionConfig.JitterFactor)

	t.Logf("过期时间统计: 最小=%v, 最大=%v, 平均=%v", min, max, avg)
	t.Logf("预期范围: 最小=%v, 最大=%v", minExpected, maxExpected)

	// 允许一定的误差
	if min < minExpected*0.9 || max > maxExpected*1.1 {
		t.Errorf("过期时间抖动超出预期范围")
	}

	// 检查是否有随机分布
	if max-min < baseExpiration*protectionConfig.JitterFactor*0.5 {
		t.Errorf("过期时间变化太小，可能未正确实现抖动")
	}
}
*/
