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
		return nil, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 获取应该存储该键的主节点名
	nodeName, exists := dc.ring.GetNode(key)
	if !exists {
		return nil, ErrorWithContext(ErrServerInternal, "无法确定键的节点")
	}

	// 获取缓存实例
	cache, exists := dc.nodes[nodeName]
	if !exists {
		return nil, ErrorWithContext(ErrServerInternal, fmt.Sprintf("节点 %s 不存在", nodeName))
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

// ================== 哈希表操作 ==================

// HGet 获取哈希表中的字段值
func (dc *DistributedCache) HGet(key, field string) (string, error) {
	return dc.HGetCtx(context.Background(), key, field)
}

// HGetCtx 获取哈希表中的字段值（带上下文）
func (dc *DistributedCache) HGetCtx(ctx context.Context, key, field string) (string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return "", err
	}

	// 从主节点获取值
	value, err := primaryNode.HGetCtx(ctx, key, field)
	if err == nil {
		return value, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound && err != ErrFieldNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			// 跳过主节点
			if node == primaryNode {
				continue
			}

			value, nodeErr := node.HGetCtx(ctx, key, field)
			if nodeErr == nil {
				return value, nil
			}
		}
	}

	return "", err
}

// HSet 设置哈希表中的字段值
func (dc *DistributedCache) HSet(key string, values ...interface{}) (int64, error) {
	return dc.HSetCtx(context.Background(), key, values...)
}

// HSetCtx 设置哈希表中的字段值（带上下文）
func (dc *DistributedCache) HSetCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上设置值
	var lastResult int64
	var lastErr error
	for _, node := range nodes {
		result, err := node.HSetCtx(ctx, key, values...)
		lastResult = result // 记录最后一个结果
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("设置哈希表字段失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return lastResult, nil
}

// HDel 删除哈希表中的字段
func (dc *DistributedCache) HDel(key string, fields ...string) (int64, error) {
	return dc.HDelCtx(context.Background(), key, fields...)
}

// HDelCtx 删除哈希表中的字段（带上下文）
func (dc *DistributedCache) HDelCtx(ctx context.Context, key string, fields ...string) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上删除字段
	var totalDeleted int64
	var lastErr error
	for _, node := range nodes {
		deleted, err := node.HDelCtx(ctx, key, fields...)
		if deleted > totalDeleted {
			totalDeleted = deleted // 记录最大删除数
		}
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key":    key,
				"fields": fields,
				"err":    err.Error(),
			}).Error("删除哈希表字段失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return totalDeleted, lastErr
	}
	return totalDeleted, nil
}

// HGetAll 获取哈希表中的所有字段和值
func (dc *DistributedCache) HGetAll(key string) (map[string]string, error) {
	return dc.HGetAllCtx(context.Background(), key)
}

// HGetAllCtx 获取哈希表中的所有字段和值（带上下文）
func (dc *DistributedCache) HGetAllCtx(ctx context.Context, key string) (map[string]string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return nil, err
	}

	// 从主节点获取值
	values, err := primaryNode.HGetAllCtx(ctx, key)
	if err == nil {
		return values, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			// 跳过主节点
			if node == primaryNode {
				continue
			}

			values, nodeErr := node.HGetAllCtx(ctx, key)
			if nodeErr == nil {
				return values, nil
			}
		}
	}

	return nil, err
}

// HExists 检查哈希表中是否存在指定字段
func (dc *DistributedCache) HExists(key, field string) (bool, error) {
	return dc.HExistsCtx(context.Background(), key, field)
}

// HExistsCtx 检查哈希表中是否存在指定字段（带上下文）
func (dc *DistributedCache) HExistsCtx(ctx context.Context, key, field string) (bool, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return false, err
	}

	// 从主节点获取值
	exists, err := primaryNode.HExistsCtx(ctx, key, field)
	if err == nil {
		return exists, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			// 跳过主节点
			if node == primaryNode {
				continue
			}

			exists, nodeErr := node.HExistsCtx(ctx, key, field)
			if nodeErr == nil {
				return exists, nil
			}
		}
	}

	return false, err
}

// HLen 获取哈希表中字段的数量
func (dc *DistributedCache) HLen(key string) (int64, error) {
	return dc.HLenCtx(context.Background(), key)
}

