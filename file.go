package cache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/binary"
	"math"
	"regexp"
	"sort"

	"go.etcd.io/bbolt"
)

// 定义 BoltDB 中使用的桶名
const (
	defaultBucket    = "default"
	hashBucket       = "hash"
	listBucket       = "list"
	setBucket        = "set"
	zsetBucket       = "zset"
	zsetScoreBucket  = "zset_score"
	expirationBucket = "expiration"

	// 压缩相关常量
	compressionThreshold = 4096    // 超过4KB的值进行压缩
	compressionFlag      = byte(1) // 标记值是否被压缩
)

// 压缩值
func compressValue(data []byte) ([]byte, error) {
	// 如果数据小于阈值，不进行压缩
	if len(data) < compressionThreshold {
		return data, nil
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(data)
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	// 添加压缩标记
	result := make([]byte, buf.Len()+1)
	result[0] = compressionFlag
	copy(result[1:], buf.Bytes())

	return result, nil
}

// 解压缩值
func decompressValue(data []byte) ([]byte, error) {
	// 检查是否被压缩
	if len(data) == 0 || data[0] != compressionFlag {
		return data, nil
	}

	zr, err := gzip.NewReader(bytes.NewReader(data[1:]))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, zr)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// FileCache 基于文件的缓存实现，使用 BoltDB 作为存储引擎
type FileCache struct {
	db       *bbolt.DB
	logger   Logger
	prefix   string
	memCache sync.Map      // 内存缓存层
	cacheTTL time.Duration // 内存缓存过期时间
}

// 缓存项结构
type cacheItem struct {
	Value      interface{} `json:"value"`
	Expiration int64       `json:"expiration"` // Unix时间戳，0表示永不过期
}

// 对象池，减少内存分配和垃圾回收压力
var itemPool = sync.Pool{
	New: func() interface{} {
		return &cacheItem{}
	},
}

// NewFileCache 创建新的文件缓存实例
func NewFileCache(cfg *FileConfig, logger Logger) (Cache, error) {
	if logger == nil {
		logger = defaultLogger
	}

	// 确保缓存目录存在
	if err := os.MkdirAll(cfg.FilePath, 0755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}

	dbPath := filepath.Join(cfg.FilePath, "cache.db")

	// 打开 BoltDB 数据库，优化配置参数
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{
		// 增加超时时间，避免高并发时锁争用问题
		Timeout: 3 * time.Second,

		// 增加页面大小，对于大数据量写入非常有效
		// 默认是4KB，可以增加到8KB或16KB
		PageSize: 16 * 1024,

		// 设置freelist类型为map而不是array，对大型数据库有性能优势
		FreelistType: bbolt.FreelistMapType,

		// 禁用freelist同步，提高写入性能
		NoFreelistSync: true,
	})
	if err != nil {
		return nil, fmt.Errorf("无法打开缓存数据库: %w", err)
	}

	// 初始化桶
	err = db.Update(func(tx *bbolt.Tx) error {
		buckets := []string{
			defaultBucket,
			hashBucket,
			listBucket,
			setBucket,
			zsetBucket,
			zsetScoreBucket,
			expirationBucket,
		}

		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("创建桶 %s 失败: %w", bucket, err)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("初始化缓存数据库失败: %w", err)
	}

	// 启动一个后台协程，定期清理过期的键
	go func(db *bbolt.DB, logger Logger) {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cleanExpiredKeys(db, logger)
		}
	}(db, logger)

	logger.WithFields(map[string]interface{}{
		"path":   dbPath,
		"prefix": cfg.Prefix,
	}).Info("文件缓存初始化成功")

	fileCache := &FileCache{
		db:       db,
		logger:   logger,
		prefix:   cfg.Prefix,
		cacheTTL: 5 * time.Minute, // 设置内存缓存默认过期时间为5分钟
	}

	// 启动定期压缩任务
	fileCache.startCompactTask()

	return fileCache, nil
}

// 清理过期的键，采用懒清理模式，每次只清理一部分
func cleanExpiredKeys(db *bbolt.DB, logger Logger) {
	now := time.Now().Unix()

	err := db.Update(func(tx *bbolt.Tx) error {
		expBucket := tx.Bucket([]byte(expirationBucket))
		if expBucket == nil {
			return fmt.Errorf("过期桶不存在")
		}

		// 遍历所有过期时间
		c := expBucket.Cursor()
		count := 0
		maxClean := 1000 // 每次最多清理1000个过期键

		for k, v := c.First(); k != nil && count < maxClean; k, v = c.Next() {
			expTime := bytesToInt64(k)
			if expTime <= now {
				// 已过期，删除键
				keysData := v
				var keys []string
				if err := json.Unmarshal(keysData, &keys); err != nil {
					logger.Errorf("解析过期键列表失败: %v", err)
					continue
				}

				// 删除每个过期的键
				for _, key := range keys {
					bucketName, bucketKey := parseKey(key)
					bucket := tx.Bucket([]byte(bucketName))
					if bucket != nil {
						bucket.Delete([]byte(bucketKey))
					}
				}

				// 删除过期时间记录
				expBucket.Delete(k)
				count++
			} else {
				// 后面的都还没过期
				break
			}
		}

		if count > 0 {
			logger.Infof("清理了 %d 个过期键", count)
		}

		return nil
	})

	if err != nil {
		logger.Errorf("清理过期键失败: %v", err)
	}
}

// 将过期时间和键关联起来
func addKeyExpiration(tx *bbolt.Tx, key string, expiration int64) error {
	if expiration == 0 {
		return nil // 永不过期
	}

	expBucket := tx.Bucket([]byte(expirationBucket))
	if expBucket == nil {
		return fmt.Errorf("过期桶不存在")
	}

	// 获取此过期时间的所有键
	expTimeBytes := int64ToBytes(expiration)
	keysData := expBucket.Get(expTimeBytes)

	var keys []string
	if keysData != nil {
		if err := json.Unmarshal(keysData, &keys); err != nil {
			return err
		}
	}

	// 添加新键
	keys = append(keys, key)
	keysData, err := json.Marshal(keys)
	if err != nil {
		return err
	}

	return expBucket.Put(expTimeBytes, keysData)
}

