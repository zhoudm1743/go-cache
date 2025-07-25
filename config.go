package cache

// Config 通用配置
type Config struct {
	// 通用配置
	Prefix string `json:"prefix"` // 键前缀
}

// MemoryConfig 内存缓存配置
type MemoryConfig struct {
	Config
}

// RedisConfig Redis缓存配置
type RedisConfig struct {
	Config
	Host     string `json:"host"`     // Redis主机
	Port     int    `json:"port"`     // Redis端口
	DB       int    `json:"db"`       // Redis数据库编号
	Password string `json:"password"` // Redis密码
	Timeout  int    `json:"timeout"`  // 超时时间（秒）
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
