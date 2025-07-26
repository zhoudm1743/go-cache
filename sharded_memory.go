package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ShardedMemoryCache 基于分段锁的内存缓存实现
type ShardedMemoryCache struct {
	data          *ShardedMapWithTTL // 分段存储数据和过期时间
	prefix        string             // 键前缀
	logger        Logger             // 日志接口
	cleanInterval time.Duration      // 清理间隔
	stopCleaner   chan struct{}      // 停止清理任务的信号
	lruEnabled    bool               // 是否启用LRU
	maxEntries    int                // 最大条目数
	maxMemoryMB   int                // 最大内存(MB)
	lru           *LRUCache          // LRU缓存
	statsMu       sync.RWMutex       // 状态信息的锁
	stats         *CacheStats        // 缓存统计信息
}

// CacheStats 缓存统计信息
type CacheStats struct {
	Hits      int64 // 命中次数
	Misses    int64 // 未命中次数
	Sets      int64 // 设置次数
	Deletes   int64 // 删除次数
	KeysCount int   // 键数量
}

// ShardedMemoryConfig 分段内存缓存配置
type ShardedMemoryConfig struct {
	Prefix         string        // 键前缀
	ShardCount     int           // 分段数量，0表示使用默认值
	CleanInterval  time.Duration // 清理间隔
	MaxEntries     int           // 最大条目数，0表示不限制
	MaxMemoryMB    int           // 最大内存(MB)，0表示不限制
	EnableLRU      bool          // 是否启用LRU
	CollectMetrics bool          // 是否收集指标
}

// NewShardedMemoryCache 创建一个新的分段内存缓存
func NewShardedMemoryCache(config *ShardedMemoryConfig, log Logger) (Cache, error) {
	if log == nil {
		log = defaultLogger
	}

	if config == nil {
		config = &ShardedMemoryConfig{
			ShardCount:    DefaultShardCount,
			CleanInterval: 5 * time.Minute, // 默认5分钟清理一次
		}
	}

	log.Info("使用分段内存缓存")

	shardCount := config.ShardCount
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}

	cache := &ShardedMemoryCache{
		data:          NewShardedMapWithTTL(shardCount),
		prefix:        config.Prefix,
		logger:        log,
		cleanInterval: config.CleanInterval,
		lruEnabled:    config.EnableLRU,
		maxEntries:    config.MaxEntries,
		maxMemoryMB:   config.MaxMemoryMB,
		stats:         &CacheStats{},
	}

	// 如果设置了LRU参数，启用LRU功能
	if cache.lruEnabled {
		maxMemoryBytes := int64(config.MaxMemoryMB) * 1024 * 1024 // 转换为字节
		cache.lru = NewLRU(config.MaxEntries, maxMemoryBytes, func(key string, value interface{}) {
			// 当条目被LRU淘汰时的回调
			log.Infof("LRU淘汰键: %s", key)
		})
	}

	// 启动定期清理过期键的任务
	cache.startCleaner()

	return cache, nil
}

// startCleaner 启动过期键清理任务
func (c *ShardedMemoryCache) startCleaner() {
	if c.cleanInterval <= 0 {
		return // 不启动清理任务
	}

	c.stopCleaner = make(chan struct{})
	go func() {
		ticker := time.NewTicker(c.cleanInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.cleanExpired()
			case <-c.stopCleaner:
				return
			}
		}
	}()
}

// cleanExpired 清理过期的键
func (c *ShardedMemoryCache) cleanExpired() {
	now := time.Now().Unix()

	// 遍历所有分段查找并删除过期键
	c.data.ForEach(func(key string, value interface{}) bool {
		// 检查键是否过期
		expTime, exists := c.data.GetTTL(key)
		if exists && expTime > 0 && expTime <= now {
			// 键已过期，删除它
			c.data.Delete(key)
			c.data.ttlMap.Delete(key)

			if c.logger != nil {
				c.logger.Debugf("过期键已删除: %s", key)
			}
		}
		return true // 继续遍历
	})
}

