package test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zhoudm1743/go-cache"
)

// CacheTestSuite 定义通用的缓存测试套件
type CacheTestSuite struct {
	Name        string      // 测试套件名称
	Cache       cache.Cache // 缓存实例
	WithRedis   bool        // 是否需要Redis
	WithCleanup bool        // 是否需要清理资源
}

// CleanupFunc 清理函数类型
type CleanupFunc func()

// SetupMemoryCache 设置内存缓存测试
func SetupMemoryCache(t *testing.T) (*CacheTestSuite, CleanupFunc) {
	config := cache.NewMemoryConfig()
	config.Prefix = "test:"
	config.CleanInterval = 1 * time.Second // 更快的清理，便于测试

	memCache, err := cache.NewMemoryCache(config, nil)
	if err != nil {
		t.Fatalf("创建内存缓存失败: %v", err)
	}

	return &CacheTestSuite{
			Name:        "MemoryCache",
			Cache:       memCache,
			WithRedis:   false,
			WithCleanup: true,
		}, func() {
			memCache.Close()
		}
}

// SetupShardedMemoryCache 设置分段锁内存缓存测试
func SetupShardedMemoryCache(t *testing.T) (*CacheTestSuite, CleanupFunc) {
	config := cache.NewShardedMemoryConfig()
	config.Prefix = "test:"
	config.CleanInterval = 1 * time.Second // 更快的清理，便于测试
	config.ShardCount = 16                 // 16个分段

	shardedCache, err := cache.NewShardedMemoryCache(config, nil)
	if err != nil {
		t.Fatalf("创建分段内存缓存失败: %v", err)
	}

	return &CacheTestSuite{
			Name:        "ShardedMemoryCache",
			Cache:       shardedCache,
			WithRedis:   false,
			WithCleanup: true,
		}, func() {
			shardedCache.Close()
		}
}

// SetupFileCache 设置文件缓存测试
func SetupFileCache(t *testing.T) (*CacheTestSuite, CleanupFunc) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}

	config := cache.NewFileConfig()
	config.Prefix = "test:"
	config.FilePath = tempDir

	fileCache, err := cache.NewFileCache(config, nil)
	if err != nil {
		t.Fatalf("创建文件缓存失败: %v", err)
	}

	return &CacheTestSuite{
			Name:        "FileCache",
			Cache:       fileCache,
			WithRedis:   false,
			WithCleanup: true,
		}, func() {
			fileCache.Close()
			// 清理临时目录
			os.RemoveAll(tempDir)
		}
}

// SetupRedisCache 设置Redis缓存测试
func SetupRedisCache(t *testing.T) (*CacheTestSuite, CleanupFunc) {
	// 检查Redis是否可用
	config := cache.NewRedisConfig()
	config.Host = "localhost"
	config.Port = 6379
	config.Prefix = "test:"

	redisCache, err := cache.NewRedisCache(config, nil)
	if err != nil {
		t.Skipf("Redis不可用，跳过测试: %v", err)
		return nil, nil
	}

	// 测试连接
	err = redisCache.Ping()
	if err != nil {
		redisCache.Close()
		t.Skipf("Redis Ping失败，跳过测试: %v", err)
		return nil, nil
	}

	return &CacheTestSuite{
			Name:        "RedisCache",
			Cache:       redisCache,
			WithRedis:   true,
			WithCleanup: true,
		}, func() {
			// 删除测试数据
			keys, _ := redisCache.Keys("test:*")
			if len(keys) > 0 {
				redisCache.Del(keys...)
			}
			redisCache.Close()
		}
}

// RunTestsForCache 运行针对特定缓存实现的所有测试
func RunTestsForCache(t *testing.T, suite *CacheTestSuite) {
	if suite == nil {
		return
	}

	t.Run("Set_Get", func(t *testing.T) { TestSetGet(t, suite) })
	t.Run("Expiration", func(t *testing.T) { TestExpiration(t, suite) })
	t.Run("Del", func(t *testing.T) { TestDel(t, suite) })
	t.Run("Exists", func(t *testing.T) { TestExists(t, suite) })
	t.Run("Counters", func(t *testing.T) { TestCounters(t, suite) })

	// 哈希表操作测试
	t.Run("HSet_HGet", func(t *testing.T) { TestHashSetGet(t, suite) })
	t.Run("HGetAll", func(t *testing.T) { TestHashGetAll(t, suite) })
	t.Run("HDel", func(t *testing.T) { TestHashDel(t, suite) })
	t.Run("HExists", func(t *testing.T) { TestHashExists(t, suite) })

	// 列表操作测试（如果是Redis或分布式缓存）
	if suite.WithRedis {
		t.Run("LPush_RPush", func(t *testing.T) { TestListPushPop(t, suite) })
		t.Run("LRange", func(t *testing.T) { TestListRange(t, suite) })
	}
}

