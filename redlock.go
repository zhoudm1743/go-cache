package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 错误定义
var (
	// ErrLockNotObtained 获取锁失败
	ErrLockNotObtained = errors.New("无法获取锁")
	// ErrLockNotHeld 锁未被持有
	ErrLockNotHeld = errors.New("锁未被持有")
)

// RedLock 使用Redlock算法实现的分布式锁
type RedLock struct {
	instances    []Cache           // 多个缓存实例，用于实现真正的分布式锁
	quorum       int               // 获取锁需要的最少成功数量
	retryCount   int               // 重试次数
	retryDelay   time.Duration     // 重试延迟
	clockDrift   float64           // 时钟漂移因子
	mutex        sync.Mutex        // 本地互斥锁，保护内部状态
	logger       Logger            // 日志接口
	acquiredKeys map[string]string // 当前持有的所有锁
}

// RedLockConfig Redlock配置
type RedLockConfig struct {
	RetryCount int           // 重试次数，默认3
	RetryDelay time.Duration // 重试延迟，默认200ms
	ClockDrift float64       // 时钟漂移因子，默认0.01 (1%)
}

// NewRedLock 创建一个新的Redlock实例
func NewRedLock(instances []Cache, config *RedLockConfig, logger Logger) *RedLock {
	if logger == nil {
		logger = defaultLogger
	}

	// 默认配置
	if config == nil {
		config = &RedLockConfig{
			RetryCount: 3,
			RetryDelay: 200 * time.Millisecond,
			ClockDrift: 0.01, // 1%
		}
	}

	// 计算仲裁数量，需要超过半数节点
	quorum := len(instances)/2 + 1

	return &RedLock{
		instances:    instances,
		quorum:       quorum,
		retryCount:   config.RetryCount,
		retryDelay:   config.RetryDelay,
		clockDrift:   config.ClockDrift,
		logger:       logger,
		acquiredKeys: make(map[string]string),
	}
}

// Lock 获取分布式锁
func (r *RedLock) Lock(key string, ttl time.Duration) (bool, error) {
	return r.LockCtx(context.Background(), key, ttl)
}

// LockCtx 获取分布式锁（带上下文）
func (r *RedLock) LockCtx(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// 生成唯一的锁标识
	val, err := r.genUniqueID()
	if err != nil {
		return false, err
	}

	// 尝试在多个节点上获取锁
	for i := 0; i < r.retryCount; i++ {
		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return false, err
		}

		start := time.Now()
		successCount := 0

		// 在所有实例上尝试获取锁
		for _, instance := range r.instances {
			// 使用SET NX命令设置锁
			err := instance.SetCtx(ctx, key, val, ttl)
			if err == nil {
				successCount++
			} else {
				r.logger.WithFields(map[string]interface{}{
					"key": key,
					"err": err.Error(),
				}).Debug("在一个节点上获取锁失败")
			}
		}

		// 计算锁的有效期
		elapsedTime := time.Since(start)
		validity := ttl - elapsedTime - time.Duration(float64(ttl)*r.clockDrift)

		// 检查是否获取了足够多的锁，并且锁仍然有效
		if successCount >= r.quorum && validity > 0 {
			// 保存锁标识
			r.acquiredKeys[key] = val
			r.logger.WithFields(map[string]interface{}{
				"key":          key,
				"successCount": successCount,
				"quorum":       r.quorum,
				"validity":     validity,
			}).Debug("获取分布式锁成功")
			return true, nil
		}

		// 获取锁失败，解锁所有节点
		r.unlockInstancesCtx(ctx, key, val)

		// 重试之前等待一段时间
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(r.retryDelay):
			// 继续下一次重试
		}
	}

	return false, ErrLockNotObtained
}

// Unlock 释放分布式锁
func (r *RedLock) Unlock(key string) error {
	return r.UnlockCtx(context.Background(), key)
}

// UnlockCtx 释放分布式锁（带上下文）
func (r *RedLock) UnlockCtx(ctx context.Context, key string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// 获取锁标识
	val, ok := r.acquiredKeys[key]
	if !ok {
		return ErrLockNotHeld
	}

	// 释放所有节点上的锁
	err := r.unlockInstancesCtx(ctx, key, val)
	if err == nil {
		// 从已获取锁的记录中删除
		delete(r.acquiredKeys, key)
	}

	return err
}