// buildKey 构建带前缀的键
func (c *ShardedMemoryCache) buildKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + key
}

// Get 获取缓存
func (c *ShardedMemoryCache) Get(key string) (string, error) {
	return c.GetCtx(context.Background(), key)
}

// GetCtx 获取缓存
func (c *ShardedMemoryCache) GetCtx(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	fullKey := c.buildKey(key)

	// 如果启用了LRU，从LRU缓存中获取
	if c.lruEnabled && c.lru != nil {
		if val, ok := c.lru.Get(fullKey); ok {
			c.incrementHits()
			if str, ok := val.(string); ok {
				return str, nil
			}
			return "", ErrorWithContext(ErrTypeMismatch, "从LRU缓存获取值")
		}
		c.incrementMisses()
		return "", ErrorWithContext(ErrKeyNotFound, "在LRU缓存中查找键")
	}

	// 使用分段映射获取值
	value, exists := c.data.Get(fullKey)
	if !exists {
		c.incrementMisses()
		return "", ErrorWithContext(ErrKeyNotFound, "在分段缓存中查找键")
	}

	// 检查键是否过期
	expTime, hasExp := c.data.GetTTL(fullKey)
	if hasExp && expTime > 0 && expTime <= time.Now().Unix() {
		c.data.Delete(fullKey)
		c.data.ttlMap.Delete(fullKey)
		c.incrementMisses()
		return "", ErrorWithContext(ErrKeyNotFound, "键已过期")
	}

	c.incrementHits()
	str, ok := value.(string)
	if !ok {
		return "", ErrorWithContext(ErrTypeMismatch, "从分段缓存获取值")
	}

	return str, nil
}

// Set 设置缓存
func (c *ShardedMemoryCache) Set(key string, value interface{}, expiration time.Duration) error {
	return c.SetCtx(context.Background(), key, value, expiration)
}

// SetCtx 设置缓存
func (c *ShardedMemoryCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fullKey := c.buildKey(key)

	// 如果启用了LRU，使用LRU缓存
	if c.lruEnabled && c.lru != nil {
		// 估算值的大小
		size := estimateSize(value)
		c.lru.Add(fullKey, value, size, expiration)
		c.incrementSets()
		return nil
	}

	// 计算过期时间戳
	var expTime int64
	if expiration > 0 {
		expTime = time.Now().Add(expiration).Unix()
	}

	// 使用分段映射设置值和过期时间
	c.data.SetWithTTL(fullKey, value, expTime)
	c.incrementSets()

	return nil
}

// Del 删除缓存
func (c *ShardedMemoryCache) Del(keys ...string) (int64, error) {
	return c.DelCtx(context.Background(), keys...)
}

// DelCtx 删除缓存
func (c *ShardedMemoryCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var count int64

	for _, key := range keys {
		fullKey := c.buildKey(key)

		// 如果启用了LRU，从LRU缓存中删除
		if c.lruEnabled && c.lru != nil {
			c.lru.Remove(fullKey)
			count++
			continue
		}

		// 从分段映射中删除
		if c.data.Exists(fullKey) {
			c.data.Delete(fullKey)
			c.data.ttlMap.Delete(fullKey)
			count++
		}
	}

	if count > 0 {
		c.incrementDeletes(count)
	}

	return count, nil
}

// Exists 检查键是否存在
func (c *ShardedMemoryCache) Exists(keys ...string) (int64, error) {
	return c.ExistsCtx(context.Background(), keys...)
}

