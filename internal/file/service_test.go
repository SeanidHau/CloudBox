package file

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
)

type fakeStorage struct {
	savedPath    string
	savedContent string
	deletedPath  string
	saveErr      error
	openErr      error
	deleteErr    error
}

type fakeReadSeekCloser struct {
	*strings.Reader
}

type fakeStorageUsageCache struct {
	values      map[int64]int64
	getErr      error
	setErr      error
	deleteErr   error
	getCalls    int
	setCalls    int
	deleteCalls int
	lastTTL     time.Duration
}

const testStorageQuotaBytes int64 = 1 << 30

func (r fakeReadSeekCloser) Close() error {
	return nil
}

func (s *fakeStorage) Save(reader io.Reader, originalName string) (string, int64, string, error) {
	if s.saveErr != nil {
		return "", 0, "", s.saveErr
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", 0, "", err
	}

	s.savedContent = string(content)
	s.savedPath = "uploads/" + originalName

	hash := sha256.Sum256(content)
	return s.savedPath, int64(len(content)), hex.EncodeToString(hash[:]), nil
}

func (s *fakeStorage) Open(storagePath string) (io.ReadSeekCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	if storagePath != s.savedPath {
		return nil, errors.New("unexpected storage path")
	}

	return fakeReadSeekCloser{
		Reader: strings.NewReader(s.savedContent),
	}, nil
}

func (s *fakeStorage) Delete(storagePath string) error {
	s.deletedPath = storagePath
	return s.deleteErr
}

func (c *fakeStorageUsageCache) Get(userID int64) (int64, bool, error) {
	c.getCalls++
	if c.getErr != nil {
		return 0, false, c.getErr
	}

	usedBytes, found := c.values[userID]
	return usedBytes, found, nil
}

func (c *fakeStorageUsageCache) Set(userID int64, usedBytes int64, ttl time.Duration) error {
	c.setCalls++
	c.lastTTL = ttl
	if c.setErr != nil {
		return c.setErr
	}
	if c.values == nil {
		c.values = make(map[int64]int64)
	}

	c.values[userID] = usedBytes
	return nil
}

func (c *fakeStorageUsageCache) Delete(userID int64) error {
	c.deleteCalls++
	if c.deleteErr != nil {
		return c.deleteErr
	}

	delete(c.values, userID)
	return nil
}

func newTestServiceWithStorage(t *testing.T, storage Storage) *Service {
	return newTestServiceWithStorageQuota(t, storage, testStorageQuotaBytes)
}

func newTestServiceWithStorageQuota(t *testing.T, storage Storage, quotaBytes int64) *Service {
	return newTestServiceWithStorageQuotaAndOptions(t, storage, quotaBytes)
}

func newTestServiceWithStorageQuotaAndOptions(
	t *testing.T,
	storage Storage,
	quotaBytes int64,
	options ...ServiceOption,
) *Service {
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

	return NewService(NewRepository(db), storage, quotaBytes, options...)
}

