# go-cache 测试套件

本目录包含 go-cache 项目的完整测试套件，用于验证各种缓存实现的正确性和性能。

## 测试组件

测试套件包含以下主要组件：

1. **基础缓存操作测试**：测试所有缓存实现的基本功能，如设置/获取值、过期、删除等。
2. **哈希表操作测试**：测试哈希表相关功能。
3. **列表操作测试**：针对支持列表操作的缓存实现（如Redis）进行测试。
4. **缓存防护测试**：测试布隆过滤器和缓存击穿/雪崩保护机制。
5. **分布式锁测试**：测试Redlock算法的实现。
6. **并发性能测试**：对比传统内存缓存和分段锁内存缓存在不同并发场景下的性能差异。

## 运行测试

### 运行所有测试

```bash
go test -v ./test
```

### 只运行特定测试

```bash
go test -v ./test -run TestMemoryCache  # 只测试内存缓存
go test -v ./test -run TestShardedMemoryCache  # 只测试分段锁内存缓存
go test -v ./test -run TestFileCache  # 只测试文件缓存
go test -v ./test -run TestRedisCache  # 只测试Redis缓存
go test -v ./test -run TestBloomFilter  # 只测试布隆过滤器
go test -v ./test -run TestRedLock  # 只测试分布式锁
```

### 运行基准测试

```bash
go test -v ./test -bench=BenchmarkMemoryCacheParallel -benchtime=5s
go test -v ./test -bench=BenchmarkShardedMemoryCacheParallel -benchtime=5s
```

### 运行性能比较测试

```bash
go test -v ./test -run TestCachePerformanceComparison
```

## 测试覆盖率

运行测试覆盖率分析：

```bash
go test -v ./test -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## 测试注意事项

1. **Redis测试**：Redis测试需要有可用的Redis实例，默认连接到localhost:6379。如果Redis不可用，相关测试会被自动跳过。
2. **并发测试**：并发测试可能需要较长时间才能完成，可以使用 `-short` 标志跳过这些测试：

```bash
go test -v ./test -short
```

## 测试结果分析

测试结果会显示各种缓存实现的功能正确性和性能特征。性能测试结果对比可以帮助开发者选择最适合特定场景的缓存策略：

- **读密集场景**：分段锁内存缓存通常会有显著性能优势
- **写密集场景**：分段锁内存缓存仍然优于传统内存缓存，但优势可能不如读密集场景明显
- **混合场景**：分段锁内存缓存在各种混合读写比例场景下都表现良好

## 测试覆盖的组件

- ✅ 内存缓存 (MemoryCache)
- ✅ 分段锁内存缓存 (ShardedMemoryCache)
- ✅ 文件缓存 (FileCache)
- ✅ Redis缓存 (RedisCache)
- ✅ 布隆过滤器 (BloomFilter)
- ✅ 缓存防护 (CacheProtection)
- ✅ 分布式锁 (RedLock)
- ✅ 并发性能 (Concurrency) 