// 将 int64 转换为字节数组
func int64ToBytes(n int64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (56 - 8*i))
	}
	return b
}

// 将字节数组转换为 int64
func bytesToInt64(b []byte) int64 {
	var n int64
	for i := 0; i < 8; i++ {
		n |= int64(b[i]) << (56 - 8*i)
	}
	return n
}

// 解析键，返回桶名和实际键名
func parseKey(key string) (string, string) {
	// 默认使用默认桶
	return defaultBucket, key
}

// buildKey 构建带前缀的键
func (f *FileCache) buildKey(key string) string {
	if f.prefix == "" {
		return key
	}
	return f.prefix + key
}

// GetClient 获取原始 BoltDB 客户端
func (f *FileCache) GetClient() interface{} {
	return f.db
}

// Close 关闭数据库连接
func (f *FileCache) Close() error {
	return f.db.Close()
}

// ================== 基本操作 ==================

// Get 获取缓存
func (f *FileCache) Get(key string) (string, error) {
	prefixedKey := f.buildKey(key)

	// 先查内存缓存
	if val, ok := f.memCache.Load(prefixedKey); ok {
		item, ok := val.(cacheItem)
		if !ok {
			return "", fmt.Errorf("内存缓存类型错误")
		}

		// 检查是否过期
		if item.Expiration > 0 && item.Expiration <= time.Now().Unix() {
			f.memCache.Delete(prefixedKey)
		} else {
			// 转换为字符串
			switch v := item.Value.(type) {
			case string:
				return v, nil
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(item.Value)
				if err != nil {
					return "", err
				}
				return string(valueBytes), nil
			}
		}
	}

	// 内存缓存未命中，查询文件缓存
	var value string
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		data := bucket.Get([]byte(prefixedKey))
		if data == nil {
			return ErrKeyNotFound // 已过期
		}

		// 解压缩数据
		decompressedData, err := decompressValue(data)
		if err != nil {
			return err
		}

		// 从对象池获取item
		item := itemPool.Get().(*cacheItem)
		defer itemPool.Put(item)

		if err := json.Unmarshal(decompressedData, item); err != nil {
			return err
		}

		// 检查是否过期
		if item.Expiration > 0 && item.Expiration <= time.Now().Unix() {
			return ErrKeyNotFound
		}

		// 转换为字符串
		switch v := item.Value.(type) {
		case string:
			value = v
		default:
			// 尝试将其他类型转换为字符串
			valueBytes, err := json.Marshal(item.Value)
			if err != nil {
				return err
			}
			value = string(valueBytes)
		}

		// 将结果放入内存缓存
		f.memCache.Store(prefixedKey, *item)

		return nil
	})

	return value, err
}

// Set 设置缓存
func (f *FileCache) Set(key string, value interface{}, expiration time.Duration) error {
	prefixedKey := f.buildKey(key)

	var exp int64
	if expiration > 0 {
		exp = time.Now().Add(expiration).Unix()
	}

	// 创建缓存项
	item := cacheItem{
		Value:      value,
		Expiration: exp,
	}

	// 更新文件缓存
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		if expiration > 0 {
			// 添加到过期时间索引
			if err := addKeyExpiration(tx, prefixedKey, exp); err != nil {
				return err
			}
		}

		// 序列化缓存项
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}

		// 压缩大型值
		compressedData, err := compressValue(data)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(prefixedKey), compressedData)
	})

	if err == nil {
		// 同时更新内存缓存
		f.memCache.Store(prefixedKey, item)
	}

	return err
}

// Del 删除缓存
func (f *FileCache) Del(keys ...string) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		for _, key := range keys {
			prefixedKey := f.buildKey(key)
			// 检查键是否存在
			if data := bucket.Get([]byte(prefixedKey)); data != nil {
				if err := bucket.Delete([]byte(prefixedKey)); err != nil {
					return err
				}
				count++

				// 同时删除内存缓存
				f.memCache.Delete(prefixedKey)
			}
		}

		return nil
	})

	return count, err
}

// Exists 检查键是否存在
func (f *FileCache) Exists(keys ...string) (int64, error) {
	var count int64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		now := time.Now().Unix()

		for _, key := range keys {
			data := bucket.Get([]byte(f.buildKey(key)))
			if data != nil {
				var item cacheItem
				if err := json.Unmarshal(data, &item); err != nil {
					return err
				}

				// 检查是否过期
				if item.Expiration == 0 || item.Expiration > now {
					count++
				}
			}
		}

		return nil
	})

	return count, err
}

// Expire 设置过期时间
func (f *FileCache) Expire(key string, expiration time.Duration) error {
	return f.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		prefixedKey := f.buildKey(key)
		data := bucket.Get([]byte(prefixedKey))
		if data == nil {
			return ErrKeyNotFound
		}

		var item cacheItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}

		// 设置新的过期时间
		var exp int64
		if expiration > 0 {
			exp = time.Now().Add(expiration).Unix()

			// 添加到过期时间索引
			if err := addKeyExpiration(tx, prefixedKey, exp); err != nil {
				return err
			}
		}

		item.Expiration = exp

		updatedData, err := json.Marshal(item)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(prefixedKey), updatedData)
	})
}

// TTL 获取剩余生存时间
func (f *FileCache) TTL(key string) (time.Duration, error) {
	var ttl time.Duration
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		data := bucket.Get([]byte(f.buildKey(key)))
		if data == nil {
			return ErrKeyNotFound // 已过期
		}

		var item cacheItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}

		// 检查过期时间
		if item.Expiration == 0 {
			ttl = -1 // 永不过期
		} else {
			now := time.Now().Unix()
			if item.Expiration <= now {
				return ErrKeyNotFound // 已过期
			}
			ttl = time.Duration(item.Expiration-now) * time.Second
		}

		return nil
	})

	return ttl, err
}

// ================== 计数器操作 ==================

// Incr 自增
func (f *FileCache) Incr(key string) (int64, error) {
	return f.IncrBy(key, 1)
}

// Decr 自减
func (f *FileCache) Decr(key string) (int64, error) {
	return f.IncrBy(key, -1)
}

