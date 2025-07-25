package cache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

func (z ZMembers) Len() int           { return len(z) }
func (z ZMembers) Less(i, j int) bool { return z[i].Score < z[j].Score }
func (z ZMembers) Swap(i, j int)      { z[i], z[j] = z[j], z[i] }

// MemoryCache 内存缓存实现
type MemoryCache struct {
	data   map[string]interface{}
	expiry map[string]time.Time
	prefix string
	mu     sync.RWMutex
	logger Logger
}

// NewMemoryCache 创建内存缓存
func NewMemoryCache(cfg *MemoryConfig, log Logger) (Cache, error) {
	if log == nil {
		log = defaultLogger
	}

	log.Info("使用内存缓存")

	return &MemoryCache{
		data:   make(map[string]interface{}),
		expiry: make(map[string]time.Time),
		prefix: cfg.Prefix,
		logger: log,
	}, nil
}

// buildKey 构建带前缀的键
func (m *MemoryCache) buildKey(key string) string {
	if m.prefix == "" {
		return key
	}
	return m.prefix + key
}

// isExpired 检查键是否过期
func (m *MemoryCache) isExpired(key string) bool {
	if exp, ok := m.expiry[key]; ok {
		return exp.Before(time.Now())
	}
	return false
}

// cleanExpired 清理过期的键
func (m *MemoryCache) cleanExpired(key string) {
	if m.isExpired(key) {
		delete(m.data, key)
		delete(m.expiry, key)
	}
}

// GetClient 获取Redis客户端（内存版本返回nil）
func (m *MemoryCache) GetClient() interface{} {
	return nil
}

// Close 关闭连接
func (m *MemoryCache) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]interface{})
	m.expiry = make(map[string]time.Time)
	return nil
}

// Get 获取缓存
func (m *MemoryCache) Get(key string) (string, error) {
	return m.GetCtx(context.Background(), key)
}

// Set 设置缓存
func (m *MemoryCache) Set(key string, value interface{}, expiration time.Duration) error {
	return m.SetCtx(context.Background(), key, value, expiration)
}

// Del 删除缓存
func (m *MemoryCache) Del(keys ...string) (int64, error) {
	return m.DelCtx(context.Background(), keys...)
}

// Exists 检查键是否存在
func (m *MemoryCache) Exists(keys ...string) (int64, error) {
	return m.ExistsCtx(context.Background(), keys...)
}

// Expire 设置过期时间
func (m *MemoryCache) Expire(key string, expiration time.Duration) error {
	return m.ExpireCtx(context.Background(), key, expiration)
}

// TTL 获取过期时间
func (m *MemoryCache) TTL(key string) (time.Duration, error) {
	return m.TTLCtx(context.Background(), key)
}

// GetCtx 获取缓存
func (m *MemoryCache) GetCtx(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	if val, ok := m.data[fullKey]; ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return "", ErrTypeMismatch
	}

	return "", ErrKeyNotFound
}

// SetCtx 设置缓存
func (m *MemoryCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.data[fullKey] = value

	if expiration > 0 {
		m.expiry[fullKey] = time.Now().Add(expiration)
	} else {
		delete(m.expiry, fullKey)
	}

	return nil
}

// DelCtx 删除缓存
func (m *MemoryCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var count int64
	for _, key := range keys {
		fullKey := m.buildKey(key)
		if _, ok := m.data[fullKey]; ok {
			delete(m.data, fullKey)
			delete(m.expiry, fullKey)
			count++
		}
	}

	return count, nil
}

// ExistsCtx 检查键是否存在
func (m *MemoryCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var count int64
	for _, key := range keys {
		fullKey := m.buildKey(key)
		m.cleanExpired(fullKey)
		if _, ok := m.data[fullKey]; ok {
			count++
		}
	}

	return count, nil
}

// ExpireCtx 设置过期时间
func (m *MemoryCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	if _, ok := m.data[fullKey]; !ok {
		return ErrKeyNotFound
	}

	if expiration > 0 {
		m.expiry[fullKey] = time.Now().Add(expiration)
	} else {
		delete(m.expiry, fullKey)
	}

	return nil
}

