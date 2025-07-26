# go-cache

GO语言高性能缓存库，支持内存、文件、Redis三种实现方式，提供统一接口和丰富的数据结构操作。

## 特性

- **多种缓存实现**：
  - 内存缓存：高性能本地缓存，支持LRU淘汰策略
  - 分段内存缓存：使用分段锁技术，大幅提升并发性能
  - 文件缓存：基于BoltDB的持久化存储，支持数据压缩，内置内存层
  - Redis缓存：支持单机、哨兵和集群模式
- **丰富的数据结构**：完整支持字符串、哈希表、列表、集合、有序集合等数据类型
- **统一接口**：所有缓存实现都遵循相同的接口，可以无缝替换
- **统一错误处理**：标准化错误类型与转换，确保跨缓存类型的一致行为
- **上下文支持**：全方法支持Context，可以进行超时控制和取消操作
- **高级功能**：
  - 缓存保护：支持布隆过滤器、过期时间抖动等机制防止缓存穿透和雪崩
  - 内存缓存层：文件缓存自带内存缓存层，减少磁盘IO
  - 数据压缩：自动压缩大体积数据，节省存储空间
  - 批量操作：支持批量读写操作，提高性能
  - 分布式锁：基于Redlock算法的可靠分布式锁实现
  - 缓存预热：支持预加载数据到缓存
- **缓存助手**：提供JSON序列化、记忆模式等便捷功能
- **并发优化**：采用分段锁技术，显著提升高并发场景下的性能
- **完整测试**：全面的单元测试和基准测试覆盖，确保功能稳定性
- **清晰的分层架构**：不同缓存类型使用专用配置，结构清晰
- **默认日志实现**：自带logrus日志实现，也支持自定义日志接口

## 安装

### 前置依赖

- Go 1.16+
- 如使用Redis缓存，需要Redis服务器
- 文件缓存依赖BoltDB (go.etcd.io/bbolt)，无需额外安装

### 安装命令

```bash
# 使用go get安装
go get -u github.com/zhoudm1743/go-cache

# 或使用go mod
# 在go.mod文件中添加：
# require github.com/zhoudm1743/go-cache v1.0.0
```

## 导入说明

本库的模块路径为 `github.com/zhoudm1743/go-cache`，但包名为 `cache`，所以在使用时需要这样导入：

```go
import "github.com/zhoudm1743/go-cache"

// 使用时以 cache 作为包名前缀
config := cache.NewMemoryConfig()
memCache, err := cache.NewMemoryCache(config, nil)

// 或使用新的分段锁内存缓存（并发性能更佳）
shardedConfig := cache.NewShardedMemoryConfig()
shardedCache, err := cache.NewShardedMemoryCache(shardedConfig, nil)
```

### 引入子模块

如果只需要使用部分功能，可以选择性地导入需要的子模块：

```go
import (
    "github.com/zhoudm1743/go-cache"
    // 其他需要的包...
)
```

## 使用示例

### 1. 内存缓存

内存缓存有两种实现：传统的全局锁实现和性能更高的分段锁实现。

#### 1.1 传统内存缓存

传统内存缓存适合一般场景，实现简单直观。

```go
package main

import (
    "fmt"
    "time"
    "context"
    
    "github.com/zhoudm1743/go-cache" // 导入路径
)

func main() {
    // 创建内存缓存配置
    memoryConfig := cache.NewMemoryConfig()
    memoryConfig.Prefix = "mem:"
    memoryConfig.MaxEntries = 10000   // 最大缓存条目数，0表示不限制
    memoryConfig.MaxMemoryMB = 100    // 最大内存使用(MB)，0表示不限制
    memoryConfig.CleanInterval = 5 * time.Minute  // 过期清理间隔
    
    // 创建内存缓存（使用默认日志）
    memoryCache, err := cache.NewMemoryCache(memoryConfig, nil)
    if err != nil {
        fmt.Println("初始化内存缓存失败:", err)
        return
    }
    defer memoryCache.Close()
    
    // 基础操作
    
    // 设置缓存（10秒过期）
    err = memoryCache.Set("hello", "world", 10*time.Second)
    if err != nil {
        fmt.Println("设置缓存失败:", err)
        return
    }
    
    // 获取缓存
    val, err := memoryCache.Get("hello")
    if err != nil {
        fmt.Println("获取缓存失败:", err)
        return
    }
    fmt.Println("缓存值:", val)
    
    // 检查键是否存在
    exists, err := memoryCache.Exists("hello")
    if err != nil {
        fmt.Println("检查键失败:", err)
    } else {
        fmt.Printf("键存在: %v\n", exists > 0)
    }
    
    // 获取过期时间
    ttl, err := memoryCache.TTL("hello")
    if err != nil {
        fmt.Println("获取过期时间失败:", err)
    } else {
        fmt.Printf("剩余时间: %v\n", ttl)
    }
    
    // 统一的错误处理
    _, err = memoryCache.Get("non_existent_key")
    if errors.Is(err, cache.ErrKeyNotFound) {
        fmt.Println("键不存在，使用标准错误类型判断")
    }
}
```

#### 1.2 分段锁内存缓存

