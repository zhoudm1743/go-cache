package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WarmupConfig 缓存预热配置
type WarmupConfig struct {
	// 并发加载的协程数，默认为5
	Concurrency int
	// 加载间隔时间，避免瞬时高负载，默认为0（无间隔）
	Interval time.Duration
	// 预热超时时间，默认为30秒
	Timeout time.Duration
	// 加载失败时是否继续，默认为true
	ContinueOnError bool
	// 加载进度回调函数
	ProgressCallback func(loaded, total int, key string, err error)
}

// DefaultWarmupConfig 返回默认的预热配置
func DefaultWarmupConfig() *WarmupConfig {
	return &WarmupConfig{
		Concurrency:     5,
		Interval:        0,
		Timeout:         30 * time.Second,
		ContinueOnError: true,
		ProgressCallback: func(loaded, total int, key string, err error) {
			// 默认无操作
		},
	}
}

// SimpleKeyGenerator 简单的键生成器，基于模式匹配
type SimpleKeyGenerator struct {
	// 键模式列表
	Patterns []string
	// 底层缓存，用于查询现有键
	Cache Cache
}

// GenerateKeys 根据模式生成键列表
func (g *SimpleKeyGenerator) GenerateKeys(ctx context.Context) ([]string, error) {
	if g.Cache == nil {
		return nil, fmt.Errorf("缓存实例未设置")
	}

	allKeys := make([]string, 0)
	for _, pattern := range g.Patterns {
		keys, err := g.Cache.KeysCtx(ctx, pattern)
		if err != nil {
			return nil, fmt.Errorf("获取键列表失败: %w", err)
		}
		allKeys = append(allKeys, keys...)
	}
	return allKeys, nil
}

// NewSimpleKeyGenerator 创建一个简单的键生成器
func NewSimpleKeyGenerator(cache Cache, patterns ...string) *SimpleKeyGenerator {
	return &SimpleKeyGenerator{
		Patterns: patterns,
		Cache:    cache,
	}
}

// WarmupCacheWithConfig 使用自定义配置预热缓存
// 这是一个公共方法，可以用于任何实现了Cache接口的缓存
func WarmupCacheWithConfig(ctx context.Context, cache Cache, keys []string, loader DataLoader, config *WarmupConfig) error {
	return warmupCache(ctx, cache, keys, loader, config)
}

// warmupCache 缓存预热实现
func warmupCache(ctx context.Context, cache Cache, keys []string, loader DataLoader, config *WarmupConfig) error {
	if config == nil {
		config = DefaultWarmupConfig()
	}

	if config.Concurrency < 1 {
		config.Concurrency = 1
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// 创建工作任务通道
	taskCh := make(chan string, len(keys))
	for _, key := range keys {
		taskCh <- key
	}
	close(taskCh)

	// 创建等待组和错误通道
	var wg sync.WaitGroup
	errCh := make(chan error, len(keys))
	resultCh := make(chan struct {
		key string
		err error
	}, len(keys))

	// 启动工作协程
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range taskCh {
				// 加载数据
				value, expiration, err := loader.LoadData(ctx, key)

				// 检查上下文是否已取消
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				default:
				}

				// 如果加载成功，则设置到缓存
				if err == nil {
					err = cache.SetCtx(ctx, key, value, expiration)
				}

				// 发送结果
				resultCh <- struct {
					key string
					err error
				}{key, err}

				// 如果设置了间隔，则等待
				if config.Interval > 0 {
					time.Sleep(config.Interval)
				}
			}
		}()
	}

	// 启动结果收集协程
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 处理结果
	var loaded int
	total := len(keys)
	for result := range resultCh {
		loaded++
		if config.ProgressCallback != nil {
			config.ProgressCallback(loaded, total, result.key, result.err)
		}

		if result.err != nil && !config.ContinueOnError {
			return fmt.Errorf("预热键 %s 失败: %w", result.key, result.err)
		}
	}

	// 检查是否有错误
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// Warmup 通用缓存预热方法
func (c *MemoryCache) Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error {
	// 生成需要预热的键
	keys, err := generator.GenerateKeys(ctx)
	if err != nil {
		return fmt.Errorf("生成预热键列表失败: %w", err)
	}

	// 预热键
	return c.WarmupKeys(ctx, keys, loader)
}

