# go-cache

GO语言缓存库，支持内存、文件、Redis三种实现方式，提供统一接口和丰富的数据结构操作。

## 特性

- **多种缓存实现**：
  - 内存缓存：高性能本地缓存，支持LRU淘汰策略
  - 文件缓存：基于BoltDB的持久化存储，支持数据压缩
  - Redis缓存：支持单机、哨兵和集群模式
- **丰富的数据结构**：完整支持字符串、哈希表、列表、集合、有序集合等数据类型
- **统一接口**：所有缓存实现都遵循相同的接口，可以无缝替换
- **上下文支持**：全方法支持Context，可以进行超时控制和取消操作
- **高级功能**：
  - 内存缓存层：文件缓存自带内存缓存层，减少磁盘IO
  - 数据压缩：自动压缩大体积数据，节省存储空间
  - 批量操作：支持批量读写操作，提高性能
  - 分布式锁：提供简单的分布式锁实现
  - 缓存预热：支持预加载数据到缓存
- **缓存助手**：提供JSON序列化、记忆模式等便捷功能
- **清晰的分层架构**：不同缓存类型使用专用配置，结构清晰
- **默认日志实现**：自带logrus日志实现，也支持自定义日志接口

## 安装

### 前置依赖

- Go 1.16+
- 如使用Redis缓存，需要Redis服务器
- 文件缓存依赖BoltDB，无需额外安装

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

内存缓存是最简单高效的缓存实现，适合单机场景和对性能要求高的场合。

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
    
    // 使用Context控制超时
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    
    // 所有方法都有带Context的版本
    val, err = memoryCache.GetCtx(ctx, "hello")
    if err != nil {
        fmt.Println("使用Context获取失败:", err)
    } else {
        fmt.Println("使用Context获取值:", val)
    }
    
    // 数值操作
    memoryCache.Set("counter", "10", 0) // 0表示永不过期
    count, _ := memoryCache.Incr("counter")
    fmt.Println("递增后:", count) // 输出: 11
    
    count, _ = memoryCache.IncrBy("counter", 5)
    fmt.Println("增加5后:", count) // 输出: 16
    
    count, _ = memoryCache.Decr("counter")
    fmt.Println("递减后:", count) // 输出: 15
    
    // 删除键
    deleted, _ := memoryCache.Del("counter")
    fmt.Println("已删除键数:", deleted)
}
```

#### 内存缓存高级特性

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建支持LRU的内存缓存
    config := cache.NewMemoryConfig()
    config.MaxEntries = 1000  // 最多存储1000个键
    config.MaxMemoryMB = 50   // 最大使用50MB内存
    
    memCache, _ := cache.NewMemoryCache(config, nil)
    defer memCache.Close()
    
    // 当缓存数量或内存超过限制时，最少使用的键会被自动淘汰
    
    // 批量操作示例
    // 设置多个键
    for i := 0; i < 10; i++ {
        key := fmt.Sprintf("key:%d", i)
        memCache.Set(key, fmt.Sprintf("value:%d", i), time.Minute)
    }
    
    // 检查多个键是否存在
    exists, _ := memCache.Exists("key:1", "key:2", "key:3")
    fmt.Printf("存在的键数量: %d\n", exists)
    
    // 删除多个键
    deleted, _ := memCache.Del("key:1", "key:2")
    fmt.Printf("删除的键数量: %d\n", deleted)
    
    // 使用模式匹配获取键列表
    keys, _ := memCache.Keys("key:*")
    fmt.Println("匹配的键:", keys)
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

#### 2.2 哨兵模式

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
    sentinelConfig.Password = "" // 如需密码验证，在这里设置
    sentinelConfig.Prefix = "sentinel:"
    
    // 连接池配置
    sentinelConfig.PoolSize = 20      // 连接池大小
    sentinelConfig.MinIdleConns = 5   // 最小空闲连接数
    
    // 创建Redis哨兵缓存
    sentinelCache, err := cache.NewRedisSentinelCache(sentinelConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis哨兵缓存失败:", err)
        return
    }
    defer sentinelCache.Close()
    
    // 使用方式与单机模式完全相同
    sentinelCache.Set("hello", "sentinel", 10*time.Second)
    val, _ := sentinelCache.Get("hello")
    fmt.Println("哨兵缓存值:", val)
}
```