// 在所有节点上释放锁
func (r *RedLock) unlockInstancesCtx(ctx context.Context, key, val string) error {
	var lastErr error

	// Lua脚本：仅当值匹配时才删除锁
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	for _, instance := range r.instances {
		// 检查是否支持脚本执行
		if redisCache, ok := instance.(*RedisCache); ok && redisCache.GetClient() != nil {
			// 使用Lua脚本删除锁，这样可以原子性地检查和删除
			client := redisCache.GetClient()
			// 这里我们简化处理，只记录错误
			_, err := client.(interface {
				Eval(string, []string, ...interface{}) (interface{}, error)
			}).Eval(script, []string{key}, val)
			if err != nil {
				lastErr = err
				r.logger.WithFields(map[string]interface{}{
					"key": key,
					"err": err.Error(),
				}).Debug("使用Lua脚本释放锁失败")
			}
		} else {
			// 不支持脚本的回退方案：先获取值，比较后删除
			// 注意：这不是原子的，存在竞态条件
			result, err := instance.GetCtx(ctx, key)
			if err == nil && result == val {
				_, err = instance.DelCtx(ctx, key)
				if err != nil {
					lastErr = err
					r.logger.WithFields(map[string]interface{}{
						"key": key,
						"err": err.Error(),
					}).Debug("释放锁失败")
				}
			}
		}
	}

	return lastErr
}

// genUniqueID 生成用于锁标识的唯一ID
func (r *RedLock) genUniqueID() (string, error) {
	// 生成16字节的随机数
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("生成唯一ID失败: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// WithLock 使用分布式锁执行函数
func (r *RedLock) WithLock(key string, ttl time.Duration, fn func() error) error {
	return r.WithLockCtx(context.Background(), key, ttl, func(ctx context.Context) error {
		return fn()
	})
}

// WithLockCtx 使用分布式锁执行函数（带上下文）
func (r *RedLock) WithLockCtx(ctx context.Context, key string, ttl time.Duration, fn func(context.Context) error) error {
	// 获取锁
	locked, err := r.LockCtx(ctx, key, ttl)
	if err != nil {
		return err
	}

	if !locked {
		return ErrLockNotObtained
	}

	// 确保锁被释放
	defer r.Unlock(key)

	// 执行受保护的代码
	return fn(ctx)
}

// ExtendLock 延长锁的有效期（适用于长时间任务）
func (r *RedLock) ExtendLock(key string, ttl time.Duration) (bool, error) {
	return r.ExtendLockCtx(context.Background(), key, ttl)
}

// ExtendLockCtx 延长锁的有效期（带上下文）
func (r *RedLock) ExtendLockCtx(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	// 获取锁标识
	val, ok := r.acquiredKeys[key]
	if !ok {
		return false, ErrLockNotHeld
	}

	// 在所有节点上尝试延长锁
	successCount := 0

	// Lua脚本：仅当值匹配时才延长有效期
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	for _, instance := range r.instances {
		// 检查是否支持脚本执行
		if redisCache, ok := instance.(*RedisCache); ok && redisCache.GetClient() != nil {
			// 使用Lua脚本延长锁，这样可以原子性地检查和更新
			client := redisCache.GetClient()
			result, err := client.(interface {
				Eval(string, []string, ...interface{}) (interface{}, error)
			}).Eval(
				script,
				[]string{key},
				val,
				int(ttl/time.Millisecond),
			)

			if err == nil {
				// 检查Lua脚本执行结果
				if intResult, ok := result.(int64); ok && intResult == 1 {
					successCount++
				}
			} else {
				r.logger.WithFields(map[string]interface{}{
					"key": key,
					"err": err.Error(),
				}).Debug("使用Lua脚本延长锁失败")
			}
		} else {
			// 不支持脚本的回退方案
			result, err := instance.GetCtx(ctx, key)
			if err == nil && result == val {
				err = instance.ExpireCtx(ctx, key, ttl)
				if err == nil {
					successCount++
				}
			}
		}
	}

	// 检查是否有足够多的节点成功延长锁
	if successCount >= r.quorum {
		return true, nil
	}

	// 如果延长失败，为安全起见，尝试释放锁
	r.logger.WithFields(map[string]interface{}{
		"key":          key,
		"successCount": successCount,
		"quorum":       r.quorum,
	}).Warn("延长分布式锁失败，将释放锁")

	// 从已获取锁的记录中删除
	delete(r.acquiredKeys, key)

	// 释放所有节点上的锁
	r.unlockInstancesCtx(ctx, key, val)

	return false, ErrLockNotObtained
}
