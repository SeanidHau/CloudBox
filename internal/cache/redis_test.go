package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStorageUsageCacheSetGetDelete(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	cache := NewRedisStorageUsageCache(client)

	usedBytes, found, err := cache.Get(42)
	if err != nil {
		t.Fatalf("get missing value: %v", err)
	}
	if found || usedBytes != 0 {
		t.Fatalf("missing value = (%d, %t), want (0, false)", usedBytes, found)
	}

	if err := cache.Set(42, 1234, time.Minute); err != nil {
		t.Fatalf("set value: %v", err)
	}

	usedBytes, found, err = cache.Get(42)
	if err != nil {
		t.Fatalf("get stored value: %v", err)
	}
	if !found || usedBytes != 1234 {
		t.Fatalf("stored value = (%d, %t), want (1234, true)", usedBytes, found)
	}

	if err := cache.Delete(42); err != nil {
		t.Fatalf("delete value: %v", err)
	}

	usedBytes, found, err = cache.Get(42)
	if err != nil {
		t.Fatalf("get deleted value: %v", err)
	}
	if found || usedBytes != 0 {
		t.Fatalf("deleted value = (%d, %t), want (0, false)", usedBytes, found)
	}
}

func TestRedisStorageUsageCacheExpiresValue(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	cache := NewRedisStorageUsageCache(client)
	if err := cache.Set(7, 99, time.Second); err != nil {
		t.Fatalf("set value: %v", err)
	}

	// 推进内存 Redis 的时间，验证 TTL 真正控制缓存生命周期。
	server.FastForward(time.Second)

	usedBytes, found, err := cache.Get(7)
	if err != nil {
		t.Fatalf("get expired value: %v", err)
	}
	if found || usedBytes != 0 {
		t.Fatalf("expired value = (%d, %t), want (0, false)", usedBytes, found)
	}
}

func TestRedisStorageUsageCacheRejectsInvalidCachedValue(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})

	cache := NewRedisStorageUsageCache(client)
	if err := client.Set(t.Context(), storageUsageKey(3), "not-a-number", time.Minute).Err(); err != nil {
		t.Fatalf("seed invalid value: %v", err)
	}

	usedBytes, found, err := cache.Get(3)
	if err == nil {
		t.Fatal("get invalid value error = nil, want error")
	}
	if found || usedBytes != 0 {
		t.Fatalf("invalid value = (%d, %t), want (0, false)", usedBytes, found)
	}
}
