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

// NewFileConfig 创建默认文件缓存配置
func NewFileConfig() *FileConfig {
	return &FileConfig{
		Config: Config{
			Prefix: "",
		},
		FilePath: "./cache",
	}
}
