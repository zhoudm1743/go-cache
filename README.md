# go-cache

GO语言缓存库，支持内存、文件、Redis三种实现方式，提供统一接口和丰富的数据结构操作。

## 特性

- **多种缓存实现**：内存缓存、文件缓存（基于BoltDB）和Redis缓存
- **丰富的数据结构**：字符串、哈希表、列表、集合、有序集合
- **统一接口**：所有缓存实现都遵循相同的接口，可以无缝替换
- **上下文支持**：全方法支持Context，可以进行超时控制和取消操作
- **高性能优化**：内存缓存层、压缩存储、批量操作等
- **清晰的分层架构**：不同缓存类型使用专用配置，结构清晰
- **默认日志实现**：自带logrus日志实现，也支持自定义日志接口

## 安装

```bash
go get github.com/zhoudm1743/go-cache
```

## 导入说明

本库的模块路径为 `github.com/zhoudm1743/go-cache`，但包名为 `cache`，所以在使用时需要这样导入：

```go
import "github.com/zhoudm1743/go-cache"

// 使用时以 cache 作为包名前缀
cache.NewMemoryConfig()
```

## 完整示例

### 1. 内存缓存

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache" // 导入路径
)

func main() {
    // 创建内存缓存配置
    memoryConfig := cache.NewMemoryConfig() // 使用包名cache
    memoryConfig.Prefix = "mem:"
    
    // 创建内存缓存（使用默认日志）
    memoryCache, err := cache.NewMemoryCache(memoryConfig, nil)
    if err != nil {
        fmt.Println("初始化内存缓存失败:", err)
        return
    }
    defer memoryCache.Close()
    
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
}
```

### 2. Redis缓存

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
    
    // 创建Redis缓存
    redisCache, err := cache.NewRedisCache(redisConfig, nil)
    if err != nil {
        fmt.Println("初始化Redis缓存失败:", err)
        return
    }
    defer redisCache.Close()
    
    // 设置和获取缓存
    redisCache.Set("hello", "world", 10*time.Second)
    val, _ := redisCache.Get("hello")
    fmt.Println("Redis缓存值:", val)
    
    // 使用哈希表
    redisCache.HSet("user:1", "name", "张三", "age", 25, "city", "北京")
    name, _ := redisCache.HGet("user:1", "name")
    fmt.Println("用户名:", name)
    
    // 获取所有字段
    fields, _ := redisCache.HGetAll("user:1")
    fmt.Printf("所有字段: %+v\n", fields)
}
```

### 3. 文件缓存

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
    fileConfig.Prefix = "file:"
    fileConfig.FilePath = "./cache_data" // 缓存文件存储路径
    
    // 创建文件缓存
    fileCache, err := cache.NewFileCache(fileConfig, nil)
    if err != nil {
        fmt.Println("初始化文件缓存失败:", err)
        return
    }
    defer fileCache.Close()
    
    // 设置缓存
    fileCache.Set("persistent", "这是持久化的数据", 24*time.Hour)
    
    // 获取缓存
    val, err := fileCache.Get("persistent")
    if err != nil {
        fmt.Println("获取文件缓存失败:", err)
    } else {
        fmt.Println("文件缓存值:", val)
    }
    
    // 使用列表操作
    fileCache.RPush("queue", "任务1", "任务2", "任务3")
    tasks, _ := fileCache.LRange("queue", 0, -1)
    fmt.Println("所有任务:", tasks)
    
    // 弹出一个任务
    task, _ := fileCache.LPop("queue")
    fmt.Println("处理任务:", task)
}
```

### 4. 使用缓存助手

缓存助手提供了更高级的操作，如JSON序列化和记忆模式：

```go
package main

import (
    "fmt"
    "time"
    
    "github.com/zhoudm1743/go-cache"
)