// IncrBy 按指定值自增
func (f *FileCache) IncrBy(key string, value int64) (int64, error) {
	var result int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		prefixedKey := f.buildKey(key)
		data := bucket.Get([]byte(prefixedKey))

		if data == nil {
			// 键不存在，创建新键并设置为指定值
			result = value
			item := cacheItem{
				Value:      result,
				Expiration: 0, // 永不过期
			}

			data, err := json.Marshal(item)
			if err != nil {
				return err
			}

			return bucket.Put([]byte(prefixedKey), data)
		}

		// 键存在，解析并增加值
		var item cacheItem
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}

		// 检查是否过期
		if item.Expiration > 0 && item.Expiration < time.Now().Unix() {
			// 已过期，创建新键
			result = value
			item = cacheItem{
				Value:      result,
				Expiration: 0, // 永不过期
			}
		} else {
			// 未过期，增加值
			var currentValue int64

			// 尝试将值转换为 int64
			switch v := item.Value.(type) {
			case float64:
				currentValue = int64(v)
			case int64:
				currentValue = v
			case int:
				currentValue = int64(v)
			case string:
				// 尝试将字符串解析为数字
				var err error
				currentValue, err = strconv.ParseInt(v, 10, 64)
				if err != nil {
					return fmt.Errorf("值不是有效的整数: %w", err)
				}
			default:
				return fmt.Errorf("值不是有效的整数")
			}

			result = currentValue + value
			item.Value = result
		}

		// 保存更新后的值
		updatedData, err := json.Marshal(item)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(prefixedKey), updatedData)
	})

	return result, err
}

// ================== 哈希表操作 ==================

// 获取哈希表的桶
func getHashBucket(tx *bbolt.Tx, key string) (*bbolt.Bucket, error) {
	// 获取哈希桶
	hashBucketObj := tx.Bucket([]byte(hashBucket))
	if hashBucketObj == nil {
		return nil, fmt.Errorf("哈希桶不存在")
	}

	// 创建或获取特定哈希表的子桶
	var bucket *bbolt.Bucket
	var err error

	// 尝试获取现有子桶
	bucket = hashBucketObj.Bucket([]byte(key))
	if bucket == nil {
		// 如果是只读事务，返回错误
		if tx.Writable() {
			// 创建新子桶
			bucket, err = hashBucketObj.CreateBucketIfNotExists([]byte(key))
			if err != nil {
				return nil, fmt.Errorf("创建哈希表桶失败: %w", err)
			}
		} else {
			return nil, ErrKeyNotFound
		}
	}

	return bucket, nil
}

// HGet 获取哈希表中的字段值
func (f *FileCache) HGet(key, field string) (string, error) {
	var value string
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		data := bucket.Get([]byte(field))
		if data == nil {
			return ErrKeyNotFound
		}

		value = string(data)
		return nil
	})

	return value, err
}

// HSet 设置哈希表中的字段值
func (f *FileCache) HSet(key string, values ...interface{}) (int64, error) {
	if len(values)%2 != 0 {
		return 0, fmt.Errorf("值必须是键值对")
	}

	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 处理键值对
		for i := 0; i < len(values); i += 2 {
			fieldName, ok := values[i].(string)
			if !ok {
				return fmt.Errorf("字段名必须是字符串")
			}

			// 将值转换为字符串
			var fieldValue string
			switch v := values[i+1].(type) {
			case string:
				fieldValue = v
			case []byte:
				fieldValue = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				fieldValue = string(valueBytes)
			}

			// 检查字段是否已存在
			if bucket.Get([]byte(fieldName)) == nil {
				count++
			}

			// 设置字段值
			if err := bucket.Put([]byte(fieldName), []byte(fieldValue)); err != nil {
				return err
			}
		}

		return nil
	})

	return count, err
}

// HDel 删除哈希表中的字段
func (f *FileCache) HDel(key string, fields ...string) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 删除每个字段
		for _, field := range fields {
			if bucket.Get([]byte(field)) != nil {
				if err := bucket.Delete([]byte(field)); err != nil {
					return err
				}
				count++
			}
		}

		return nil
	})

	return count, err
}

// HGetAll 获取哈希表中的所有字段和值
func (f *FileCache) HGetAll(key string) (map[string]string, error) {
	result := make(map[string]string)
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 遍历所有键值对
		return bucket.ForEach(func(k, v []byte) error {
			result[string(k)] = string(v)
			return nil
		})
	})

	return result, err
}

// HExists 检查哈希表中是否存在指定字段
func (f *FileCache) HExists(key, field string) (bool, error) {
	var exists bool
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		exists = bucket.Get([]byte(field)) != nil
		return nil
	})

	return exists, err
}

// HLen 获取哈希表中的字段数量
func (f *FileCache) HLen(key string) (int64, error) {
	var count int64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getHashBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 遍历所有键值对并计数
		return bucket.ForEach(func(k, v []byte) error {
			count++
			return nil
		})
	})

	return count, err
}

// ================== 列表操作 ==================

// 获取列表的桶
func getListBucket(tx *bbolt.Tx, key string) (*bbolt.Bucket, error) {
	// 获取列表桶
	listBucketObj := tx.Bucket([]byte(listBucket))
	if listBucketObj == nil {
		return nil, fmt.Errorf("列表桶不存在")
	}

	// 创建或获取特定列表的子桶
	var bucket *bbolt.Bucket
	var err error

	// 尝试获取现有子桶
	bucket = listBucketObj.Bucket([]byte(key))
	if bucket == nil {
		// 如果是只读事务，返回错误
		if tx.Writable() {
			// 创建新子桶
			bucket, err = listBucketObj.CreateBucketIfNotExists([]byte(key))
			if err != nil {
				return nil, fmt.Errorf("创建列表桶失败: %w", err)
			}
		} else {
			return nil, ErrKeyNotFound
		}
	}

	return bucket, nil
}

// 列表元素结构
type listItem struct {
	Index int64  `json:"index"`
	Value string `json:"value"`
}

// 获取列表的长度
func getListLength(bucket *bbolt.Bucket) (int64, error) {
	lengthBytes := bucket.Get([]byte("length"))
	if lengthBytes == nil {
		return 0, nil
	}

	var length int64
	if err := json.Unmarshal(lengthBytes, &length); err != nil {
		return 0, err
	}

	return length, nil
}