分段锁内存缓存适合高并发场景，可显著提升读写性能。

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建分段锁内存缓存配置
    config := cache.NewShardedMemoryConfig()
    config.Prefix = "sharded:"
    config.ShardCount = 32           // 分段数量，推荐使用2的幂
    config.MaxEntries = 10000        // 最大缓存条目数
    config.MaxMemoryMB = 100         // 最大内存使用(MB)
    config.CleanInterval = 5 * time.Minute  // 过期清理间隔
    config.EnableLRU = true          // 启用LRU淘汰策略
    config.CollectMetrics = true     // 收集性能指标
    
    // 创建分段锁内存缓存
    shardedCache, err := cache.NewShardedMemoryCache(config, nil)
    if err != nil {
        fmt.Println("初始化分段锁内存缓存失败:", err)
        return
    }
    defer shardedCache.Close()
    
    // 使用方式与传统内存缓存完全相同
    shardedCache.Set("hello", "world", 10*time.Second)
    val, _ := shardedCache.Get("hello")
    fmt.Println("缓存值:", val)
    
    // 在高并发场景下性能显著提升
    // 读密集型场景：性能提升约1.9倍
    // 读写均衡场景：性能提升约2.8倍
    // 写密集型场景：性能提升约1.8倍
    
    // 获取缓存统计信息
    stats := shardedCache.GetStats()
    fmt.Printf("缓存统计: 命中=%d, 未命中=%d, 设置=%d, 删除=%d\n", 
        stats.Hits, stats.Misses, stats.Sets, stats.Deletes)
}
```

### 2. Redis缓存

Redis缓存支持单机模式、哨兵模式和集群模式，适合分布式系统和需要数据共享的场景。

#### 2.1 单机模式

```go
package main

import (
    "fmt"
    "time"
    "errors"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建Redis缓存配置
    redisConfig := cache.NewRedisConfig()
    redisConfig.Host = "localhost"
    redisConfig.Port = 6379
    redisConfig.DB = 0
    redisConfig.Password = "" // 如需密码验证，在这里设置
    redisConfig.Timeout = 5   // 连接超时时间（秒）
    redisConfig.Prefix = "app:"  // 键前缀
    
    // 连接池配置
    redisConfig.PoolSize = 10      // 连接池大小
    redisConfig.MinIdleConns = 2   // 最小空闲连接数
    
    // 创建Redis缓存
    redisCache, err := cache.NewRedisCache(redisConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis缓存失败:", err)
        return
    }
    defer redisCache.Close()
    
    // 基础操作
    redisCache.Set("hello", "world", 10*time.Second)
    val, _ := redisCache.Get("hello")
    fmt.Println("Redis缓存值:", val)
    
    // 统一错误处理
    _, err = redisCache.Get("non_existent_key")
    if errors.Is(err, cache.ErrKeyNotFound) {
        fmt.Println("键不存在，标准化错误处理")
    }
    
    // 使用哈希表
    redisCache.HSet("user:1", 
        "name", "张三", 
        "age", 25, 
        "city", "北京")
    
    name, _ := redisCache.HGet("user:1", "name")
    fmt.Println("用户名:", name)
    
    // 获取所有字段
    fields, _ := redisCache.HGetAll("user:1")
    fmt.Printf("所有字段: %+v\n", fields)
    
    // 删除字段
    deleted, _ := redisCache.HDel("user:1", "city")
    fmt.Printf("已删除 %d 个字段\n", deleted)
}
```

#### 2.2 哨兵模式与集群模式

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建Redis哨兵配置
    sentinelConfig := cache.NewRedisSentinelConfig()
    sentinelConfig.MasterName = "mymaster"
    sentinelConfig.Addrs = []string{
        "localhost:26379",
        "localhost:26380",
        "localhost:26381",
    }
    sentinelConfig.DB = 0
    sentinelConfig.Password = ""
    sentinelConfig.Prefix = "sentinel:"
    
    // 创建Redis哨兵缓存
    sentinelCache, err := cache.NewRedisSentinelCache(sentinelConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis哨兵缓存失败:", err)
        return
    }
    defer sentinelCache.Close()
    
    // 集群模式示例
    clusterConfig := cache.NewRedisClusterConfig()
    clusterConfig.Addrs = []string{
        "localhost:7000",
        "localhost:7001",
        "localhost:7002",
    }
    clusterConfig.Password = ""
    clusterConfig.Prefix = "cluster:"
    
    // 创建Redis集群缓存
    clusterCache, err := cache.NewRedisClusterCache(clusterConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis集群缓存失败:", err)
        return
    }
    defer clusterCache.Close()
    
    // 使用方式与单机模式完全相同
}
```

#### 2.3 Redlock分布式锁

Redlock是Redis官方推荐的分布式锁算法，提供更可靠的分布式同步机制。