// 基础的键值操作测试

// TestSetGet 测试设置和获取值
func TestSetGet(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 测试设置和获取字符串
	err := c.Set("test:string", "hello world", 5*time.Minute)
	if err != nil {
		t.Fatalf("设置字符串失败: %v", err)
	}

	val, err := c.Get("test:string")
	if err != nil {
		t.Fatalf("获取字符串失败: %v", err)
	}

	if val != "hello world" {
		t.Errorf("获取的值不正确，期望 'hello world'，实际 '%s'", val)
	}

	// 测试设置和获取数字
	err = c.Set("test:number", "42", 5*time.Minute)
	if err != nil {
		t.Fatalf("设置数字失败: %v", err)
	}

	val, err = c.Get("test:number")
	if err != nil {
		t.Fatalf("获取数字失败: %v", err)
	}

	if val != "42" {
		t.Errorf("获取的值不正确，期望 '42'，实际 '%s'", val)
	}

	// 测试获取不存在的键
	_, err = c.Get("test:nonexistent")
	if err == nil {
		t.Error("获取不存在的键应该返回错误")
	}
}

// TestExpiration 测试过期功能
func TestExpiration(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache
	t.Logf("开始测试过期功能，缓存类型: %s", suite.Name)

	// 设置一个快速过期的键
	err := c.Set("test:expire", "will expire", 1*time.Second)
	if err != nil {
		t.Fatalf("设置过期键失败: %v", err)
	}
	t.Log("成功设置过期键，过期时间为1秒")

	// 立即检查TTL
	ttl, err := c.TTL("test:expire")
	if err != nil {
		t.Fatalf("获取TTL失败: %v", err)
	}
	t.Logf("获取TTL成功，剩余时间: %v", ttl)

	if ttl <= 0 {
		t.Errorf("TTL应该为正数，实际为 %v", ttl)
	}

	// 等待键过期
	t.Log("等待2秒钟，等待键过期...")
	time.Sleep(2 * time.Second)

	// 针对文件缓存，给异步删除一些时间完成
	if suite.Name == "FileCache" {
		t.Log("文件缓存，额外等待100ms...")
		time.Sleep(100 * time.Millisecond)
	}

	// 键应该已经过期
	val, err := c.Get("test:expire")
	t.Logf("尝试获取过期键结果: 值=%v, 错误=%v", val, err)
	if err == nil {
		t.Error("获取已过期的键应该返回错误")
	} else {
		t.Logf("获取过期键成功返回错误: %v", err)
	}

	// 测试过期时间修改
	err = c.Set("test:update-expire", "update ttl", 10*time.Minute)
	if err != nil {
		t.Fatalf("设置键失败: %v", err)
	}
	t.Log("设置第二个键成功，初始过期时间为10分钟")

	// 修改过期时间
	err = c.Expire("test:update-expire", 1*time.Second)
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}
	t.Log("修改过期时间成功，设为1秒")

	// 等待键过期
	t.Log("等待2秒钟，等待键过期...")
	time.Sleep(2 * time.Second)

	// 针对文件缓存，给异步删除一些时间完成
	if suite.Name == "FileCache" {
		t.Log("文件缓存，额外等待100ms...")
		time.Sleep(100 * time.Millisecond)
	}

	// 键应该已经过期
	val, err = c.Get("test:update-expire")
	t.Logf("尝试获取第二个过期键结果: 值=%v, 错误=%v", val, err)
	if err == nil {
		t.Error("获取已过期的键应该返回错误")
	} else {
		t.Logf("获取过期键成功返回错误: %v", err)
	}
}

