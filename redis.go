package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache Redis 缓存实现
type RedisCache struct {
	client *redis.Client
	logger Logger
	prefix string
}

// NewRedisCache 创建新的 Redis 缓存实例
func NewRedisCache(cfg *RedisConfig, log Logger) (Cache, error) {
	if log == nil {
		log = defaultLogger
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis 连接失败: %w", err)
	}

	log.WithFields(map[string]interface{}{
		"host":   cfg.Host,
		"port":   cfg.Port,
		"db":     cfg.DB,
		"prefix": cfg.Prefix,
	}).Info("Redis 连接成功")

	return &RedisCache{
		client: rdb,
		logger: log,
		prefix: cfg.Prefix,
	}, nil
}

// buildKey 构建带前缀的键
func (r *RedisCache) buildKey(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + key
}

// GetClient 获取原始 Redis 客户端
func (r *RedisCache) GetClient() interface{} {
	return r.client
}

// Close 关闭连接
func (r *RedisCache) Close() error {
	return r.client.Close()
}

// ================== 默认方法（不带 Context） ==================

// 基础操作
func (r *RedisCache) Get(key string) (string, error) {
	return r.client.Get(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(context.Background(), r.buildKey(key), value, expiration).Err()
}

func (r *RedisCache) Del(keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	return r.client.Del(context.Background(), prefixedKeys...).Result()
}

func (r *RedisCache) Exists(keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	return r.client.Exists(context.Background(), prefixedKeys...).Result()
}

func (r *RedisCache) Expire(key string, expiration time.Duration) error {
	return r.client.Expire(context.Background(), r.buildKey(key), expiration).Err()
}

func (r *RedisCache) TTL(key string) (time.Duration, error) {
	return r.client.TTL(context.Background(), r.buildKey(key)).Result()
}

// 字符串操作
func (r *RedisCache) Incr(key string) (int64, error) {
	return r.client.Incr(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) Decr(key string) (int64, error) {
	return r.client.Decr(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) IncrBy(key string, value int64) (int64, error) {
	return r.client.IncrBy(context.Background(), r.buildKey(key), value).Result()
}

// 哈希操作
func (r *RedisCache) HGet(key, field string) (string, error) {
	return r.client.HGet(context.Background(), r.buildKey(key), field).Result()
}

func (r *RedisCache) HSet(key string, values ...interface{}) (int64, error) {
	return r.client.HSet(context.Background(), r.buildKey(key), values...).Result()
}

func (r *RedisCache) HDel(key string, fields ...string) (int64, error) {
	return r.client.HDel(context.Background(), r.buildKey(key), fields...).Result()
}

func (r *RedisCache) HGetAll(key string) (map[string]string, error) {
	return r.client.HGetAll(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) HExists(key, field string) (bool, error) {
	return r.client.HExists(context.Background(), r.buildKey(key), field).Result()
}

func (r *RedisCache) HLen(key string) (int64, error) {
	return r.client.HLen(context.Background(), r.buildKey(key)).Result()
}

// 列表操作
func (r *RedisCache) LPush(key string, values ...interface{}) (int64, error) {
	return r.client.LPush(context.Background(), r.buildKey(key), values...).Result()
}

func (r *RedisCache) RPush(key string, values ...interface{}) (int64, error) {
	return r.client.RPush(context.Background(), r.buildKey(key), values...).Result()
}

func (r *RedisCache) LPop(key string) (string, error) {
	return r.client.LPop(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) RPop(key string) (string, error) {
	return r.client.RPop(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) LLen(key string) (int64, error) {
	return r.client.LLen(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) LRange(key string, start, stop int64) ([]string, error) {
	return r.client.LRange(context.Background(), r.buildKey(key), start, stop).Result()
}

// 集合操作
func (r *RedisCache) SAdd(key string, members ...interface{}) (int64, error) {
	return r.client.SAdd(context.Background(), r.buildKey(key), members...).Result()
}

func (r *RedisCache) SRem(key string, members ...interface{}) (int64, error) {
	return r.client.SRem(context.Background(), r.buildKey(key), members...).Result()
}

func (r *RedisCache) SMembers(key string) ([]string, error) {
	return r.client.SMembers(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) SIsMember(key string, member interface{}) (bool, error) {
	return r.client.SIsMember(context.Background(), r.buildKey(key), member).Result()
}

func (r *RedisCache) SCard(key string) (int64, error) {
	return r.client.SCard(context.Background(), r.buildKey(key)).Result()
}

// 有序集合操作
func (r *RedisCache) ZAdd(key string, members ...Z) (int64, error) {
	// 转换为redis.Z
	redisMembers := make([]redis.Z, len(members))
	for i, m := range members {
		redisMembers[i] = redis.Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	return r.client.ZAdd(context.Background(), r.buildKey(key), redisMembers...).Result()
}

func (r *RedisCache) ZRem(key string, members ...interface{}) (int64, error) {
	return r.client.ZRem(context.Background(), r.buildKey(key), members...).Result()
}

func (r *RedisCache) ZRange(key string, start, stop int64) ([]string, error) {
	return r.client.ZRange(context.Background(), r.buildKey(key), start, stop).Result()
}

func (r *RedisCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	result, err := r.client.ZRangeWithScores(context.Background(), r.buildKey(key), start, stop).Result()
	if err != nil {
		return nil, err
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

func (r *RedisCache) ZCard(key string) (int64, error) {
	return r.client.ZCard(context.Background(), r.buildKey(key)).Result()
}

func (r *RedisCache) ZScore(key, member string) (float64, error) {
	return r.client.ZScore(context.Background(), r.buildKey(key), member).Result()
}

// 其他操作
func (r *RedisCache) Keys(pattern string) ([]string, error) {
	// 对于 Keys 操作，我们需要添加前缀到模式中
	prefixedPattern := r.buildKey(pattern)
	keys, err := r.client.Keys(context.Background(), prefixedPattern).Result()
	if err != nil {
		return nil, err
	}

	// 移除前缀
	if r.prefix != "" {
		for i, key := range keys {
			keys[i] = key[len(r.prefix):]
		}
	}

	return keys, nil
}

func (r *RedisCache) Ping() error {
	return r.client.Ping(context.Background()).Err()
}

// ================== 带 Context 的方法 ==================

// 基础操作
func (r *RedisCache) GetCtx(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, r.buildKey(key)).Result()
}

func (r *RedisCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, r.buildKey(key), value, expiration).Err()
}

func (r *RedisCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	return r.client.Del(ctx, prefixedKeys...).Result()
}

func (r *RedisCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	// 转换所有键为带前缀的键
	prefixedKeys := make([]string, len(keys))
	for i, key := range keys {
		prefixedKeys[i] = r.buildKey(key)
	}
	return r.client.Exists(ctx, prefixedKeys...).Result()
}

func (r *RedisCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	return r.client.Expire(ctx, r.buildKey(key), expiration).Err()
}

func (r *RedisCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, r.buildKey(key)).Result()
}

// 字符串操作
func (r *RedisCache) IncrCtx(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

func (r *RedisCache) DecrCtx(ctx context.Context, key string) (int64, error) {
	return r.client.Decr(ctx, key).Result()
}

func (r *RedisCache) IncrByCtx(ctx context.Context, key string, value int64) (int64, error) {
	return r.client.IncrBy(ctx, key, value).Result()
}

// 哈希操作
func (r *RedisCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	return r.client.HGet(ctx, key, field).Result()
}

func (r *RedisCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return r.client.HSet(ctx, key, values...).Result()
}

func (r *RedisCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	return r.client.HDel(ctx, key, fields...).Result()
}

func (r *RedisCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	return r.client.HGetAll(ctx, key).Result()
}

func (r *RedisCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	return r.client.HExists(ctx, key, field).Result()
}

func (r *RedisCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	return r.client.HLen(ctx, key).Result()
}

// 列表操作
func (r *RedisCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return r.client.LPush(ctx, key, values...).Result()
}

func (r *RedisCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return r.client.RPush(ctx, key, values...).Result()
}

func (r *RedisCache) LPopCtx(ctx context.Context, key string) (string, error) {
	return r.client.LPop(ctx, key).Result()
}

func (r *RedisCache) RPopCtx(ctx context.Context, key string) (string, error) {
	return r.client.RPop(ctx, key).Result()
}

func (r *RedisCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	return r.client.LLen(ctx, key).Result()
}

func (r *RedisCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.client.LRange(ctx, key, start, stop).Result()
}

// 集合操作
func (r *RedisCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return r.client.SAdd(ctx, key, members...).Result()
}

func (r *RedisCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return r.client.SRem(ctx, key, members...).Result()
}

func (r *RedisCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}

func (r *RedisCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	return r.client.SIsMember(ctx, key, member).Result()
}

func (r *RedisCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	return r.client.SCard(ctx, key).Result()
}

// 有序集合操作
func (r *RedisCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	// 转换为redis.Z
	redisMembers := make([]redis.Z, len(members))
	for i, m := range members {
		redisMembers[i] = redis.Z{
			Score:  m.Score,
			Member: m.Member,
		}
	}

	return r.client.ZAdd(ctx, r.buildKey(key), redisMembers...).Result()
}

func (r *RedisCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return r.client.ZRem(ctx, r.buildKey(key), members...).Result()
}

func (r *RedisCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.client.ZRange(ctx, r.buildKey(key), start, stop).Result()
}

func (r *RedisCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	result, err := r.client.ZRangeWithScores(ctx, r.buildKey(key), start, stop).Result()
	if err != nil {
		return nil, err
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

func (r *RedisCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	return r.client.ZCard(ctx, r.buildKey(key)).Result()
}

func (r *RedisCache) ZScoreCtx(ctx context.Context, key, member string) (float64, error) {
	return r.client.ZScore(ctx, r.buildKey(key), member).Result()
}

// 其他操作
func (r *RedisCache) KeysCtx(ctx context.Context, pattern string) ([]string, error) {
	return r.client.Keys(ctx, pattern).Result()
}

func (r *RedisCache) PingCtx(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
