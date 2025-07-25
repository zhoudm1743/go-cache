package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DistributedCache 基于一致性哈希的分布式缓存实现
type DistributedCache struct {
	ring         *ConsistentHash  // 一致性哈希环
	nodes        map[string]Cache // 节点名到缓存实例的映射
	logger       Logger           // 日志接口
	mutex        sync.RWMutex     // 保护并发访问
	replicaCount int              // 每个键的副本数量
}

// DistributedCacheConfig 分布式缓存配置
type DistributedCacheConfig struct {
	VirtualNodeMultiplier int    // 虚拟节点倍数
	ReplicaCount          int    // 每个键的副本数量
	DefaultPrefix         string // 默认键前缀
}

// NewDistributedCache 创建分布式缓存实例
func NewDistributedCache(config *DistributedCacheConfig, logger Logger) *DistributedCache {
	if logger == nil {
		logger = defaultLogger
	}

	// 默认配置
	if config == nil {
		config = &DistributedCacheConfig{
			VirtualNodeMultiplier: defaultVirtualNodeMultiplier,
			ReplicaCount:          1,
			DefaultPrefix:         "",
		}
	}

	// 创建一致性哈希环
	ring := NewConsistentHash()
	ring.SetVirtualNodeMultiplier(config.VirtualNodeMultiplier)

	// 确保至少有1个副本
	replicaCount := config.ReplicaCount
	if replicaCount < 1 {
		replicaCount = 1
	}

	return &DistributedCache{
		ring:         ring,
		nodes:        make(map[string]Cache),
		logger:       logger,
		replicaCount: replicaCount,
	}
}

// AddNode 添加缓存节点
func (dc *DistributedCache) AddNode(nodeName string, cache Cache) error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	// 检查节点是否已存在
	if _, exists := dc.nodes[nodeName]; exists {
		return fmt.Errorf("节点 %s 已存在", nodeName)
	}

	// 添加到节点映射
	dc.nodes[nodeName] = cache

	// 添加到一致性哈希环
	dc.ring.AddNode(nodeName)

	dc.logger.WithFields(map[string]interface{}{
		"node": nodeName,
	}).Info("添加缓存节点")

	return nil
}

// RemoveNode 移除缓存节点
func (dc *DistributedCache) RemoveNode(nodeName string) error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	// 检查节点是否存在
	if _, exists := dc.nodes[nodeName]; !exists {
		return fmt.Errorf("节点 %s 不存在", nodeName)
	}

	// 关闭缓存连接
	if err := dc.nodes[nodeName].Close(); err != nil {
		dc.logger.WithFields(map[string]interface{}{
			"node": nodeName,
			"err":  err.Error(),
		}).Error("关闭节点连接失败")
	}

	// 从节点映射中移除
	delete(dc.nodes, nodeName)

	// 从一致性哈希环中移除
	dc.ring.RemoveNode(nodeName)

	dc.logger.WithFields(map[string]interface{}{
		"node": nodeName,
	}).Info("移除缓存节点")

	return nil
}

// getNodesForKey 获取一个键应该存储在哪些节点上
func (dc *DistributedCache) getNodesForKey(key string) []Cache {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		return nil
	}

	// 获取应该存储该键的节点名
	nodeNames := dc.ring.GetNodesForKey(key, dc.replicaCount)
	if len(nodeNames) == 0 {
		return nil
	}

	// 转换为缓存实例
	result := make([]Cache, 0, len(nodeNames))
	for _, name := range nodeNames {
		if cache, exists := dc.nodes[name]; exists {
			result = append(result, cache)
		}
	}

	return result
}

// getPrimaryNodeForKey 获取一个键的主节点
func (dc *DistributedCache) getPrimaryNodeForKey(key string) (Cache, error) {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		return nil, errors.New("没有可用的缓存节点")
	}

	// 获取应该存储该键的主节点名
	nodeName, exists := dc.ring.GetNode(key)
	if !exists {
		return nil, errors.New("无法确定键的节点")
	}

	// 获取缓存实例
	cache, exists := dc.nodes[nodeName]
	if !exists {
		return nil, fmt.Errorf("节点 %s 不存在", nodeName)
	}

	return cache, nil
}

// Set 设置键值对
func (dc *DistributedCache) Set(key string, value interface{}, expiration time.Duration) error {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	// 在所有节点上设置值（支持副本）
	var lastErr error
	for _, node := range nodes {
		if err := node.Set(key, value, expiration); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置缓存失败")
			lastErr = err
		}
	}

	return lastErr
}

