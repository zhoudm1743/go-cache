package cache

import "time"

// Config 通用配置
type Config struct {
	// 通用配置
	Prefix string `json:"prefix"` // 键前缀
}

// MemoryConfig 内存缓存配置
type MemoryConfig struct {
	Config
	// LRU相关配置
	MaxEntries    int           `json:"max_entries"`    // 最大条目数，0表示不限制
	MaxMemoryMB   int           `json:"max_memory_mb"`  // 最大内存(MB)，0表示不限制
	CleanInterval time.Duration `json:"clean_interval"` // 清理间隔，默认5分钟
}

// RedisConfig Redis缓存配置
type RedisConfig struct {
	Config
	Host     string `json:"host"`     // Redis主机
	Port     int    `json:"port"`     // Redis端口
	DB       int    `json:"db"`       // Redis数据库编号
	Password string `json:"password"` // Redis密码
	Timeout  int    `json:"timeout"`  // 超时时间（秒）
	// 连接池配置
	PoolSize     int `json:"pool_size"`      // 连接池大小
	MinIdleConns int `json:"min_idle_conns"` // 最小空闲连接数
	MaxConnAge   int `json:"max_conn_age"`   // 连接最大存活时间(秒)
}

// RedisClusterConfig Redis集群配置
type RedisClusterConfig struct {
	Config
	Addrs    []string `json:"addrs"`    // Redis集群节点地址列表，格式为 host:port
	Password string   `json:"password"` // Redis密码
	Timeout  int      `json:"timeout"`  // 超时时间（秒）
	// 连接池配置
	PoolSize     int `json:"pool_size"`      // 连接池大小
	MinIdleConns int `json:"min_idle_conns"` // 最小空闲连接数
	MaxConnAge   int `json:"max_conn_age"`   // 连接最大存活时间(秒)
	// 集群特有配置
	MaxRedirects   int  `json:"max_redirects"`    // 最大重定向次数
	ReadOnly       bool `json:"read_only"`        // 是否只读模式
	RouteByLatency bool `json:"route_by_latency"` // 是否按延迟路由
	RouteRandomly  bool `json:"route_randomly"`   // 是否随机路由
}

// RedisSentinelConfig Redis哨兵配置
type RedisSentinelConfig struct {
	Config
	MasterName string   `json:"master_name"` // 主节点名称
	Addrs      []string `json:"addrs"`       // 哨兵节点地址列表
	Password   string   `json:"password"`    // Redis密码
	DB         int      `json:"db"`          // Redis数据库编号
	Timeout    int      `json:"timeout"`     // 超时时间（秒）
	// 连接池配置
	PoolSize     int `json:"pool_size"`      // 连接池大小
	MinIdleConns int `json:"min_idle_conns"` // 最小空闲连接数
	MaxConnAge   int `json:"max_conn_age"`   // 连接最大存活时间(秒)
}

// FileConfig 文件缓存配置
type FileConfig struct {
	Config
	FilePath string `json:"file_path"` // 文件缓存路径
}

// NewMemoryConfig 创建默认内存缓存配置
func NewMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		Config: Config{
			Prefix: "",
		},
		MaxEntries:    0,               // 默认不限制条目数
		MaxMemoryMB:   0,               // 默认不限制内存
		CleanInterval: 5 * time.Minute, // 默认5分钟清理一次
	}
}

// NewRedisConfig 创建默认Redis缓存配置
func NewRedisConfig() *RedisConfig {
	return &RedisConfig{
		Config: Config{
			Prefix: "",
		},
		Host:     "localhost",
		Port:     6379,
		DB:       0,
		Password: "",
		Timeout:  3,
		// 连接池默认配置
		PoolSize:     10,
		MinIdleConns: 5,
		MaxConnAge:   3600, // 1小时
	}
}

// NewRedisClusterConfig 创建默认Redis集群配置
func NewRedisClusterConfig() *RedisClusterConfig {
	return &RedisClusterConfig{
		Config: Config{
			Prefix: "",
		},
		Addrs:          []string{"localhost:6379"}, // 默认至少提供一个节点
		Password:       "",
		Timeout:        3,
		PoolSize:       10,
		MinIdleConns:   5,
		MaxConnAge:     3600, // 1小时
		MaxRedirects:   3,    // 最多重定向3次
		ReadOnly:       false,
		RouteByLatency: false,
		RouteRandomly:  false,
	}
}

// NewRedisSentinelConfig 创建默认Redis哨兵配置
func NewRedisSentinelConfig() *RedisSentinelConfig {
	return &RedisSentinelConfig{
		Config: Config{
			Prefix: "",
		},
		MasterName:   "mymaster",
		Addrs:        []string{"localhost:26379"}, // 默认至少提供一个哨兵节点
		Password:     "",
		DB:           0,
		Timeout:      3,
		PoolSize:     10,
		MinIdleConns: 5,
		MaxConnAge:   3600, // 1小时
	}
}

// NewFileConfig 创建默认文件缓存配置
func NewFileConfig() *FileConfig {
	return &FileConfig{
		Config: Config{
			Prefix: "",
		},
		FilePath: "./cache",
	}
}