// TestDel 测试删除功能
func TestDel(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置几个键
	keys := []string{"test:del1", "test:del2", "test:del3"}
	for i, key := range keys {
		err := c.Set(key, fmt.Sprintf("value%d", i), 5*time.Minute)
		if err != nil {
			t.Fatalf("设置键失败: %v", err)
		}
	}

	// 删除单个键
	count, err := c.Del("test:del1")
	if err != nil {
		t.Fatalf("删除键失败: %v", err)
	}

	if count != 1 {
		t.Errorf("删除键数量不正确，期望 1，实际 %d", count)
	}

	// 检查键是否已删除
	_, err = c.Get("test:del1")
	if err == nil {
		t.Error("获取已删除的键应该返回错误")
	}

	// 删除多个键
	count, err = c.Del("test:del2", "test:del3")
	if err != nil {
		t.Fatalf("批量删除键失败: %v", err)
	}

	if count != 2 {
		t.Errorf("删除键数量不正确，期望 2，实际 %d", count)
	}
}

// TestExists 测试键存在性检查
func TestExists(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置测试键
	err := c.Set("test:exists", "value", 5*time.Minute)
	if err != nil {
		t.Fatalf("设置键失败: %v", err)
	}

	// 检查存在的键
	count, err := c.Exists("test:exists")
	if err != nil {
		t.Fatalf("检查键存在性失败: %v", err)
	}

	if count != 1 {
		t.Errorf("存在的键数量不正确，期望 1，实际 %d", count)
	}

	// 检查不存在的键
	count, err = c.Exists("test:nonexistent")
	if err != nil {
		t.Fatalf("检查键存在性失败: %v", err)
	}

	if count != 0 {
		t.Errorf("不存在的键应该返回0，实际 %d", count)
	}

	// 检查多个键
	err = c.Set("test:exists2", "value2", 5*time.Minute)
	if err != nil {
		t.Fatalf("设置键失败: %v", err)
	}

	count, err = c.Exists("test:exists", "test:exists2", "test:nonexistent")
	if err != nil {
		t.Fatalf("检查多个键存在性失败: %v", err)
	}

	if count != 2 {
		t.Errorf("存在的键数量不正确，期望 2，实际 %d", count)
	}
}

// TestCounters 测试计数器操作
func TestCounters(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 测试递增
	val, err := c.Incr("test:counter")
	if err != nil {
		t.Fatalf("递增失败: %v", err)
	}

	if val != 1 {
		t.Errorf("递增结果不正确，期望 1，实际 %d", val)
	}

	// 再次递增
	val, err = c.Incr("test:counter")
	if err != nil {
		t.Fatalf("递增失败: %v", err)
	}

	if val != 2 {
		t.Errorf("递增结果不正确，期望 2，实际 %d", val)
	}

	// 增加指定值
	val, err = c.IncrBy("test:counter", 3)
	if err != nil {
		t.Fatalf("增加指定值失败: %v", err)
	}

	if val != 5 {
		t.Errorf("增加指定值结果不正确，期望 5，实际 %d", val)
	}

	// 递减
	val, err = c.Decr("test:counter")
	if err != nil {
		t.Fatalf("递减失败: %v", err)
	}

	if val != 4 {
		t.Errorf("递减结果不正确，期望 4，实际 %d", val)
	}

	// 使用上下文
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	val, err = c.IncrByCtx(ctx, "test:counter", 2)
	if err != nil {
		t.Fatalf("带上下文增加指定值失败: %v", err)
	}

	if val != 6 {
		t.Errorf("带上下文增加指定值结果不正确，期望 6，实际 %d", val)
	}
}

// 哈希表操作测试

// TestHashSetGet 测试哈希表设置和获取
func TestHashSetGet(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置哈希表字段
	count, err := c.HSet("test:hash", "field1", "value1", "field2", 42, "field3", "value3")
	if err != nil {
		t.Fatalf("设置哈希表字段失败: %v", err)
	}

	if count != 3 {
		t.Errorf("设置的字段数量不正确，期望 3，实际 %d", count)
	}

	// 获取单个字段
	val, err := c.HGet("test:hash", "field1")
	if err != nil {
		t.Fatalf("获取哈希表字段失败: %v", err)
	}

	if val != "value1" {
		t.Errorf("获取的字段值不正确，期望 'value1'，实际 '%s'", val)
	}

	// 获取数字字段
	val, err = c.HGet("test:hash", "field2")
	if err != nil {
		t.Fatalf("获取哈希表数字字段失败: %v", err)
	}

	// Redis和内存缓存可能对数字的处理不同，只检查是否有值
	if val == "" {
		t.Error("获取的字段值不应为空")
	}

	// 获取不存在的字段
	_, err = c.HGet("test:hash", "nonexistent")
	if err == nil {
		t.Error("获取不存在的字段应该返回错误")
	}
}