// TTLCtx 获取过期时间
func (m *MemoryCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	if _, ok := m.data[fullKey]; !ok {
		return 0, ErrKeyNotFound
	}

	if exp, ok := m.expiry[fullKey]; ok {
		remaining := exp.Sub(time.Now())
		if remaining < 0 {
			// 过期了，应该被清理，但由于我们可能并行访问，所以这里再次检查
			delete(m.data, fullKey)
			delete(m.expiry, fullKey)
			return 0, ErrKeyNotFound
		}
		return remaining, nil
	}

	// 如果没有设置过期时间，返回-1表示永不过期
	return -1, nil
}

// Decr 递减
func (m *MemoryCache) Decr(key string) (int64, error) {
	return m.DecrCtx(context.Background(), key)
}

// DecrCtx 递减
func (m *MemoryCache) DecrCtx(ctx context.Context, key string) (int64, error) {
	return m.IncrByCtx(ctx, key, -1)
}

// Incr 递增
func (m *MemoryCache) Incr(key string) (int64, error) {
	return m.IncrCtx(context.Background(), key)
}

// IncrCtx 递增
func (m *MemoryCache) IncrCtx(ctx context.Context, key string) (int64, error) {
	return m.IncrByCtx(ctx, key, 1)
}

// IncrBy 按指定值递增
func (m *MemoryCache) IncrBy(key string, value int64) (int64, error) {
	return m.IncrByCtx(context.Background(), key, value)
}

// IncrByCtx 按指定值递增
func (m *MemoryCache) IncrByCtx(ctx context.Context, key string, value int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	var current int64
	if val, ok := m.data[fullKey]; ok {
		switch v := val.(type) {
		case int64:
			current = v
		case string:
			if num, err := parseMemoryInt64(v); err == nil {
				current = num
			} else {
				return 0, err
			}
		default:
			return 0, errors.New("值类型无法递增")
		}
	}

	current += value
	m.data[fullKey] = current
	return current, nil
}

// parseMemoryInt64 将字符串转换为int64
func parseMemoryInt64(s string) (int64, error) {
	var val int64
	if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
		return 0, err
	}
	return val, nil
}

// HGet 获取哈希表字段值
func (m *MemoryCache) HGet(key, field string) (string, error) {
	return m.HGetCtx(context.Background(), key, field)
}

// HSet 设置哈希表字段值
func (m *MemoryCache) HSet(key string, values ...interface{}) (int64, error) {
	return m.HSetCtx(context.Background(), key, values...)
}

// HDel 删除哈希表字段
func (m *MemoryCache) HDel(key string, fields ...string) (int64, error) {
	return m.HDelCtx(context.Background(), key, fields...)
}

// HGetAll 获取哈希表所有字段值
func (m *MemoryCache) HGetAll(key string) (map[string]string, error) {
	return m.HGetAllCtx(context.Background(), key)
}

// HExists 检查哈希表字段是否存在
func (m *MemoryCache) HExists(key, field string) (bool, error) {
	return m.HExistsCtx(context.Background(), key, field)
}

// HLen 获取哈希表字段数量
func (m *MemoryCache) HLen(key string) (int64, error) {
	return m.HLenCtx(context.Background(), key)
}

