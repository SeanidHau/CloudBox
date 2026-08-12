package share

import (
	"errors"
	"testing"
	"time"
)

func TestAccessControlLocksPasswordFailuresForTenMinutes(t *testing.T) {
	control := NewAccessControl()
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	control.now = func() time.Time { return now }
	for range PasswordFailureLimit {
		control.RecordPasswordFailure("share", "anonymous-ip")
	}
	if !control.PasswordLocked("share", "anonymous-ip") {
		t.Fatal("password attempts should be locked after five failures")
	}
	now = now.Add(PasswordLockDuration)
	if control.PasswordLocked("share", "anonymous-ip") {
		t.Fatal("password attempts should unlock after ten minutes")
	}
}

func TestAccessControlLimitsDownloadsPerIPWindow(t *testing.T) {
	control := NewAccessControl()
	for count := 0; count < DownloadRateLimit; count++ {
		if !control.AllowDownload("share", "anonymous-ip") {
			t.Fatalf("download %d should be allowed", count+1)
		}
	}
	if control.AllowDownload("share", "anonymous-ip") {
		t.Fatal("download above limit should be rejected")
	}
	if !control.AllowDownload("share", "another-ip") {
		t.Fatal("another IP should have an independent limit")
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