// TestHashGetAll 测试获取哈希表所有字段
func TestHashGetAll(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置哈希表
	_, err := c.HSet("test:hashall", "name", "张三", "age", "30", "city", "北京")
	if err != nil {
		t.Fatalf("设置哈希表失败: %v", err)
	}

	// 获取所有字段
	fields, err := c.HGetAll("test:hashall")
	if err != nil {
		t.Fatalf("获取所有字段失败: %v", err)
	}

	// 检查字段数量
	if len(fields) != 3 {
		t.Errorf("字段数量不正确，期望 3，实际 %d", len(fields))
	}

	// 检查字段值
	expectedFields := map[string]string{
		"name": "张三",
		"age":  "30",
		"city": "北京",
	}

	for field, expectedValue := range expectedFields {
		if value, exists := fields[field]; !exists {
			t.Errorf("字段 '%s' 不存在", field)
		} else if value != expectedValue {
			t.Errorf("字段 '%s' 的值不正确，期望 '%s'，实际 '%s'", field, expectedValue, value)
		}
	}

	// 获取不存在的哈希表
	_, err = c.HGetAll("test:nonexistent")
	if err == nil {
		// 有些实现可能返回空映射而不是错误
		fields, _ := c.HGetAll("test:nonexistent")
		if len(fields) > 0 {
			t.Error("获取不存在的哈希表应该返回空映射或错误")
		}
	}
}

// TestHashDel 测试删除哈希表字段
func TestHashDel(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置哈希表
	_, err := c.HSet("test:hashdel", "field1", "value1", "field2", "value2", "field3", "value3")
	if err != nil {
		t.Fatalf("设置哈希表失败: %v", err)
	}

	// 删除单个字段
	count, err := c.HDel("test:hashdel", "field1")
	if err != nil {
		t.Fatalf("删除哈希表字段失败: %v", err)
	}

	if count != 1 {
		t.Errorf("删除的字段数量不正确，期望 1，实际 %d", count)
	}

	// 检查字段是否已删除
	_, err = c.HGet("test:hashdel", "field1")
	if err == nil {
		t.Error("获取已删除的字段应该返回错误")
	}

	// 删除多个字段
	count, err = c.HDel("test:hashdel", "field2", "field3", "nonexistent")
	if err != nil {
		t.Fatalf("批量删除哈希表字段失败: %v", err)
	}

	if count != 2 {
		t.Errorf("删除的字段数量不正确，期望 2，实际 %d", count)
	}

	// 检查哈希表是否为空
	fields, err := c.HGetAll("test:hashdel")
	if err != nil {
		// 如果返回错误，说明哈希表可能已经被删除
		return
	}

	if len(fields) > 0 {
		t.Errorf("哈希表应该为空，实际有 %d 个字段", len(fields))
	}
}

// TestHashExists 测试检查哈希表字段是否存在
func TestHashExists(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 设置哈希表
	_, err := c.HSet("test:hashexists", "field1", "value1")
	if err != nil {
		t.Fatalf("设置哈希表失败: %v", err)
	}

	// 检查存在的字段
	exists, err := c.HExists("test:hashexists", "field1")
	if err != nil {
		t.Fatalf("检查字段存在性失败: %v", err)
	}

	if !exists {
		t.Error("字段应该存在，但返回不存在")
	}

	// 检查不存在的字段
	exists, err = c.HExists("test:hashexists", "nonexistent")
	if err != nil {
		t.Fatalf("检查字段存在性失败: %v", err)
	}

	if exists {
		t.Error("字段不应该存在，但返回存在")
	}

	// 获取哈希表长度
	length, err := c.HLen("test:hashexists")
	if err != nil {
		t.Fatalf("获取哈希表长度失败: %v", err)
	}

	if length != 1 {
		t.Errorf("哈希表长度不正确，期望 1，实际 %d", length)
	}
}

// 列表操作测试