// Get 获取键值
func (dc *DistributedCache) Get(key string) (string, error) {
	// 尝试从主节点获取
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return "", err
	}

	// 从主节点获取值
	value, err := primaryNode.Get(key)
	if err == nil {
		return value, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			// 跳过主节点
			if node == primaryNode {
				continue
			}

			value, nodeErr := node.Get(key)
			if nodeErr == nil {
				return value, nil
			}
		}
	}

	return "", err
}

// Del 删除键
func (dc *DistributedCache) Del(keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	// 按键分组，确定每个键的节点
	keysByNode := make(map[Cache][]string)

	for _, key := range keys {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			keysByNode[node] = append(keysByNode[node], key)
		}
	}

	// 在每个节点上删除相应的键
	var totalDeleted int64
	var lastErr error

	for node, nodeKeys := range keysByNode {
		deleted, err := node.Del(nodeKeys...)
		totalDeleted += deleted

		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"keys": nodeKeys,
				"err":  err.Error(),
			}).Error("删除缓存失败")
			lastErr = err
		}
	}

	return totalDeleted, lastErr
}

// Exists 检查键是否存在
func (dc *DistributedCache) Exists(keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	var totalCount int64
	var lastErr error

	// 对每个键分别检查
	for _, key := range keys {
		primaryNode, err := dc.getPrimaryNodeForKey(key)
		if err != nil {
			lastErr = err
			continue
		}

		// 在主节点上检查
		count, err := primaryNode.Exists(key)
		totalCount += count

		if err != nil {
			lastErr = err
		}
	}

	return totalCount, lastErr
}

// Expire 设置过期时间
func (dc *DistributedCache) Expire(key string, expiration time.Duration) error {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	// 在所有节点上设置过期时间
	var lastErr error
	for _, node := range nodes {
		if err := node.Expire(key, expiration); err != nil && err != ErrKeyNotFound {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置过期时间失败")
			lastErr = err
		}
	}

	return lastErr
}

// TTL 获取过期时间
func (dc *DistributedCache) TTL(key string) (time.Duration, error) {
	// 从主节点获取TTL
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	return primaryNode.TTL(key)
}

// ================== 带Context的方法实现 ==================

// SetCtx 设置键值对
func (dc *DistributedCache) SetCtx(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	// 在所有节点上设置值（支持副本）
	var lastErr error
	for _, node := range nodes {
		if err := node.SetCtx(ctx, key, value, expiration); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置缓存失败")
			lastErr = err
		}
	}

	return lastErr
}

// GetCtx 获取键值
func (dc *DistributedCache) GetCtx(ctx context.Context, key string) (string, error) {
	// 尝试从主节点获取
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return "", err
	}

	// 从主节点获取值
	value, err := primaryNode.GetCtx(ctx, key)
	if err == nil {
		return value, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			// 跳过主节点
			if node == primaryNode {
				continue
			}

			value, nodeErr := node.GetCtx(ctx, key)
			if nodeErr == nil {
				return value, nil
			}
		}
	}

	return "", err
}

// DelCtx 删除键
func (dc *DistributedCache) DelCtx(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	// 按键分组，确定每个键的节点
	keysByNode := make(map[Cache][]string)

	for _, key := range keys {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			keysByNode[node] = append(keysByNode[node], key)
		}
	}

	// 在每个节点上删除相应的键
	var totalDeleted int64
	var lastErr error

	for node, nodeKeys := range keysByNode {
		deleted, err := node.DelCtx(ctx, nodeKeys...)
		totalDeleted += deleted

		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"keys": nodeKeys,
				"err":  err.Error(),
			}).Error("删除缓存失败")
			lastErr = err
		}
	}

	return totalDeleted, lastErr
}

// ExistsCtx 检查键是否存在
func (dc *DistributedCache) ExistsCtx(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	var totalCount int64
	var lastErr error

	// 对每个键分别检查
	for _, key := range keys {
		primaryNode, err := dc.getPrimaryNodeForKey(key)
		if err != nil {
			lastErr = err
			continue
		}

		// 在主节点上检查
		count, err := primaryNode.ExistsCtx(ctx, key)
		totalCount += count

		if err != nil {
			lastErr = err
		}
	}

	return totalCount, lastErr
}