```go
package main

import (
    "fmt"
    "time"
    "context"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建多个Redis实例（实际应用中通常是不同服务器上的实例）
    var instances []cache.Cache
    
    // 创建3个Redis实例
    redisConfig1 := cache.NewRedisConfig()
    redisConfig1.Host = "localhost"
    redisConfig1.Port = 6379
    redisConfig1.DB = 0
    
    redis1, _ := cache.NewRedisCache(redisConfig1, nil)
    instances = append(instances, redis1)
    
    // 实际应用中应添加更多独立Redis实例
    // 这里简化为测试目的，使用相同Redis的不同DB
    redisConfig2 := cache.NewRedisConfig()
    redisConfig2.Host = "localhost"
    redisConfig2.Port = 6379
    redisConfig2.DB = 1
    
    redis2, _ := cache.NewRedisCache(redisConfig2, nil)
    instances = append(instances, redis2)
    
    redisConfig3 := cache.NewRedisConfig()
    redisConfig3.Host = "localhost"
    redisConfig3.Port = 6379
    redisConfig3.DB = 2
    
    redis3, _ := cache.NewRedisCache(redisConfig3, nil)
    instances = append(instances, redis3)
    
    // 创建Redlock配置
    redlockConfig := &cache.RedLockConfig{
        RetryCount: 3,                   // 获取锁的重试次数
        RetryDelay: 200 * time.Millisecond, // 重试间隔
        ClockDrift: 0.01,                // 时钟漂移因子(1%)
    }
    
    // 创建Redlock实例
    redlock := cache.NewRedLock(instances, redlockConfig, nil)
    
    // 获取分布式锁
    lockKey := "my_distributed_lock"
    lockTTL := 10 * time.Second
    
    // 尝试获取锁
    locked, err := redlock.Lock(lockKey, lockTTL)
    if err != nil {
        fmt.Println("获取锁出错:", err)
        return
    }
    
    if locked {
        fmt.Println("成功获取分布式锁")
        
        // 执行需要锁保护的操作...
        time.Sleep(2 * time.Second)
        
        // 完成后释放锁
        err = redlock.Unlock(lockKey)
        if err != nil {
            fmt.Println("释放锁出错:", err)
        } else {
            fmt.Println("成功释放分布式锁")
        }
    } else {
        fmt.Println("无法获取分布式锁")
    }
    
    // 使用更便捷的WithLock方法
    err = redlock.WithLock(lockKey, lockTTL, func() error {
        // 在锁保护下执行的操作
        fmt.Println("在分布式锁保护下执行操作")
        time.Sleep(1 * time.Second)
        return nil
    })
    
    if err != nil {
        fmt.Println("带锁操作失败:", err)
    } else {
        fmt.Println("带锁操作成功完成")
    }
    
    // 对于长时间任务，可以延长锁的有效期
    locked, _ = redlock.Lock(lockKey, 5*time.Second)
    if locked {
        // 执行部分操作...
        time.Sleep(2 * time.Second)
        
        // 延长锁的有效期
        extended, _ := redlock.ExtendLock(lockKey, 5*time.Second)
        if extended {
            fmt.Println("锁的有效期已成功延长")
            // 继续执行更多操作...
            time.Sleep(2 * time.Second)
        }
        
        // 完成后释放锁
        redlock.Unlock(lockKey)
    }
    
    // 清理资源
    for _, instance := range instances {
        instance.Close()
    }
}
```

### 3. 文件缓存

文件缓存基于BoltDB实现，提供持久化存储能力，适合需要数据持久化且不依赖Redis等外部服务的场景。

```go
package main

import (
    "fmt"
    "time"
    "errors"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建文件缓存配置
    fileConfig := cache.NewFileConfig()
    fileConfig.Prefix = "file:"           // 键前缀
    fileConfig.FilePath = "./cache_data"  // 缓存文件存储路径
    fileConfig.MemoryTTL = 5 * time.Minute // 内存层缓存过期时间
    fileConfig.CompactionInterval = 24 * time.Hour // 数据库压缩间隔
    
    // 创建文件缓存
    fileCache, err := cache.NewFileCache(fileConfig, nil)
    if err != nil {
        fmt.Println("初始化文件缓存失败:", err)
        return
    }
    defer fileCache.Close()
    
    // 基本操作
    // 设置缓存，24小时过期
    err = fileCache.Set("persistent", "这是持久化的数据", 24*time.Hour)
    if err != nil {
        fmt.Println("设置文件缓存失败:", err)
        return
    }
    
    // 获取缓存
    val, err := fileCache.Get("persistent")
    if err != nil {
        fmt.Println("获取文件缓存失败:", err)
    } else {
        fmt.Println("文件缓存值:", val)
    }
    
    // 统一错误处理
    _, err = fileCache.Get("non_existent_key")
    if errors.Is(err, cache.ErrKeyNotFound) {
        fmt.Println("文件缓存中键不存在")
        
        // 错误可能包含上下文信息
        fmt.Println("完整错误:", err)
    }
    
    // 设置后立即过期
    fileCache.Set("expire_soon", "即将过期", 1*time.Second)
    time.Sleep(2 * time.Second)
    
    // 过期键自动处理
    _, err = fileCache.Get("expire_soon")
    if err != nil {
        fmt.Println("键已过期并被清理:", err)
    }
    
    // 哈希表操作
    fileCache.HSet("user_data", "name", "李四", "age", 30)
    name, _ := fileCache.HGet("user_data", "name")
    fmt.Println("用户名:", name)
    
    // 获取所有字段
    fields, _ := fileCache.HGetAll("user_data")
    fmt.Printf("所有字段: %+v\n", fields)
    
    // 列表操作
    fileCache.LPush("tasks", "任务1", "任务2")
    fileCache.RPush("tasks", "任务3")
    
    // 获取列表所有元素
    tasks, _ := fileCache.LRange("tasks", 0, -1)
    fmt.Println("所有任务:", tasks)
    
    // 检查键过期时间
    ttl, err := fileCache.TTL("persistent")
    if err != nil {
        fmt.Println("获取TTL失败:", err)
    } else {
        fmt.Printf("剩余生存时间: %v\n", ttl)
    }
    
    // 文件缓存的内存层可以大幅提升读取性能
    // 首次读取从磁盘加载，之后的读取从内存获取
    start := time.Now()
    fileCache.Get("persistent") // 可能从磁盘读取
    firstRead := time.Since(start)
    
    start = time.Now()
    fileCache.Get("persistent") // 直接从内存读取
    secondRead := time.Since(start)
    
    fmt.Printf("首次读取: %v, 二次读取: %v (从内存层，速度更快)\n", 
        firstRead, secondRead)
}
```

#### 文件缓存的高级特性

