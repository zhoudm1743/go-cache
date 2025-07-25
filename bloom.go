package cache

import (
	"hash/fnv"
	"math"
	"sync"
)

// BloomFilter 布隆过滤器实现，用于防止缓存穿透
type BloomFilter struct {
	bitSet    []bool       // 位图
	size      uint         // 布隆过滤器大小
	hashCount uint         // 哈希函数个数
	mutex     sync.RWMutex // 并发控制
}

// NewBloomFilter 创建一个新的布隆过滤器
// expectedItems: 预期元素数量
// falsePositiveRate: 可接受的误判率，如0.01表示1%
func NewBloomFilter(expectedItems int, falsePositiveRate float64) *BloomFilter {
	// 计算所需的布隆过滤器大小
	size := optimalBitSize(expectedItems, falsePositiveRate)

	// 计算所需的哈希函数个数
	hashCount := optimalHashCount(size, expectedItems)

	return &BloomFilter{
		bitSet:    make([]bool, size),
		size:      size,
		hashCount: hashCount,
	}
}

// Add 向布隆过滤器中添加一个元素
func (bf *BloomFilter) Add(item string) {
	bf.mutex.Lock()
	defer bf.mutex.Unlock()

	// 对元素应用所有哈希函数并设置相应的位
	for i := uint(0); i < bf.hashCount; i++ {
		position := bf.hash(item, i) % bf.size
		bf.bitSet[position] = true
	}
}

// Contains 检查元素是否可能在布隆过滤器中
// 返回true: 元素可能存在（有误判可能）
// 返回false: 元素肯定不存在
func (bf *BloomFilter) Contains(item string) bool {
	bf.mutex.RLock()
	defer bf.mutex.RUnlock()

	// 对元素应用所有哈希函数并检查相应的位
	for i := uint(0); i < bf.hashCount; i++ {
		position := bf.hash(item, i) % bf.size
		if !bf.bitSet[position] {
			return false // 如果有任何一位未设置，元素肯定不存在
		}
	}

	return true // 所有位都设置，元素可能存在
}

// AddBatch 批量添加元素到布隆过滤器
func (bf *BloomFilter) AddBatch(items []string) {
	bf.mutex.Lock()
	defer bf.mutex.Unlock()

	for _, item := range items {
		for i := uint(0); i < bf.hashCount; i++ {
			position := bf.hash(item, i) % bf.size
			bf.bitSet[position] = true
		}
	}
}

// Reset 重置布隆过滤器
func (bf *BloomFilter) Reset() {
	bf.mutex.Lock()
	defer bf.mutex.Unlock()

	bf.bitSet = make([]bool, bf.size)
}

// 计算布隆过滤器的最优大小
func optimalBitSize(expectedItems int, falsePositiveRate float64) uint {
	size := -float64(expectedItems) * math.Log(falsePositiveRate) / math.Pow(math.Log(2), 2)
	return uint(math.Ceil(size))
}

// 计算布隆过滤器的最优哈希函数个数
func optimalHashCount(size uint, expectedItems int) uint {
	count := float64(size) / float64(expectedItems) * math.Log(2)
	return uint(math.Max(1, math.Round(count)))
}

// hash 哈希函数
// 使用FNV算法的变种实现多个不同的哈希函数
func (bf *BloomFilter) hash(item string, seed uint) uint {
	h := fnv.New64a()
	h.Write([]byte(item))
	h.Write([]byte{byte(seed)})
	return uint(h.Sum64())
}
