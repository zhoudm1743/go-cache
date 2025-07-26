package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache LRU缓存实现
type LRUCache struct {
	// 最大条目数，0表示不限制
	maxEntries int
	// 最大内存使用量(bytes)，0表示不限制
	maxMemory int64
	// 当前使用的内存量估计(bytes)
	usedMemory int64
	// 双向链表用于LRU策略
	ll *list.List
	// 键到链表元素的映射
	cache map[string]*list.Element
	// 清除回调函数，当条目被淘汰时调用
	onEvicted func(key string, value interface{})
	// 互斥锁
	mutex sync.Mutex
}

// lruEntry 是链表中的缓存条目
type lruEntry struct {
	key        string
	value      interface{}
	size       int64     // 该条目占用的内存大小估计
	expiry     time.Time // 过期时间
	accessedAt time.Time // 最后访问时间
}

// NewLRU 创建一个新的LRU缓存
func NewLRU(maxEntries int, maxMemory int64, onEvicted func(string, interface{})) *LRUCache {
	return &LRUCache{
		maxEntries: maxEntries,
		maxMemory:  maxMemory,
		ll:         list.New(),
		cache:      make(map[string]*list.Element),
		onEvicted:  onEvicted,
	}
}

// Add 添加或更新缓存中的条目
func (c *LRUCache) Add(key string, value interface{}, size int64, expiration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 计算过期时间
	var expiry time.Time
	if expiration > 0 {
		expiry = time.Now().Add(expiration)
	}

	// 如果已存在，则更新值并移到前面
	if ee, ok := c.cache[key]; ok {
		entry := ee.Value.(*lruEntry)
		c.ll.MoveToFront(ee)
		oldSize := entry.size
		entry.value = value
		entry.size = size
		entry.expiry = expiry
		entry.accessedAt = time.Now()
		c.usedMemory = c.usedMemory - oldSize + size
		c.checkCapacity()
		return
	}

	// 新增条目
	entry := &lruEntry{key: key, value: value, size: size, expiry: expiry, accessedAt: time.Now()}
	element := c.ll.PushFront(entry)
	c.cache[key] = element
	c.usedMemory += size
	c.checkCapacity()
}

// Get 获取缓存中的条目，如果不存在或已过期则返回false
func (c *LRUCache) Get(key string) (value interface{}, ok bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if ele, hit := c.cache[key]; hit {
		entry := ele.Value.(*lruEntry)

		// 检查是否过期
		if !entry.expiry.IsZero() && time.Now().After(entry.expiry) {
			c.removeElement(ele)
			return nil, false
		}

		// 更新访问时间并移到前面
		entry.accessedAt = time.Now()
		c.ll.MoveToFront(ele)
		return entry.value, true
	}
	return nil, false
}

// Remove 从缓存中移除指定键
func (c *LRUCache) Remove(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if ele, hit := c.cache[key]; hit {
		c.removeElement(ele)
	}
}

// RemoveOldest 移除最老的条目
func (c *LRUCache) RemoveOldest() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.ll.Len() == 0 {
		return
	}

	c.removeElement(c.ll.Back())
}

// removeElement 从缓存中移除元素
func (c *LRUCache) removeElement(e *list.Element) {
	c.ll.Remove(e)
	entry := e.Value.(*lruEntry)
	delete(c.cache, entry.key)
	c.usedMemory -= entry.size
	if c.onEvicted != nil {
		c.onEvicted(entry.key, entry.value)
	}
}

// Len 返回缓存中的条目数量
func (c *LRUCache) Len() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.ll.Len()
}

// Clear 清空缓存
func (c *LRUCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.onEvicted != nil {
		for _, e := range c.cache {
			entry := e.Value.(*lruEntry)
			c.onEvicted(entry.key, entry.value)
		}
	}

	c.ll = list.New()
	c.cache = make(map[string]*list.Element)
	c.usedMemory = 0
}

// Keys 返回缓存中的所有键
func (c *LRUCache) Keys() []string {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	keys := make([]string, 0, len(c.cache))
	for k := range c.cache {
		keys = append(keys, k)
	}
	return keys
}

// GetTTL 获取键的剩余生存时间
func (c *LRUCache) GetTTL(key string) (time.Duration, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if ele, ok := c.cache[key]; ok {
		entry := ele.Value.(*lruEntry)
		// 如果没有过期时间，返回-1
		if entry.expiry.IsZero() {
			return -1 * time.Second, true
		}
		// 如果已过期，删除并返回false
		if !entry.expiry.IsZero() && time.Now().After(entry.expiry) {
			return 0, false
		}
		// 返回剩余时间
		return entry.expiry.Sub(time.Now()), true
	}
	return 0, false
}

// cleanExpired 清理过期条目
func (c *LRUCache) cleanExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()
	for _, ele := range c.cache {
		entry := ele.Value.(*lruEntry)
		if !entry.expiry.IsZero() && now.After(entry.expiry) {
			c.removeElement(ele)
		}
	}
}

// 检查容量并在必要时删除旧条目
func (c *LRUCache) checkCapacity() {
	// 检查条目数量限制
	if c.maxEntries > 0 {
		for c.ll.Len() > c.maxEntries {
			c.removeElement(c.ll.Back())
		}
	}

	// 检查内存使用量限制
	if c.maxMemory > 0 {
		for c.usedMemory > c.maxMemory && c.ll.Len() > 0 {
			c.removeElement(c.ll.Back())
		}
	}
}

// StartJanitor 启动垃圾收集器，定期清理过期条目
func (c *LRUCache) StartJanitor(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			c.cleanExpired()
		}
	}()
}

// estimateSize 估计对象大小(bytes)
func estimateSize(v interface{}) int64 {
	switch val := v.(type) {
	case string:
		return int64(len(val))
	case []byte:
		return int64(len(val))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return 8
	default:
		// 默认估算值
		return 64
	}
}
