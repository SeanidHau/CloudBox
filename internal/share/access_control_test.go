package share

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAccessControlLocksPasswordFailuresForTenMinutes(t *testing.T) {
	control := NewAccessControl()
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	control.now = func() time.Time { return now }
	for range PasswordFailureLimit {
		if err := control.RecordPasswordFailure("share", "anonymous-ip"); err != nil {
			t.Fatalf("record password failure: %v", err)
		}
	}
	locked, err := control.PasswordLocked("share", "anonymous-ip")
	if err != nil {
		t.Fatalf("check password lock: %v", err)
	}
	if !locked {
		t.Fatal("password attempts should be locked after five failures")
	}
	now = now.Add(PasswordLockDuration)
	locked, err = control.PasswordLocked("share", "anonymous-ip")
	if err != nil {
		t.Fatalf("check expired password lock: %v", err)
	}
	if locked {
		t.Fatal("password attempts should unlock after ten minutes")
	}
}

func TestAccessControlLimitsDownloadsPerIPWindow(t *testing.T) {
	control := NewAccessControl()
	for count := 0; count < DownloadRateLimit; count++ {
		allowed, err := control.AllowDownload("share", "anonymous-ip")
		if err != nil {
			t.Fatalf("allow download %d: %v", count+1, err)
		}
		if !allowed {
			t.Fatalf("download %d should be allowed", count+1)
		}
	}
	allowed, err := control.AllowDownload("share", "anonymous-ip")
	if err != nil {
		t.Fatalf("check rate limit: %v", err)
	}
	if allowed {
		t.Fatal("download above limit should be rejected")
	}
	allowed, err = control.AllowDownload("share", "another-ip")
	if err != nil {
		t.Fatalf("allow another IP: %v", err)
	}
	if !allowed {
		t.Fatal("another IP should have an independent limit")
	}
}

func TestRedisAccessControlSharesPasswordLockAndDownloadWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	first := NewRedisAccessControl(client)
	second := NewRedisAccessControl(client)
	for range PasswordFailureLimit {
		if err := first.RecordPasswordFailure("share", "anonymous-ip"); err != nil {
			t.Fatalf("record Redis password failure: %v", err)
		}
	}
	locked, err := second.PasswordLocked("share", "anonymous-ip")
	if err != nil {
		t.Fatalf("check Redis password lock: %v", err)
	}
	if !locked {
		t.Fatal("password lock should be shared by Redis-backed controllers")
	}

	for count := 0; count < DownloadRateLimit; count++ {
		allowed, err := first.AllowDownload("download-share", "anonymous-ip")
		if err != nil || !allowed {
			t.Fatalf("download %d = (%t, %v), want allowed", count+1, allowed, err)
		}
	}
	allowed, err := second.AllowDownload("download-share", "anonymous-ip")
	if err != nil {
		t.Fatalf("check shared Redis rate limit: %v", err)
	}
	if allowed {
		t.Fatal("download rate limit should be shared by Redis-backed controllers")
	}
}

func TestServiceRecordsHashedAnonymousAccess(t *testing.T) {
	repo := newTestRepository(t)
	storage := &fakeStorage{content: map[string][]byte{"uploads/document.txt": []byte("shared content")}}
	service := NewService(repo, storage)
	fileID := createTestFile(t, repo, 1, "active")
	created, err := service.Create(1, fileID, "secret", nil, nil)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ipHash := HashIP("203.0.113.25")

	for range PasswordFailureLimit {
		_, err := service.GetPublicFileFromIP(created.Token, "wrong", ipHash)
		if !errors.Is(err, ErrSharePasswordInvalid) {
			t.Fatalf("wrong password error = %v", err)
		}
	}
	if _, err := service.GetPublicFileFromIP(created.Token, "secret", ipHash); !errors.Is(err, ErrSharePasswordLocked) {
		t.Fatalf("locked password error = %v, want %v", err, ErrSharePasswordLocked)
	}

	rows, err := repo.db.Query(`SELECT ip_hash, result FROM share_access_audits WHERE token = $1 ORDER BY id`, created.Token)
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var storedHash, result string
		if err := rows.Scan(&storedHash, &result); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if storedHash != ipHash || storedHash == "203.0.113.25" {
			t.Fatalf("stored IP hash = %q", storedHash)
		}
		results = append(results, result)
	}
	if len(results) != PasswordFailureLimit+1 || results[len(results)-1] != string(AccessLocked) {
		t.Fatalf("audit results = %#v, want failures and locked result", results)
	}
}