// HLenCtx 获取哈希表中字段的数量（带上下文）
func (dc *DistributedCache) HLenCtx(ctx context.Context, key string) (int64, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	// 从主节点获取值
	length, err := primaryNode.HLenCtx(ctx, key)
	if err == nil {
		return length, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			length, nodeErr := node.HLenCtx(ctx, key)
			if nodeErr == nil {
				return length, nil
			}
		}
	}

	return 0, err
}

// ================== 列表操作 ==================

// LPush 将一个或多个值推入列表的左端（头部）
func (dc *DistributedCache) LPush(key string, values ...interface{}) (int64, error) {
	return dc.LPushCtx(context.Background(), key, values...)
}

// LPushCtx 将一个或多个值推入列表的左端（带上下文）
func (dc *DistributedCache) LPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上执行操作
	var lastResult int64
	var lastErr error
	for _, node := range nodes {
		result, err := node.LPushCtx(ctx, key, values...)
		lastResult = result
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("LPush操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return lastResult, nil
}

// RPush 将一个或多个值推入列表的右端（尾部）
func (dc *DistributedCache) RPush(key string, values ...interface{}) (int64, error) {
	return dc.RPushCtx(context.Background(), key, values...)
}

// RPushCtx 将一个或多个值推入列表的右端（带上下文）
func (dc *DistributedCache) RPushCtx(ctx context.Context, key string, values ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上执行操作
	var lastResult int64
	var lastErr error
	for _, node := range nodes {
		result, err := node.RPushCtx(ctx, key, values...)
		lastResult = result
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("RPush操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return lastResult, nil
}

// LPop 移除并返回列表的第一个元素（头部）
func (dc *DistributedCache) LPop(key string) (string, error) {
	return dc.LPopCtx(context.Background(), key)
}

// LPopCtx 移除并返回列表的第一个元素（带上下文）
func (dc *DistributedCache) LPopCtx(ctx context.Context, key string) (string, error) {
	// 在分布式环境中，弹出操作需要保证一致性
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return "", err
	}

	// 从主节点获取值
	value, err := primaryNode.LPopCtx(ctx, key)
	if err != nil {
		return "", err
	}

	// 如果主节点成功，也需要在其他副本节点执行相同操作以保持一致性
	nodes := dc.getNodesForKey(key)
	for _, node := range nodes {
		if node == primaryNode {
			continue
		}

		// 这里不关心结果，只需要保持一致
		_, nodeErr := node.LPopCtx(ctx, key)
		if nodeErr != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": nodeErr.Error(),
			}).Warn("副本节点LPop操作失败")
		}
	}

	return value, nil
}

// RPop 移除并返回列表的最后一个元素（尾部）
func (dc *DistributedCache) RPop(key string) (string, error) {
	return dc.RPopCtx(context.Background(), key)
}

// RPopCtx 移除并返回列表的最后一个元素（带上下文）
func (dc *DistributedCache) RPopCtx(ctx context.Context, key string) (string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return "", err
	}

	// 从主节点获取值
	value, err := primaryNode.RPopCtx(ctx, key)
	if err != nil {
		return "", err
	}

	// 在其他副本节点执行相同操作以保持一致性
	nodes := dc.getNodesForKey(key)
	for _, node := range nodes {
		if node == primaryNode {
			continue
		}

		_, nodeErr := node.RPopCtx(ctx, key)
		if nodeErr != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": nodeErr.Error(),
			}).Warn("副本节点RPop操作失败")
		}
	}

	return value, nil
}

// LLen 获取列表的长度
func (dc *DistributedCache) LLen(key string) (int64, error) {
	return dc.LLenCtx(context.Background(), key)
}

// LLenCtx 获取列表的长度（带上下文）
func (dc *DistributedCache) LLenCtx(ctx context.Context, key string) (int64, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	// 从主节点获取长度
	length, err := primaryNode.LLenCtx(ctx, key)
	if err == nil {
		return length, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			length, nodeErr := node.LLenCtx(ctx, key)
			if nodeErr == nil {
				return length, nil
			}
		}
	}

	return 0, err
}

// LRange 获取列表指定范围内的元素
func (dc *DistributedCache) LRange(key string, start, stop int64) ([]string, error) {
	return dc.LRangeCtx(context.Background(), key, start, stop)
}

