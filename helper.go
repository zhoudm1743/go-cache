package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CacheHelper 缓存助手类，提供更高级的封装
type CacheHelper struct {
	cache  Cache
	logger Logger
	prefix string // 键前缀
}

// NewCacheHelper 创建缓存助手
func NewCacheHelper(cache Cache, log Logger, prefix string) *CacheHelper {
	if log == nil {
		log = defaultLogger
	}

	return &CacheHelper{
		cache:  cache,
		logger: log,
		prefix: prefix,
	}
}

// buildKey 构建带前缀的键
func (h *CacheHelper) buildKey(key string) string {
	if h.prefix == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", h.prefix, key)
}

// ================== 默认方法（不带 Context） ==================

// SetJSON 存储 JSON 对象
func (h *CacheHelper) SetJSON(key string, value interface{}, expiration time.Duration) error {
	return h.SetJSONCtx(context.Background(), key, value, expiration)
}

// GetJSON 获取 JSON 对象
func (h *CacheHelper) GetJSON(key string, dest interface{}) error {
	return h.GetJSONCtx(context.Background(), key, dest)
}

// Remember 记忆模式：如果缓存不存在则执行函数并缓存结果
func (h *CacheHelper) Remember(key string, expiration time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	return h.RememberCtx(context.Background(), key, expiration, fn)
}

// RememberJSON 记忆模式的 JSON 版本
func (h *CacheHelper) RememberJSON(key string, expiration time.Duration, dest interface{}, fn func() (interface{}, error)) error {
	return h.RememberJSONCtx(context.Background(), key, expiration, dest, fn)
}

// Lock 分布式锁
func (h *CacheHelper) Lock(key string, expiration time.Duration) (bool, error) {
	return h.LockCtx(context.Background(), key, expiration)
}

// Unlock 释放分布式锁
func (h *CacheHelper) Unlock(key string) error {
	return h.UnlockCtx(context.Background(), key)
}

// WithLock 使用分布式锁执行函数
func (h *CacheHelper) WithLock(key string, expiration time.Duration, fn func() error) error {
	return h.WithLockCtx(context.Background(), key, expiration, fn)
}

// BatchGet 批量获取
func (h *CacheHelper) BatchGet(keys []string) (map[string]string, error) {
	return h.BatchGetCtx(context.Background(), keys)
}

// BatchSet 批量设置
func (h *CacheHelper) BatchSet(data map[string]interface{}, expiration time.Duration) error {
	return h.BatchSetCtx(context.Background(), data, expiration)
}

// FlushByPattern 根据模式删除键
func (h *CacheHelper) FlushByPattern(pattern string) (int64, error) {
	return h.FlushByPatternCtx(context.Background(), pattern)
}

// GetOrSet 获取或设置：如果键不存在则设置默认值
func (h *CacheHelper) GetOrSet(key string, defaultValue interface{}, expiration time.Duration) (string, error) {
	return h.GetOrSetCtx(context.Background(), key, defaultValue, expiration)
}

// ================== 带 Context 的方法 ==================

// SetJSONCtx 存储 JSON 对象
func (h *CacheHelper) SetJSONCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	return h.cache.SetCtx(ctx, h.buildKey(key), string(data), expiration)
}

