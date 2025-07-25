package cache

import (
	"hash/crc32"
	"sort"
	"strconv"
	"sync"
)

// 一致性哈希环的默认节点倍数
const defaultVirtualNodeMultiplier = 100

// ConsistentHash 一致性哈希环实现
type ConsistentHash struct {
	nodes          map[uint32]string        // 哈希值到节点名的映射
	sortedKeys     []uint32                 // 已排序的哈希值切片
	nodeMultiplier int                      // 虚拟节点倍数
	hashFunc       func(data []byte) uint32 // 哈希函数
	mutex          sync.RWMutex             // 保护并发访问
}

// NewConsistentHash 创建一个新的一致性哈希环
func NewConsistentHash() *ConsistentHash {
	return &ConsistentHash{
		nodes:          make(map[uint32]string),
		sortedKeys:     make([]uint32, 0),
		nodeMultiplier: defaultVirtualNodeMultiplier,
		hashFunc:       crc32.ChecksumIEEE,
	}
}

// SetVirtualNodeMultiplier 设置虚拟节点倍数
func (c *ConsistentHash) SetVirtualNodeMultiplier(multiplier int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if multiplier <= 0 {
		multiplier = defaultVirtualNodeMultiplier
	}
	c.nodeMultiplier = multiplier
}

// SetHashFunc 设置自定义哈希函数
func (c *ConsistentHash) SetHashFunc(hashFunc func(data []byte) uint32) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.hashFunc = hashFunc
}

// AddNode 添加一个节点到哈希环
func (c *ConsistentHash) AddNode(nodeName string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 为每个节点创建多个虚拟节点
	for i := 0; i < c.nodeMultiplier; i++ {
		// 以"节点名#序号"作为虚拟节点名
		virtualNodeName := nodeName + "#" + strconv.Itoa(i)
		hashValue := c.hashFunc([]byte(virtualNodeName))
		c.nodes[hashValue] = nodeName // 将虚拟节点映射到实际节点名
		c.sortedKeys = append(c.sortedKeys, hashValue)
	}

	// 对哈希值重新排序
	sort.Slice(c.sortedKeys, func(i, j int) bool {
		return c.sortedKeys[i] < c.sortedKeys[j]
	})
}

// RemoveNode 从哈希环中移除一个节点
func (c *ConsistentHash) RemoveNode(nodeName string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 删除所有虚拟节点
	for i := 0; i < c.nodeMultiplier; i++ {
		virtualNodeName := nodeName + "#" + strconv.Itoa(i)
		hashValue := c.hashFunc([]byte(virtualNodeName))
		delete(c.nodes, hashValue)
	}

	// 重建排序的哈希值数组
	c.sortedKeys = make([]uint32, 0, len(c.nodes))
	for k := range c.nodes {
		c.sortedKeys = append(c.sortedKeys, k)
	}
	sort.Slice(c.sortedKeys, func(i, j int) bool {
		return c.sortedKeys[i] < c.sortedKeys[j]
	})
}

// GetNode 获取一个键应该存储在哪个节点上
func (c *ConsistentHash) GetNode(key string) (string, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if len(c.sortedKeys) == 0 {
		return "", false
	}

	// 计算键的哈希值
	hashValue := c.hashFunc([]byte(key))

	// 在环上顺时针寻找第一个节点
	idx := sort.Search(len(c.sortedKeys), func(i int) bool {
		return c.sortedKeys[i] >= hashValue
	})

	// 如果搜索结果超出范围，则回到环的起始位置
	if idx == len(c.sortedKeys) {
		idx = 0
	}

	// 返回对应的节点名
	nodeName, exists := c.nodes[c.sortedKeys[idx]]
	return nodeName, exists
}

// GetNodes 获取所有节点名的唯一列表
func (c *ConsistentHash) GetNodes() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// 使用map去重
	nodeSet := make(map[string]bool)
	for _, node := range c.nodes {
		nodeSet[node] = true
	}

	// 转换为切片
	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	return nodes
}

// GetNodesForKey 获取一个键应该存储的所有节点（用于备份）
func (c *ConsistentHash) GetNodesForKey(key string, replicaCount int) []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	uniqueNodes := make(map[string]bool)
	result := make([]string, 0, replicaCount)

	if len(c.sortedKeys) == 0 {
		return result
	}

	// 计算键的哈希值
	hashValue := c.hashFunc([]byte(key))

	// 在环上顺时针寻找第一个节点
	idx := sort.Search(len(c.sortedKeys), func(i int) bool {
		return c.sortedKeys[i] >= hashValue
	})
	if idx == len(c.sortedKeys) {
		idx = 0
	}

	// 继续沿环查找指定数量的唯一节点
	for len(uniqueNodes) < replicaCount && len(uniqueNodes) < len(c.GetNodes()) {
		nodeName, exists := c.nodes[c.sortedKeys[idx]]
		if exists && !uniqueNodes[nodeName] {
			uniqueNodes[nodeName] = true
			result = append(result, nodeName)
		}
		idx = (idx + 1) % len(c.sortedKeys)
	}

	return result
}

// Clear 清空哈希环
func (c *ConsistentHash) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.nodes = make(map[uint32]string)
	c.sortedKeys = make([]uint32, 0)
}

// NodeCount 返回环上不同节点的数量
func (c *ConsistentHash) NodeCount() int {
	return len(c.GetNodes())
}
