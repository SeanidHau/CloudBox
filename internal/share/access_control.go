package share

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DownloadRateLimit    = 20
	DownloadRateWindow   = time.Minute
	PasswordFailureLimit = 5
	PasswordLockDuration = 10 * time.Minute
)

type AccessAction string

const (
	AccessInfo     AccessAction = "info"
	AccessPreview  AccessAction = "preview"
	AccessDownload AccessAction = "download"
	AccessSave     AccessAction = "save"
)

type AccessResult string

const (
	AccessAllowed     AccessResult = "allowed"
	AccessDenied      AccessResult = "denied"
	AccessRateLimited AccessResult = "rate_limited"
	AccessLocked      AccessResult = "locked"
)

type AccessAudit struct {
	Token  string
	IPHash string
	Action AccessAction
	Result AccessResult
}

type AccessAuditor interface {
	RecordShareAccess(AccessAudit) error
}

// AccessController stores the short-lived public-link controls. The database
// remains responsible for durable share data and the access audit trail.
type AccessController interface {
	AllowDownload(token string, ipHash string) (bool, error)
	PasswordLocked(token string, ipHash string) (bool, error)
	RecordPasswordFailure(token string, ipHash string) error
	ClearPasswordFailures(token string, ipHash string) error
}

type accessKey struct {
	token  string
	ipHash string
}

type downloadWindow struct {
	startedAt time.Time
	count     int
}

type passwordFailures struct {
	count     int
	lockedTil time.Time
}

// AccessControl tracks only short-lived anonymous request state. The audit
// store receives a one-way IP hash rather than an address.
type AccessControl struct {
	now      func() time.Time
	mu       sync.Mutex
	download map[accessKey]downloadWindow
	password map[accessKey]passwordFailures
}

func NewAccessControl() *AccessControl {
	return &AccessControl{
		now:      time.Now,
		download: make(map[accessKey]downloadWindow),
		password: make(map[accessKey]passwordFailures),
	}
}

func HashIP(ip string) string {
	sum := sha256.Sum256([]byte(ip))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *AccessControl) AllowDownload(token string, ipHash string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := accessKey{token: token, ipHash: ipHash}
	window := c.download[key]
	if window.startedAt.IsZero() || now.Sub(window.startedAt) >= DownloadRateWindow {
		c.download[key] = downloadWindow{startedAt: now, count: 1}
		return true, nil
	}
	if window.count >= DownloadRateLimit {
		return false, nil
	}
	window.count++
	c.download[key] = window
	return true, nil
}

func (c *AccessControl) PasswordLocked(token string, ipHash string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.password[accessKey{token: token, ipHash: ipHash}]
	return entry.lockedTil.After(c.now()), nil
}

func (c *AccessControl) RecordPasswordFailure(token string, ipHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	key := accessKey{token: token, ipHash: ipHash}
	entry := c.password[key]
	if entry.lockedTil.After(now) {
		return nil
	}
	entry.count++
	if entry.count >= PasswordFailureLimit {
		entry.count = 0
		entry.lockedTil = now.Add(PasswordLockDuration)
	}
	c.password[key] = entry
	return nil
}

func (c *AccessControl) ClearPasswordFailures(token string, ipHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.password, accessKey{token: token, ipHash: ipHash})
	return nil
}

const redisShareAccessKeyPrefix = "cloudbox:share-access:"

// RedisAccessControl makes password locking and rate limiting consistent when
// several CloudBox API instances serve the same public share link.
type RedisAccessControl struct {
	client redis.Cmdable
}

func NewRedisAccessControl(client redis.Cmdable) *RedisAccessControl {
	return &RedisAccessControl{client: client}
}

var redisAllowDownload = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
if count == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[1])
end
if count > tonumber(ARGV[2]) then
  return 0
end
return 1
`)

var redisRecordPasswordFailure = redis.NewScript(`
if redis.call("EXISTS", KEYS[2]) == 1 then
  return 1
end
local failures = redis.call("INCR", KEYS[1])
if failures == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[1])
end
if failures >= tonumber(ARGV[2]) then
  redis.call("SET", KEYS[2], "1", "EX", ARGV[3])
  redis.call("DEL", KEYS[1])
  return 1
end
return 0
`)

func (c *RedisAccessControl) AllowDownload(token string, ipHash string) (bool, error) {
	result, err := redisAllowDownload.Run(
		context.Background(),
		c.client,
		[]string{redisDownloadKey(token, ipHash)},
		int(DownloadRateWindow/time.Second),
		DownloadRateLimit,
	).Int()
	if err != nil {
		return false, fmt.Errorf("increment share download rate: %w", err)
	}
	return result == 1, nil
}

func (c *RedisAccessControl) PasswordLocked(token string, ipHash string) (bool, error) {
	locked, err := c.client.Exists(context.Background(), redisPasswordLockKey(token, ipHash)).Result()
	if err != nil {
		return false, fmt.Errorf("check share password lock: %w", err)
	}
	return locked > 0, nil
}

func (c *RedisAccessControl) RecordPasswordFailure(token string, ipHash string) error {
	_, err := redisRecordPasswordFailure.Run(
		context.Background(),
		c.client,
		[]string{redisPasswordFailureKey(token, ipHash), redisPasswordLockKey(token, ipHash)},
		int(PasswordLockDuration/time.Second),
		PasswordFailureLimit,
		int(PasswordLockDuration/time.Second),
	).Int()
	if err != nil {
		return fmt.Errorf("record share password failure: %w", err)
	}
	return nil
}

func (c *RedisAccessControl) ClearPasswordFailures(token string, ipHash string) error {
	if err := c.client.Del(context.Background(), redisPasswordFailureKey(token, ipHash)).Err(); err != nil {
		return fmt.Errorf("clear share password failures: %w", err)
	}
	return nil
}

func redisDownloadKey(token string, ipHash string) string {
	return redisShareAccessKeyPrefix + "download:" + token + ":" + ipHash
}

func redisPasswordFailureKey(token string, ipHash string) string {
	return redisShareAccessKeyPrefix + "password-failures:" + token + ":" + ipHash
}

func redisPasswordLockKey(token string, ipHash string) string {
	return redisShareAccessKeyPrefix + "password-lock:" + token + ":" + ipHash
}