// ExistsCtx 检查键是否存在
func (c *ShardedMemoryCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var count int64

	for _, key := range keys {
		fullKey := c.buildKey(key)

		// 检查LRU缓存
		if c.lruEnabled && c.lru != nil {
			if _, exists := c.lru.Get(fullKey); exists {
				count++
			}
			continue
		}

		// 检查分段映射
		if c.data.Exists(fullKey) {
			// 检查是否过期
			expTime, hasExp := c.data.GetTTL(fullKey)
			if !hasExp || expTime == 0 || expTime > time.Now().Unix() {
				count++
			}
		}
	}

	return count, nil
}

// Expire 设置过期时间
func (c *ShardedMemoryCache) Expire(key string, expiration time.Duration) error {
	return c.ExpireCtx(context.Background(), key, expiration)
}

// ExpireCtx 设置过期时间
func (c *ShardedMemoryCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fullKey := c.buildKey(key)

	// 设置LRU缓存的过期时间
	if c.lruEnabled && c.lru != nil {
		val, exists := c.lru.Get(fullKey)
		if !exists {
			return ErrorWithContext(ErrKeyNotFound, "在LRU缓存中设置过期时间")
		}

		// 更新LRU缓存中的过期时间
		size := estimateSize(val)
		c.lru.Add(fullKey, val, size, expiration)
		return nil
	}

	// 设置分段映射中的过期时间
	if !c.data.Exists(fullKey) {
		return ErrorWithContext(ErrKeyNotFound, "在分段缓存中设置过期时间")
	}

	// 计算过期时间戳
	var expTime int64
	if expiration > 0 {
		expTime = time.Now().Add(expiration).Unix()
	}

	// 获取原值并保持不变
	if val, ok := c.data.Get(fullKey); ok {
		// 更新值和过期时间
		c.data.SetWithTTL(fullKey, val, expTime)
	}

	return nil
}

// TTL 获取剩余生存时间
func (c *ShardedMemoryCache) TTL(key string) (time.Duration, error) {
	return c.TTLCtx(context.Background(), key)
}

// TTLCtx 获取剩余生存时间
func (c *ShardedMemoryCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	fullKey := c.buildKey(key)

	// 检查键是否存在
	exists := false
	var expTime int64 = -1

	// 从LRU缓存中获取TTL
	if c.lruEnabled && c.lru != nil {
		// LRU缓存不直接支持TTL查询，我们需要借助底层映射
		if ttl, ok := c.lru.GetTTL(fullKey); ok {
			return ttl, nil
		}
		return -1 * time.Second, ErrorWithContext(ErrKeyNotFound, "键不存在或没有过期时间")
	}

	// 从分段映射中获取TTL
	if _, ok := c.data.Get(fullKey); ok {
		exists = true
		// 获取过期时间
		if exp, hasExp := c.data.GetTTL(fullKey); hasExp && exp > 0 {
			expTime = exp
		}
	}

	if !exists {
		return -2 * time.Second, ErrorWithContext(ErrKeyNotFound, "键不存在")
	}

	if expTime == -1 || expTime == 0 {
		return -1 * time.Second, nil // 表示永久键
	}

	// 计算剩余时间
	now := time.Now().Unix()
	if expTime <= now {
		// 键已过期
		return -2 * time.Second, ErrorWithContext(ErrKeyNotFound, "键已过期")
	}

	remaining := time.Duration(expTime-now) * time.Second
	return remaining, nil
}

// 增加统计计数器
func (c *ShardedMemoryCache) incrementHits() {
	if c.stats != nil {
		c.statsMu.Lock()
		c.stats.Hits++
		c.statsMu.Unlock()
	}
}

func (c *ShardedMemoryCache) incrementMisses() {
	if c.stats != nil {
		c.statsMu.Lock()
		c.stats.Misses++
		c.statsMu.Unlock()
	}
}

func (c *ShardedMemoryCache) incrementSets() {
	if c.stats != nil {
		c.statsMu.Lock()
		c.stats.Sets++
		c.statsMu.Unlock()
	}
}

func (c *ShardedMemoryCache) incrementDeletes(count int64) {
	if c.stats != nil {
		c.statsMu.Lock()
		c.stats.Deletes += count
		c.statsMu.Unlock()
	}
}