// TestListPushPop 测试列表推入和弹出操作
func TestListPushPop(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 左侧推入
	count, err := c.LPush("test:list", "item1", "item2", "item3")
	if err != nil {
		t.Fatalf("左侧推入失败: %v", err)
	}

	if count != 3 {
		t.Errorf("推入的元素数量不正确，期望 3，实际 %d", count)
	}

	// 右侧推入
	count, err = c.RPush("test:list", "item4", "item5")
	if err != nil {
		t.Fatalf("右侧推入失败: %v", err)
	}

	if count != 5 {
		t.Errorf("列表长度不正确，期望 5，实际 %d", count)
	}

	// 获取列表长度
	length, err := c.LLen("test:list")
	if err != nil {
		t.Fatalf("获取列表长度失败: %v", err)
	}

	if length != 5 {
		t.Errorf("列表长度不正确，期望 5，实际 %d", length)
	}

	// 左侧弹出
	val, err := c.LPop("test:list")
	if err != nil {
		t.Fatalf("左侧弹出失败: %v", err)
	}

	if val != "item3" {
		t.Errorf("左侧弹出的值不正确，期望 'item3'，实际 '%s'", val)
	}

	// 右侧弹出
	val, err = c.RPop("test:list")
	if err != nil {
		t.Fatalf("右侧弹出失败: %v", err)
	}

	if val != "item5" {
		t.Errorf("右侧弹出的值不正确，期望 'item5'，实际 '%s'", val)
	}

	// 再次检查长度
	length, err = c.LLen("test:list")
	if err != nil {
		t.Fatalf("获取列表长度失败: %v", err)
	}

	if length != 3 {
		t.Errorf("列表长度不正确，期望 3，实际 %d", length)
	}
}

// TestListRange 测试列表范围获取
func TestListRange(t *testing.T, suite *CacheTestSuite) {
	c := suite.Cache

	// 准备测试数据
	_, err := c.Del("test:listrange") // 确保列表不存在
	if err != nil {
		t.Fatalf("删除列表失败: %v", err)
	}

	_, err = c.RPush("test:listrange", "item1", "item2", "item3", "item4", "item5")
	if err != nil {
		t.Fatalf("推入列表失败: %v", err)
	}

	// 获取全部范围
	items, err := c.LRange("test:listrange", 0, -1)
	if err != nil {
		t.Fatalf("获取列表范围失败: %v", err)
	}

	if len(items) != 5 {
		t.Errorf("列表元素数量不正确，期望 5，实际 %d", len(items))
	}

	// 检查元素顺序
	expected := []string{"item1", "item2", "item3", "item4", "item5"}
	for i, item := range items {
		if i < len(expected) && item != expected[i] {
			t.Errorf("元素 %d 不正确，期望 '%s'，实际 '%s'", i, expected[i], item)
		}
	}

	// 获取部分范围
	items, err = c.LRange("test:listrange", 1, 3)
	if err != nil {
		t.Fatalf("获取列表部分范围失败: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("列表部分范围元素数量不正确，期望 3，实际 %d", len(items))
	}

	// 检查部分范围元素
	expected = []string{"item2", "item3", "item4"}
	for i, item := range items {
		if i < len(expected) && item != expected[i] {
			t.Errorf("部分范围元素 %d 不正确，期望 '%s'，实际 '%s'", i, expected[i], item)
		}
	}
}

// SetupAllCacheTypes 设置所有缓存类型的测试
func SetupAllCacheTypes(t *testing.T) []*CacheTestSuite {
	var suites []*CacheTestSuite
	var cleanups []CleanupFunc

	// 设置内存缓存测试
	memSuite, memCleanup := SetupMemoryCache(t)
	if memSuite != nil {
		suites = append(suites, memSuite)
		cleanups = append(cleanups, memCleanup)
	}

	// 设置分段锁内存缓存测试
	shardedSuite, shardedCleanup := SetupShardedMemoryCache(t)
	if shardedSuite != nil {
		suites = append(suites, shardedSuite)
		cleanups = append(cleanups, shardedCleanup)
	}

	// 设置文件缓存测试
	fileSuite, fileCleanup := SetupFileCache(t)
	if fileSuite != nil {
		suites = append(suites, fileSuite)
		cleanups = append(cleanups, fileCleanup)
	}

	// 设置Redis缓存测试
	redisSuite, redisCleanup := SetupRedisCache(t)
	if redisSuite != nil {
		suites = append(suites, redisSuite)
		cleanups = append(cleanups, redisCleanup)
	}

	// 注册测试结束后的清理函数
	t.Cleanup(func() {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				cleanup()
			}
		}
	})

	return suites
}