// LRangeCtx 获取列表指定范围内的元素（带上下文）
func (dc *DistributedCache) LRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return nil, err
	}

	// 从主节点获取值
	values, err := primaryNode.LRangeCtx(ctx, key, start, stop)
	if err == nil {
		return values, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			values, nodeErr := node.LRangeCtx(ctx, key, start, stop)
			if nodeErr == nil {
				return values, nil
			}
		}
	}

	return nil, err
}

// ================== 集合操作 ==================

// SAdd 添加一个或多个成员到集合
func (dc *DistributedCache) SAdd(key string, members ...interface{}) (int64, error) {
	return dc.SAddCtx(context.Background(), key, members...)
}

// SAddCtx 添加一个或多个成员到集合（带上下文）
func (dc *DistributedCache) SAddCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上添加成员
	var lastResult int64
	var lastErr error
	for _, node := range nodes {
		result, err := node.SAddCtx(ctx, key, members...)
		lastResult = result
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("SAdd操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return lastResult, nil
}

// SRem 移除集合中一个或多个成员
func (dc *DistributedCache) SRem(key string, members ...interface{}) (int64, error) {
	return dc.SRemCtx(context.Background(), key, members...)
}

// SRemCtx 移除集合中一个或多个成员（带上下文）
func (dc *DistributedCache) SRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上移除成员
	var maxRemoved int64
	var lastErr error
	for _, node := range nodes {
		removed, err := node.SRemCtx(ctx, key, members...)
		if removed > maxRemoved {
			maxRemoved = removed
		}
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("SRem操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return maxRemoved, lastErr
	}
	return maxRemoved, nil
}

// SMembers 获取集合中所有成员
func (dc *DistributedCache) SMembers(key string) ([]string, error) {
	return dc.SMembersCtx(context.Background(), key)
}

// SMembersCtx 获取集合中所有成员（带上下文）
func (dc *DistributedCache) SMembersCtx(ctx context.Context, key string) ([]string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return nil, err
	}

	// 从主节点获取成员
	members, err := primaryNode.SMembersCtx(ctx, key)
	if err == nil {
		return members, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			members, nodeErr := node.SMembersCtx(ctx, key)
			if nodeErr == nil {
				return members, nil
			}
		}
	}

	return nil, err
}

// SIsMember 判断成员是否在集合中
func (dc *DistributedCache) SIsMember(key string, member interface{}) (bool, error) {
	return dc.SIsMemberCtx(context.Background(), key, member)
}

// SIsMemberCtx 判断成员是否在集合中（带上下文）
func (dc *DistributedCache) SIsMemberCtx(ctx context.Context, key string, member interface{}) (bool, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return false, err
	}

	// 从主节点检查成员是否存在
	exists, err := primaryNode.SIsMemberCtx(ctx, key, member)
	if err == nil {
		return exists, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			exists, nodeErr := node.SIsMemberCtx(ctx, key, member)
			if nodeErr == nil {
				return exists, nil
			}
		}
	}

	return false, err
}

// SCard 获取集合的成员数
func (dc *DistributedCache) SCard(key string) (int64, error) {
	return dc.SCardCtx(context.Background(), key)
}

// SCardCtx 获取集合的成员数（带上下文）
func (dc *DistributedCache) SCardCtx(ctx context.Context, key string) (int64, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	// 从主节点获取成员数
	count, err := primaryNode.SCardCtx(ctx, key)
	if err == nil {
		return count, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			count, nodeErr := node.SCardCtx(ctx, key)
			if nodeErr == nil {
				return count, nil
			}
		}
	}

	return 0, err
}

// ================== 有序集合操作 ==================

// ZAdd 添加一个或多个成员到有序集合
func (dc *DistributedCache) ZAdd(key string, members ...Z) (int64, error) {
	return dc.ZAddCtx(context.Background(), key, members...)
}

