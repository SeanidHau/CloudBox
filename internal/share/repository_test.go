package share

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/005_folders.sql",
		"../../migrations/007_file_shares.sql",
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'user-1', 'hash-1')`); err != nil {
		t.Fatalf("insert user 1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (2, 'user-2', 'hash-2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	return NewRepository(db)
}

func createTestFile(t *testing.T, repo *Repository, userID int64, status string) int64 {
	t.Helper()

	result, err := repo.db.Exec(
		`INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status) VALUES (?, ?, ?, ?, ?, ?)`,
		userID,
		"document.txt",
		"uploads/document.txt",
		15,
		"text/plain",
		status,
	)
	if err != nil {
		t.Fatalf("insert user file: %v", err)
	}

	fileID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get user file ID: %v", err)
	}

	// Tests insert user_files directly, so they must also create the object link
	// that normal uploads populate through the file service.
	objectResult, err := repo.db.Exec(
		`INSERT INTO file_objects (file_hash, storage_path, size, content_type, reference_count) VALUES (?, ?, ?, ?, ?)`,
		"share-test-object-"+strconv.FormatInt(fileID, 10),
		"uploads/document.txt",
		15,
		"text/plain",
		1,
	)
	if err != nil {
		t.Fatalf("insert file object: %v", err)
	}

	objectID, err := objectResult.LastInsertId()
	if err != nil {
		t.Fatalf("get file object ID: %v", err)
	}
	if _, err := repo.db.Exec(`UPDATE user_files SET object_id = ? WHERE id = ?`, objectID, fileID); err != nil {
		t.Fatalf("link user file to object: %v", err)
	}

	return fileID
}

func TestRepositoryCreateAndFindShare(t *testing.T) {
	repo := newTestRepository(t)
	fileID := createTestFile(t, repo, 1, "active")
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	maxDownloads := int64(3)

	created, err := repo.Create(&Share{
		Token:        "share-token",
		UserFileID:   fileID,
		PasswordHash: "password-hash",
		ExpiresAt:    &expiresAt,
		MaxDownloads: &maxDownloads,
	})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if created.Token != "share-token" || created.UserFileID != fileID {
		t.Fatalf("created share = %#v, want token and file ID", created)
	}
	if created.PasswordHash != "password-hash" {
		t.Fatalf("password hash = %q, want password-hash", created.PasswordHash)
	}
	if created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expires at = %v, want %v", created.ExpiresAt, expiresAt)
	}
	if created.MaxDownloads == nil || *created.MaxDownloads != maxDownloads {
		t.Fatalf("max downloads = %v, want %d", created.MaxDownloads, maxDownloads)
	}
	if created.DownloadCount != 0 {
		t.Fatalf("download count = %d, want 0", created.DownloadCount)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("created at should be set")
	}

	found, err := repo.FindByToken("share-token")
	if err != nil {
		t.Fatalf("find share: %v", err)
	}
	if found.Token != created.Token || found.UserFileID != created.UserFileID {
		t.Fatalf("found share = %#v, want %#v", found, created)
	}

	if _, err := repo.FindByToken("missing-token"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("missing share error = %v, want %v", err, ErrShareNotFound)
	}
}

func TestRepositoryCreateShareWithoutOptionalValues(t *testing.T) {
	repo := newTestRepository(t)
	fileID := createTestFile(t, repo, 1, "active")

	created, err := repo.Create(&Share{
		Token:      "permanent-share",
		UserFileID: fileID,
	})
	if err != nil {
		t.Fatalf("create share without optional values: %v", err)
	}

	// NULL in SQLite should be represented by nil in Go, not a zero-value time or limit.
	if created.ExpiresAt != nil {
		t.Fatalf("expires at = %v, want nil", created.ExpiresAt)
	}
	if created.MaxDownloads != nil {
		t.Fatalf("max downloads = %v, want nil", created.MaxDownloads)
	}
	if created.PasswordHash != "" {
		t.Fatalf("password hash = %q, want empty", created.PasswordHash)
	}
}

func TestRepositoryHasActiveFile(t *testing.T) {
	repo := newTestRepository(t)
	activeFileID := createTestFile(t, repo, 1, "active")
	deletedFileID := createTestFile(t, repo, 1, "deleted")
	otherUserFileID := createTestFile(t, repo, 2, "active")

	tests := []struct {
		name   string
		userID int64
		fileID int64
		want   bool
	}{
		{name: "owner active file", userID: 1, fileID: activeFileID, want: true},
		{name: "owner deleted file", userID: 1, fileID: deletedFileID, want: false},
		{name: "other user file", userID: 1, fileID: otherUserFileID, want: false},
		{name: "missing file", userID: 1, fileID: 999, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := repo.HasActiveFile(test.userID, test.fileID)
			if err != nil {
				t.Fatalf("check active file: %v", err)
			}
			if got != test.want {
				t.Fatalf("has active file = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRepositoryFindActiveFileByShareToken(t *testing.T) {
	repo := newTestRepository(t)
	fileID := createTestFile(t, repo, 1, "active")

	if _, err := repo.Create(&Share{
		Token:      "active-file-share",
		UserFileID: fileID,
	}); err != nil {
		t.Fatalf("create share: %v", err)
	}

	file, err := repo.FindActiveFileByShareToken("active-file-share")
	if err != nil {
		t.Fatalf("find active shared file: %v", err)
	}
	if file.ID != fileID || file.OriginalName != "document.txt" {
		t.Fatalf("shared file = %#v, want document.txt", file)
	}

	if _, err := repo.db.Exec(`UPDATE user_files SET status = 'deleted' WHERE id = ?`, fileID); err != nil {
		t.Fatalf("delete source file: %v", err)
	}
	if _, err := repo.FindActiveFileByShareToken("active-file-share"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("deleted source file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestRepositoryReserveDownload(t *testing.T) {
	repo := newTestRepository(t)
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(2)

	if _, err := repo.Create(&Share{
		Token:        "limited-share",
		UserFileID:   fileID,
		MaxDownloads: &maxDownloads,
	}); err != nil {
		t.Fatalf("create limited share: %v", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		reserved, err := repo.ReserveDownload("limited-share")
		if err != nil {
			t.Fatalf("reserve download %d: %v", attempt, err)
		}
		if !reserved {
			t.Fatalf("reserve download %d = false, want true", attempt)
		}
	}

	reserved, err := repo.ReserveDownload("limited-share")
	if err != nil {
		t.Fatalf("reserve over-limit download: %v", err)
	}
	if reserved {
		t.Fatal("reserve over-limit download = true, want false")
	}

	share, err := repo.FindByToken("limited-share")
	if err != nil {
		t.Fatalf("find limited share: %v", err)
	}
	if share.DownloadCount != maxDownloads {
		t.Fatalf("download count = %d, want %d", share.DownloadCount, maxDownloads)
	}

	expiredAt := time.Now().UTC().Add(-time.Hour)
	if _, err := repo.Create(&Share{
		Token:      "expired-share",
		UserFileID: fileID,
		ExpiresAt:  &expiredAt,
	}); err != nil {
		t.Fatalf("create expired share: %v", err)
	}

	reserved, err = repo.ReserveDownload("expired-share")
	if err != nil {
		t.Fatalf("reserve expired download: %v", err)
	}
	if reserved {
		t.Fatal("reserve expired download = true, want false")
	}
}

func TestRepositoryListAndDeleteSharesByUser(t *testing.T) {
	repo := newTestRepository(t)
	userOneFileID := createTestFile(t, repo, 1, "active")
	userTwoFileID := createTestFile(t, repo, 2, "active")

	for _, share := range []Share{
		{Token: "user-one-first", UserFileID: userOneFileID},
		{Token: "user-one-second", UserFileID: userOneFileID},
		{Token: "user-two-share", UserFileID: userTwoFileID},
	} {
		if _, err := repo.Create(&share); err != nil {
			t.Fatalf("create share %q: %v", share.Token, err)
		}
	}

	userOneShares, err := repo.ListByUser(1)
	if err != nil {
		t.Fatalf("list user one shares: %v", err)
	}
	if len(userOneShares) != 2 {
		t.Fatalf("user one share count = %d, want 2", len(userOneShares))
	}
	for _, share := range userOneShares {
		if share.Token == "user-two-share" {
			t.Fatal("user one should not see user two share")
		}
	}

	if err := repo.DeleteByToken(1, "user-two-share"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("delete other user share error = %v, want %v", err, ErrShareNotFound)
	}
	if _, err := repo.FindByToken("user-two-share"); err != nil {
		t.Fatalf("other user share should remain: %v", err)
	}

	if err := repo.DeleteByToken(1, "user-one-first"); err != nil {
		t.Fatalf("delete own share: %v", err)
	}
	if _, err := repo.FindByToken("user-one-first"); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("deleted share error = %v, want %v", err, ErrShareNotFound)
	}
}
