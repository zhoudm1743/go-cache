# go-cache 缓存防护功能

本项目增加了完善的缓存防护功能，包括缓存穿透防护和缓存雪崩防护机制。

## 缓存穿透防护

缓存穿透是指查询一个不存在的数据，因为不存在，所以不会被缓存，导致每次都要到后端查询，造成后端数据库压力过大。

### 布隆过滤器

布隆过滤器是一种空间效率很高的概率型数据结构，用于判断一个元素是否可能存在于集合中。

特点：
- 空间效率高，使用位图存储数据
- 可能有误判（认为存在的可能不存在），但不会漏判（认为不存在的一定不存在）
- 适合大数据量场景下的快速判断

使用方法：

```go
// 创建缓存防护配置
protection := cache.NewCacheProtection()

// 启用布隆过滤器防护（10000个元素，0.1%误判率）
protection.EnableBloomFilterProtection(10000, 0.001)

// 创建带防护功能的缓存
protectedCache := cache.NewProtectedCache(memoryCache, protection, nil)

// 使用缓存
value, err := protectedCache.Get(key)
```

### 缓存空值

当查询一个不存在的数据时，将空值结果也缓存起来，但设置较短的过期时间，这样可以有效防止缓存穿透。

使用方法：

```go
// 使用GetWithLoader方法，自动处理空值缓存
value, err := protectedCache.GetWithLoader(ctx, key, func(ctx context.Context) (interface{}, time.Duration, error) {
    // 从数据库加载数据
    result, err := db.Query(key)
    if err != nil {
        // 如果是不存在错误，返回ErrKeyNotFound
        // 系统会自动缓存空值，避免后续重复查询
        return nil, 5*time.Minute, cache.ErrKeyNotFound
    }
    return result, 30*time.Minute, nil
})
```

## 缓存雪崩防护

缓存雪崩是指在同一时刻有大量缓存失效，导致所有请求都落到数据库上，造成数据库瞬间压力过大。

### 过期时间随机化

系统会在设置缓存时，自动对过期时间添加一个随机因子，使得同时设置的缓存不会在同一时间过期。

```go
// 默认的随机因子为0.1，表示在设置时间上下浮动10%
// 例如设置5分钟过期，实际过期时间会在4.5分钟到5.5分钟之间随机分布

// 设置缓存时自动应用随机过期时间
protectedCache.Set(key, value, 5*time.Minute)
```

## 完整示例

查看 `example/protection/main.go` 文件获取完整的示例代码。

运行示例：

```bash
cd example/protection
go run main.go
```

## 实际应用建议

1. 对于大型应用，建议同时使用布隆过滤器和缓存空值策略防止缓存穿透
2. 过期时间随机化对所有类型的缓存都适用，可以有效防止缓存雪崩
3. 布隆过滤器需要预先加载已知的键，适合对已有数据集较为稳定的场景
4. 缓存空值策略更加通用，但会占用一定的缓存空间

## 高级配置

```go
// 自定义缓存防护配置
protection := cache.NewCacheProtection()

// 配置布隆过滤器
protection.EnableBloomFilter = true
protection.ExpectedItems = 100000      // 预期元素数量
protection.FalsePositiveRate = 0.0001  // 误判率0.01%

// 配置过期时间随机化
protection.EnableExpirationJitter = true
protection.JitterFactor = 0.2          // 设置随机因子为0.2，表示上下浮动20%

// 创建带防护功能的缓存
protectedCache := cache.NewProtectedCache(memoryCache, protection, logger)
``` 