```go
package main

import (
    "fmt"
    "time"
    "context"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    fileConfig := cache.NewFileConfig()
    fileConfig.FilePath = "./advanced_cache"
    
    fileCache, _ := cache.NewFileCache(fileConfig, nil)
    defer fileCache.Close()
    
    // 1. 文件缓存自带内存缓存层，热点数据会优先从内存获取，减少磁盘IO
    
    // 2. 自动压缩大数据
    // 当数据超过一定大小(默认4KB)时，会自动进行压缩存储
    bigData := make([]byte, 10*1024) // 10KB数据
    for i := range bigData {
        bigData[i] = byte(i % 256)
    }
    
    fileCache.Set("big_data", string(bigData), time.Hour)
    
    // 3. 事务支持
    // 通过Context可以控制操作超时
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    fileCache.SetCtx(ctx, "ctx_key", "with context", time.Minute)
    val, _ := fileCache.GetCtx(ctx, "ctx_key")
    fmt.Println("通过Context获取:", val)
    
    // 4. 数据库整理
    // 文件缓存会自动定期整理数据库文件，也可以手动触发
    // 此功能需要访问内部实现，实际使用中通常依赖自动整理
    
    // 5. 模式匹配查找键
    for i := 0; i < 10; i++ {
        key := fmt.Sprintf("pattern:%d", i)
        fileCache.Set(key, fmt.Sprintf("值%d", i), time.Hour)
    }
    
    // 查找匹配模式的键
    matchedKeys, _ := fileCache.Keys("pattern:*")
    fmt.Println("匹配的键数量:", len(matchedKeys))
    fmt.Println("部分匹配的键:", matchedKeys[:3])
}
```

### 4. 缓存助手

缓存助手（CacheHelper）提供了更高级的缓存操作，如JSON序列化、记忆模式、分布式锁等功能，能极大简化开发。

```go
package main

import (
    "fmt"
    "time"
    "context"
    "errors"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 先创建一个缓存实例（这里使用内存缓存）
    memConfig := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(memConfig, nil)
    defer memCache.Close()
    
    // 创建缓存助手（使用默认日志）
    helper := cache.NewCacheHelper(memCache, nil, "helper:")
    
    // ------- 1. JSON对象操作 -------
    type User struct {
        ID       int      `json:"id"`
        Name     string   `json:"name"`
        Age      int      `json:"age"`
        Email    string   `json:"email"`
        Tags     []string `json:"tags"`
        IsActive bool     `json:"is_active"`
    }
    
    // 存储JSON对象
    user := User{
        ID:       1,
        Name:     "张三",
        Age:      28,
        Email:    "zhangsan@example.com",
        Tags:     []string{"用户", "VIP", "活跃"},
        IsActive: true,
    }
    
    // 将对象序列化为JSON并缓存
    err := helper.SetJSON("user:1", user, 30*time.Second)
    if err != nil {
        fmt.Println("存储JSON失败:", err)
        return
    }
    
    // 从缓存获取并反序列化为对象
    var retrievedUser User
    err = helper.GetJSON("user:1", &retrievedUser)
    if err != nil {
        // 检查错误类型
        if errors.Is(err, cache.ErrKeyNotFound) {
            fmt.Println("用户信息未找到")
        } else {
            fmt.Println("获取JSON失败:", err)
        }
        return
    }
    fmt.Printf("用户信息: %+v\n", retrievedUser)
    
    // ------- 2. 记忆模式 -------
    // 如果缓存不存在，执行函数并缓存结果；如果缓存存在，直接返回缓存结果
    result, err := helper.Remember("expensive-operation", 5*time.Minute, func() (interface{}, error) {
        // 模拟耗时操作
        fmt.Println("执行耗时操作...")
        time.Sleep(500 * time.Millisecond)
        return "操作结果数据", nil
    })
    if err != nil {
        // 处理统一错误
        if errors.Is(err, cache.ErrConnectionFailed) {
            fmt.Println("连接缓存服务器失败")
        } else if errors.Is(err, cache.ErrTimeout) {
            fmt.Println("操作超时")
        } else {
            fmt.Println("记忆模式失败:", err)
        }
        return
    }
    fmt.Println("第一次结果:", result)
    
    // 再次调用，会直接从缓存返回结果
    start := time.Now()
    result, _ = helper.Remember("expensive-operation", 5*time.Minute, func() (interface{}, error) {
        // 这个函数不会被执行
        fmt.Println("这行不会被打印")
        time.Sleep(500 * time.Millisecond)
        return "新结果", nil
    })
    fmt.Println("第二次结果:", result)
    fmt.Printf("响应时间: %v (从缓存获取，所以很快)\n", time.Since(start))
    
    // ------- 3. JSON记忆模式 -------
    // 结合JSON和记忆模式的便捷方法
    var stats struct {
        Count      int       `json:"count"`
        LastUpdate time.Time `json:"last_update"`
    }
    
    helper.RememberJSON("stats", 1*time.Minute, &stats, func() (interface{}, error) {
        return struct {
            Count      int       `json:"count"`
            LastUpdate time.Time `json:"last_update"`
        }{
            Count:      42,
            LastUpdate: time.Now(),
        }, nil
    })
    
    fmt.Printf("统计信息: %+v\n", stats)
    
    // ------- 4. GetOrSet缓存模式 -------
    // 如果键存在则获取值，不存在则设置默认值并返回
    value, err := helper.GetOrSet("default-key", "默认值", time.Hour)
    if err != nil {
        fmt.Println("GetOrSet错误:", err)
        return
    }
    fmt.Println("GetOrSet结果:", value)
    
    // ------- 5. 批量操作 -------
    // 批量设置
    batchData := map[string]interface{}{
        "batch:1": "批量值1",
        "batch:2": "批量值2",
        "batch:3": "批量值3",
    }
    helper.BatchSet(batchData, 5*time.Minute)
    
    // 批量获取
    keys := []string{"batch:1", "batch:2", "batch:3", "batch:non-existent"}
    values, _ := helper.BatchGet(keys)
    fmt.Printf("批量获取结果: %v\n", values)
    
    // ------- 6. 错误提取和分析 -------
    _, err = helper.Get("non_existent_key")
    if err != nil {
        // 获取错误的根本原因
        rootErr := cache.GetRootError(err)
        fmt.Println("根本错误:", rootErr)
        
        // 检查是否是标准错误类型
        if cache.IsStandardError(err) {
            fmt.Println("这是一个标准化的缓存错误")
        }
        
        // 获取带有上下文的错误信息
        fmt.Println("完整错误信息:", err.Error())
    }
}
```