func TestServiceUploadAndOpenForDownload(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "test.txt", "text/plain", strings.NewReader("hello cloudbox"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if uploaded.StoragePath != storage.savedPath {
		t.Fatalf("storage path = %q, want %q", uploaded.StoragePath, storage.savedPath)
	}
	if uploaded.Size != int64(len("hello cloudbox")) {
		t.Fatalf("size = %d, want %d", uploaded.Size, len("hello cloudbox"))
	}
	if uploaded.ParentID != nil {
		t.Fatalf("root file parent ID = %v, want nil", *uploaded.ParentID)
	}

	userFile, reader, err := service.OpenForDownload(1, uploaded.ID)
	if err != nil {
		t.Fatalf("open for download: %v", err)
	}
	defer reader.Close()

	if userFile.ID != uploaded.ID {
		t.Fatalf("download file id = %d, want %d", userFile.ID, uploaded.ID)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read downloaded content: %v", err)
	}
	if string(content) != "hello cloudbox" {
		t.Fatalf("downloaded content = %q, want %q", string(content), "hello cloudbox")
	}
}

func TestServiceGetStorageUsage(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	first, err := service.Upload(1, "first.txt", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("upload first file: %v", err)
	}
	if _, err := service.Upload(1, "second.txt", "text/plain", strings.NewReader("world!")); err != nil {
		t.Fatalf("upload second file: %v", err)
	}
	if err := service.SoftDelete(1, first.ID); err != nil {
		t.Fatalf("soft delete first file: %v", err)
	}

	usage, err := service.GetStorageUsage(1)
	if err != nil {
		t.Fatalf("get storage usage: %v", err)
	}
	if usage.UsedBytes != 11 {
		t.Fatalf("used bytes = %d, want 11", usage.UsedBytes)
	}
	if usage.QuotaBytes != testStorageQuotaBytes {
		t.Fatalf("quota bytes = %d, want %d", usage.QuotaBytes, testStorageQuotaBytes)
	}
	if usage.AvailableBytes != testStorageQuotaBytes-11 {
		t.Fatalf("available bytes = %d, want %d", usage.AvailableBytes, testStorageQuotaBytes-11)
	}
}

func TestServicePermanentlyDeleteRemovesLastObject(t *testing.T) {
	storage := &fakeStorage{}
	cache := &fakeStorageUsageCache{values: map[int64]int64{1: 7}}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithStorageUsageCache(cache, time.Minute),
	)

	file, err := service.Upload(1, "permanent.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	cache.deleteCalls = 0
	if err := service.PermanentlyDelete(1, file.ID); err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}
	if storage.deletedPath != file.StoragePath {
		t.Fatalf("deleted storage path = %q, want %q", storage.deletedPath, file.StoragePath)
	}
	if cache.deleteCalls != 1 {
		t.Fatalf("cache delete calls = %d, want 1", cache.deleteCalls)
	}
	if _, err := service.repo.FindDeletedByID(1, file.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("find permanently deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestServicePermanentlyDeleteSucceedsWhenStorageCleanupFails(t *testing.T) {
	storage := &fakeStorage{deleteErr: errors.New("storage is unavailable")}
	service := newTestServiceWithStorage(t, storage)

	file, err := service.Upload(1, "cleanup-error.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	if err := service.PermanentlyDelete(1, file.ID); err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}
	if _, err := service.repo.FindDeletedByID(1, file.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("find permanently deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestServiceGetStorageUsageUsesCache(t *testing.T) {
	cache := &fakeStorageUsageCache{
		values: map[int64]int64{1: 123},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithStorageUsageCache(cache, time.Minute),
	)

	usage, err := service.GetStorageUsage(1)
	if err != nil {
		t.Fatalf("get storage usage: %v", err)
	}
	if usage.UsedBytes != 123 {
		t.Fatalf("used bytes = %d, want cached value 123", usage.UsedBytes)
	}
	if cache.getCalls != 1 || cache.setCalls != 0 {
		t.Fatalf("cache calls = get %d, set %d, want get 1 and set 0", cache.getCalls, cache.setCalls)
	}
}

func TestServiceGetStorageUsageFallsBackToDatabase(t *testing.T) {
	cache := &fakeStorageUsageCache{
		getErr: errors.New("Redis is unavailable"),
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithStorageUsageCache(cache, 2*time.Minute),
	)

	if _, err := service.Upload(1, "report.txt", "text/plain", strings.NewReader("report")); err != nil {
		t.Fatalf("upload file: %v", err)
	}

	usage, err := service.GetStorageUsage(1)
	if err != nil {
		t.Fatalf("get storage usage: %v", err)
	}
	if usage.UsedBytes != int64(len("report")) {
		t.Fatalf("used bytes = %d, want %d", usage.UsedBytes, len("report"))
	}
	if cache.setCalls != 1 || cache.lastTTL != 2*time.Minute {
		t.Fatalf("cache set = %d with TTL %s, want once with TTL 2m0s", cache.setCalls, cache.lastTTL)
	}
}

func TestServiceUploadsInvalidateStorageUsageCache(t *testing.T) {
	cache := &fakeStorageUsageCache{
		values: map[int64]int64{1: 0},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithStorageUsageCache(cache, time.Minute),
	)

	if _, err := service.Upload(1, "first.txt", "text/plain", strings.NewReader("content")); err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if cache.deleteCalls != 1 {
		t.Fatalf("cache delete calls after upload = %d, want 1", cache.deleteCalls)
	}

	object, err := service.repo.CreateFileObject("instant-cache-hash", "uploads/shared.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create instant upload object: %v", err)
	}
	if _, err := service.InstantUpload(1, "copy.txt", object.FileHash); err != nil {
		t.Fatalf("instant upload: %v", err)
	}
	if cache.deleteCalls != 2 {
		t.Fatalf("cache delete calls after instant upload = %d, want 2", cache.deleteCalls)
	}
}

func TestServiceEnsureStorageQuotaUsesDatabaseInsteadOfCache(t *testing.T) {
	cache := &fakeStorageUsageCache{
		values: map[int64]int64{1: 0},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		5,
		WithStorageUsageCache(cache, time.Minute),
	)

	object, err := service.repo.CreateFileObject("quota-cache-hash", "uploads/full.txt", 5, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	if _, err := service.repo.CreateWithObject(1, "full.txt", object); err != nil {
		t.Fatalf("create user file: %v", err)
	}

	if err := service.EnsureStorageQuota(1, 1); !errors.Is(err, ErrStorageQuotaExceeded) {
		t.Fatalf("storage quota error = %v, want %v", err, ErrStorageQuotaExceeded)
	}
}

func TestServiceEnforcesStorageQuota(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorageQuota(t, storage, 5)

	if _, err := service.Upload(1, "first.txt", "text/plain", strings.NewReader("hello")); err != nil {
		t.Fatalf("upload within quota: %v", err)
	}
	if _, err := service.Upload(1, "second.txt", "text/plain", strings.NewReader("!")); !errors.Is(err, ErrStorageQuotaExceeded) {
		t.Fatalf("upload over quota error = %v, want %v", err, ErrStorageQuotaExceeded)
	}
	if storage.deletedPath != "uploads/second.txt" {
		t.Fatalf("rejected upload should be deleted, got %q", storage.deletedPath)
	}

	object, err := service.repo.CreateFileObject("instant-over-quota", "uploads/shared.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create instant upload object: %v", err)
	}
	if _, err := service.InstantUpload(1, "copy.txt", object.FileHash); !errors.Is(err, ErrStorageQuotaExceeded) {
		t.Fatalf("instant upload over quota error = %v, want %v", err, ErrStorageQuotaExceeded)
	}
}

func TestServiceUploadValidatesInput(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	if _, err := service.Upload(1, "", "text/plain", strings.NewReader("content")); !errors.Is(err, ErrOriginalNameRequired) {
		t.Fatalf("empty original name error = %v, want %v", err, ErrOriginalNameRequired)
	}

	if _, err := service.Upload(1, "test.txt", "text/plain", nil); !errors.Is(err, ErrContentRequired) {
		t.Fatalf("nil content error = %v, want %v", err, ErrContentRequired)
	}
}

func TestServiceUploadDeduplicatesFileObjects(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	first, err := service.Upload(1, "first.txt", "text/plain", strings.NewReader("same content"))
	if err != nil {
		t.Fatalf("upload first file: %v", err)
	}
	second, err := service.Upload(1, "second.txt", "text/plain", strings.NewReader("same content"))
	if err != nil {
		t.Fatalf("upload second file: %v", err)
	}

	var objectCount int
	if err := service.repo.db.QueryRow(`SELECT COUNT(*) FROM file_objects`).Scan(&objectCount); err != nil {
		t.Fatalf("count file objects: %v", err)
	}
	if objectCount != 1 {
		t.Fatalf("file object count = %d, want 1", objectCount)
	}

	var firstObjectID int64
	if err := service.repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = ?`, first.ID).Scan(&firstObjectID); err != nil {
		t.Fatalf("query first object ID: %v", err)
	}
	var secondObjectID int64
	if err := service.repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = ?`, second.ID).Scan(&secondObjectID); err != nil {
		t.Fatalf("query second object ID: %v", err)
	}
	if firstObjectID == 0 || firstObjectID != secondObjectID {
		t.Fatalf("object IDs = %d and %d, want one shared object", firstObjectID, secondObjectID)
	}

	var referenceCount int
	if err := service.repo.db.QueryRow(`SELECT reference_count FROM file_objects WHERE id = ?`, firstObjectID).Scan(&referenceCount); err != nil {
		t.Fatalf("query reference count: %v", err)
	}
	if referenceCount != 2 {
		t.Fatalf("reference count = %d, want 2", referenceCount)
	}

	if storage.deletedPath != "uploads/second.txt" {
		t.Fatalf("deleted path = %q, want %q", storage.deletedPath, "uploads/second.txt")
	}
}

func TestServiceUploadIntoFolder(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	folder, err := service.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	uploaded, err := service.UploadIntoFolder(
		1,
		&folder.ID,
		"report.txt",
		"text/plain",
		strings.NewReader("folder content"),
	)
	if err != nil {
		t.Fatalf("upload into folder: %v", err)
	}
	if uploaded.ParentID == nil || *uploaded.ParentID != folder.ID {
		t.Fatalf("file parent ID = %v, want %d", uploaded.ParentID, folder.ID)
	}

	otherUserFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}

	otherStorage := &fakeStorage{}
	otherService := &Service{repo: service.repo, storage: otherStorage}
	if _, err := otherService.UploadIntoFolder(
		1,
		&otherUserFolder.ID,
		"forbidden.txt",
		"text/plain",
		strings.NewReader("content"),
	); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user folder error = %v, want %v", err, ErrFolderNotFound)
	}
	if otherStorage.savedPath != "" {
		t.Fatalf("invalid folder should not save content, got %q", otherStorage.savedPath)
	}
}

func TestServiceListActiveInFolder(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	folder, err := service.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	rootFile, err := service.Upload(1, "root.txt", "text/plain", strings.NewReader("root"))
	if err != nil {
		t.Fatalf("upload root file: %v", err)
	}
	folderFile, err := service.UploadIntoFolder(1, &folder.ID, "report.txt", "text/plain", strings.NewReader("report"))
	if err != nil {
		t.Fatalf("upload folder file: %v", err)
	}

	rootFiles, err := service.ListActive(1)
	if err != nil {
		t.Fatalf("list root files: %v", err)
	}
	if len(rootFiles) != 1 || rootFiles[0].ID != rootFile.ID {
		t.Fatalf("root files = %#v, want root.txt", rootFiles)
	}

	folderFiles, err := service.ListActiveInFolder(1, &folder.ID)
	if err != nil {
		t.Fatalf("list folder files: %v", err)
	}
	if len(folderFiles) != 1 || folderFiles[0].ID != folderFile.ID {
		t.Fatalf("folder files = %#v, want report.txt", folderFiles)
	}

	otherFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if _, err := service.ListActiveInFolder(1, &otherFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user folder error = %v, want %v", err, ErrFolderNotFound)
	}
}

func TestServiceMoveActiveFile(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})
	folder, err := service.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	uploaded, err := service.Upload(1, "report.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	moved, err := service.MoveActive(1, uploaded.ID, &folder.ID)
	if err != nil {
		t.Fatalf("move file into folder: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != folder.ID {
		t.Fatalf("moved parent ID = %v, want %d", moved.ParentID, folder.ID)
	}

	movedToRoot, err := service.MoveActive(1, uploaded.ID, nil)
	if err != nil {
		t.Fatalf("move file to root: %v", err)
	}
	if movedToRoot.ParentID != nil {
		t.Fatalf("root parent ID = %v, want nil", movedToRoot.ParentID)
	}

	otherFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if _, err := service.MoveActive(1, uploaded.ID, &otherFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user folder error = %v, want %v", err, ErrFolderNotFound)
	}

	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	if _, err := service.MoveActive(1, uploaded.ID, &folder.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("move deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestServiceRenameActiveFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "draft.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	renamed, err := service.RenameActive(1, uploaded.ID, "  final.txt  ")
	if err != nil {
		t.Fatalf("rename file: %v", err)
	}
	if renamed.OriginalName != "final.txt" {
		t.Fatalf("original name = %q, want %q", renamed.OriginalName, "final.txt")
	}
	if renamed.StoragePath != uploaded.StoragePath {
		t.Fatalf("storage path = %q, want unchanged %q", renamed.StoragePath, uploaded.StoragePath)
	}

	if _, err := service.RenameActive(1, uploaded.ID, "   "); !errors.Is(err, ErrOriginalNameRequired) {
		t.Fatalf("empty name error = %v, want %v", err, ErrOriginalNameRequired)
	}

	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	if _, err := service.RenameActive(1, uploaded.ID, "deleted.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("rename deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestServiceInstantUpload(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	const fileHash = "a9c2a8c997d2a80c4756e14b6c80e7a5ed8f0262ba1e430ac0c0e751ea0b3abe"
	object, err := service.repo.CreateFileObject(fileHash, "uploads/original.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}

	created, err := service.InstantUpload(1, "instant-copy.txt", fileHash)
	if err != nil {
		t.Fatalf("instant upload: %v", err)
	}
	if created.OriginalName != "instant-copy.txt" {
		t.Fatalf("original name = %q, want %q", created.OriginalName, "instant-copy.txt")
	}
	if created.StoragePath != object.StoragePath {
		t.Fatalf("storage path = %q, want %q", created.StoragePath, object.StoragePath)
	}
	if storage.savedPath != "" {
		t.Fatalf("instant upload should not save a file, got %q", storage.savedPath)
	}

	var referenceCount int
	if err := service.repo.db.QueryRow(`SELECT reference_count FROM file_objects WHERE id = ?`, object.ID).Scan(&referenceCount); err != nil {
		t.Fatalf("query reference count: %v", err)
	}
	if referenceCount != 1 {
		t.Fatalf("reference count = %d, want 1", referenceCount)
	}

	if _, err := service.InstantUpload(1, "copy.txt", "missing-hash"); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("missing hash error = %v, want %v", err, ErrFileObjectNotFound)
	}
	if _, err := service.InstantUpload(1, "copy.txt", ""); !errors.Is(err, ErrFileHashRequired) {
		t.Fatalf("empty hash error = %v, want %v", err, ErrFileHashRequired)
	}
}

func TestServiceInstantUploadIntoFolder(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	object, err := service.repo.CreateFileObject(
		"instant-folder-hash",
		"uploads/original.txt",
		15,
		"text/plain",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	folder, err := service.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	created, err := service.InstantUploadIntoFolder(1, &folder.ID, "copy.txt", object.FileHash)
	if err != nil {
		t.Fatalf("instant upload into folder: %v", err)
	}
	if created.ParentID == nil || *created.ParentID != folder.ID {
		t.Fatalf("file parent ID = %v, want %d", created.ParentID, folder.ID)
	}
	if storage.savedPath != "" {
		t.Fatalf("instant upload should not save a file, got %q", storage.savedPath)
	}

	otherFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if _, err := service.InstantUploadIntoFolder(1, &otherFolder.ID, "forbidden.txt", object.FileHash); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user folder error = %v, want %v", err, ErrFolderNotFound)
	}
}

func TestServiceCreateAndListFolders(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	documents, err := service.CreateFolder(1, nil, " documents ")
	if err != nil {
		t.Fatalf("create root folder: %v", err)
	}
	if documents.Name != "documents" || documents.ParentID != nil {
		t.Fatalf("root folder = %#v, want trimmed root folder", documents)
	}

	photos, err := service.CreateFolder(1, &documents.ID, "photos")
	if err != nil {
		t.Fatalf("create child folder: %v", err)
	}
	if photos.ParentID == nil || *photos.ParentID != documents.ID {
		t.Fatalf("child folder parent = %v, want %d", photos.ParentID, documents.ID)
	}

	rootFolders, err := service.ListFolders(1, nil)
	if err != nil {
		t.Fatalf("list root folders: %v", err)
	}
	if len(rootFolders) != 1 || rootFolders[0].ID != documents.ID {
		t.Fatalf("root folders = %#v, want documents", rootFolders)
	}

	childFolders, err := service.ListFolders(1, &documents.ID)
	if err != nil {
		t.Fatalf("list child folders: %v", err)
	}
	if len(childFolders) != 1 || childFolders[0].ID != photos.ID {
		t.Fatalf("child folders = %#v, want photos", childFolders)
	}

	if _, err := service.CreateFolder(1, nil, "documents"); !errors.Is(err, ErrFolderAlreadyExists) {
		t.Fatalf("duplicate name error = %v, want %v", err, ErrFolderAlreadyExists)
	}
	if _, err := service.CreateFolder(1, nil, "   "); !errors.Is(err, ErrFolderNameRequired) {
		t.Fatalf("empty name error = %v, want %v", err, ErrFolderNameRequired)
	}

	otherUserFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if _, err := service.CreateFolder(1, &otherUserFolder.ID, "forbidden"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user parent error = %v, want %v", err, ErrFolderNotFound)
	}
	if _, err := service.ListFolders(1, &otherUserFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user list error = %v, want %v", err, ErrFolderNotFound)
	}

	music, err := service.CreateFolder(1, nil, "music")
	if err != nil {
		t.Fatalf("create second root folder: %v", err)
	}
	renamed, err := service.RenameFolder(1, documents.ID, "work")
	if err != nil {
		t.Fatalf("rename folder: %v", err)
	}
	if renamed.Name != "work" {
		t.Fatalf("folder name = %q, want %q", renamed.Name, "work")
	}
	if _, err := service.RenameFolder(1, documents.ID, "   "); !errors.Is(err, ErrFolderNameRequired) {
		t.Fatalf("empty rename error = %v, want %v", err, ErrFolderNameRequired)
	}
	if _, err := service.RenameFolder(1, documents.ID, music.Name); !errors.Is(err, ErrFolderAlreadyExists) {
		t.Fatalf("duplicate rename error = %v, want %v", err, ErrFolderAlreadyExists)
	}
	if _, err := service.RenameFolder(1, otherUserFolder.ID, "forbidden"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user rename error = %v, want %v", err, ErrFolderNotFound)
	}
}

func TestServiceMoveFolder(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	documents, err := service.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create documents folder: %v", err)
	}
	photos, err := service.CreateFolder(1, &documents.ID, "photos")
	if err != nil {
		t.Fatalf("create photos folder: %v", err)
	}
	archive, err := service.CreateFolder(1, nil, "archive")
	if err != nil {
		t.Fatalf("create archive folder: %v", err)
	}

	moved, err := service.MoveFolder(1, documents.ID, &archive.ID)
	if err != nil {
		t.Fatalf("move documents folder: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != archive.ID {
		t.Fatalf("moved parent ID = %v, want %d", moved.ParentID, archive.ID)
	}

	if _, err := service.MoveFolder(1, documents.ID, &documents.ID); !errors.Is(err, ErrFolderMoveCycle) {
		t.Fatalf("move folder into itself error = %v, want %v", err, ErrFolderMoveCycle)
	}
	if _, err := service.MoveFolder(1, documents.ID, &photos.ID); !errors.Is(err, ErrFolderMoveCycle) {
		t.Fatalf("move folder into descendant error = %v, want %v", err, ErrFolderMoveCycle)
	}

	otherFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if _, err := service.MoveFolder(1, documents.ID, &otherFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("move folder into other user folder error = %v, want %v", err, ErrFolderNotFound)
	}

	if _, err := service.CreateFolder(1, nil, "documents"); err != nil {
		t.Fatalf("create root folder with moved name: %v", err)
	}
	if _, err := service.MoveFolder(1, documents.ID, nil); !errors.Is(err, ErrFolderAlreadyExists) {
		t.Fatalf("move folder to duplicate root error = %v, want %v", err, ErrFolderAlreadyExists)
	}
}

func TestServiceDeleteFolder(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	empty, err := service.CreateFolder(1, nil, "empty")
	if err != nil {
		t.Fatalf("create empty folder: %v", err)
	}
	if err := service.DeleteFolder(1, empty.ID); err != nil {
		t.Fatalf("delete empty folder: %v", err)
	}
	if _, err := service.ListFolders(1, &empty.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("deleted folder error = %v, want %v", err, ErrFolderNotFound)
	}

	parent, err := service.CreateFolder(1, nil, "parent")
	if err != nil {
		t.Fatalf("create parent folder: %v", err)
	}
	if _, err := service.CreateFolder(1, &parent.ID, "child"); err != nil {
		t.Fatalf("create child folder: %v", err)
	}
	if err := service.DeleteFolder(1, parent.ID); !errors.Is(err, ErrFolderNotEmpty) {
		t.Fatalf("delete folder with child error = %v, want %v", err, ErrFolderNotEmpty)
	}

	fileFolder, err := service.CreateFolder(1, nil, "with-file")
	if err != nil {
		t.Fatalf("create file folder: %v", err)
	}
	uploaded, err := service.UploadIntoFolder(1, &fileFolder.ID, "report.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("upload folder file: %v", err)
	}
	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete folder file: %v", err)
	}
	if err := service.DeleteFolder(1, fileFolder.ID); !errors.Is(err, ErrFolderNotEmpty) {
		t.Fatalf("delete folder with trashed file error = %v, want %v", err, ErrFolderNotEmpty)
	}

	otherFolder, err := service.repo.CreateFolder(2, nil, "private")
	if err != nil {
		t.Fatalf("create other user folder: %v", err)
	}
	if err := service.DeleteFolder(1, otherFolder.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user delete error = %v, want %v", err, ErrFolderNotFound)
	}
}