// GetStats 获取缓存统计信息
func (c *ShardedMemoryCache) GetStats() *CacheStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()

	// 创建统计信息副本
	stats := &CacheStats{
		Hits:    c.stats.Hits,
		Misses:  c.stats.Misses,
		Sets:    c.stats.Sets,
		Deletes: c.stats.Deletes,
	}

	// 获取键数量
	stats.KeysCount = c.data.Size()

	return stats
}

// Close 关闭缓存
func (c *ShardedMemoryCache) Close() error {
	// 停止清理任务
	if c.stopCleaner != nil {
		close(c.stopCleaner)
	}

	// 清空数据
	c.data.Clear()

	// 清空LRU缓存
	if c.lru != nil {
		c.lru.Clear()
	}

	return nil
}

// GetClient 获取原始客户端（内存版本返回nil）
func (c *ShardedMemoryCache) GetClient() interface{} {
	return nil
}

// 以下是基本的数值操作实现

// Incr 将键的值加1
func (c *ShardedMemoryCache) Incr(key string) (int64, error) {
	return c.IncrBy(key, 1)
}

// IncrCtx 将键的值加1（带上下文）
func (c *ShardedMemoryCache) IncrCtx(ctx context.Context, key string) (int64, error) {
	return c.IncrByCtx(ctx, key, 1)
}

// Decr 将键的值减1
func (c *ShardedMemoryCache) Decr(key string) (int64, error) {
	return c.IncrBy(key, -1)
}

// DecrCtx 将键的值减1（带上下文）
func (c *ShardedMemoryCache) DecrCtx(ctx context.Context, key string) (int64, error) {
	return c.IncrByCtx(ctx, key, -1)
}

// IncrBy 将键的值增加指定的量
func (c *ShardedMemoryCache) IncrBy(key string, value int64) (int64, error) {
	return c.IncrByCtx(context.Background(), key, value)
}

// IncrByCtx 将键的值增加指定的量（带上下文）
func (c *ShardedMemoryCache) IncrByCtx(ctx context.Context, key string, value int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	fullKey := c.buildKey(key)

	// 获取当前值
	var currentVal int64 = 0
	var valExists bool

	// 从LRU缓存或分段映射获取值
	if c.lruEnabled && c.lru != nil {
		if val, ok := c.lru.Get(fullKey); ok {
			valStr, isString := val.(string)
			if isString {
				parsedVal, err := parseMemoryInt64(valStr)
				if err != nil {
					return 0, err
				}
				currentVal = parsedVal
				valExists = true
			}
		}
	} else {
		if val, ok := c.data.Get(fullKey); ok {
			valStr, isString := val.(string)
			if isString {
				parsedVal, err := parseMemoryInt64(valStr)
				if err != nil {
					return 0, err
				}
				currentVal = parsedVal
				valExists = true
			}
		}
	}

	// 计算新值
	newVal := currentVal + value

	// 设置新值
	// 如果键不存在，将其设置为永久键
	var ttl time.Duration = 0
	if valExists {
		// 保持原来的过期时间
		if c.lruEnabled && c.lru != nil {
			// 对于LRU缓存，我们需要重新计算TTL
			if remaining, ok := c.lru.GetTTL(fullKey); ok && remaining > 0 {
				ttl = remaining
			}
		} else {
			if exp, hasExp := c.data.GetTTL(fullKey); hasExp && exp > 0 {
				// 计算剩余时间
				now := time.Now().Unix()
				if exp > now {
					ttl = time.Duration(exp-now) * time.Second
				}
			}
		}
	}

	// 设置新值
	err := c.SetCtx(ctx, key, fmt.Sprintf("%d", newVal), ttl)
	if err != nil {
		return 0, err
	}

	return newVal, nil
}