### 5. 缓存预热

缓存预热是一种优化技术，通过预先加载可能会被访问的数据到缓存中，避免系统启动后大量缓存未命中导致的性能问题。go-cache提供了完善的缓存预热功能。

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建缓存实例
    memConfig := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(memConfig, nil)
    defer memCache.Close()
    
    // ------- 1. 简单预热：使用预定义数据 -------
    fmt.Println("1. 使用预定义数据进行简单预热:")
    
    // 准备预热数据
    warmupData := map[string]interface{}{
        "product:1": "iPhone 13",
        "product:2": "MacBook Pro",
        "product:3": "iPad Air",
        "product:4": "Apple Watch",
    }
    
    // 创建数据加载器
    dataLoader := cache.NewMapDataLoader(warmupData, 10*time.Minute)
    
    // 指定要预热的键
    keysToWarmup := []string{"product:1", "product:2", "product:3"}
    
    // 执行预热
    err := memCache.WarmupKeys(context.Background(), keysToWarmup, dataLoader)
    if err != nil {
        fmt.Println("预热失败:", err)
    }
    
    // 验证预热结果
    for _, key := range keysToWarmup {
        val, _ := memCache.Get(key)
        fmt.Printf("键 %s = %s\n", key, val)
    }
    
    // ------- 2. 使用自定义数据加载器 -------
    fmt.Println("\n2. 使用自定义数据加载器:")
    
    // 创建自定义数据加载器
    customLoader := cache.NewFunctionDataLoader(func(ctx context.Context, key string) (interface{}, error) {
        // 模拟从数据库加载数据
        fmt.Printf("从数据库加载键 %s\n", key)
        
        // 根据不同键加载不同数据
        switch key {
        case "user:1":
            return `{"id": 1, "name": "张三"}`, nil
        case "user:2":
            return `{"id": 2, "name": "李四"}`, nil
        case "user:3":
            return `{"id": 3, "name": "王五"}`, nil
        default:
            return nil, fmt.Errorf("未知的键: %s", key)
        }
    })
    
    // 执行预热
    userKeys := []string{"user:1", "user:2", "user:3"}
    err = memCache.WarmupKeys(context.Background(), userKeys, customLoader)
    if err != nil {
        fmt.Println("预热失败:", err)
    }
    
    // 验证预热结果
    for _, key := range userKeys {
        val, _ := memCache.Get(key)
        fmt.Printf("键 %s = %s\n", key, val)
    }
    
    // ------- 3. 使用键生成器 -------
    fmt.Println("\n3. 使用键生成器:")
    
    // 先在缓存中设置一些键，以便键生成器能找到它们
    prefixes := []string{"api:", "db:", "config:"}
    for _, prefix := range prefixes {
        for i := 1; i <= 3; i++ {
            key := fmt.Sprintf("%s%d", prefix, i)
            memCache.Set(key, fmt.Sprintf("value-%s%d", prefix, i), time.Hour)
        }
    }
    
    // 创建键生成器，只预热api开头的键
    keyGenerator := cache.NewSimpleKeyGenerator(memCache, "api:*")
    
    // 创建数据加载器
    refreshLoader := cache.NewFunctionDataLoader(func(ctx context.Context, key string) (interface{}, error) {
        // 获取原值并更新
        val, err := memCache.Get(key)
        if err != nil {
            return "default-value", nil
        }
        return fmt.Sprintf("%s-refreshed", val), nil
    })
    
    // 执行预热
    err = memCache.Warmup(context.Background(), refreshLoader, keyGenerator)
    if err != nil {
        fmt.Println("预热失败:", err)
    }
    
    // 验证api:前缀的键已被更新
    keys, _ := memCache.Keys("api:*")
    for _, key := range keys {
        val, _ := memCache.Get(key)
        fmt.Printf("键 %s = %s\n", key, val)
    }
    
    // ------- 4. 使用自定义预热配置 -------
    fmt.Println("\n4. 使用自定义预热配置:")
    
    // 创建自定义预热配置
    warmupConfig := cache.DefaultWarmupConfig()
    warmupConfig.Concurrency = 2 // 限制并发数为2
    warmupConfig.Interval = 50 * time.Millisecond // 每次加载间隔50ms
    warmupConfig.ContinueOnError = true // 出错时继续
    warmupConfig.ProgressCallback = func(loaded, total int, key string, err error) {
        if err != nil {
            fmt.Printf("预热进度: %d/%d, 键: %s, 错误: %v\n", loaded, total, key, err)
        } else {
            fmt.Printf("预热进度: %d/%d, 键: %s\n", loaded, total, key, err)
        }
    }
    
    // 准备测试数据
    testKeys := []string{"test:1", "test:2", "test:3", "test:error"}
    testLoader := cache.NewFunctionDataLoader(func(ctx context.Context, key string) (interface{}, error) {
        if key == "test:error" {
            return nil, fmt.Errorf("模拟错误")
        }
        return fmt.Sprintf("test-value-%s", key), nil
    })
    
    // 执行预热
    err = cache.WarmupCacheWithConfig(context.Background(), memCache, testKeys, testLoader, warmupConfig)
    if err != nil {
        fmt.Println("使用自定义配置预热失败:", err)
    }
}
```

### 4. 缓存保护

go-cache提供了多种缓存保护机制，用于防止缓存穿透、缓存击穿和缓存雪崩等问题。

```go
package main

