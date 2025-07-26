package test

import (
	"testing"
)

// TestMemoryCache 测试内存缓存
func TestMemoryCache(t *testing.T) {
	suite, cleanup := SetupMemoryCache(t)
	if suite != nil {
		t.Logf("运行内存缓存测试...")
		defer cleanup()
		RunTestsForCache(t, suite)
	}
}

// TestShardedMemoryCache 测试分段锁内存缓存
func TestShardedMemoryCache(t *testing.T) {
	suite, cleanup := SetupShardedMemoryCache(t)
	if suite != nil {
		t.Logf("运行分段锁内存缓存测试...")
		defer cleanup()
		RunTestsForCache(t, suite)
	}
}

// TestFileCache 测试文件缓存
func TestFileCache(t *testing.T) {
	suite, cleanup := SetupFileCache(t)
	if suite != nil {
		t.Logf("运行文件缓存测试...")
		defer cleanup()
		RunTestsForCache(t, suite)
	}
}

// TestRedisCache 测试Redis缓存
func TestRedisCache(t *testing.T) {
	suite, cleanup := SetupRedisCache(t)
	if suite != nil {
		t.Logf("运行Redis缓存测试...")
		defer cleanup()
		RunTestsForCache(t, suite)
	}
}

// TestAllCaches 运行所有缓存实现的测试
func TestAllCaches(t *testing.T) {
	suites := SetupAllCacheTypes(t)

	for _, suite := range suites {
		if suite != nil {
			t.Run(suite.Name, func(t *testing.T) {
				t.Logf("运行 %s 测试...", suite.Name)
				RunTestsForCache(t, suite)
			})
		}
	}
}
