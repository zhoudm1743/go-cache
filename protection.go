package cache

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// 初始化随机数生成器
func init() {
	rand.Seed(time.Now().UnixNano())
}

// CacheProtection 缓存防护配置
type CacheProtection struct {
	// 缓存穿透防护
	EnableBloomFilter bool    // 是否启用布隆过滤器
	ExpectedItems     int     // 预期元素数量
	FalsePositiveRate float64 // 可接受的误判率
	bloomFilter       *BloomFilter

	// 缓存雪崩防护
	EnableExpirationJitter bool    // 是否启用过期时间随机化
	JitterFactor           float64 // 随机因子，默认为0.1，表示上下10%的随机范围

	mutex sync.RWMutex
}

// NewCacheProtection 创建一个新的缓存防护配置
func NewCacheProtection() *CacheProtection {
	return &CacheProtection{
		EnableBloomFilter:      false,
		ExpectedItems:          10000,
		FalsePositiveRate:      0.01,
		EnableExpirationJitter: true,
		JitterFactor:           0.1,
	}
}

// EnableBloomFilterProtection 启用布隆过滤器防护
func (p *CacheProtection) EnableBloomFilterProtection(expectedItems int, falsePositiveRate float64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.EnableBloomFilter = true
	p.ExpectedItems = expectedItems
	p.FalsePositiveRate = falsePositiveRate
	p.bloomFilter = NewBloomFilter(expectedItems, falsePositiveRate)
}

// AddToBloomFilter 添加键到布隆过滤器
func (p *CacheProtection) AddToBloomFilter(key string) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.EnableBloomFilter && p.bloomFilter != nil {
		p.bloomFilter.Add(key)
	}
}

// ExistsInBloomFilter 检查键是否可能存在于布隆过滤器中
func (p *CacheProtection) ExistsInBloomFilter(key string) bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.EnableBloomFilter && p.bloomFilter != nil {
		return p.bloomFilter.Contains(key)
	}
	return true // 如果未启用布隆过滤器，总是返回true
}

// ApplyJitter 应用过期时间随机抖动
func (p *CacheProtection) ApplyJitter(expiration time.Duration) time.Duration {
	if !p.EnableExpirationJitter || expiration <= 0 {
		return expiration
	}

	// 应用上下p.JitterFactor的随机因子
	jitter := 1.0 + (rand.Float64()*2*p.JitterFactor - p.JitterFactor)
	return time.Duration(float64(expiration.Nanoseconds()) * jitter)
}

// ProtectedCache 缓存防护包装器
type ProtectedCache struct {
	cache      Cache            // 底层缓存
	protection *CacheProtection // 缓存防护配置
	nilValue   string           // 空值标记，用于缓存查询不存在的键
	logger     Logger           // 日志记录器
}

// NewProtectedCache 创建一个新的带防护功能的缓存
func NewProtectedCache(cache Cache, protection *CacheProtection, logger Logger) *ProtectedCache {
	if logger == nil {
		logger = defaultLogger
	}

	if protection == nil {
		protection = NewCacheProtection()
	}

	return &ProtectedCache{
		cache:      cache,
		protection: protection,
		nilValue:   "$$NIL$$", // 特殊标记，表示键不存在
		logger:     logger,
	}
}

// Get 获取缓存，带有防护功能
func (pc *ProtectedCache) Get(key string) (string, error) {
	// 布隆过滤器防护
	if pc.protection.EnableBloomFilter && !pc.protection.ExistsInBloomFilter(key) {
		pc.logger.WithFields(map[string]interface{}{
			"key": key,
		}).Debug("布隆过滤器拦截")
		return "", ErrKeyNotFound
	}

	// 获取缓存
	value, err := pc.cache.Get(key)

	// 处理特殊的空值标记
	if err == nil && value == pc.nilValue {
		return "", ErrKeyNotFound
	}

	return value, err
}

