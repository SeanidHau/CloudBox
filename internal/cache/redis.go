package cache

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const storageUsageKeyPrefix = "cloudbox:storage-usage:"

type RedisStorageUsageCache struct {
	client redis.Cmdable
}

func NewRedisStorageUsageCache(client redis.Cmdable) *RedisStorageUsageCache {
	return &RedisStorageUsageCache{
		client: client,
	}
}

func (c *RedisStorageUsageCache) Get(userID int64) (int64, bool, error) {
	value, err := c.client.Get(context.Background(), storageUsageKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	usedBytes, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, err
	}

	return usedBytes, true, nil
}

func (c *RedisStorageUsageCache) Set(userID int64, usedBytes int64, ttl time.Duration) error {
	return c.client.Set(
		context.Background(),
		storageUsageKey(userID),
		usedBytes,
		ttl,
	).Err()
}

func (c *RedisStorageUsageCache) Delete(userID int64) error {
	return c.client.Del(context.Background(), storageUsageKey(userID)).Err()
}

func storageUsageKey(userID int64) string {
	return storageUsageKeyPrefix + strconv.FormatInt(userID, 10)
}
