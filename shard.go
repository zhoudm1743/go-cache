package cache

import (
	"hash/fnv"
	"sync"
)

const (
	// 默认分段数量，应该是2的幂，便于位运算
	DefaultShardCount = 32
)

// ShardedMap 分段映射，每个分段有独立的互斥锁
type ShardedMap struct {
	shards    []*mapShard // 分段
	shardMask uint32      // 分段掩码，用于快速计算分段索引
	count     int         // 分段数量
}

// mapShard 分段，包含数据和独立的锁
type mapShard struct {
	items map[string]interface{} // 存储键值对
	mu    sync.RWMutex           // 每个分段的读写锁
}

// NewShardedMap 创建一个新的分段映射
func NewShardedMap(shardCount int) *ShardedMap {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}

	// 确保shardCount是2的幂
	if shardCount&(shardCount-1) != 0 {
		// 向上取最近的2的幂
		shardCount--
		shardCount |= shardCount >> 1
		shardCount |= shardCount >> 2
		shardCount |= shardCount >> 4
		shardCount |= shardCount >> 8
		shardCount |= shardCount >> 16
		shardCount++
	}

	sm := &ShardedMap{
		shards:    make([]*mapShard, shardCount),
		shardMask: uint32(shardCount - 1),
		count:     shardCount,
	}

	// 初始化每个分段
	for i := 0; i < shardCount; i++ {
		sm.shards[i] = &mapShard{
			items: make(map[string]interface{}),
		}
	}

	return sm
}

// getShard 根据键获取对应的分段
func (sm *ShardedMap) getShard(key string) *mapShard {
	hasher := fnv.New32a()
	hasher.Write([]byte(key))
	hashValue := hasher.Sum32()
	index := hashValue & sm.shardMask // 快速计算分段索引
	return sm.shards[index]
}

// Set 设置键值对
func (sm *ShardedMap) Set(key string, value interface{}) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.items[key] = value
}

// Get 获取值
func (sm *ShardedMap) Get(key string) (interface{}, bool) {
	shard := sm.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	val, ok := shard.items[key]
	return val, ok
}

// Delete 删除键值对
func (sm *ShardedMap) Delete(key string) {
	shard := sm.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	delete(shard.items, key)
}

// Exists 检查键是否存在
func (sm *ShardedMap) Exists(key string) bool {
	shard := sm.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	_, ok := shard.items[key]
	return ok
}

// ForEach 遍历所有键值对，注意这个操作会锁定所有分段
// handler函数返回false可以提前终止遍历
func (sm *ShardedMap) ForEach(handler func(key string, value interface{}) bool) {
	// 依次锁定每个分段并遍历
	for _, shard := range sm.shards {
		shard.mu.RLock()
		for k, v := range shard.items {
			shard.mu.RUnlock() // 暂时释放锁，减少锁定时间
			if !handler(k, v) {
				return // 如果handler返回false，终止遍历
			}
			shard.mu.RLock() // 重新获取锁
		}
		shard.mu.RUnlock()
	}
}

// Keys 获取所有键
func (sm *ShardedMap) Keys() []string {
	var keys []string

	// 遍历所有分段收集键
	for _, shard := range sm.shards {
		shard.mu.RLock()
		for key := range shard.items {
			keys = append(keys, key)
		}
		shard.mu.RUnlock()
	}

	return keys
}

// Clear 清空所有数据
func (sm *ShardedMap) Clear() {
	for _, shard := range sm.shards {
		shard.mu.Lock()
		shard.items = make(map[string]interface{})
		shard.mu.Unlock()
	}
}

// ShardedMapWithTTL 带TTL的分段映射
type ShardedMapWithTTL struct {
	*ShardedMap             // 嵌入基本的分段映射
	ttlMap      *ShardedMap // 存储键的过期时间
}

// NewShardedMapWithTTL 创建一个带TTL的分段映射
func NewShardedMapWithTTL(shardCount int) *ShardedMapWithTTL {
	return &ShardedMapWithTTL{
		ShardedMap: NewShardedMap(shardCount),
		ttlMap:     NewShardedMap(shardCount),
	}
}

// SetWithTTL 设置带过期时间的键值对
func (sm *ShardedMapWithTTL) SetWithTTL(key string, value interface{}, expiration int64) {
	sm.Set(key, value)
	if expiration > 0 {
		sm.ttlMap.Set(key, expiration)
	} else {
		// 如果expiration为0，表示永不过期，删除ttlMap中的条目
		sm.ttlMap.Delete(key)
	}
}

// GetTTL 获取键的过期时间
func (sm *ShardedMapWithTTL) GetTTL(key string) (int64, bool) {
	val, ok := sm.ttlMap.Get(key)
	if !ok {
		return 0, false
	}
	return val.(int64), true
}

// Size 返回所有分段中键值对的总数（近似值）
func (sm *ShardedMap) Size() int {
	count := 0
	for _, shard := range sm.shards {
		shard.mu.RLock()
		count += len(shard.items)
		shard.mu.RUnlock()
	}
	return count
}

// ForEachShard 分段执行函数，每个分段独立加锁
func (sm *ShardedMap) ForEachShard(fn func(shard *mapShard)) {
	for _, shard := range sm.shards {
		shard.mu.Lock()
		fn(shard)
		shard.mu.Unlock()
	}
}

// ForEachShardRead 分段执行只读函数，每个分段独立加读锁
func (sm *ShardedMap) ForEachShardRead(fn func(shard *mapShard)) {
	for _, shard := range sm.shards {
		shard.mu.RLock()
		fn(shard)
		shard.mu.RUnlock()
	}
}