func main() {
    // 先创建一个缓存实例（这里使用内存缓存）
    memConfig := cache.NewMemoryConfig()
    memCache, _ := cache.NewMemoryCache(memConfig, nil)
    defer memCache.Close()
    
    // 创建缓存助手（使用默认日志）
    helper := cache.NewCacheHelper(memCache, nil, "helper:")
    
    // JSON对象操作
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
    
    // 使用记忆模式（如果缓存不存在，执行函数并缓存结果）
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
    
    // 使用RememberJSON处理对象
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
// 左侧添加元素
cache.LPush("logs", "日志1", "日志2")

// 右侧添加元素
cache.RPush("logs", "日志3", "日志4")

// 左侧弹出元素
item, _ := cache.LPop("logs")

// 右侧弹出元素
item, _ = cache.RPop("logs")

// 获取列表长度
length, _ := cache.LLen("logs")

// 获取列表范围
items, _ := cache.LRange("logs", 0, -1) // 获取所有元素
```

### 3. 集合操作

```go
// 添加元素到集合
cache.SAdd("tags", "php", "python", "golang")

// 检查元素是否存在
exists, _ := cache.SIsMember("tags", "golang")

// 获取所有元素
members, _ := cache.SMembers("tags")

// 删除元素
cache.SRem("tags", "php")

// 获取集合大小
size, _ := cache.SCard("tags")
```

### 4. 有序集合操作

```go
// 添加成员及分数
cache.ZAdd("ranking", 
    cache.Z{Score: 100, Member: "user1"},
    cache.Z{Score: 85.5, Member: "user2"},
    cache.Z{Score: 95, Member: "user3"})

// 获取成员排名
members, _ := cache.ZRange("ranking", 0, 2) // 前三名

// 获取成员和分数
scoresWithMembers, _ := cache.ZRangeWithScores("ranking", 0, -1)

// 获取成员分数
score, _ := cache.ZScore("ranking", "user1")

// 删除成员
cache.ZRem("ranking", "user2")
```

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

// 实现所有必需的Logger接口方法
func (l *MyLogger) Info(args ...interface{}) {
    l.logger.Println("INFO:", args)
}

func (l *MyLogger) Error(args ...interface{}) {
    l.logger.Println("ERROR:", args)
}

// 实现其他必需的方法...
func (l *MyLogger) Debug(args ...interface{}) { /* ... */ }
func (l *MyLogger) Warn(args ...interface{}) { /* ... */ }
func (l *MyLogger) Fatal(args ...interface{}) { /* ... */ }
func (l *MyLogger) Debugf(format string, args ...interface{}) { /* ... */ }
func (l *MyLogger) Infof(format string, args ...interface{}) { /* ... */ }
func (l *MyLogger) Warnf(format string, args ...interface{}) { /* ... */ }
func (l *MyLogger) Errorf(format string, args ...interface{}) { /* ... */ }
func (l *MyLogger) Fatalf(format string, args ...interface{}) { /* ... */ }
func (l *MyLogger) WithFields(fields map[string]interface{}) cache.Logger {
    return l // 简单实现，忽略字段
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

## 常见问题

### 1. Redis连接问题

如果遇到Redis连接问题，请检查：
- Redis服务是否启动
- IP和端口是否正确
- 密码是否正确
- 防火墙设置

### 2. 文件缓存权限问题

如果遇到文件缓存创建失败，请确保：
- 应用有写入权限
- 路径存在或可创建
- 磁盘空间充足

### 3. 内存缓存过大

内存缓存没有自动清理机制，只有过期的键会被清理。如需限制内存使用，可以：
- 适当设置过期时间
- 定期手动清理不需要的键
- 考虑使用文件或Redis缓存

## 性能优化建议

1. **合理选择缓存类型**
   - 读多写少且不需要持久化：内存缓存
   - 需要持久化但不需要共享：文件缓存
   - 需要多实例共享：Redis缓存

2. **批量操作**
   - 对于多键操作，使用批量方法如`MGet`、`MSet`

3. **避免大值存储**
   - 大值（超过1MB）应考虑压缩或分片存储

4. **合理设置TTL**
   - 设置合适的过期时间避免缓存过大

## 更多功能

更多高级功能和完整API文档，请参考项目源代码。

## 贡献指南

欢迎提交Issue和Pull Request，一起改进这个项目！

## 许可证

本项目采用MIT许可证。详细信息请参见LICENSE文件。