// ZAddCtx 添加一个或多个成员到有序集合（带上下文）
func (dc *DistributedCache) ZAddCtx(ctx context.Context, key string, members ...Z) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上添加成员
	var lastResult int64
	var lastErr error
	for _, node := range nodes {
		result, err := node.ZAddCtx(ctx, key, members...)
		lastResult = result
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("ZAdd操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return lastResult, nil
}

// ZRem 移除有序集合中一个或多个成员
func (dc *DistributedCache) ZRem(key string, members ...interface{}) (int64, error) {
	return dc.ZRemCtx(context.Background(), key, members...)
}

// ZRemCtx 移除有序集合中一个或多个成员（带上下文）
func (dc *DistributedCache) ZRemCtx(ctx context.Context, key string, members ...interface{}) (int64, error) {
	nodes := dc.getNodesForKey(key)
	if len(nodes) == 0 {
		return 0, ErrorWithContext(ErrServerInternal, "没有可用的缓存节点")
	}

	// 在所有节点上移除成员
	var maxRemoved int64
	var lastErr error
	for _, node := range nodes {
		removed, err := node.ZRemCtx(ctx, key, members...)
		if removed > maxRemoved {
			maxRemoved = removed
		}
		if err != nil {
			dc.logger.WithFields(map[string]interface{}{
				"key": key,
				"err": err.Error(),
			}).Error("ZRem操作失败")
			lastErr = err
		}
	}

	if lastErr != nil {
		return maxRemoved, lastErr
	}
	return maxRemoved, nil
}

// ZRange 获取有序集合中指定范围的成员
func (dc *DistributedCache) ZRange(key string, start, stop int64) ([]string, error) {
	return dc.ZRangeCtx(context.Background(), key, start, stop)
}

// ZRangeCtx 获取有序集合中指定范围的成员（带上下文）
func (dc *DistributedCache) ZRangeCtx(ctx context.Context, key string, start, stop int64) ([]string, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return nil, err
	}

	// 从主节点获取成员
	members, err := primaryNode.ZRangeCtx(ctx, key, start, stop)
	if err == nil {
		return members, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			members, nodeErr := node.ZRangeCtx(ctx, key, start, stop)
			if nodeErr == nil {
				return members, nil
			}
		}
	}

	return nil, err
}

// ZRangeWithScores 获取有序集合中指定范围的成员和分数
func (dc *DistributedCache) ZRangeWithScores(key string, start, stop int64) ([]Z, error) {
	return dc.ZRangeWithScoresCtx(context.Background(), key, start, stop)
}

// ZRangeWithScoresCtx 获取有序集合中指定范围的成员和分数（带上下文）
func (dc *DistributedCache) ZRangeWithScoresCtx(ctx context.Context, key string, start, stop int64) ([]Z, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return nil, err
	}

	// 从主节点获取成员和分数
	membersWithScores, err := primaryNode.ZRangeWithScoresCtx(ctx, key, start, stop)
	if err == nil {
		return membersWithScores, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			membersWithScores, nodeErr := node.ZRangeWithScoresCtx(ctx, key, start, stop)
			if nodeErr == nil {
				return membersWithScores, nil
			}
		}
	}

	return nil, err
}

// ZCard 获取有序集合的成员数
func (dc *DistributedCache) ZCard(key string) (int64, error) {
	return dc.ZCardCtx(context.Background(), key)
}

// ZCardCtx 获取有序集合的成员数（带上下文）
func (dc *DistributedCache) ZCardCtx(ctx context.Context, key string) (int64, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	// 从主节点获取成员数
	count, err := primaryNode.ZCardCtx(ctx, key)
	if err == nil {
		return count, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			count, nodeErr := node.ZCardCtx(ctx, key)
			if nodeErr == nil {
				return count, nil
			}
		}
	}

	return 0, err
}

// ZScore 获取有序集合成员的分数
func (dc *DistributedCache) ZScore(key, member string) (float64, error) {
	return dc.ZScoreCtx(context.Background(), key, member)
}

// ZScoreCtx 获取有序集合成员的分数（带上下文）
func (dc *DistributedCache) ZScoreCtx(ctx context.Context, key, member string) (float64, error) {
	// 获取主节点
	primaryNode, err := dc.getPrimaryNodeForKey(key)
	if err != nil {
		return 0, err
	}

	// 从主节点获取分数
	score, err := primaryNode.ZScoreCtx(ctx, key, member)
	if err == nil {
		return score, nil
	}

	// 如果主节点失败，尝试从其他副本节点获取
	if err != ErrKeyNotFound {
		nodes := dc.getNodesForKey(key)
		for _, node := range nodes {
			if node == primaryNode {
				continue
			}

			score, nodeErr := node.ZScoreCtx(ctx, key, member)
			if nodeErr == nil {
				return score, nil
			}
		}
	}

	return 0, err
}