// HGetCtx 获取哈希表字段值
func (m *MemoryCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return "", ErrKeyNotFound
	}

	hashMap, ok := val.(map[string]interface{})
	if !ok {
		return "", errors.New("值类型不是哈希表")
	}

	fieldVal, ok := hashMap[field]
	if !ok {
		return "", ErrKeyNotFound
	}

	switch v := fieldVal.(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// HSetCtx 设置哈希表字段值
func (m *MemoryCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	if len(values)%2 != 0 {
		return 0, errors.New("哈希表字段和值必须成对出现")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	var hashMap map[string]interface{}

	// 如果键存在，获取现有哈希表
	if val, ok := m.data[fullKey]; ok {
		existingMap, ok := val.(map[string]interface{})
		if !ok {
			return 0, errors.New("值类型不是哈希表")
		}
		hashMap = existingMap
	} else {
		// 否则创建新哈希表
		hashMap = make(map[string]interface{})
		m.data[fullKey] = hashMap
	}

	// 设置字段值
	var count int64
	for i := 0; i < len(values); i += 2 {
		fieldName, ok := values[i].(string)
		if !ok {
			return count, fmt.Errorf("字段名必须是字符串，位置 %d", i)
		}

		_, exists := hashMap[fieldName]
		hashMap[fieldName] = values[i+1]

		if !exists {
			count++
		}
	}

	return count, nil
}

// HDelCtx 删除哈希表字段
func (m *MemoryCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	hashMap, ok := val.(map[string]interface{})
	if !ok {
		return 0, errors.New("值类型不是哈希表")
	}

	var count int64
	for _, field := range fields {
		if _, ok := hashMap[field]; ok {
			delete(hashMap, field)
			count++
		}
	}

	return count, nil
}

// HGetAllCtx 获取哈希表所有字段值
func (m *MemoryCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return make(map[string]string), nil
	}

	hashMap, ok := val.(map[string]interface{})
	if !ok {
		return nil, errors.New("值类型不是哈希表")
	}

	result := make(map[string]string, len(hashMap))
	for k, v := range hashMap {
		switch vt := v.(type) {
		case string:
			result[k] = vt
		default:
			result[k] = fmt.Sprintf("%v", vt)
		}
	}

	return result, nil
}

// HExistsCtx 检查哈希表字段是否存在
func (m *MemoryCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return false, nil
	}

	hashMap, ok := val.(map[string]interface{})
	if !ok {
		return false, errors.New("值类型不是哈希表")
	}

	_, exists := hashMap[field]
	return exists, nil
}

// HLenCtx 获取哈希表字段数量
func (m *MemoryCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	hashMap, ok := val.(map[string]interface{})
	if !ok {
		return 0, errors.New("值类型不是哈希表")
	}

	return int64(len(hashMap)), nil
}

// Keys 获取所有匹配的键
func (m *MemoryCache) Keys(pattern string) ([]string, error) {
	return m.KeysCtx(context.Background(), pattern)
}

// KeysCtx 获取所有匹配的键
func (m *MemoryCache) KeysCtx(ctx context.Context, pattern string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for key := range m.data {
		// 清理过期键
		m.cleanExpired(key)

		// 如果键已过期，跳过
		if _, ok := m.data[key]; !ok {
			continue
		}

		// 移除前缀进行匹配
		rawKey := key
		if m.prefix != "" && strings.HasPrefix(key, m.prefix) {
			rawKey = key[len(m.prefix):]
		}

		// 支持通配符 * 匹配
		matched := false
		if pattern == "*" {
			matched = true
		} else if !strings.Contains(pattern, "*") {
			matched = (pattern == rawKey)
		} else if strings.HasSuffix(pattern, "*") && strings.HasPrefix(rawKey, pattern[:len(pattern)-1]) {
			// 前缀匹配 (prefix*)
			matched = true
		} else if strings.HasPrefix(pattern, "*") && strings.HasSuffix(rawKey, pattern[1:]) {
			// 后缀匹配 (*suffix)
			matched = true
		} else if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") && len(pattern) > 2 {
			// 中间匹配 (*middle*)
			mid := pattern[1 : len(pattern)-1]
			matched = strings.Contains(rawKey, mid)
		}

		if matched {
			keys = append(keys, rawKey)
		}
	}

	return keys, nil
}

// matchPattern 简单实现通配符匹配
func matchPattern(pattern, str string) bool {
	// 这是一个简单的实现，仅支持 * 通配符
	if pattern == "*" {
		return true
	}

	// 处理前缀匹配 (prefix*)
	if strings.HasSuffix(pattern, "*") && strings.HasPrefix(str, pattern[:len(pattern)-1]) {
		return true
	}

	// 处理后缀匹配 (*suffix)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(str, pattern[1:]) {
		return true
	}

	// 处理中间匹配 (*middle*)
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") && len(pattern) > 2 {
		mid := pattern[1 : len(pattern)-1]
		return strings.Contains(str, mid)
	}

	return pattern == str
}

// Ping 测试连接
func (m *MemoryCache) Ping() error {
	return m.PingCtx(context.Background())
}

// PingCtx 测试连接
func (m *MemoryCache) PingCtx(ctx context.Context) error {
	return nil // 内存缓存总是可用
}

