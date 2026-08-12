package share

import (
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
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

func (c *AccessControl) AllowDownload(token string, ipHash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	key := accessKey{token: token, ipHash: ipHash}
	window := c.download[key]
	if window.startedAt.IsZero() || now.Sub(window.startedAt) >= DownloadRateWindow {
		c.download[key] = downloadWindow{startedAt: now, count: 1}
		return true
	}
	if window.count >= DownloadRateLimit {
		return false
	}
	window.count++
	c.download[key] = window
	return true
}

func (c *AccessControl) PasswordLocked(token string, ipHash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.password[accessKey{token: token, ipHash: ipHash}]
	return entry.lockedTil.After(c.now())
}

func (c *AccessControl) RecordPasswordFailure(token string, ipHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	key := accessKey{token: token, ipHash: ipHash}
	entry := c.password[key]
	if entry.lockedTil.After(now) {
		return
	}
	entry.count++
	if entry.count >= PasswordFailureLimit {
		entry.count = 0
		entry.lockedTil = now.Add(PasswordLockDuration)
	}
	c.password[key] = entry
}

func (c *AccessControl) ClearPasswordFailures(token string, ipHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.password, accessKey{token: token, ipHash: ipHash})
}