// 设置列表的长度
func setListLength(bucket *bbolt.Bucket, length int64) error {
	lengthBytes, err := json.Marshal(length)
	if err != nil {
		return err
	}

	return bucket.Put([]byte("length"), lengthBytes)
}

// 获取列表元素的键
func getListItemKey(index int64) []byte {
	return []byte(fmt.Sprintf("item:%d", index))
}

// LPush 将一个或多个值插入到列表头部
func (f *FileCache) LPush(key string, values ...interface{}) (int64, error) {
	var length int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err = getListLength(bucket)
		if err != nil {
			return err
		}

		// 插入元素到头部，从前往后处理，这样最后一个元素会在最前面
		for i := 0; i < len(values); i++ {
			// 将值转换为字符串
			var value string
			switch v := values[i].(type) {
			case string:
				value = v
			case []byte:
				value = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				value = string(valueBytes)
			}

			// 将所有元素后移
			for j := length; j > 0; j-- {
				itemKey := getListItemKey(j - 1)
				itemData := bucket.Get(itemKey)
				if itemData != nil {
					if err := bucket.Put(getListItemKey(j), itemData); err != nil {
						return err
					}
				}
			}

			// 插入新元素到头部
			item := listItem{
				Index: 0,
				Value: value,
			}

			itemData, err := json.Marshal(item)
			if err != nil {
				return err
			}

			if err := bucket.Put(getListItemKey(0), itemData); err != nil {
				return err
			}

			length++
		}

		// 更新长度
		return setListLength(bucket, length)
	})

	return length, err
}

// RPush 将一个或多个值插入到列表尾部
func (f *FileCache) RPush(key string, values ...interface{}) (int64, error) {
	var length int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err = getListLength(bucket)
		if err != nil {
			return err
		}

		// 插入元素到尾部
		for _, v := range values {
			// 将值转换为字符串
			var value string
			switch val := v.(type) {
			case string:
				value = val
			case []byte:
				value = string(val)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(val)
				if err != nil {
					return err
				}
				value = string(valueBytes)
			}

			// 插入新元素到尾部
			item := listItem{
				Index: length,
				Value: value,
			}

			itemData, err := json.Marshal(item)
			if err != nil {
				return err
			}

			if err := bucket.Put(getListItemKey(length), itemData); err != nil {
				return err
			}

			length++
		}

		// 更新长度
		return setListLength(bucket, length)
	})

	return length, err
}

// LPop 移除并返回列表头部元素
func (f *FileCache) LPop(key string) (string, error) {
	var value string
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err := getListLength(bucket)
		if err != nil {
			return err
		}

		if length == 0 {
			return ErrKeyNotFound
		}

		// 获取头部元素
		itemData := bucket.Get(getListItemKey(0))
		if itemData == nil {
			return fmt.Errorf("列表元素不存在")
		}

		var item listItem
		if err := json.Unmarshal(itemData, &item); err != nil {
			return err
		}

		value = item.Value

		// 将所有元素前移
		for i := int64(0); i < length-1; i++ {
			nextItemData := bucket.Get(getListItemKey(i + 1))
			if nextItemData != nil {
				if err := bucket.Put(getListItemKey(i), nextItemData); err != nil {
					return err
				}
			}
		}

		// 删除最后一个元素
		if err := bucket.Delete(getListItemKey(length - 1)); err != nil {
			return err
		}

		// 更新长度
		return setListLength(bucket, length-1)
	})

	return value, err
}

// RPop 移除并返回列表尾部元素
func (f *FileCache) RPop(key string) (string, error) {
	var value string
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err := getListLength(bucket)
		if err != nil {
			return err
		}

		if length == 0 {
			return ErrKeyNotFound
		}

		// 获取尾部元素
		itemData := bucket.Get(getListItemKey(length - 1))
		if itemData == nil {
			return fmt.Errorf("列表元素不存在")
		}

		var item listItem
		if err := json.Unmarshal(itemData, &item); err != nil {
			return err
		}

		value = item.Value

		// 删除尾部元素
		if err := bucket.Delete(getListItemKey(length - 1)); err != nil {
			return err
		}

		// 更新长度
		return setListLength(bucket, length-1)
	})

	return value, err
}

// LLen 获取列表长度
func (f *FileCache) LLen(key string) (int64, error) {
	var length int64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err = getListLength(bucket)
		return err
	})

	return length, err
}

// LRange 获取列表指定范围内的元素
func (f *FileCache) LRange(key string, start, stop int64) ([]string, error) {
	var result []string
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getListBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取当前长度
		length, err := getListLength(bucket)
		if err != nil {
			return err
		}

		// 处理负索引
		if start < 0 {
			start = length + start
			if start < 0 {
				start = 0
			}
		}

		if stop < 0 {
			stop = length + stop
			if stop < 0 {
				stop = 0
			}
		}

		// 确保索引在有效范围内
		if start >= length {
			return nil
		}

		if stop >= length {
			stop = length - 1
		}

		// 获取范围内的元素
		for i := start; i <= stop; i++ {
			itemData := bucket.Get(getListItemKey(i))
			if itemData == nil {
				continue
			}

			var item listItem
			if err := json.Unmarshal(itemData, &item); err != nil {
				return err
			}

			result = append(result, item.Value)
		}

		return nil
	})

	return result, err
}

// ================== 集合操作 ==================

// 获取集合的桶
func getSetBucket(tx *bbolt.Tx, key string) (*bbolt.Bucket, error) {
	// 获取集合桶
	setBucketObj := tx.Bucket([]byte(setBucket))
	if setBucketObj == nil {
		return nil, fmt.Errorf("集合桶不存在")
	}

	// 创建或获取特定集合的子桶
	var bucket *bbolt.Bucket
	var err error

	// 尝试获取现有子桶
	bucket = setBucketObj.Bucket([]byte(key))
	if bucket == nil {
		// 如果是只读事务，返回错误
		if tx.Writable() {
			// 创建新子桶
			bucket, err = setBucketObj.CreateBucketIfNotExists([]byte(key))
			if err != nil {
				return nil, fmt.Errorf("创建集合桶失败: %w", err)
			}
		} else {
			return nil, ErrKeyNotFound
		}
	}

	return bucket, nil
}

