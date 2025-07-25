package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSentinelCache Redis 哨兵模式缓存实现
type RedisSentinelCache struct {
	client *redis.Client
	logger Logger
	prefix string
}

// NewRedisSentinelCache 创建新的 Redis 哨兵模式缓存实例
func NewRedisSentinelCache(cfg *RedisSentinelConfig, log Logger) (Cache, error) {
	if log == nil {
		log = defaultLogger
	}

	// 创建 Redis 哨兵客户端
	failoverOpts := &redis.FailoverOptions{
		MasterName:    cfg.MasterName,
		SentinelAddrs: cfg.Addrs,
		Password:      cfg.Password,
		DB:            cfg.DB,
		// 连接池设置
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		// 连接超时设置
		DialTimeout:  time.Duration(cfg.Timeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.Timeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Timeout) * time.Second,
		// 连接健康检查
		OnConnect: func(ctx context.Context, cn *redis.Conn) error {
			log.Debug("Redis哨兵模式连接创建")
			return nil
		},
	}

	rdb := redis.NewFailoverClient(failoverOpts)

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis 哨兵模式连接失败: %w", err)
	}

	log.WithFields(map[string]interface{}{
		"masterName": cfg.MasterName,
		"sentinels":  cfg.Addrs,
		"db":         cfg.DB,
		"prefix":     cfg.Prefix,
		"poolSize":   cfg.PoolSize,
	}).Info("Redis 哨兵模式连接成功")

	// 启动连接池监控
	go monitorRedisSentinelPool(rdb, log)

	return &RedisSentinelCache{
		client: rdb,
		logger: log,
		prefix: cfg.Prefix,
	}, nil
}

// 监控Redis连接池状态
func monitorRedisSentinelPool(client *redis.Client, log Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := client.PoolStats()
		log.WithFields(map[string]interface{}{
			"total_conns": stats.TotalConns,
			"idle_conns":  stats.IdleConns,
			"stale_conns": stats.StaleConns,
			"hits":        stats.Hits,
			"misses":      stats.Misses,
			"timeouts":    stats.Timeouts,
		}).Debug("Redis哨兵连接池状态")
	}
}

// handleRedisError 处理Redis错误，将Redis特定错误转换为通用缓存错误
func (r *RedisSentinelCache) handleRedisError(err error) error {
	if err == nil {
		return nil
	}

	// 处理键不存在错误
	if err == redis.Nil {
		return ErrKeyNotFound
	}

	// 处理连接错误
	if err.Error() == "redis: client is closed" {
		r.logger.Error("Redis客户端已关闭")
		return fmt.Errorf("Redis连接已关闭: %w", err)
	}

	// 处理超时错误
	if err.Error() == "context deadline exceeded" {
		r.logger.Error("Redis操作超时")
		return fmt.Errorf("Redis操作超时: %w", err)
	}

	// 处理网络错误
	if err.Error() == "redis: connection pool timeout" {
		r.logger.Error("Redis连接池超时")
		return fmt.Errorf("Redis连接池资源耗尽: %w", err)
	}

	// 处理哨兵特有错误
	if err.Error() == "READONLY You can't write against a read only replica" {
		r.logger.Error("尝试在只读副本上写入")
		return fmt.Errorf("尝试在只读副本上写入: %w", err)
	}

	if err.Error() == "MASTERDOWN Link with MASTER is down and replica-serve-stale-data is set to 'no'" {
		r.logger.Error("主节点故障，副本不可用")
		return fmt.Errorf("主节点故障，副本不可用: %w", err)
	}

	// 其他错误直接返回
	return err
}

// buildKey 构建带前缀的键
func (r *RedisSentinelCache) buildKey(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + key
}

// GetClient 获取原始 Redis 客户端
func (r *RedisSentinelCache) GetClient() interface{} {
	return r.client
}

// Close 关闭连接
func (r *RedisSentinelCache) Close() error {
	r.logger.Info("关闭Redis哨兵连接池")
	return r.client.Close()
}

// ================== 默认方法（不带 Context） ==================