import (
    "fmt"
    "time"
    "context"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建基础缓存（这里使用内存缓存作为示例）
    memConfig := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(memConfig, nil)
    defer memCache.Close()
    
    // 创建缓存保护配置
    protectionConfig := cache.NewProtectionConfig()
    
    // 1. 布隆过滤器配置（防止缓存穿透）
    protectionConfig.BloomFilterSize = 10000 // 布隆过滤器大小
    protectionConfig.BloomFilterFPRate = 0.01 // 假阳性率
    protectionConfig.EnableBloomFilter = true // 启用布隆过滤器
    
    // 2. 缓存穿透保护（空值缓存）
    protectionConfig.CachePenetrationProtection = true
    protectionConfig.NilValueExpiration = 5 * time.Minute // 空值缓存过期时间
    
    // 3. 缓存雪崩保护（过期时间随机化）
    protectionConfig.CacheAvalancheProtection = true
    protectionConfig.MaxJitterPercent = 10 // 最大抖动比例(10%)
    
    // 创建受保护的缓存
    protectedCache := cache.NewCacheProtection(memCache, protectionConfig, nil)
    
    // ===== 布隆过滤器示例 =====
    
    // 注册键到布隆过滤器
    protectedCache.RegisterKeys("user:1", "user:2", "user:3")
    
    // 检查键是否可能存在
    exists := protectedCache.MightExist("user:1")
    fmt.Println("user:1 可能存在:", exists) // true
    
    exists = protectedCache.MightExist("user:999")
    fmt.Println("user:999 可能存在:", exists) // false (很可能)
    
    // 当MightExist返回false时，键一定不存在
    // 当返回true时，键可能存在（有小概率假阳性）
    
    // ===== 缓存穿透保护示例 =====
    
    // 模拟数据访问函数
    dataLoader := func(key string) (interface{}, error) {
        // 模拟数据库查询
        if key == "valid-key" {
            return "数据库中的值", nil
        }
        return nil, fmt.Errorf("数据不存在")
    }
    
    // 使用GetOrLoad方法（带穿透保护）
    value, err := protectedCache.GetOrLoad("valid-key", 5*time.Minute, dataLoader)
    if err != nil {
        fmt.Println("加载数据失败:", err)
    } else {
        fmt.Println("获取的值:", value)
    }
    
    // 对于不存在的键，空值会被缓存
    value, err = protectedCache.GetOrLoad("invalid-key", 5*time.Minute, dataLoader)
    if err != nil {
        fmt.Println("加载数据失败:", err)
        
        // 尝试再次获取同一个不存在的键
        _, err = protectedCache.GetOrLoad("invalid-key", 5*time.Minute, dataLoader)
        if err != nil {
            fmt.Println("第二次请求直接从缓存返回空值，不会查询数据库")
        }
    }
    
    // ===== 缓存雪崩保护示例 =====
    
    // 同时设置多个键，相同的过期时间会被随机化
    expiration := 5 * time.Minute
    for i := 0; i < 10; i++ {
        key := fmt.Sprintf("batch-key:%d", i)
        // 过期时间会在 4分30秒 到 5分30秒之间随机分布（±10%）
        protectedCache.Set(key, fmt.Sprintf("值%d", i), expiration)
    }
    
    // 检查几个键的实际过期时间
    for i := 0; i < 3; i++ {
        key := fmt.Sprintf("batch-key:%d", i)
        ttl, _ := protectedCache.TTL(key)
        fmt.Printf("键 %s 的过期时间: %v\n", key, ttl)
    }
}
```

#### 布隆过滤器原理与应用

布隆过滤器是一种空间效率很高的概率性数据结构，用于判断一个元素是否在集合中。

- **优点**: 空间占用小、查询速度快
- **缺点**: 有一定的假阳性概率，无法删除元素
- **主要应用**: 防止缓存穿透，避免大量不存在的键查询直接打到数据库

```go
// 布隆过滤器单独使用示例
bloomFilter := cache.NewBloomFilter(10000, 0.01)

// 添加元素
bloomFilter.Add("key1")
bloomFilter.Add("key2")

// 检查元素
exists := bloomFilter.Contains("key1") // true
exists = bloomFilter.Contains("key3") // false（除非假阳性）

// 重置过滤器
bloomFilter.Reset()
```

#### 缓存保护策略详解

| 问题 | 描述 | 防护策略 |
|------|------|----------|
| 缓存穿透 | 查询不存在的数据，每次都会穿透到底层数据源 | 1. 布隆过滤器过滤不存在的键<br>2. 对空值进行缓存 |
| 缓存击穿 | 热点数据过期瞬间，大量请求直击数据库 | 1. 互斥锁（单进程）<br>2. 分布式锁（多进程） |
| 缓存雪崩 | 大量缓存同时过期，系统压力骤增 | 1. 过期时间随机化<br>2. 多级缓存<br>3. 服务熔断与降级 |

## 配置选项详解

### 内存缓存配置

```go
memConfig := cache.NewMemoryConfig()
memConfig.Prefix = "app:" // 键前缀，用于区分不同应用的数据
```

### Redis缓存配置

```go
redisConfig := cache.NewRedisConfig()
redisConfig.Host = "localhost"     // Redis服务器地址
redisConfig.Port = 6379            // Redis端口
redisConfig.DB = 0                 // 数据库编号
redisConfig.Password = "password"  // 密码（如需）
redisConfig.Timeout = 5            // 连接超时（秒）
redisConfig.Prefix = "myapp:"      // 键前缀
```

### 文件缓存配置

```go
fileConfig := cache.NewFileConfig()
fileConfig.FilePath = "./cache_data"  // 缓存文件存储路径
fileConfig.Prefix = "disk:"           // 键前缀
```

## 高级数据结构操作

go-cache支持Redis风格的丰富数据结构，包括字符串、哈希表、列表、集合和有序集合，所有缓存实现都支持这些数据结构操作。

### 1. 哈希表操作

```go
// 设置多个字段
cache.HSet("user:100", 
    "name", "李四", 
    "age", 30, 
    "email", "lisi@example.com", 
    "is_vip", true)