// SAdd 将一个或多个成员元素加入到集合中
func (f *FileCache) SAdd(key string, members ...interface{}) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 添加成员
		for _, member := range members {
			// 将值转换为字符串
			var value string
			switch v := member.(type) {
			case string:
				value = v
			case []byte:
				value = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				value = string(valueBytes)
			}

			// 检查成员是否已存在
			if bucket.Get([]byte(value)) == nil {
				// 添加新成员
				if err := bucket.Put([]byte(value), []byte{1}); err != nil {
					return err
				}
				count++
			}
		}

		return nil
	})

	return count, err
}

// SMembers 返回集合中的所有成员
func (f *FileCache) SMembers(key string) ([]string, error) {
	var members []string
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 遍历所有成员
		return bucket.ForEach(func(k, v []byte) error {
			members = append(members, string(k))
			return nil
		})
	})

	return members, err
}

// SRem 移除集合中的一个或多个成员
func (f *FileCache) SRem(key string, members ...interface{}) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := getSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 移除成员
		for _, member := range members {
			// 将值转换为字符串
			var value string
			switch v := member.(type) {
			case string:
				value = v
			case []byte:
				value = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				value = string(valueBytes)
			}

			// 检查成员是否存在
			if bucket.Get([]byte(value)) != nil {
				// 删除成员
				if err := bucket.Delete([]byte(value)); err != nil {
					return err
				}
				count++
			}
		}

		return nil
	})

	return count, err
}

// SCard 获取集合的成员数
func (f *FileCache) SCard(key string) (int64, error) {
	var count int64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 遍历所有成员并计数
		return bucket.ForEach(func(k, v []byte) error {
			count++
			return nil
		})
	})

	return count, err
}

// SIsMember 判断成员元素是否是集合的成员
func (f *FileCache) SIsMember(key string, member interface{}) (bool, error) {
	var isMember bool
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 将值转换为字符串
		var value string
		switch v := member.(type) {
		case string:
			value = v
		case []byte:
			value = string(v)
		default:
			// 尝试将其他类型转换为字符串
			valueBytes, err := json.Marshal(v)
			if err != nil {
				return err
			}
			value = string(valueBytes)
		}

		// 检查成员是否存在
		isMember = bucket.Get([]byte(value)) != nil
		return nil
	})

	return isMember, err
}

// ================== 带 Context 的基本操作 ==================

// GetCtx 获取缓存（带上下文）
func (f *FileCache) GetCtx(ctx context.Context, key string) (string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.Get(key)
}

// SetCtx 设置缓存（带上下文）
func (f *FileCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Set(key, value, expiration)
}

// DelCtx 删除缓存（带上下文）
func (f *FileCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.Del(keys...)
}

// ExistsCtx 检查键是否存在（带上下文）
func (f *FileCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.Exists(keys...)
}

// ExpireCtx 设置过期时间（带上下文）
func (f *FileCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Expire(key, expiration)
}

// TTLCtx 获取剩余生存时间（带上下文）
func (f *FileCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.TTL(key)
}

// ================== 带 Context 的计数器操作 ==================

// IncrCtx 自增（带上下文）
func (f *FileCache) IncrCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.Incr(key)
}

// DecrCtx 自减（带上下文）
func (f *FileCache) DecrCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.Decr(key)
}

// IncrByCtx 按指定值自增（带上下文）
func (f *FileCache) IncrByCtx(ctx context.Context, key string, value int64) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.IncrBy(key, value)
}

// ================== 带 Context 的哈希表操作 ==================

// HGetCtx 获取哈希表中的字段值（带上下文）
func (f *FileCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.HGet(key, field)
}

// HSetCtx 设置哈希表中的字段值（带上下文）
func (f *FileCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.HSet(key, values...)
}

// HDelCtx 删除哈希表中的字段（带上下文）
func (f *FileCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.HDel(key, fields...)
}

// HGetAllCtx 获取哈希表中的所有字段和值（带上下文）
func (f *FileCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.HGetAll(key)
}

// HExistsCtx 检查哈希表中是否存在指定字段（带上下文）
func (f *FileCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return f.HExists(key, field)
}

// HLenCtx 获取哈希表中的字段数量（带上下文）
func (f *FileCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.HLen(key)
}

// ================== 带 Context 的列表操作 ==================

// LPushCtx 将一个或多个值插入到列表头部（带上下文）
func (f *FileCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.LPush(key, values...)
}

// RPushCtx 将一个或多个值插入到列表尾部（带上下文）
func (f *FileCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.RPush(key, values...)
}

// LPopCtx 移除并返回列表头部元素（带上下文）
func (f *FileCache) LPopCtx(ctx context.Context, key string) (string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.LPop(key)
}

// RPopCtx 移除并返回列表尾部元素（带上下文）
func (f *FileCache) RPopCtx(ctx context.Context, key string) (string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return f.RPop(key)
}

// LLenCtx 获取列表长度（带上下文）
func (f *FileCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.LLen(key)
}

// LRangeCtx 获取列表指定范围内的元素（带上下文）
func (f *FileCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.LRange(key, start, stop)
}

// ================== 带 Context 的集合操作 ==================

// SAddCtx 将一个或多个成员元素加入到集合中（带上下文）
func (f *FileCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.SAdd(key, members...)
}

// SMembersCtx 返回集合中的所有成员（带上下文）
func (f *FileCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.SMembers(key)
}

// SRemCtx 移除集合中的一个或多个成员（带上下文）
func (f *FileCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.SRem(key, members...)
}

// SCardCtx 获取集合的成员数（带上下文）
func (f *FileCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.SCard(key)
}

// SIsMemberCtx 判断成员元素是否是集合的成员（带上下文）
func (f *FileCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return f.SIsMember(key, member)
}

// ================== 键操作 ==================