// GetJSONCtx 获取 JSON 对象
func (h *CacheHelper) GetJSONCtx(ctx context.Context, key string, dest interface{}) error {
	data, err := h.cache.GetCtx(ctx, h.buildKey(key))
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

// RememberCtx 记忆模式：如果缓存不存在则执行函数并缓存结果
func (h *CacheHelper) RememberCtx(ctx context.Context, key string, expiration time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	fullKey := h.buildKey(key)

	// 先尝试从缓存获取
	data, err := h.cache.GetCtx(ctx, fullKey)
	if err == nil {
		return data, nil
	}

	// 缓存不存在，执行函数
	result, err := fn()
	if err != nil {
		return nil, err
	}

	// 缓存结果
	if err := h.cache.SetCtx(ctx, fullKey, result, expiration); err != nil {
		h.logger.WithFields(map[string]interface{}{
			"key":   fullKey,
			"error": err,
		}).Error("缓存设置失败")
	}

	return result, nil
}

// RememberJSONCtx 记忆模式的 JSON 版本
func (h *CacheHelper) RememberJSONCtx(ctx context.Context, key string, expiration time.Duration, dest interface{}, fn func() (interface{}, error)) error {
	fullKey := h.buildKey(key)

	// 先尝试从缓存获取
	if err := h.GetJSONCtx(ctx, key, dest); err == nil {
		return nil
	}

	// 缓存不存在，执行函数
	result, err := fn()
	if err != nil {
		return err
	}

	// 缓存结果
	if err := h.SetJSONCtx(ctx, key, result, expiration); err != nil {
		h.logger.WithFields(map[string]interface{}{
			"key":   fullKey,
			"error": err,
		}).Warn("缓存设置失败")
	}

	// 将结果复制到目标对象
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// LockCtx 分布式锁
func (h *CacheHelper) LockCtx(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	fullKey := fmt.Sprintf("lock:%s", h.buildKey(key))

	// 使用 SET NX EX 实现分布式锁
	// 检查键是否存在
	exists, err := h.cache.ExistsCtx(ctx, fullKey)
	if err != nil {
		return false, err
	}

	if exists > 0 {
		return false, nil
	}

	// 设置锁
	err = h.cache.SetCtx(ctx, fullKey, "locked", expiration)
	if err != nil {
		return false, err
	}

	return true, nil
}

// UnlockCtx 释放分布式锁
func (h *CacheHelper) UnlockCtx(ctx context.Context, key string) error {
	fullKey := fmt.Sprintf("lock:%s", h.buildKey(key))
	_, err := h.cache.DelCtx(ctx, fullKey)
	return err
}

// WithLockCtx 使用分布式锁执行函数
func (h *CacheHelper) WithLockCtx(ctx context.Context, key string, expiration time.Duration, fn func() error) error {
	// 获取锁
	locked, err := h.LockCtx(ctx, key, expiration)
	if err != nil {
		return fmt.Errorf("获取锁失败: %w", err)
	}
	if !locked {
		return fmt.Errorf("无法获取锁: %s", key)
	}

	// 确保释放锁
	defer func() {
		if err := h.UnlockCtx(ctx, key); err != nil {
			h.logger.WithFields(map[string]interface{}{
				"key":   key,
				"error": err,
			}).Error("释放锁失败")
		}
	}()

	// 执行函数
	return fn()
}

// BatchGetCtx 批量获取
func (h *CacheHelper) BatchGetCtx(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return make(map[string]string), nil
	}

	// 构建完整键名
	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = h.buildKey(key)
	}

	// 批量获取
	result := make(map[string]string)
	for i, fullKey := range fullKeys {
		val, err := h.cache.GetCtx(ctx, fullKey)
		if err == nil {
			result[keys[i]] = val
		}
	}

	return result, nil
}

// BatchSetCtx 批量设置
func (h *CacheHelper) BatchSetCtx(ctx context.Context, data map[string]interface{}, expiration time.Duration) error {
	if len(data) == 0 {
		return nil
	}

	// 批量设置
	for key, value := range data {
		fullKey := h.buildKey(key)
		if err := h.cache.SetCtx(ctx, fullKey, value, expiration); err != nil {
			return err
		}
	}

	return nil
}

// FlushByPatternCtx 根据模式删除键
func (h *CacheHelper) FlushByPatternCtx(ctx context.Context, pattern string) (int64, error) {
	// 先获取匹配的键
	fullPattern := h.buildKey(pattern)
	keys, err := h.cache.KeysCtx(ctx, fullPattern)
	if err != nil {
		return 0, err
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// 删除匹配的键
	return h.cache.DelCtx(ctx, keys...)
}

// GetOrSetCtx 获取或设置：如果键不存在则设置默认值
func (h *CacheHelper) GetOrSetCtx(ctx context.Context, key string, defaultValue interface{}, expiration time.Duration) (string, error) {
	fullKey := h.buildKey(key)

	// 先尝试获取
	val, err := h.cache.GetCtx(ctx, fullKey)
	if err == nil {
		return val, nil
	}

	// 键不存在，设置默认值
	if err != ErrKeyNotFound {
		return "", err
	}

	// 设置默认值
	strValue := fmt.Sprintf("%v", defaultValue)
	err = h.cache.SetCtx(ctx, fullKey, strValue, expiration)
	if err != nil {
		return "", err
	}

	return strValue, nil
}