// 获取单个字段
name, _ := cache.HGet("user:100", "name")

// 删除字段
cache.HDel("user:100", "is_vip")

// 获取所有字段
allFields, _ := cache.HGetAll("user:100")

// 检查字段是否存在
exists, _ := cache.HExists("user:100", "email")

// 获取字段数量
count, _ := cache.HLen("user:100")
```

### 2. 列表操作

```go
// 左侧添加元素（头部）
cache.LPush("logs", "日志1", "日志2")
// 此时列表为: ["日志2", "日志1"]

// 右侧添加元素（尾部）
cache.RPush("logs", "日志3", "日志4")
// 此时列表为: ["日志2", "日志1", "日志3", "日志4"]

// 左侧弹出元素（头部）
item, _ := cache.LPop("logs")
// 返回 "日志2"，列表变为: ["日志1", "日志3", "日志4"]

// 右侧弹出元素（尾部）
item, _ = cache.RPop("logs")
// 返回 "日志4"，列表变为: ["日志1", "日志3"]

// 获取列表长度
length, _ := cache.LLen("logs")
// 返回 2

// 获取列表范围
// 参数: 键名, 起始索引, 结束索引
// 索引从0开始，负数表示从末尾计算：-1表示最后一个元素，-2表示倒数第二个，依此类推
items, _ := cache.LRange("logs", 0, -1) // 获取所有元素
// 返回 ["日志1", "日志3"]

// 获取部分元素
items, _ = cache.LRange("logs", 0, 0) // 获取第一个元素
// 返回 ["日志1"]
```

### 3. 集合操作

集合是无序且唯一的元素集合，支持添加、删除、判断元素是否存在等操作。

```go
// 添加元素到集合
cache.SAdd("tags", "php", "python", "golang")

// 检查元素是否存在
exists, _ := cache.SIsMember("tags", "golang")
// 返回 true

// 获取所有元素
members, _ := cache.SMembers("tags")
// 返回 ["php", "python", "golang"] (顺序可能不同，因为集合是无序的)

// 删除元素
removed, _ := cache.SRem("tags", "php")
// 返回 1，表示成功删除1个元素

// 获取集合大小
size, _ := cache.SCard("tags")
// 返回 2
```

### 4. 有序集合操作

有序集合类似集合，但每个元素关联一个分数，用于排序。

```go
// 添加成员及分数
cache.ZAdd("ranking", 
    cache.Z{Score: 100, Member: "user1"},
    cache.Z{Score: 85.5, Member: "user2"},
    cache.Z{Score: 95, Member: "user3"})

// 获取成员排名（按分数从小到大）
// 参数：键名, 起始索引, 结束索引
members, _ := cache.ZRange("ranking", 0, 2) // 前三名
// 返回 ["user2", "user3", "user1"]

// 获取成员和分数
scoresWithMembers, _ := cache.ZRangeWithScores("ranking", 0, -1)
// 返回 [{Score:85.5, Member:"user2"}, {Score:95, Member:"user3"}, {Score:100, Member:"user1"}]

// 获取单个成员分数
score, _ := cache.ZScore("ranking", "user1")
// 返回 100

// 删除成员
removed, _ := cache.ZRem("ranking", "user2")
// 返回 1，表示成功删除1个成员

// 获取有序集合大小
count, _ := cache.ZCard("ranking")
// 返回 2
```

### 5. 字符串和数值操作

除了基础的Get/Set操作外，字符串类型还支持数值递增/递减操作。

```go
// 设置计数器初始值
cache.Set("counter", "10", 0) // 0表示永不过期

// 递增计数器
newValue, _ := cache.Incr("counter")
// 返回 11

// 增加指定值
newValue, _ = cache.IncrBy("counter", 5)
// 返回 16

// 递减计数器
newValue, _ = cache.Decr("counter")
// 返回 15

// 减少指定值 (可通过IncrBy实现，传负数)
newValue, _ = cache.IncrBy("counter", -3)
// 返回 12
```

### 6. 键过期和管理操作

```go
// 设置键过期时间
cache.Expire("mykey", 30*time.Second)

// 获取键剩余生存时间
ttl, _ := cache.TTL("mykey")
// 返回剩余时间，如果键不存在或已过期，返回负值

// 查找匹配的键
keys, _ := cache.Keys("user:*")
// 返回所有以user:开头的键

// 删除多个键
deleted, _ := cache.Del("key1", "key2", "key3")
// 返回实际删除的键数量

// 检查多个键是否存在
count, _ := cache.Exists("key1", "key2", "key3")
// 返回存在的键数量
```

**注意**: 所有数据结构操作都有对应的带Context版本（方法名后加Ctx），可用于超时控制和取消操作。

## 自定义日志

默认情况下，go-cache使用logrus作为日志实现。您可以通过以下两种方式自定义日志：

### 1. 设置全局默认日志

```go
package main

import (
    "log"
    "os"
    
    "github.com/zhoudm1743/go-cache"
)

// 自定义日志实现
type MyLogger struct {
    logger *log.Logger
}

// 实现Logger接口的所有方法
func (l *MyLogger) Info(args ...interface{}) {
    l.logger.Println("INFO:", args)
}

func (l *MyLogger) Error(args ...interface{}) {
    l.logger.Println("ERROR:", args)
}