// Keys 查找所有符合给定模式的键
func (f *FileCache) Keys(pattern string) ([]string, error) {
	var keys []string

	// 如果模式是 "*"，则返回所有键
	if pattern == "*" {
		err := f.db.View(func(tx *bbolt.Tx) error {
			// 遍历所有桶
			buckets := []string{
				defaultBucket,
				hashBucket,
				listBucket,
				setBucket,
				zsetBucket,
			}

			for _, bucketName := range buckets {
				bucket := tx.Bucket([]byte(bucketName))
				if bucket == nil {
					continue
				}

				// 对于默认桶，直接获取所有键
				if bucketName == defaultBucket {
					err := bucket.ForEach(func(k, v []byte) error {
						// 去除前缀
						key := string(k)
						if f.prefix != "" && strings.HasPrefix(key, f.prefix) {
							key = key[len(f.prefix):]
						}
						keys = append(keys, key)
						return nil
					})
					if err != nil {
						return err
					}
				} else {
					// 对于其他桶，获取所有子桶名称
					err := bucket.ForEach(func(k, v []byte) error {
						if v == nil { // 子桶
							// 去除前缀
							key := string(k)
							if f.prefix != "" && strings.HasPrefix(key, f.prefix) {
								key = key[len(f.prefix):]
							}
							keys = append(keys, key)
						}
						return nil
					})
					if err != nil {
						return err
					}
				}
			}

			return nil
		})

		return keys, err
	}

	// 如果模式不是 "*"，则需要进行模式匹配
	// 将 Redis 风格的通配符模式转换为正则表达式
	regexPattern := "^"
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			regexPattern += ".*"
		case '?':
			regexPattern += "."
		case '[', ']', '(', ')', '.', '+', '|', '^', '$', '\\':
			regexPattern += "\\" + string(pattern[i])
		default:
			regexPattern += string(pattern[i])
		}
	}
	regexPattern += "$"

	regex, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil, err
	}

	// 获取所有键并进行模式匹配
	allKeys, err := f.Keys("*")
	if err != nil {
		return nil, err
	}

	// 过滤符合模式的键
	for _, key := range allKeys {
		if regex.MatchString(key) {
			keys = append(keys, key)
		}
	}

	return keys, nil
}

// KeysCtx 查找所有符合给定模式的键（带上下文）
func (f *FileCache) KeysCtx(ctx context.Context, pattern string) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.Keys(pattern)
}

// ================== 有序集合操作 ==================

// 有序集合成员结构
type zsetMember struct {
	Score float64 `json:"score"`
	Value string  `json:"value"`
}

// 获取有序集合的桶
func getZSetBucket(tx *bbolt.Tx, key string) (*bbolt.Bucket, error) {
	// 获取有序集合桶
	zsetBucketObj := tx.Bucket([]byte(zsetBucket))
	if zsetBucketObj == nil {
		return nil, fmt.Errorf("有序集合桶不存在")
	}

	// 创建或获取特定有序集合的子桶
	var bucket *bbolt.Bucket
	var err error

	// 尝试获取现有子桶
	bucket = zsetBucketObj.Bucket([]byte(key))
	if bucket == nil {
		// 如果是只读事务，返回错误
		if tx.Writable() {
			// 创建新子桶
			bucket, err = zsetBucketObj.CreateBucketIfNotExists([]byte(key))
			if err != nil {
				return nil, fmt.Errorf("创建有序集合桶失败: %w", err)
			}
		} else {
			return nil, ErrKeyNotFound
		}
	}

	return bucket, nil
}

// 获取有序集合分数索引的桶
func getZSetScoreBucket(tx *bbolt.Tx, key string) (*bbolt.Bucket, error) {
	// 获取有序集合分数索引桶
	zsetScoreBucketObj := tx.Bucket([]byte(zsetScoreBucket))
	if zsetScoreBucketObj == nil {
		return nil, fmt.Errorf("有序集合分数索引桶不存在")
	}

	// 创建或获取特定有序集合的分数索引子桶
	var bucket *bbolt.Bucket
	var err error

	// 尝试获取现有子桶
	bucket = zsetScoreBucketObj.Bucket([]byte(key))
	if bucket == nil {
		// 如果是只读事务，返回错误
		if tx.Writable() {
			// 创建新子桶
			bucket, err = zsetScoreBucketObj.CreateBucketIfNotExists([]byte(key))
			if err != nil {
				return nil, fmt.Errorf("创建有序集合分数索引桶失败: %w", err)
			}
		} else {
			return nil, ErrKeyNotFound
		}
	}

	return bucket, nil
}

// 将浮点数转换为可排序的字节数组
func float64ToSortableBytes(f float64) []byte {
	bits := math.Float64bits(f)
	bytes := make([]byte, 8)

	// 对于正数，设置最高位为1
	if f >= 0 {
		bits |= (1 << 63)
	} else {
		// 对于负数，翻转所有位
		bits = ^bits
	}

	// 转换为大端字节序
	binary.BigEndian.PutUint64(bytes, bits)
	return bytes
}

// ZAdd 将一个或多个成员元素及其分数值加入到有序集合中
func (f *FileCache) ZAdd(key string, members ...Z) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		// 获取有序集合桶
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取分数索引桶
		scoreBucket, err := getZSetScoreBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 处理成员-分数对
		for _, m := range members {
			score := m.Score

			// 解析成员
			var member string
			switch v := m.Member.(type) {
			case string:
				member = v
			case []byte:
				member = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				member = string(valueBytes)
			}

			// 检查成员是否已存在
			oldData := bucket.Get([]byte(member))
			if oldData != nil {
				// 成员已存在，更新分数
				var oldMember zsetMember
				if err := json.Unmarshal(oldData, &oldMember); err != nil {
					return err
				}

				// 如果分数相同，不做任何操作
				if oldMember.Score == score {
					continue
				}

				// 删除旧的分数索引
				oldScoreKey := float64ToSortableBytes(oldMember.Score)
				if err := scoreBucket.Delete(oldScoreKey); err != nil {
					return err
				}
			} else {
				// 成员不存在，计数加一
				count++
			}

			// 创建新的成员数据
			memberData := zsetMember{
				Score: score,
				Value: member,
			}

			// 序列化成员数据
			data, err := json.Marshal(memberData)
			if err != nil {
				return err
			}

			// 保存成员数据
			if err := bucket.Put([]byte(member), data); err != nil {
				return err
			}

			// 保存分数索引
			scoreKey := float64ToSortableBytes(score)
			if err := scoreBucket.Put(scoreKey, []byte(member)); err != nil {
				return err
			}
		}

		return nil
	})

	return count, err
}