// LLen 获取列表长度
func (m *MemoryCache) LLen(key string) (int64, error) {
	return m.LLenCtx(context.Background(), key)
}

// LLenCtx 获取列表长度
func (m *MemoryCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	list, ok := val.([]interface{})
	if !ok {
		return 0, errors.New("值类型不是列表")
	}

	return int64(len(list)), nil
}

// LPush 在列表左侧添加元素
func (m *MemoryCache) LPush(key string, values ...interface{}) (int64, error) {
	return m.LPushCtx(context.Background(), key, values...)
}

// LPushCtx 在列表左侧添加元素
func (m *MemoryCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	var list []interface{}

	// 如果键存在，获取现有列表
	if val, ok := m.data[fullKey]; ok {
		existingList, ok := val.([]interface{})
		if !ok {
			return 0, errors.New("值类型不是列表")
		}
		list = existingList
	} else {
		// 创建新列表
		list = []interface{}{}
	}

	// 从左侧添加元素（每个元素都放在最前面）
	newList := make([]interface{}, 0, len(list)+len(values))
	for i := 0; i < len(values); i++ {
		newList = append(newList, values[i])
	}
	newList = append(newList, list...)

	m.data[fullKey] = newList
	return int64(len(newList)), nil
}

// RPush 在列表右侧添加元素
func (m *MemoryCache) RPush(key string, values ...interface{}) (int64, error) {
	return m.RPushCtx(context.Background(), key, values...)
}

// RPushCtx 在列表右侧添加元素
func (m *MemoryCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	var list []interface{}

	// 如果键存在，获取现有列表
	if val, ok := m.data[fullKey]; ok {
		existingList, ok := val.([]interface{})
		if !ok {
			return 0, errors.New("值类型不是列表")
		}
		list = existingList
	} else {
		// 创建新列表
		list = []interface{}{}
	}

	// 从右侧添加元素
	list = append(list, values...)

	m.data[fullKey] = list
	return int64(len(list)), nil
}

// LPop 弹出列表左侧元素
func (m *MemoryCache) LPop(key string) (string, error) {
	return m.LPopCtx(context.Background(), key)
}