// 添加Ping方法
func (c *ShardedMemoryCache) Ping() error {
	return nil
}

func (c *ShardedMemoryCache) PingCtx(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Keys 根据模式获取键列表
func (c *ShardedMemoryCache) Keys(pattern string) ([]string, error) {
	return c.KeysCtx(context.Background(), pattern)
}

// KeysCtx 根据模式获取键列表（带上下文）
func (c *ShardedMemoryCache) KeysCtx(ctx context.Context, pattern string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var result []string

	// 获取所有键
	var keys []string

	if c.lruEnabled && c.lru != nil {
		// 从LRU缓存获取键
		keys = c.lru.Keys()
	} else {
		// 从分段映射获取键
		keys = c.data.Keys()
	}

	// 对获取的键进行匹配
	prefixLen := len(c.prefix)
	for _, key := range keys {
		// 如果有前缀，移除前缀再匹配
		matchKey := key
		if c.prefix != "" && len(key) > prefixLen {
			matchKey = key[prefixLen:]
		}

		// 使用通配符匹配
		if matchPattern(pattern, matchKey) {
			result = append(result, matchKey)
		}
	}

	return result, nil
}

// ================== 哈希表操作 ==================

// HGet 获取哈希表中的字段值
func (c *ShardedMemoryCache) HGet(key, field string) (string, error) {
	return c.HGetCtx(context.Background(), key, field)
}

// HGetCtx 获取哈希表中的字段值（带上下文）
func (c *ShardedMemoryCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	fullKey := c.buildKey(key)

	// 获取哈希表
	hashMap, err := c.getHashMap(fullKey)
	if err != nil {
		return "", err
	}

	// 获取字段值
	if val, ok := hashMap[field]; ok {
		if str, isStr := val.(string); isStr {
			return str, nil
		}
		return fmt.Sprint(val), nil
	}

	return "", ErrorWithContext(ErrFieldNotFound, fmt.Sprintf("字段'%s'不存在", field))
}

// getHashMap 获取哈希表
func (c *ShardedMemoryCache) getHashMap(key string) (map[string]interface{}, error) {
	// 从LRU缓存获取
	if c.lruEnabled && c.lru != nil {
		if val, ok := c.lru.Get(key); ok {
			if m, isMap := val.(map[string]interface{}); isMap {
				return m, nil
			}
			return nil, ErrorWithContext(ErrTypeMismatch, "值不是哈希表")
		}
		return nil, ErrorWithContext(ErrKeyNotFound, "哈希表不存在")
	}

	// 从分段映射获取
	val, exists := c.data.Get(key)
	if !exists {
		return nil, ErrorWithContext(ErrKeyNotFound, "哈希表不存在")
	}

	// 检查是否过期
	if ttl, hasExp := c.data.GetTTL(key); hasExp && ttl > 0 && ttl <= time.Now().Unix() {
		c.data.Delete(key)
		c.data.ttlMap.Delete(key)
		return nil, ErrorWithContext(ErrKeyNotFound, "哈希表已过期")
	}

	if m, isMap := val.(map[string]interface{}); isMap {
		return m, nil
	}

	return nil, ErrorWithContext(ErrTypeMismatch, "值不是哈希表")
}

// HSet 设置哈希表中的字段值
func (c *ShardedMemoryCache) HSet(key string, values ...interface{}) (int64, error) {
	return c.HSetCtx(context.Background(), key, values...)
}

// HSetCtx 设置哈希表中的字段值（带上下文）
func (c *ShardedMemoryCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if len(values) == 0 || len(values)%2 != 0 {
		return 0, ErrorWithContext(ErrInvalidArgument, "values参数必须为偶数个")
	}

	fullKey := c.buildKey(key)

	// 获取哈希表，如果不存在则创建
	var hashMap map[string]interface{}
	var err error
	var isNew bool

	hashMap, err = c.getHashMap(fullKey)
	if err != nil {
		// 如果是不存在错误，则创建新哈希表
		if errors.Is(err, ErrKeyNotFound) {
			hashMap = make(map[string]interface{})
			isNew = true
		} else {
			return 0, err
		}
	}

	// 设置字段值
	var count int64
	for i := 0; i < len(values); i += 2 {
		fieldName := fmt.Sprint(values[i])
		fieldValue := values[i+1]

		// 如果是第一次设置该字段，计数加一
		if _, exists := hashMap[fieldName]; !exists {
			count++
		}

		hashMap[fieldName] = fieldValue
	}

	// 存储更新后的哈希表
	if c.lruEnabled && c.lru != nil {
		// 对于LRU缓存，使用永久键存储哈希表
		c.lru.Add(fullKey, hashMap, estimateSize(hashMap), 0)
	} else {
		// 存储到分段映射
		// 如果是新哈希表，使用永久键
		if isNew {
			c.data.SetWithTTL(fullKey, hashMap, 0)
		} else {
			// 获取原来的过期时间（如果有）
			var expTime int64
			if exp, hasExp := c.data.GetTTL(fullKey); hasExp {
				expTime = exp
			}
			c.data.SetWithTTL(fullKey, hashMap, expTime)
		}
	}

	return count, nil
}

// HDel 删除哈希表中的字段
func (c *ShardedMemoryCache) HDel(key string, fields ...string) (int64, error) {
	return c.HDelCtx(context.Background(), key, fields...)
}

// HDelCtx 删除哈希表中的字段（带上下文）
func (c *ShardedMemoryCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if len(fields) == 0 {
		return 0, nil
	}

	fullKey := c.buildKey(key)

	// 获取哈希表
	hashMap, err := c.getHashMap(fullKey)
	if err != nil {
		return 0, err
	}

	// 删除字段
	var count int64
	for _, field := range fields {
		if _, exists := hashMap[field]; exists {
			delete(hashMap, field)
			count++
		}
	}

	// 如果删除后哈希表为空，则删除整个键
	if len(hashMap) == 0 {
		_, _ = c.DelCtx(ctx, key)
		return count, nil
	}

	// 存储更新后的哈希表
	if c.lruEnabled && c.lru != nil {
		// 获取原来的过期时间（如果有）
		var ttl time.Duration = 0
		if remaining, ok := c.lru.GetTTL(fullKey); ok && remaining > 0 {
			ttl = remaining
		}
		c.lru.Add(fullKey, hashMap, estimateSize(hashMap), ttl)
	} else {
		// 获取原来的过期时间（如果有）
		var expTime int64
		if exp, hasExp := c.data.GetTTL(fullKey); hasExp {
			expTime = exp
			// 更新值和过期时间
			c.data.SetWithTTL(fullKey, hashMap, expTime)
		} else {
			// 没有过期时间，使用0（永久）
			c.data.SetWithTTL(fullKey, hashMap, 0)
		}
	}

	return count, nil
}

// HGetAll 获取哈希表中的所有字段和值
func (c *ShardedMemoryCache) HGetAll(key string) (map[string]string, error) {
	return c.HGetAllCtx(context.Background(), key)
}

// HGetAllCtx 获取哈希表中的所有字段和值（带上下文）
func (c *ShardedMemoryCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fullKey := c.buildKey(key)

	// 获取哈希表
	hashMap, err := c.getHashMap(fullKey)
	if err != nil {
		return nil, err
	}

	// 转换为字符串映射
	result := make(map[string]string, len(hashMap))
	for k, v := range hashMap {
		result[k] = fmt.Sprint(v)
	}

	return result, nil
}

// HExists 检查哈希表中是否存在指定字段
func (c *ShardedMemoryCache) HExists(key, field string) (bool, error) {
	return c.HExistsCtx(context.Background(), key, field)
}

// HExistsCtx 检查哈希表中是否存在指定字段（带上下文）
func (c *ShardedMemoryCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	fullKey := c.buildKey(key)

	// 获取哈希表
	hashMap, err := c.getHashMap(fullKey)
	if err != nil {
		return false, err
	}

	// 检查字段是否存在
	_, exists := hashMap[field]
	return exists, nil
}

// HLen 获取哈希表中字段的数量
func (c *ShardedMemoryCache) HLen(key string) (int64, error) {
	return c.HLenCtx(context.Background(), key)
}

// HLenCtx 获取哈希表中字段的数量（带上下文）
func (c *ShardedMemoryCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	fullKey := c.buildKey(key)

	// 获取哈希表
	hashMap, err := c.getHashMap(fullKey)
	if err != nil {
		return 0, err
	}

	return int64(len(hashMap)), nil
}

// ================== 列表操作 ==================

// LPush 将值推入列表左端
func (c *ShardedMemoryCache) LPush(key string, values ...interface{}) (int64, error) {
	return c.LPushCtx(context.Background(), key, values...)
}

// LPushCtx 将值推入列表左端（带上下文）
func (c *ShardedMemoryCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// RPush 将值推入列表右端
func (c *ShardedMemoryCache) RPush(key string, values ...interface{}) (int64, error) {
	return c.RPushCtx(context.Background(), key, values...)
}

// RPushCtx 将值推入列表右端（带上下文）
func (c *ShardedMemoryCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// LPop 从列表左端弹出值
func (c *ShardedMemoryCache) LPop(key string) (string, error) {
	return c.LPopCtx(context.Background(), key)
}

// LPopCtx 从列表左端弹出值（带上下文）
func (c *ShardedMemoryCache) LPopCtx(ctx context.Context, key string) (string, error) {
	return "", ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// RPop 从列表右端弹出值
func (c *ShardedMemoryCache) RPop(key string) (string, error) {
	return c.RPopCtx(context.Background(), key)
}

// RPopCtx 从列表右端弹出值（带上下文）
func (c *ShardedMemoryCache) RPopCtx(ctx context.Context, key string) (string, error) {
	return "", ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// LLen 获取列表长度
func (c *ShardedMemoryCache) LLen(key string) (int64, error) {
	return c.LLenCtx(context.Background(), key)
}

// LLenCtx 获取列表长度（带上下文）
func (c *ShardedMemoryCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// LRange 获取列表范围
func (c *ShardedMemoryCache) LRange(key string, start, stop int64) ([]string, error) {
	return c.LRangeCtx(context.Background(), key, start, stop)
}

// LRangeCtx 获取列表范围（带上下文）
func (c *ShardedMemoryCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return nil, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持列表操作")
}

// ================== 集合操作 ==================

// SAdd 添加成员到集合
func (c *ShardedMemoryCache) SAdd(key string, members ...interface{}) (int64, error) {
	return c.SAddCtx(context.Background(), key, members...)
}

// SAddCtx 添加成员到集合（带上下文）
func (c *ShardedMemoryCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持集合操作")
}

// SRem 从集合中移除成员
func (c *ShardedMemoryCache) SRem(key string, members ...interface{}) (int64, error) {
	return c.SRemCtx(context.Background(), key, members...)
}

// SRemCtx 从集合中移除成员（带上下文）
func (c *ShardedMemoryCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持集合操作")
}

// SMembers 获取集合所有成员
func (c *ShardedMemoryCache) SMembers(key string) ([]string, error) {
	return c.SMembersCtx(context.Background(), key)
}

// SMembersCtx 获取集合所有成员（带上下文）
func (c *ShardedMemoryCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	return nil, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持集合操作")
}

// SIsMember 检查成员是否在集合中
func (c *ShardedMemoryCache) SIsMember(key string, member interface{}) (bool, error) {
	return c.SIsMemberCtx(context.Background(), key, member)
}

// SIsMemberCtx 检查成员是否在集合中（带上下文）
func (c *ShardedMemoryCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	return false, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持集合操作")
}

// SCard 获取集合成员数
func (c *ShardedMemoryCache) SCard(key string) (int64, error) {
	return c.SCardCtx(context.Background(), key)
}

// SCardCtx 获取集合成员数（带上下文）
func (c *ShardedMemoryCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持集合操作")
}

// ================== 有序集合操作 ==================

// ZAdd 添加成员到有序集合
func (c *ShardedMemoryCache) ZAdd(key string, members ...Z) (int64, error) {
	return c.ZAddCtx(context.Background(), key, members...)
}

// ZAddCtx 添加成员到有序集合（带上下文）
func (c *ShardedMemoryCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ZRem 从有序集合中移除成员
func (c *ShardedMemoryCache) ZRem(key string, members ...interface{}) (int64, error) {
	return c.ZRemCtx(context.Background(), key, members...)
}

// ZRemCtx 从有序集合中移除成员（带上下文）
func (c *ShardedMemoryCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ZRange 获取有序集合范围
func (c *ShardedMemoryCache) ZRange(key string, start, stop int64) ([]string, error) {
	return c.ZRangeCtx(context.Background(), key, start, stop)
}

// ZRangeCtx 获取有序集合范围（带上下文）
func (c *ShardedMemoryCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return nil, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ZRangeWithScores 获取有序集合范围（带分数）
func (c *ShardedMemoryCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	return c.ZRangeWithScoresCtx(context.Background(), key, start, stop)
}

// ZRangeWithScoresCtx 获取有序集合范围（带分数，带上下文）
func (c *ShardedMemoryCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	return nil, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ZCard 获取有序集合成员数
func (c *ShardedMemoryCache) ZCard(key string) (int64, error) {
	return c.ZCardCtx(context.Background(), key)
}

// ZCardCtx 获取有序集合成员数（带上下文）
func (c *ShardedMemoryCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ZScore 获取成员的分数
func (c *ShardedMemoryCache) ZScore(key, member string) (float64, error) {
	return c.ZScoreCtx(context.Background(), key, member)
}

// ZScoreCtx 获取成员的分数（带上下文）
func (c *ShardedMemoryCache) ZScoreCtx(ctx context.Context, key, member string) (float64, error) {
	return 0, ErrorWithContext(ErrNotSupported, "分段锁内存缓存不支持有序集合操作")
}

// ================== 缓存预热操作 ==================

// Warmup 使用数据加载器和键生成器预热缓存
func (c *ShardedMemoryCache) Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error {
	if generator == nil {
		return ErrorWithContext(ErrInvalidArgument, "键生成器不能为空")
	}

	// 生成键
	keys, err := generator.GenerateKeys(ctx)
	if err != nil {
		return WrapError(err, "生成预热键失败")
	}

	// 使用键和加载器预热
	return c.WarmupKeys(ctx, keys, loader)
}

// WarmupKeys 使用指定的键列表和数据加载器预热缓存
func (c *ShardedMemoryCache) WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error {
	if loader == nil {
		return ErrorWithContext(ErrInvalidArgument, "数据加载器不能为空")
	}

	if len(keys) == 0 {
		return nil // 没有键需要预热
	}

	// 记录错误但继续执行
	var lastErr error

	for _, key := range keys {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// 加载数据
			value, expiration, err := loader.LoadData(ctx, key)
			if err != nil {
				c.logger.WithFields(map[string]interface{}{
					"key": key,
					"err": err.Error(),
				}).Error("预热键失败")
				lastErr = err
				continue
			}

			// 设置到缓存
			err = c.SetCtx(ctx, key, value, expiration)
			if err != nil {
				c.logger.WithFields(map[string]interface{}{
					"key": key,
					"err": err.Error(),
				}).Error("预热键设置失败")
				lastErr = err
			}
		}
	}

	return lastErr
}