// ZScore 返回有序集合中成员的分数值
func (f *FileCache) ZScore(key string, member string) (float64, error) {
	var score float64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取成员数据
		data := bucket.Get([]byte(member))
		if data == nil {
			return ErrKeyNotFound
		}

		// 解析成员数据
		var memberData zsetMember
		if err := json.Unmarshal(data, &memberData); err != nil {
			return err
		}

		score = memberData.Score
		return nil
	})

	return score, err
}

// ZRem 移除有序集合中的一个或多个成员
func (f *FileCache) ZRem(key string, members ...interface{}) (int64, error) {
	var count int64
	err := f.db.Update(func(tx *bbolt.Tx) error {
		// 获取有序集合桶
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取分数索引桶
		scoreBucket, err := getZSetScoreBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 处理每个成员
		for _, m := range members {
			// 将成员转换为字符串
			var member string
			switch v := m.(type) {
			case string:
				member = v
			case []byte:
				member = string(v)
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(v)
				if err != nil {
					return err
				}
				member = string(valueBytes)
			}

			// 获取成员数据
			data := bucket.Get([]byte(member))
			if data == nil {
				continue
			}

			// 解析成员数据
			var memberData zsetMember
			if err := json.Unmarshal(data, &memberData); err != nil {
				return err
			}

			// 删除分数索引
			scoreKey := float64ToSortableBytes(memberData.Score)
			if err := scoreBucket.Delete(scoreKey); err != nil {
				return err
			}

			// 删除成员
			if err := bucket.Delete([]byte(member)); err != nil {
				return err
			}

			count++
		}

		return nil
	})

	return count, err
}

// ZCard 获取有序集合的成员数
func (f *FileCache) ZCard(key string) (int64, error) {
	var count int64
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 遍历所有成员并计数
		return bucket.ForEach(func(k, v []byte) error {
			count++
			return nil
		})
	})

	return count, err
}

// ZRange 返回有序集合中指定区间内的成员
func (f *FileCache) ZRange(key string, start, stop int64) ([]string, error) {
	var result []string
	err := f.db.View(func(tx *bbolt.Tx) error {
		// 获取有序集合桶
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取所有成员及其分数
		var members []zsetMember
		err = bucket.ForEach(func(k, v []byte) error {
			var member zsetMember
			if err := json.Unmarshal(v, &member); err != nil {
				return err
			}
			members = append(members, member)
			return nil
		})
		if err != nil {
			return err
		}

		// 按分数排序
		sort.Slice(members, func(i, j int) bool {
			if members[i].Score == members[j].Score {
				return members[i].Value < members[j].Value
			}
			return members[i].Score < members[j].Score
		})

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
			if stop < 0 {
				stop = 0
			}
		}

		// 确保索引在有效范围内
		if start >= length {
			return nil
		}

		if stop >= length {
			stop = length - 1
		}

		// 获取范围内的成员
		for i := start; i <= stop; i++ {
			result = append(result, members[i].Value)
		}

		return nil
	})

	return result, err
}

// ZRangeByScore 返回有序集合中指定分数区间内的成员
func (f *FileCache) ZRangeByScore(key string, min, max float64) ([]string, error) {
	var result []string
	err := f.db.View(func(tx *bbolt.Tx) error {
		// 获取有序集合桶
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取所有成员及其分数
		var members []zsetMember
		err = bucket.ForEach(func(k, v []byte) error {
			var member zsetMember
			if err := json.Unmarshal(v, &member); err != nil {
				return err
			}

			// 只添加在分数范围内的成员
			if member.Score >= min && member.Score <= max {
				members = append(members, member)
			}
			return nil
		})
		if err != nil {
			return err
		}

		// 按分数排序
		sort.Slice(members, func(i, j int) bool {
			if members[i].Score == members[j].Score {
				return members[i].Value < members[j].Value
			}
			return members[i].Score < members[j].Score
		})

		// 获取所有成员
		for _, member := range members {
			result = append(result, member.Value)
		}

		return nil
	})

	return result, err
}

// ZRangeWithScores 返回有序集合中指定区间内的成员和分数
func (f *FileCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	var result []Z
	err := f.db.View(func(tx *bbolt.Tx) error {
		// 获取有序集合桶
		bucket, err := getZSetBucket(tx, f.buildKey(key))
		if err != nil {
			return err
		}

		// 获取所有成员及其分数
		var members []zsetMember
		err = bucket.ForEach(func(k, v []byte) error {
			var member zsetMember
			if err := json.Unmarshal(v, &member); err != nil {
				return err
			}
			members = append(members, member)
			return nil
		})
		if err != nil {
			return err
		}

		// 按分数排序
		sort.Slice(members, func(i, j int) bool {
			if members[i].Score == members[j].Score {
				return members[i].Value < members[j].Value
			}
			return members[i].Score < members[j].Score
		})

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
			if stop < 0 {
				stop = 0
			}
		}

		// 确保索引在有效范围内
		if start >= length {
			return nil
		}

		if stop >= length {
			stop = length - 1
		}

		// 获取范围内的成员和分数
		for i := start; i <= stop; i++ {
			result = append(result, Z{
				Score:  members[i].Score,
				Member: members[i].Value,
			})
		}

		return nil
	})

	return result, err
}

// ================== 带 Context 的有序集合操作 ==================

// ZAddCtx 将一个或多个成员元素及其分数值加入到有序集合中（带上下文）
func (f *FileCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.ZAdd(key, members...)
}

// ZScoreCtx 返回有序集合中成员的分数值（带上下文）
func (f *FileCache) ZScoreCtx(ctx context.Context, key string, member string) (float64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.ZScore(key, member)
}

// ZRemCtx 移除有序集合中的一个或多个成员（带上下文）
func (f *FileCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.ZRem(key, members...)
}