// 基础操作
func (r *RedisSentinelCache) Get(key string) (string, error) {
	result, err := r.client.Get(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) Set(key string, value interface{}, expiration time.Duration) error {
	return r.handleRedisError(r.client.Set(context.Background(), r.buildKey(key), value, expiration).Err())
}

func (r *RedisSentinelCache) Del(keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	result, err := r.client.Del(context.Background(), prefixedKeys...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) Exists(keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	result, err := r.client.Exists(context.Background(), prefixedKeys...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) Expire(key string, expiration time.Duration) error {
	return r.handleRedisError(r.client.Expire(context.Background(), r.buildKey(key), expiration).Err())
}

func (r *RedisSentinelCache) TTL(key string) (time.Duration, error) {
	result, err := r.client.TTL(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 字符串操作
func (r *RedisSentinelCache) Incr(key string) (int64, error) {
	result, err := r.client.Incr(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) Decr(key string) (int64, error) {
	result, err := r.client.Decr(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) IncrBy(key string, value int64) (int64, error) {
	result, err := r.client.IncrBy(context.Background(), r.buildKey(key), value).Result()
	return result, r.handleRedisError(err)
}

// 哈希操作
func (r *RedisSentinelCache) HGet(key, field string) (string, error) {
	result, err := r.client.HGet(context.Background(), r.buildKey(key), field).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HSet(key string, values ...interface{}) (int64, error) {
	result, err := r.client.HSet(context.Background(), r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HDel(key string, fields ...string) (int64, error) {
	result, err := r.client.HDel(context.Background(), r.buildKey(key), fields...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HGetAll(key string) (map[string]string, error) {
	result, err := r.client.HGetAll(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HExists(key, field string) (bool, error) {
	result, err := r.client.HExists(context.Background(), r.buildKey(key), field).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HLen(key string) (int64, error) {
	result, err := r.client.HLen(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 列表操作
func (r *RedisSentinelCache) LPush(key string, values ...interface{}) (int64, error) {
	result, err := r.client.LPush(context.Background(), r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) RPush(key string, values ...interface{}) (int64, error) {
	result, err := r.client.RPush(context.Background(), r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LPop(key string) (string, error) {
	result, err := r.client.LPop(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) RPop(key string) (string, error) {
	result, err := r.client.RPop(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LLen(key string) (int64, error) {
	result, err := r.client.LLen(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LRange(key string, start, stop int64) ([]string, error) {
	result, err := r.client.LRange(context.Background(), r.buildKey(key), start, stop).Result()
	return result, r.handleRedisError(err)
}

// 集合操作
func (r *RedisSentinelCache) SAdd(key string, members ...interface{}) (int64, error) {
	result, err := r.client.SAdd(context.Background(), r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SRem(key string, members ...interface{}) (int64, error) {
	result, err := r.client.SRem(context.Background(), r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SMembers(key string) ([]string, error) {
	result, err := r.client.SMembers(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SIsMember(key string, member interface{}) (bool, error) {
	result, err := r.client.SIsMember(context.Background(), r.buildKey(key), member).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SCard(key string) (int64, error) {
	result, err := r.client.SCard(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 有序集合操作
func (r *RedisSentinelCache) ZAdd(key string, members ...Z) (int64, error) {
	// 转换为redis.Z
	redisMembers := make([]redis.Z, len(members))
	for i, m := range members {
		redisMembers[i] = redis.Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	result, err := r.client.ZAdd(context.Background(), r.buildKey(key), redisMembers...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRem(key string, members ...interface{}) (int64, error) {
	result, err := r.client.ZRem(context.Background(), r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRange(key string, start, stop int64) ([]string, error) {
	result, err := r.client.ZRange(context.Background(), r.buildKey(key), start, stop).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	result, err := r.client.ZRangeWithScores(context.Background(), r.buildKey(key), start, stop).Result()
	if err != nil {
		return nil, r.handleRedisError(err)
	}

	// 转换为我们自定义的Z结构体
	members := make([]Z, len(result))
	for i, m := range result {
		members[i] = Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	return members, nil
}

func (r *RedisSentinelCache) ZCard(key string) (int64, error) {
	result, err := r.client.ZCard(context.Background(), r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZScore(key, member string) (float64, error) {
	result, err := r.client.ZScore(context.Background(), r.buildKey(key), member).Result()
	return result, r.handleRedisError(err)
}

// 其他操作
func (r *RedisSentinelCache) Keys(pattern string) ([]string, error) {
	// 对于 Keys 操作，我们需要添加前缀到模式中
	prefixedPattern := r.buildKey(pattern)
	keys, err := r.client.Keys(context.Background(), prefixedPattern).Result()
	if err != nil {
		return nil, r.handleRedisError(err)
	}

	// 移除前缀
	if r.prefix != "" {
		for i, key := range keys {
			keys[i] = key[len(r.prefix):]
		}
	}

	return keys, nil
}

func (r *RedisSentinelCache) Ping() error {
	return r.handleRedisError(r.client.Ping(context.Background()).Err())
}

// ================== 带 Context 的方法 ==================

// 基础操作
func (r *RedisSentinelCache) GetCtx(ctx context.Context, key string) (string, error) {
	result, err := r.client.Get(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.handleRedisError(r.client.Set(ctx, r.buildKey(key), value, expiration).Err())
}

func (r *RedisSentinelCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	result, err := r.client.Del(ctx, prefixedKeys...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	result, err := r.client.Exists(ctx, prefixedKeys...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	return r.handleRedisError(r.client.Expire(ctx, r.buildKey(key), expiration).Err())
}

func (r *RedisSentinelCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	result, err := r.client.TTL(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 字符串操作
func (r *RedisSentinelCache) IncrCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.Incr(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) DecrCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.Decr(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) IncrByCtx(ctx context.Context, key string, value int64) (int64, error) {
	result, err := r.client.IncrBy(ctx, r.buildKey(key), value).Result()
	return result, r.handleRedisError(err)
}

// 哈希操作
func (r *RedisSentinelCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	result, err := r.client.HGet(ctx, r.buildKey(key), field).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	result, err := r.client.HSet(ctx, r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	result, err := r.client.HDel(ctx, r.buildKey(key), fields...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	result, err := r.client.HGetAll(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	result, err := r.client.HExists(ctx, r.buildKey(key), field).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.HLen(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 列表操作
func (r *RedisSentinelCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	result, err := r.client.LPush(ctx, r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	result, err := r.client.RPush(ctx, r.buildKey(key), values...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LPopCtx(ctx context.Context, key string) (string, error) {
	result, err := r.client.LPop(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) RPopCtx(ctx context.Context, key string) (string, error) {
	result, err := r.client.RPop(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.LLen(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	result, err := r.client.LRange(ctx, r.buildKey(key), start, stop).Result()
	return result, r.handleRedisError(err)
}

// 集合操作
func (r *RedisSentinelCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	result, err := r.client.SAdd(ctx, r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	result, err := r.client.SRem(ctx, r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	result, err := r.client.SMembers(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	result, err := r.client.SIsMember(ctx, r.buildKey(key), member).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.SCard(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

// 有序集合操作
func (r *RedisSentinelCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	// 转换为redis.Z
	redisMembers := make([]redis.Z, len(members))
	for i, m := range members {
		redisMembers[i] = redis.Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	result, err := r.client.ZAdd(ctx, r.buildKey(key), redisMembers...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	result, err := r.client.ZRem(ctx, r.buildKey(key), members...).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	result, err := r.client.ZRange(ctx, r.buildKey(key), start, stop).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	result, err := r.client.ZRangeWithScores(ctx, r.buildKey(key), start, stop).Result()
	if err != nil {
		return nil, r.handleRedisError(err)
	}

	// 转换为我们自定义的Z结构体
	members := make([]Z, len(result))
	for i, m := range result {
		members[i] = Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	return members, nil
}

func (r *RedisSentinelCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	result, err := r.client.ZCard(ctx, r.buildKey(key)).Result()
	return result, r.handleRedisError(err)
}

func (r *RedisSentinelCache) ZScoreCtx(ctx context.Context, key, member string) (float64, error) {
	result, err := r.client.ZScore(ctx, r.buildKey(key), member).Result()
	return result, r.handleRedisError(err)
}

// 其他操作
func (r *RedisSentinelCache) KeysCtx(ctx context.Context, pattern string) ([]string, error) {
	// 对于 Keys 操作，我们需要添加前缀到模式中
	prefixedPattern := r.buildKey(pattern)
	keys, err := r.client.Keys(ctx, prefixedPattern).Result()
	if err != nil {
		return nil, r.handleRedisError(err)
	}

	// 移除前缀
	if r.prefix != "" {
		for i, key := range keys {
			keys[i] = key[len(r.prefix):]
		}
	}

	return keys, nil
}

func (r *RedisSentinelCache) PingCtx(ctx context.Context) error {
	return r.handleRedisError(r.client.Ping(ctx).Err())
}

// Warmup 实现缓存预热
func (r *RedisSentinelCache) Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error {
	keys, err := generator.GenerateKeys(ctx)
	if err != nil {
		return err
	}
	return r.WarmupKeys(ctx, keys, loader)
}

// WarmupKeys 实现特定键的缓存预热
func (r *RedisSentinelCache) WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error {
	for _, key := range keys {
		val, exp, err := loader.LoadData(ctx, key)
		if err != nil {
			r.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("加载键数据失败")
			continue
		}

		err = r.SetCtx(ctx, key, val, exp)
		if err != nil {
			r.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置缓存失败")
		}
	}
	return nil
}