// ExpireCtx 设置过期时间
func (dc *DistributedCache) ExpireCtx(ctx context.Context, key string, expiration time.Duration) error {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	// 在所有节点上设置过期时间
	var lastErr error
	for _, node := range nodes {
		if err := node.ExpireCtx(ctx, key, expiration); err != nil && err != ErrKeyNotFound {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置过期时间失败")
			lastErr = err
		}
	}

	return lastErr
}

// TTLCtx 获取过期时间
func (dc *DistributedCache) TTLCtx(ctx context.Context, key string) (time.Duration, error) {
	// 从主节点获取TTL
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	return primaryNode.TTLCtx(ctx, key)
}

// 实现其他必要方法，许多方法可能不适用于分布式环境，这里只提供部分常用方法

// GetClient 获取底层客户端，对分布式缓存无意义
func (dc *DistributedCache) GetClient() interface{} {
	return dc.nodes
}

// Close 关闭所有连接
func (dc *DistributedCache) Close() error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	var lastErr error
	for name, node := range dc.nodes {
		if err := node.Close(); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"node": name,
				"err":  err.Error(),
			}).Error("关闭节点连接失败")
			lastErr = err
		}
	}

	// 清空节点和哈希环
	dc.nodes = make(map[string]Cache)
	dc.ring.Clear()

	return lastErr
}

// Ping 测试所有节点连接
func (dc *DistributedCache) Ping() error {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	var lastErr error
	for name, node := range dc.nodes {
		if err := node.Ping(); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"node": name,
				"err":  err.Error(),
			}).Error("节点连接测试失败")
			lastErr = err
		}
	}

	return lastErr
}

// PingCtx 测试所有节点连接
func (dc *DistributedCache) PingCtx(ctx context.Context) error {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	var lastErr error
	for name, node := range dc.nodes {
		if err := node.PingCtx(ctx); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"node": name,
				"err":  err.Error(),
			}).Error("节点连接测试失败")
			lastErr = err
		}
	}

	return lastErr
}

// Warmup 预热所有节点的缓存
func (dc *DistributedCache) Warmup(ctx context.Context, loader DataLoader, generator KeyGenerator) error {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	if len(dc.nodes) == 0 {
		return errors.New("没有可用的缓存节点")
	}

	// 获取所有需要预热的键
	keys, err := generator.GenerateKeys(ctx)
	if err != nil {
		return err
	}

	// 按节点分组键
	keysByNode := make(map[Cache][]string)
	for _, key := range keys {
		// 获取主节点
		primaryNode, err := dc.getPrimaryNodeForKey(key)
		if err != nil {
			continue
		}
		keysByNode[primaryNode] = append(keysByNode[primaryNode], key)
	}

	// 在每个节点上预热相应的键
	var lastErr error
	for node, nodeKeys := range keysByNode {
		if err := node.WarmupKeys(ctx, nodeKeys, loader); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"keysCount": len(nodeKeys),
				"err":       err.Error(),
			}).Error("预热缓存失败")
			lastErr = err
		}
	}

	return lastErr
}

// WarmupKeys 预热指定键的缓存
func (dc *DistributedCache) WarmupKeys(ctx context.Context, keys []string, loader DataLoader) error {
	// 按节点分组键
	keysByNode := make(map[Cache][]string)
	for _, key := range keys {
		// 获取应该存储该键的所有节点
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			keysByNode[node] = append(keysByNode[node], key)
		}
	}

	// 在每个节点上预热相应的键
	var lastErr error
	for node, nodeKeys := range keysByNode {
		if err := node.WarmupKeys(ctx, nodeKeys, loader); err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"keysCount": len(nodeKeys),
				"err":       err.Error(),
			}).Error("预热缓存失败")
			lastErr = err
		}
	}

	return lastErr
}

// GetNodeCount 获取节点数量
func (dc *DistributedCache) GetNodeCount() int {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()
	return len(dc.nodes)
}

// GetNodeNames 获取所有节点名
func (dc *DistributedCache) GetNodeNames() []string {
	dc.mutex.RLock()
	defer dc.mutex.RUnlock()

	names := make([]string, 0, len(dc.nodes))
	for name := range dc.nodes {
		names = append(names, name)
	}

	return names
}

// 注意：为简化示例，分布式缓存不实现其他复杂操作如哈希表、列表、集合等
// 在实际使用中，可以根据需要添加这些方法的实现