#### 2.3 集群模式

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建Redis集群配置
    clusterConfig := cache.NewRedisClusterConfig()
    clusterConfig.Addrs = []string{
        "localhost:7000",
        "localhost:7001",
        "localhost:7002",
        "localhost:7003",
        "localhost:7004",
        "localhost:7005",
    }
    clusterConfig.Password = "" // 如需密码验证，在这里设置
    clusterConfig.Prefix = "cluster:"
    
    // 集群特有配置
    clusterConfig.MaxRedirects = 3    // 最大重定向次数
    clusterConfig.RouteByLatency = true  // 按延迟路由
    
    // 创建Redis集群缓存
    clusterCache, err := cache.NewRedisClusterCache(clusterConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis集群缓存失败:", err)
        return
    }
    defer clusterCache.Close()
    
    // 使用方式与单机模式完全相同
    clusterCache.Set("hello", "cluster", 10*time.Second)
    val, _ := clusterCache.Get("hello")
    fmt.Println("集群缓存值:", val)
}
```

### 3. 文件缓存

文件缓存基于BoltDB实现，提供持久化存储能力，适合需要数据持久化且不依赖Redis等外部服务的场景。

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 创建文件缓存配置
    fileConfig := cache.NewFileConfig()
    fileConfig.Prefix = "file:"           // 键前缀
    fileConfig.FilePath = "./cache_data"  // 缓存文件存储路径
    
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
    
    // 检查键是否存在
    exists, _ := fileCache.Exists("persistent")
    fmt.Printf("键存在: %v\n", exists > 0)
    
    // 列表操作
    fileCache.RPush("queue", "任务1", "任务2", "任务3")
    
    // 获取列表长度
    length, _ := fileCache.LLen("queue")
    fmt.Printf("队列长度: %d\n", length)
    
    // 获取所有元素
    tasks, _ := fileCache.LRange("queue", 0, -1)
    fmt.Println("所有任务:", tasks)
    
    // 弹出一个任务
    task, _ := fileCache.LPop("queue")
    fmt.Println("处理任务:", task)
    
    // 批量操作
    // 文件缓存支持批量读写操作，可以提高性能
    batchData := map[string]interface{}{
        "key1": "value1",
        "key2": "value2",
        "key3": "value3",
    }
    
    // 实现批量设置
    for k, v := range batchData {
        fileCache.Set(k, v, time.Hour)
    }
    
    // 获取多个键
    keys := []string{"key1", "key2"}
    for _, key := range keys {
        val, _ := fileCache.Get(key)
        fmt.Printf("键 %s: %s\n", key, val)
    }
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
        fmt.Println("获取JSON失败:", err)
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
        fmt.Println("记忆模式失败:", err)
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
    
    // ------- 4. 分布式锁 -------
    // 尝试获取锁
    locked, err := helper.Lock("my-lock", 10*time.Second)
    if err != nil {
        fmt.Println("锁操作失败:", err)
        return
    }
    
    if locked {
        fmt.Println("成功获取锁")
        // 执行需要加锁的操作...
        
        // 操作完成后释放锁
        helper.Unlock("my-lock")
        fmt.Println("已释放锁")
    } else {
        fmt.Println("无法获取锁，锁已被其他进程持有")
    }
    
    // 更简洁的写法：使用WithLock方法
    err = helper.WithLock("another-lock", 10*time.Second, func() error {
        // 在这里执行需要加锁的操作
        fmt.Println("在锁保护下执行操作")
        time.Sleep(100 * time.Millisecond)
        return nil // 如果返回错误，会被传递出去
    })
    if err != nil {
        fmt.Println("带锁操作失败:", err)
    }
    
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
    
    // ------- 6. 带Context的操作 -------
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    
    // 所有方法都有Ctx后缀版本，支持超时控制
    helper.SetJSONCtx(ctx, "ctx-user", user, time.Minute)
    
    var ctxUser User
    err = helper.GetJSONCtx(ctx, "ctx-user", &ctxUser)
    if err != nil {
        fmt.Println("使用Context获取JSON失败:", err)
    } else {
        fmt.Println("使用Context获取到用户:", ctxUser.Name)
    }
    
    // ------- 7. 获取或设置 -------
    // 如果键存在则获取，不存在则设置默认值并返回
    val, _ := helper.GetOrSet("default-key", "默认值", time.Hour)
    fmt.Println("GetOrSet结果:", val)
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

## 常见问题

### 1. Redis连接问题

如果遇到Redis连接问题，请检查：

- **连接配置**：确保IP、端口和密码正确
  ```go
  redisConfig := cache.NewRedisConfig()
  redisConfig.Host = "redis-server"  // 检查主机名是否正确
  redisConfig.Port = 6379            // 检查端口是否正确
  redisConfig.Password = "password"  // 检查密码是否正确
  ```

- **网络问题**：
  - 检查Redis服务是否启动
  - 检查防火墙设置
  - 尝试增加连接超时时间：`redisConfig.Timeout = 10`

- **连接池配置**：高负载下可能需要调整连接池
  ```go
  redisConfig.PoolSize = 50      // 增加连接池大小
  redisConfig.MinIdleConns = 10  // 设置最小空闲连接
  ```

- **调试连接问题**：
  ```go
  // 使用自定义日志，设置Debug级别，观察详细连接信息
  logger := logrus.New()
  logger.SetLevel(logrus.DebugLevel)
  redisCache, err := cache.NewRedisCache(redisConfig, logger)
  ```

### 2. 文件缓存权限问题

如果遇到文件缓存创建失败，请确保：

- **应用有写入权限**：确保应用对缓存目录有读写权限
- **路径存在**：
  ```go
  // 确保路径存在
  fileConfig := cache.NewFileConfig()
  fileConfig.FilePath = "/var/cache/myapp"  // 确保此目录存在且有权限
  ```
- **磁盘空间**：确保磁盘空间充足

### 3. 内存缓存过大

内存缓存默认不限制大小，只有过期的键会被清理。为避免内存过度使用：

- **设置LRU限制**：
  ```go
  memConfig := cache.NewMemoryConfig()
  memConfig.MaxEntries = 10000     // 最多存储10000个键
  memConfig.MaxMemoryMB = 100      // 最多使用100MB内存
  ```

- **合理设置TTL**：为键设置合适的过期时间
  ```go
  // 避免使用0（永不过期）
  cache.Set("key", "value", 24*time.Hour)  // 24小时后过期
  ```

- **定期清理**：对于大型应用，考虑定期手动清理不需要的键
  ```go
  // 清理特定前缀的键
  keys, _ := cache.Keys("temp:*")
  if len(keys) > 0 {
      cache.Del(keys...)
  }
  ```

## 性能优化建议

### 1. 选择合适的缓存类型

- **内存缓存**：适用于高频访问、低延迟要求的场景
  - 优点：速度最快，无网络开销
  - 缺点：不持久化，重启后数据丢失，单机存储有限

- **文件缓存**：适用于需要持久化但不需要共享的场景
  - 优点：数据持久化，无外部依赖
  - 缺点：读写速度较内存慢，不适合分布式

- **Redis缓存**：适用于需要数据共享的分布式场景
  - 优点：支持分布式，功能丰富
  - 缺点：依赖外部服务，有网络开销

### 2. 批量操作优化

对于多键操作，尽量使用批量方法以减少开销：

```go
// 不推荐 - 多次单键操作
for i := 0; i < 100; i++ {
    cache.Get(fmt.Sprintf("key%d", i))
}