// WarmupKeys 预热指定的键列表
func (c *MemoryCache) WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error {
	c.logger.WithFields(map[string]interface{}{
		"key_count": len(keys),
	}).Info("开始缓存预热")

	config := DefaultWarmupConfig()
	config.ProgressCallback = func(loaded, total int, key string, err error) {
		if err != nil {
			c.logger.WithFields(map[string]interface{}{
				"key":      key,
				"progress": fmt.Sprintf("%d/%d", loaded, total),
				"error":    err.Error(),
			}).Warn("预热键失败")
		} else {
			c.logger.WithFields(map[string]interface{}{
				"key":      key,
				"progress": fmt.Sprintf("%d/%d", loaded, total),
			}).Debug("预热键成功")
		}
	}

	err := warmupCache(ctx, c, keys, loader, config)
	if err != nil {
		c.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("缓存预热失败")
		return err
	}

	c.logger.Info("缓存预热完成")
	return nil
}

// Warmup Redis缓存预热方法
func (r *RedisCache) Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error {
	// 生成需要预热的键
	keys, err := generator.GenerateKeys(ctx)
	if err != nil {
		return fmt.Errorf("生成预热键列表失败: %w", err)
	}

	// 预热键
	return r.WarmupKeys(ctx, keys, loader)
}

// WarmupKeys Redis预热指定的键列表
func (r *RedisCache) WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error {
	r.logger.WithFields(map[string]interface{}{
		"key_count": len(keys),
	}).Info("开始Redis缓存预热")

	config := DefaultWarmupConfig()
	// 增加Redis的并发数
	config.Concurrency = 10
	// 增加Redis的超时时间
	config.Timeout = 60 * time.Second
	config.ProgressCallback = func(loaded, total int, key string, err error) {
		if err != nil {
			r.logger.WithFields(map[string]interface{}{
				"key":      key,
				"progress": fmt.Sprintf("%d/%d", loaded, total),
				"error":    err.Error(),
			}).Warn("预热Redis键失败")
		} else {
			r.logger.WithFields(map[string]interface{}{
				"key":      key,
				"progress": fmt.Sprintf("%d/%d", loaded, total),
			}).Debug("预热Redis键成功")
		}
	}

	err := warmupCache(ctx, r, keys, loader, config)
	if err != nil {
		r.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Redis缓存预热失败")
		return err
	}

	r.logger.Info("Redis缓存预热完成")
	return nil
}

// 一些常用的DataLoader实现示例

// FunctionDataLoader 基于函数的数据加载器
type FunctionDataLoader struct {
	LoadFunc func(ctx context.Context, key string) (interface{}, time.Duration, error)
}

// LoadData 加载数据
func (l *FunctionDataLoader) LoadData(ctx context.Context, key string) (interface{}, time.Duration, error) {
	return l.LoadFunc(ctx, key)
}

// NewFunctionDataLoader 创建一个基于函数的数据加载器
func NewFunctionDataLoader(fn func(ctx context.Context, key string) (interface{}, time.Duration, error)) *FunctionDataLoader {
	return &FunctionDataLoader{
		LoadFunc: fn,
	}
}

// MapDataLoader 基于映射表的数据加载器
type MapDataLoader struct {
	Data       map[string]interface{}
	Expiration time.Duration
}

// LoadData 从映射表加载数据
func (l *MapDataLoader) LoadData(ctx context.Context, key string) (interface{}, time.Duration, error) {
	if value, ok := l.Data[key]; ok {
		return value, l.Expiration, nil
	}
	return nil, 0, ErrKeyNotFound
}

// NewMapDataLoader 创建一个基于映射表的数据加载器
func NewMapDataLoader(data map[string]interface{}, expiration time.Duration) *MapDataLoader {
	return &MapDataLoader{
		Data:       data,
		Expiration: expiration,
	}
}