func (l *MyLogger) Debug(args ...interface{}) { 
    l.logger.Println("DEBUG:", args)
}

func (l *MyLogger) Warn(args ...interface{}) { 
    l.logger.Println("WARN:", args)
}

func (l *MyLogger) Fatal(args ...interface{}) { 
    l.logger.Println("FATAL:", args)
    os.Exit(1)
}

func (l *MyLogger) Debugf(format string, args ...interface{}) { 
    l.logger.Printf("DEBUG: "+format, args...)
}

func (l *MyLogger) Infof(format string, args ...interface{}) { 
    l.logger.Printf("INFO: "+format, args...)
}

func (l *MyLogger) Warnf(format string, args ...interface{}) { 
    l.logger.Printf("WARN: "+format, args...)
}

func (l *MyLogger) Errorf(format string, args ...interface{}) { 
    l.logger.Printf("ERROR: "+format, args...)
}

func (l *MyLogger) Fatalf(format string, args ...interface{}) { 
    l.logger.Printf("FATAL: "+format, args...)
    os.Exit(1)
}

func (l *MyLogger) WithFields(fields map[string]interface{}) cache.Logger {
    // 简单实现，将字段信息添加到日志消息中
    newLogger := &MyLogger{
        logger: l.logger,
    }
    return newLogger
}

func main() {
    // 创建自定义日志
    customLogger := &MyLogger{
        logger: log.New(os.Stdout, "", log.LstdFlags),
    }
    
    // 设置为默认日志
    cache.SetDefaultLogger(customLogger)
    
    // 之后创建的所有缓存实例都会使用这个日志
    config := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(config, nil) // 使用默认日志
    
    // 也可以单独为某个实例设置日志
    redisConfig := cache.NewRedisConfig()
    redisCache, _ := cache.NewRedisCache(redisConfig, customLogger)
}
```

### 2. 为特定缓存实例设置日志

```go
package main

import (
    "github.com/sirupsen/logrus"
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建logrus日志实例并自定义配置
    logger := logrus.New()
    logger.SetLevel(logrus.DebugLevel)
    logger.SetFormatter(&logrus.JSONFormatter{})
    
    // 包装为符合cache.Logger接口的适配器（如果需要）
    // 注意：本库默认日志已经是logrus实现，此处仅为示例
    
    // 为特定缓存实例设置日志
    memConfig := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(memConfig, logger)
    
    // 使用缓存...
}
```

## 测试与性能比较

go-cache提供了全面的测试套件，包括单元测试和基准测试，用于验证功能正确性和评估性能。

### 运行测试

```bash
# 运行所有测试
go test -v ./test

# 运行特定测试
go test -v ./test -run TestMemoryCache
go test -v ./test -run TestRedLock

# 运行性能基准测试
go test -v ./test -run=^$ -bench=. -benchtime=1s
```

### 性能比较结果

下面是在不同场景下全局锁内存缓存与分段锁内存缓存的性能比较：

| 场景 | 传统内存缓存 | 分段锁内存缓存 | 性能提升 |
|------|------------|--------------|---------|
| 读密集(90%读,10%写) | 42.98ms | 23.00ms | 1.87倍 |
| 读写均衡(50%读,50%写) | 66.51ms | 24.00ms | 2.77倍 |
| 写密集(10%读,90%写) | 43.88ms | 23.87ms | 1.84倍 |

*以上性能测试在100,000次操作、8个并行goroutine下进行*

## 常见问题

### 1. Redis连接问题

如果遇到Redis连接问题，请检查：

- **连接配置**：确保IP、端口和密码正确
- **网络问题**：检查防火墙设置，尝试增加连接超时时间
- **连接池配置**：高负载下可能需要调整连接池大小
- **错误诊断**：利用标准错误类型（如`cache.ErrConnectionFailed`）来诊断问题

### 2. 文件缓存过期问题

文件缓存会同步管理内存层和磁盘层的过期键：

- 当通过`Get`读取时，系统会检查键是否过期并同步删除过期键
- 后台定时任务会清理过期键，但可能存在一定延迟
- 对于高频访问场景，推荐使用更短的`MemoryTTL`配置

### 3. 分布式锁可靠性

使用Redlock算法时需要注意：

- 使用多个独立的Redis实例（最少3个）以提高可靠性
- 每个锁操作都应设置合理的过期时间，避免死锁
- 考虑使用`WithLock`方法而非直接操作锁，以确保锁的正确释放
- 对于长时间任务，使用`ExtendLock`延长锁的有效期

## 总结

go-cache库提供了一套完整的缓存解决方案，适用于多种应用场景，从单机应用到分布式系统。通过统一接口、丰富的数据结构操作、可靠的分布式锁和高性能的分段锁实现，可以大幅提升应用的性能和可靠性。

主要优势：

- **统一接口**：所有缓存实现遵循相同接口，便于切换和测试
- **完整数据结构**：支持字符串、哈希表、列表、集合和有序集合
- **高并发性能**：分段锁技术显著提升并发读写性能
- **可靠分布式锁**：基于Redlock算法的高可靠分布式锁实现
- **统一错误处理**：标准化错误类型和处理机制，简化错误诊断
- **完整测试覆盖**：全面的单元测试和基准测试确保稳定性

最佳实践：

1. 对于单机高并发场景，使用分段锁内存缓存
2. 对于需要持久化的数据，使用文件缓存
3. 对于分布式系统，使用Redis缓存并搭配Redlock分布式锁
4. 始终使用缓存保护机制防止缓存穿透、击穿和雪崩
5. 利用统一错误处理简化错误逻辑

## 许可证

本项目采用MIT许可证。详细信息请参见LICENSE文件。