// ZCardCtx 获取有序集合的成员数（带上下文）
func (f *FileCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.ZCard(key)
}

// ZRangeCtx 返回有序集合中指定区间内的成员（带上下文）
func (f *FileCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.ZRange(key, start, stop)
}

// ZRangeByScoreCtx 返回有序集合中指定分数区间内的成员（带上下文）
func (f *FileCache) ZRangeByScoreCtx(ctx context.Context, key string, min, max float64) ([]string, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.ZRangeByScore(key, min, max)
}

// ZRangeWithScoresCtx 返回有序集合中指定区间内的成员和分数（带上下文）
func (f *FileCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.ZRangeWithScores(key, start, stop)
}

// ================== 连接检查 ==================

// Ping 检查缓存连接是否正常
func (f *FileCache) Ping() error {
	// 对于文件缓存，只需检查数据库是否可以访问
	return f.db.View(func(tx *bbolt.Tx) error {
		// 尝试访问一个桶，如果可以访问则连接正常
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("默认桶不存在")
		}
		return nil
	})
}

// PingCtx 检查缓存连接是否正常（带上下文）
func (f *FileCache) PingCtx(ctx context.Context) error {
	// 检查上下文是否已取消
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.Ping()
}

// BatchSet 批量设置缓存，减少事务开销
func (f *FileCache) BatchSet(items map[string]interface{}, expiration time.Duration) error {
	if len(items) == 0 {
		return nil
	}

	var exp int64
	if expiration > 0 {
		exp = time.Now().Add(expiration).Unix()
	}

	// 更新文件缓存
	err := f.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		for key, value := range items {
			prefixedKey := f.buildKey(key)

			if expiration > 0 {
				// 添加到过期时间索引
				if err := addKeyExpiration(tx, prefixedKey, exp); err != nil {
					return err
				}
			}

			item := cacheItem{
				Value:      value,
				Expiration: exp,
			}

			data, err := json.Marshal(item)
			if err != nil {
				return err
			}

			// 压缩大型值
			compressedData, err := compressValue(data)
			if err != nil {
				return err
			}

			if err := bucket.Put([]byte(prefixedKey), compressedData); err != nil {
				return err
			}

			// 同时更新内存缓存
			f.memCache.Store(prefixedKey, item)
		}

		return nil
	})

	return err
}

// BatchGet 批量获取缓存，减少事务开销
func (f *FileCache) BatchGet(keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return make(map[string]string), nil
	}

	result := make(map[string]string)
	missingKeys := make([]string, 0)
	prefixedKeys := make(map[string]string) // 原始key -> 带前缀key

	// 先从内存缓存中查找
	for _, key := range keys {
		prefixedKey := f.buildKey(key)
		prefixedKeys[key] = prefixedKey

		if val, ok := f.memCache.Load(prefixedKey); ok {
			item, ok := val.(cacheItem)
			if !ok {
				continue
			}

			// 检查是否过期
			if item.Expiration > 0 && item.Expiration <= time.Now().Unix() {
				f.memCache.Delete(prefixedKey)
				missingKeys = append(missingKeys, key)
				continue
			}

			// 转换为字符串
			switch v := item.Value.(type) {
			case string:
				result[key] = v
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(item.Value)
				if err != nil {
					continue
				}
				result[key] = string(valueBytes)
			}
		} else {
			missingKeys = append(missingKeys, key)
		}
	}

	// 如果所有键都在内存缓存中找到，直接返回
	if len(missingKeys) == 0 {
		return result, nil
	}

	// 从文件缓存中查找缺失的键
	err := f.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(defaultBucket))
		if bucket == nil {
			return fmt.Errorf("桶不存在")
		}

		now := time.Now().Unix()

		for _, key := range missingKeys {
			prefixedKey := prefixedKeys[key]
			data := bucket.Get([]byte(prefixedKey))
			if data == nil {
				continue
			}

			// 解压缩数据
			decompressedData, err := decompressValue(data)
			if err != nil {
				continue
			}

			var item cacheItem
			if err := json.Unmarshal(decompressedData, &item); err != nil {
				continue
			}

			// 检查是否过期
			if item.Expiration > 0 && item.Expiration <= now {
				continue
			}

			// 转换为字符串
			switch v := item.Value.(type) {
			case string:
				result[key] = v
			default:
				// 尝试将其他类型转换为字符串
				valueBytes, err := json.Marshal(item.Value)
				if err != nil {
					continue
				}
				result[key] = string(valueBytes)
			}

			// 将结果放入内存缓存
			f.memCache.Store(prefixedKey, item)
		}

		return nil
	})

	return result, err
}

// CompactDB 压缩数据库文件，回收空间
func (f *FileCache) CompactDB() error {
	// 获取数据库路径
	dbPath := ""
	err := f.db.View(func(tx *bbolt.Tx) error {
		dbPath = tx.DB().Path()
		return nil
	})
	if err != nil {
		return err
	}

	// 创建临时文件
	tempFile := dbPath + ".compact"

	// 创建目标数据库
	dstDB, err := bbolt.Open(tempFile, 0600, &bbolt.Options{
		Timeout: 3 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("无法创建临时数据库: %w", err)
	}
	defer dstDB.Close()

	// 复制所有数据
	err = bbolt.Compact(dstDB, f.db, 0)
	if err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("压缩数据库失败: %w", err)
	}

	// 关闭源数据库
	f.db.Close()

	// 替换文件
	err = os.Rename(tempFile, dbPath)
	if err != nil {
		return fmt.Errorf("替换数据库文件失败: %w", err)
	}

	// 重新打开数据库
	f.db, err = bbolt.Open(dbPath, 0600, &bbolt.Options{
		Timeout:        3 * time.Second,
		PageSize:       16 * 1024,
		FreelistType:   bbolt.FreelistMapType,
		NoFreelistSync: true,
	})

	return err
}

// 启动定期压缩任务
func (f *FileCache) startCompactTask() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour) // 每天压缩一次
		defer ticker.Stop()

		for range ticker.C {
			err := f.CompactDB()
			if err != nil {
				f.logger.Errorf("压缩数据库失败: %v", err)
			} else {
				f.logger.Info("数据库压缩成功")
			}
		}
	}()
}