// LPopCtx 弹出列表左侧元素
func (m *MemoryCache) LPopCtx(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return "", ErrKeyNotFound
	}

	list, ok := val.([]interface{})
	if !ok {
		return "", errors.New("值类型不是列表")
	}

	if len(list) == 0 {
		return "", ErrKeyNotFound
	}

	// 获取第一个元素
	first := list[0]

	// 更新列表
	m.data[fullKey] = list[1:]

	// 如果列表为空，考虑删除键
	if len(list) == 1 {
		delete(m.data, fullKey)
	}

	// 将值转换为字符串
	switch v := first.(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// RPop 弹出列表右侧元素
func (m *MemoryCache) RPop(key string) (string, error) {
	return m.RPopCtx(context.Background(), key)
}

// RPopCtx 弹出列表右侧元素
func (m *MemoryCache) RPopCtx(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return "", ErrKeyNotFound
	}

	list, ok := val.([]interface{})
	if !ok {
		return "", errors.New("值类型不是列表")
	}

	if len(list) == 0 {
		return "", ErrKeyNotFound
	}

	// 获取最后一个元素
	last := list[len(list)-1]

	// 更新列表
	m.data[fullKey] = list[:len(list)-1]

	// 如果列表为空，考虑删除键
	if len(list) == 1 {
		delete(m.data, fullKey)
	}

	// 将值转换为字符串
	switch v := last.(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// LRange 获取列表范围内的元素
func (m *MemoryCache) LRange(key string, start, stop int64) ([]string, error) {
	return m.LRangeCtx(context.Background(), key, start, stop)
}

// LRangeCtx 获取列表范围内的元素
func (m *MemoryCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return []string{}, nil
	}

	list, ok := val.([]interface{})
	if !ok {
		return nil, errors.New("值类型不是列表")
	}

	// 调整负索引
	length := int64(len(list))
	if start < 0 {
		start = length + start
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop = length + stop
		if stop < 0 {
			stop = -1
		}
	}

	// 处理边界情况
	if start >= length || start > stop {
		return []string{}, nil
	}
	if stop >= length {
		stop = length - 1
	}

	// 提取范围元素
	result := make([]string, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		item := list[i]
		switch v := item.(type) {
		case string:
			result = append(result, v)
		default:
			result = append(result, fmt.Sprintf("%v", v))
		}
	}

	return result, nil
}

// 实现所有集合操作
// SAdd 添加集合成员
func (m *MemoryCache) SAdd(key string, members ...interface{}) (int64, error) {
	return m.SAddCtx(context.Background(), key, members...)
}

// SRem 删除集合成员
func (m *MemoryCache) SRem(key string, members ...interface{}) (int64, error) {
	return m.SRemCtx(context.Background(), key, members...)
}

// SMembers 获取集合所有成员
func (m *MemoryCache) SMembers(key string) ([]string, error) {
	return m.SMembersCtx(context.Background(), key)
}

// SIsMember 检查成员是否在集合中
func (m *MemoryCache) SIsMember(key string, member interface{}) (bool, error) {
	return m.SIsMemberCtx(context.Background(), key, member)
}

// SCard 获取集合成员数
func (m *MemoryCache) SCard(key string) (int64, error) {
	return m.SCardCtx(context.Background(), key)
}

// SAddCtx 添加集合成员
func (m *MemoryCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	var set map[string]bool

	// 如果键存在，获取现有集合
	if val, ok := m.data[fullKey]; ok {
		existingSet, ok := val.(map[string]bool)
		if !ok {
			return 0, errors.New("值类型不是集合")
		}
		set = existingSet
	} else {
		// 创建新集合
		set = make(map[string]bool)
		m.data[fullKey] = set
	}

	// 添加成员
	var added int64
	for _, member := range members {
		// 将成员转换为字符串
		strMember := fmt.Sprintf("%v", member)
		if !set[strMember] {
			set[strMember] = true
			added++
		}
	}

	return added, nil
}

// SRemCtx 删除集合成员
func (m *MemoryCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	set, ok := val.(map[string]bool)
	if !ok {
		return 0, errors.New("值类型不是集合")
	}

	// 删除成员
	var removed int64
	for _, member := range members {
		strMember := fmt.Sprintf("%v", member)
		if set[strMember] {
			delete(set, strMember)
			removed++
		}
	}

	// 如果集合为空，删除键
	if len(set) == 0 {
		delete(m.data, fullKey)
	}

	return removed, nil
}

// SMembersCtx 获取集合所有成员
func (m *MemoryCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return []string{}, nil
	}

	set, ok := val.(map[string]bool)
	if !ok {
		return nil, errors.New("值类型不是集合")
	}

	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}

	return members, nil
}

// SIsMemberCtx 检查成员是否在集合中
func (m *MemoryCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return false, nil
	}

	set, ok := val.(map[string]bool)
	if !ok {
		return false, errors.New("值类型不是集合")
	}

	strMember := fmt.Sprintf("%v", member)
	return set[strMember], nil
}

// SCardCtx 获取集合成员数
func (m *MemoryCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	set, ok := val.(map[string]bool)
	if !ok {
		return 0, errors.New("值类型不是集合")
	}

	return int64(len(set)), nil
}

// 实现所有有序集合操作
// ZAdd 添加有序集合成员
func (m *MemoryCache) ZAdd(key string, members ...Z) (int64, error) {
	return m.ZAddCtx(context.Background(), key, members...)
}

// ZRem 删除有序集合成员
func (m *MemoryCache) ZRem(key string, members ...interface{}) (int64, error) {
	return m.ZRemCtx(context.Background(), key, members...)
}

// ZRange 获取有序集合范围
func (m *MemoryCache) ZRange(key string, start, stop int64) ([]string, error) {
	return m.ZRangeCtx(context.Background(), key, start, stop)
}

// ZRangeWithScores 获取有序集合范围及分数
func (m *MemoryCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	return m.ZRangeWithScoresCtx(context.Background(), key, start, stop)
}

// ZCard 获取有序集合成员数
func (m *MemoryCache) ZCard(key string) (int64, error) {
	return m.ZCardCtx(context.Background(), key)
}