// 推荐 - 使用批量操作
keys := make([]string, 100)
for i := 0; i < 100; i++ {
    keys[i] = fmt.Sprintf("key%d", i)
}
helper := cache.NewCacheHelper(cache, nil, "")
values, _ := helper.BatchGet(keys)
```

### 3. 合理使用缓存助手

利用缓存助手简化开发并提高性能：

```go
// 使用记忆模式避免重复计算
result, _ := helper.Remember("expensive-key", time.Hour, func() (interface{}, error) {
    // 耗时操作，如数据库查询、复杂计算等
    return expensiveOperation()
})
```

### 4. 优化键设计

- **避免过长的键名**：长键名会增加内存使用并可能影响性能
- **使用前缀进行分类**：`user:1:profile`, `user:1:settings`
- **避免热点键**：对于高频访问的数据，考虑分片或使用本地缓存

### 5. 内存管理

对于内存缓存，合理控制内存使用：

```go
// 设置LRU参数
config := cache.NewMemoryConfig()
config.MaxEntries = 100000  // 最多10万个键
config.MaxMemoryMB = 500    // 最多使用500MB内存
```

### 6. 使用压缩

对于大体积数据，文件缓存会自动压缩，但应注意：

- 压缩有CPU开销，只适合大于4KB的数据
- 压缩可以显著减少磁盘和网络开销

## 更多功能

更多高级功能和完整API文档，请参考以下源代码：

- `interface.go`: 所有缓存接口定义
- `memory.go`: 内存缓存实现
- `file.go`: 文件缓存实现
- `redis.go`: Redis缓存实现
- `helper.go`: 缓存助手实现
- `warmup.go`: 缓存预热相关功能

## 贡献指南

欢迎提交Issue和Pull Request，一起改进这个项目！

- 代码贡献前请先创建Issue讨论
- 所有代码需通过单元测试
- 遵循Go语言规范和项目代码风格

## 许可证

本项目采用MIT许可证。详细信息请参见LICENSE文件。
