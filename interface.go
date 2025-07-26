package cache

import (
	"context"
	"errors"
	"time"
)

// 错误定义
var (
	// ErrKeyNotFound 键不存在错误
	ErrKeyNotFound = errors.New("键不存在")
	// ErrTypeMismatch 类型不匹配错误
	ErrTypeMismatch = errors.New("值类型不匹配")
	// ErrKeyExists 键已存在错误
	ErrKeyExists = errors.New("键已存在")
	// ErrNotSupported 操作不支持错误
	ErrNotSupported = errors.New("操作不支持")
	// ErrInvalidArgument 无效参数错误
	ErrInvalidArgument = errors.New("无效参数")
	// ErrConnectionFailed 连接失败错误
	ErrConnectionFailed = errors.New("连接失败")
	// ErrTimeout 操作超时错误
	ErrTimeout = errors.New("操作超时")
	// ErrCacheFull 缓存已满错误
	ErrCacheFull = errors.New("缓存已满")
	// ErrFieldNotFound 字段不存在错误(用于哈希表操作)
	ErrFieldNotFound = errors.New("字段不存在")
	// ErrServerInternal 服务器内部错误
	ErrServerInternal = errors.New("服务器内部错误")
)

// Z 是有序集合的成员结构
type Z struct {
	Score  float64
	Member interface{}
}

// DataLoader 数据加载接口，用于缓存预热
type DataLoader interface {
	// LoadData 加载数据到缓存
	// key: 缓存键
	// 返回: (缓存值, 过期时间, 错误)
	LoadData(ctx context.Context, key string) (interface{}, time.Duration, error)
}

// KeyGenerator 键生成器接口，用于生成需要预热的键
type KeyGenerator interface {
	// GenerateKeys 生成需要预热的键列表
	GenerateKeys(ctx context.Context) ([]string, error)
}

// Cache 缓存接口
type Cache interface {
	// 缓存预热相关方法
	Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error
	WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error

	// 默认方法（不带 Context，使用 unified.Background()）
	// 基础操作
	Get(key string) (string, error)
	Set(key string, value interface{}, expiration time.Duration) error
	Del(keys ...string) (int64, error)
	Exists(keys ...string) (int64, error)
	Expire(key string, expiration time.Duration) error
	TTL(key string) (time.Duration, error)

	// 字符串操作
	Incr(key string) (int64, error)
	Decr(key string) (int64, error)
	IncrBy(key string, value int64) (int64, error)

	// 哈希操作
	HGet(key, field string) (string, error)
	HSet(key string, values ...interface{}) (int64, error)
	HDel(key string, fields ...string) (int64, error)
	HGetAll(key string) (map[string]string, error)
	HExists(key, field string) (bool, error)
	HLen(key string) (int64, error)

	// 列表操作
	LPush(key string, values ...interface{}) (int64, error)
	RPush(key string, values ...interface{}) (int64, error)
	LPop(key string) (string, error)
	RPop(key string) (string, error)
	LLen(key string) (int64, error)
	LRange(key string, start, stop int64) ([]string, error)

	// 集合操作
	SAdd(key string, members ...interface{}) (int64, error)
	SRem(key string, members ...interface{}) (int64, error)
	SMembers(key string) ([]string, error)
	SIsMember(key string, member interface{}) (bool, error)
	SCard(key string) (int64, error)

	// 有序集合操作
	ZAdd(key string, members ...Z) (int64, error)
	ZRem(key string, members ...interface{}) (int64, error)
	ZRange(key string, start, stop int64) ([]string, error)
	ZRangeWithScores(key string, start, stop int64) ([]Z, error)
	ZCard(key string) (int64, error)
	ZScore(key, member string) (float64, error)

	// 其他操作
	Keys(pattern string) ([]string, error)
	Ping() error

	// 带 Context 的方法（精细控制）
	// 基础操作
	GetCtx(ctx context.Context, key string) (string, error)
	SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	DelCtx(ctx context.Context, keys ...string) (int64, error)
	ExistsCtx(ctx context.Context, keys ...string) (int64, error)
	ExpireCtx(ctx context.Context, key string, expiration time.Duration) error
	TTLCtx(ctx context.Context, key string) (time.Duration, error)

	// 字符串操作
	IncrCtx(ctx context.Context, key string) (int64, error)
	DecrCtx(ctx context.Context, key string) (int64, error)
	IncrByCtx(ctx context.Context, key string, value int64) (int64, error)

	// 哈希操作
	HGetCtx(ctx context.Context, key, field string) (string, error)
	HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error)
	HDelCtx(ctx context.Context, key string, fields ...string) (int64, error)
	HGetAllCtx(ctx context.Context, key string) (map[string]string, error)
	HExistsCtx(ctx context.Context, key, field string) (bool, error)
	HLenCtx(ctx context.Context, key string) (int64, error)

	// 列表操作
	LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error)
	RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error)
	LPopCtx(ctx context.Context, key string) (string, error)
	RPopCtx(ctx context.Context, key string) (string, error)
	LLenCtx(ctx context.Context, key string) (int64, error)
	LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error)

	// 集合操作
	SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error)
	SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error)
	SMembersCtx(ctx context.Context, key string) ([]string, error)
	SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error)
	SCardCtx(ctx context.Context, key string) (int64, error)

	// 有序集合操作
	ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error)
	ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error)
	ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error)
	ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error)
	ZCardCtx(ctx context.Context, key string) (int64, error)
	ZScoreCtx(ctx context.Context, key, member string) (float64, error)

	// 其他操作
	KeysCtx(ctx context.Context, pattern string) ([]string, error)
	PingCtx(ctx context.Context) error

	// 工具方法
	Close() error
	GetClient() interface{}
}

// ZMember 有序集合成员
type ZMember struct {
	Score  float64
	Member interface{}
}

// ZMembers 有序集合成员列表
type ZMembers []ZMember