// ZScore 获取有序集合成员分数
func (m *MemoryCache) ZScore(key, member string) (float64, error) {
	return m.ZScoreCtx(context.Background(), key, member)
}

// ZAddCtx 添加有序集合成员
func (m *MemoryCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取或创建有序集合
	var zset map[interface{}]float64
	if val, ok := m.data[fullKey]; ok {
		if existingZSet, ok := val.(map[interface{}]float64); ok {
			zset = existingZSet
		} else {
			// 类型不匹配，创建新的
			zset = make(map[interface{}]float64)
			m.data[fullKey] = zset
		}
	} else {
		zset = make(map[interface{}]float64)
		m.data[fullKey] = zset
	}

	// 添加成员
	var added int64
	for _, member := range members {
		if _, exists := zset[member.Member]; !exists {
			added++
		}
		zset[member.Member] = member.Score
	}

	return added, nil
}

// ZRemCtx 删除有序集合成员
func (m *MemoryCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取有序集合
	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	zset, ok := val.(map[interface{}]float64)
	if !ok {
		return 0, ErrTypeMismatch
	}

	// 删除成员
	var removed int64
	for _, member := range members {
		if _, exists := zset[member]; exists {
			delete(zset, member)
			removed++
		}
	}

	return removed, nil
}

// ZRangeCtx 获取有序集合范围
func (m *MemoryCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取有序集合
	val, ok := m.data[fullKey]
	if !ok {
		return []string{}, nil
	}

	zset, ok := val.(map[interface{}]float64)
	if !ok {
		return nil, ErrTypeMismatch
	}

	// 转换为ZMembers并排序
	members := make(ZMembers, 0, len(zset))
	for member, score := range zset {
		members = append(members, ZMember{Score: score, Member: member})
	}
	sort.Sort(members)

	// 处理负索引
	length := int64(len(members))
	if start < 0 {
		start = length + start
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop = length + stop
	}
	if stop >= length {
		stop = length - 1
	}
	if start > stop || start >= length {
		return []string{}, nil
	}

	// 提取结果
	result := make([]string, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		result = append(result, fmt.Sprintf("%v", members[i].Member))
	}

	return result, nil
}

// ZRangeWithScoresCtx 获取有序集合范围及分数
func (m *MemoryCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取有序集合
	val, ok := m.data[fullKey]
	if !ok {
		return []Z{}, nil
	}

	zset, ok := val.(map[interface{}]float64)
	if !ok {
		return nil, ErrTypeMismatch
	}

	// 转换为ZMembers并排序
	members := make(ZMembers, 0, len(zset))
	for member, score := range zset {
		members = append(members, ZMember{Score: score, Member: member})
	}
	sort.Sort(members)

	// 处理负索引
	length := int64(len(members))
	if start < 0 {
		start = length + start
		if start < 0 {
			start = 0
		}
	}
	if stop < 0 {
		stop = length + stop
	}
	if stop >= length {
		stop = length - 1
	}
	if start > stop || start >= length {
		return []Z{}, nil
	}

	// 提取结果
	result := make([]Z, 0, stop-start+1)
	for i := start; i <= stop; i++ {
		result = append(result, Z{Score: members[i].Score, Member: members[i].Member})
	}

	return result, nil
}

// ZCardCtx 获取有序集合成员数
func (m *MemoryCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取有序集合
	val, ok := m.data[fullKey]
	if !ok {
		return 0, nil
	}

	zset, ok := val.(map[interface{}]float64)
	if !ok {
		return 0, ErrTypeMismatch
	}

	return int64(len(zset)), nil
}

// ZScoreCtx 获取有序集合成员分数
func (m *MemoryCache) ZScoreCtx(ctx context.Context, key, member string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	fullKey := m.buildKey(key)
	m.cleanExpired(fullKey)

	// 获取有序集合
	val, ok := m.data[fullKey]
	if !ok {
		return 0, ErrKeyNotFound
	}

	zset, ok := val.(map[interface{}]float64)
	if !ok {
		return 0, ErrTypeMismatch
	}

	score, ok := zset[member]
	if !ok {
		return 0, ErrKeyNotFound
	}

	return score, nil
}