// Set 设置缓存，带有防护功能
func (pc *ProtectedCache) Set(key string, value interface{}, expiration time.Duration) error {
	// 如果值不是nil，添加到布隆过滤器
	if value != nil {
		pc.protection.AddToBloomFilter(key)
	}

	// 应用过期时间随机化，防止缓存雪崩
	jitteredExpiration := pc.protection.ApplyJitter(expiration)

	// 记录实际使用的过期时间
	if expiration > 0 && pc.protection.EnableExpirationJitter {
		pc.logger.WithFields(map[string]interface{}{
			"key":      key,
			"original": expiration,
			"jittered": jitteredExpiration,
		}).Debug("应用过期时间随机化")
	}

	// 设置缓存
	return pc.cache.Set(key, value, jitteredExpiration)
}

// SetNil 缓存空值，用于防止缓存穿透
func (pc *ProtectedCache) SetNil(key string, expiration time.Duration) error {
	// 空值使用较短的过期时间，避免长时间占用缓存
	shortExpiration := expiration / 10
	if shortExpiration < 1*time.Minute {
		shortExpiration = 1 * time.Minute
	}
	if shortExpiration > 10*time.Minute {
		shortExpiration = 10 * time.Minute
	}

	// 应用过期时间随机化
	jitteredExpiration := pc.protection.ApplyJitter(shortExpiration)

	// 设置空值缓存
	return pc.cache.Set(key, pc.nilValue, jitteredExpiration)
}

// 以下是包装Cache接口的其他方法

// Del 删除缓存
func (pc *ProtectedCache) Del(keys ...string) (int64, error) {
	return pc.cache.Del(keys...)
}

// Exists 检查键是否存在
func (pc *ProtectedCache) Exists(keys ...string) (int64, error) {
	return pc.cache.Exists(keys...)
}

// Expire 设置过期时间
func (pc *ProtectedCache) Expire(key string, expiration time.Duration) error {
	// 应用过期时间随机化
	jitteredExpiration := pc.protection.ApplyJitter(expiration)
	return pc.cache.Expire(key, jitteredExpiration)
}

// TTL 获取过期时间
func (pc *ProtectedCache) TTL(key string) (time.Duration, error) {
	return pc.cache.TTL(key)
}

// GetWithLoader 获取缓存，如果不存在则从加载器获取
func (pc *ProtectedCache) GetWithLoader(ctx context.Context, key string, loader func(ctx context.Context) (interface{}, time.Duration, error)) (string, error) {
	// 先尝试从缓存获取
	value, err := pc.Get(key)
	if err == nil {
		return value, nil
	}

	// 如果是键不存在错误，且布隆过滤器认为键不存在，直接返回错误
	if err == ErrKeyNotFound && pc.protection.EnableBloomFilter && !pc.protection.ExistsInBloomFilter(key) {
		return "", ErrKeyNotFound
	}

	// 缓存未命中，从加载器获取
	loadedValue, expiration, err := loader(ctx)
	if err != nil {
		// 如果加载失败并且是因为键不存在，缓存空值
		if err == ErrKeyNotFound {
			pc.SetNil(key, expiration)
		}
		return "", err
	}

	// 加载成功，缓存结果
	pc.Set(key, loadedValue, expiration)

	// 转换为字符串返回
	switch v := loadedValue.(type) {
	case string:
		return v, nil
	default:
		return "", ErrTypeMismatch
	}
}

// Close 关闭缓存
func (pc *ProtectedCache) Close() error {
	return pc.cache.Close()
}

// GetClient 获取底层客户端
func (pc *ProtectedCache) GetClient() interface{} {
	return pc.cache.GetClient()
}

// GetCache 获取底层缓存
func (pc *ProtectedCache) GetCache() Cache {
	return pc.cache
}

// GetProtection 获取防护配置
func (pc *ProtectedCache) GetProtection() *CacheProtection {
	return pc.protection
}

// GetNilValue 获取空值标记
func (pc *ProtectedCache) GetNilValue() string {
	return pc.nilValue
}
