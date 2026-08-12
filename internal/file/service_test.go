package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
)

type fakeStorage struct {
	savedPath    string
	savedContent string
	deletedPath  string
	deletedPaths []string
	saveErr      error
	openErr      error
	deleteErr    error
}

type fakeJobEnqueuer struct {
	job     *jobmodule.Job
	err     error
	userID  int64
	jobType string
	payload any
	calls   int
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

type fakeStorageQuotaProvider struct {
	quotas map[int64]int64
}

func (p fakeStorageQuotaProvider) StorageQuotaBytes(userID int64) (int64, error) {
	return p.quotas[userID], nil
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
	s.deletedPaths = append(s.deletedPaths, storagePath)
	return s.deleteErr
}

func (e *fakeJobEnqueuer) EnqueueForUser(
	userID int64,
	jobType string,
	payload any,
) (*jobmodule.Job, error) {
	e.calls++
	e.userID = userID
	e.jobType = jobType
	e.payload = payload

	return e.job, e.err
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
		"../../migrations/008_background_jobs.sql",
		"../../migrations/009_background_job_user.sql",
		"../../migrations/010_file_preview.sql",
		"../../migrations/011_file_scans.sql",
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

func TestServiceVerifyActiveFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "verified.txt", "text/plain", strings.NewReader("verified content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	if err := service.VerifyActiveFile(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("verify active file: %v", err)
	}
}

func TestServiceVerifyActiveFileDetectsModifiedContent(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "modified.txt", "text/plain", strings.NewReader("original content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	// Simulate an object that was changed after its hash was stored in the database.
	storage.savedContent = "modified content"

	if err := service.VerifyActiveFile(context.Background(), uploaded.ID); !errors.Is(err, ErrFileIntegrityMismatch) {
		t.Fatalf("verify modified file error = %v, want %v", err, ErrFileIntegrityMismatch)
	}
}

func TestServiceVerifyActiveFileRejectsDeletedFile(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "deleted.txt", "text/plain", strings.NewReader("deleted content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, uploaded.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	if err := service.VerifyActiveFile(context.Background(), uploaded.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("verify deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestServiceEnqueueFileVerificationChecksOwnershipAndQueue(t *testing.T) {
	storage := &fakeStorage{}
	queue := &fakeJobEnqueuer{
		job: &jobmodule.Job{ID: "verify-job", Status: jobmodule.StatusQueued},
	}
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		storage,
		testStorageQuotaBytes,
		WithJobEnqueuer(queue),
	)

	uploaded, err := service.Upload(1, "verify.txt", "text/plain", strings.NewReader("verify content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	queued, err := service.EnqueueFileVerification(1, uploaded.ID)
	if err != nil {
		t.Fatalf("enqueue verification: %v", err)
	}
	if queued.ID != "verify-job" || queue.calls != 1 {
		t.Fatalf("queued job/calls = %#v/%d, want verify-job/1", queued, queue.calls)
	}
	if queue.userID != 1 || queue.jobType != jobmodule.TypeVerifyFile {
		t.Fatalf("queue user/type = %d/%q, want 1/%q", queue.userID, queue.jobType, jobmodule.TypeVerifyFile)
	}
	payload, ok := queue.payload.(VerifyFilePayload)
	if !ok || payload.FileID != uploaded.ID {
		t.Fatalf("queue payload = %#v, want file ID %d", queue.payload, uploaded.ID)
	}

	if _, err := service.EnqueueFileVerification(2, uploaded.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("enqueue other user file error = %v, want %v", err, ErrFileNotFound)
	}
	if queue.calls != 1 {
		t.Fatalf("queue calls after rejected ownership = %d, want 1", queue.calls)
	}
}

func TestServiceEnqueueFileVerificationRequiresQueue(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	uploaded, err := service.Upload(1, "verify.txt", "text/plain", strings.NewReader("verify content"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}

	if _, err := service.EnqueueFileVerification(1, uploaded.ID); !errors.Is(err, ErrJobQueueUnavailable) {
		t.Fatalf("enqueue without queue error = %v, want %v", err, ErrJobQueueUnavailable)
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

func TestServiceUsesPerUserStorageQuota(t *testing.T) {
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		100,
		WithStorageQuotaProvider(fakeStorageQuotaProvider{quotas: map[int64]int64{1: 5, 2: 10}}),
	)

	if err := service.EnsureStorageQuota(1, 6); !errors.Is(err, ErrStorageQuotaExceeded) {
		t.Fatalf("user one quota error = %v, want %v", err, ErrStorageQuotaExceeded)
	}
	if err := service.EnsureStorageQuota(2, 6); err != nil {
		t.Fatalf("user two quota error = %v, want success", err)
	}
	usage, err := service.GetStorageUsage(1)
	if err != nil {
		t.Fatalf("get user one usage: %v", err)
	}
	if usage.QuotaBytes != 5 || usage.AvailableBytes != 5 {
		t.Fatalf("user one usage = %#v, want quota and available 5", usage)
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

func TestServicePermanentlyDeleteRemovesPreviewStorage(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	object, err := service.repo.CreateFileObject(
		"preview-cleanup-hash",
		"uploads/source.png",
		100,
		"image/png",
	)
	if err != nil {
		t.Fatalf("create source object: %v", err)
	}
	file, err := service.repo.CreateWithObject(1, "source.png", object)
	if err != nil {
		t.Fatalf("create user file: %v", err)
	}
	if _, err := service.repo.CreateFilePreview(&FilePreview{
		FileObjectID: object.ID,
		StoragePath:  "uploads/source-preview.png",
		Size:         50,
		ContentType:  "image/png",
		Width:        320,
		Height:       160,
	}); err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if err := service.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	if err := service.PermanentlyDelete(1, file.ID); err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}

	deleted := make(map[string]bool, len(storage.deletedPaths))
	for _, storagePath := range storage.deletedPaths {
		deleted[storagePath] = true
	}
	if !deleted[object.StoragePath] || !deleted["uploads/source-preview.png"] {
		t.Fatalf("deleted storage paths = %#v, want source and preview", storage.deletedPaths)
	}
}

func TestServiceCleanupDeletedBeforeRemovesOnlyExpiredFiles(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)
	now := time.Now().UTC().Truncate(time.Second)

	oldFile, err := service.Upload(1, "old.txt", "text/plain", strings.NewReader("old"))
	if err != nil {
		t.Fatalf("upload old file: %v", err)
	}
	recentFile, err := service.Upload(2, "recent.txt", "text/plain", strings.NewReader("recent"))
	if err != nil {
		t.Fatalf("upload recent file: %v", err)
	}
	if err := service.SoftDelete(1, oldFile.ID); err != nil {
		t.Fatalf("soft delete old file: %v", err)
	}
	if err := service.SoftDelete(2, recentFile.ID); err != nil {
		t.Fatalf("soft delete recent file: %v", err)
	}

	if _, err := service.repo.db.Exec(`UPDATE user_files SET deleted_at = $1 WHERE id = $2`, now.Add(-48*time.Hour), oldFile.ID); err != nil {
		t.Fatalf("set old deleted time: %v", err)
	}
	if _, err := service.repo.db.Exec(`UPDATE user_files SET deleted_at = $1 WHERE id = $2`, now.Add(-time.Hour), recentFile.ID); err != nil {
		t.Fatalf("set recent deleted time: %v", err)
	}

	cleaned, err := service.CleanupDeletedBefore(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("clean up expired files: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned files = %d, want 1", cleaned)
	}
	if _, err := service.repo.FindDeletedByID(1, oldFile.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("find cleaned file error = %v, want %v", err, ErrFileNotFound)
	}
	if _, err := service.repo.FindDeletedByID(2, recentFile.ID); err != nil {
		t.Fatalf("find recent deleted file: %v", err)
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

func TestServiceListActiveMapsScanStatesToAvailability(t *testing.T) {
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithVirusScanner(&fakeVirusScanner{}),
	)

	readyObject, err := service.repo.CreateFileObject("availability-ready", "uploads/ready.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create ready object: %v", err)
	}
	readyFile, err := service.repo.CreateWithObject(1, "ready.txt", readyObject)
	if err != nil {
		t.Fatalf("create ready file: %v", err)
	}
	setFileScanStatus(t, service.repo, readyObject.ID, ScanStatusClean)

	processingObject, err := service.repo.CreateFileObject("availability-processing", "uploads/processing.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create processing object: %v", err)
	}
	processingFile, err := service.repo.CreateWithObject(1, "processing.txt", processingObject)
	if err != nil {
		t.Fatalf("create processing file: %v", err)
	}
	if _, _, err := service.repo.CreatePendingFileScan(processingObject.ID); err != nil {
		t.Fatalf("create pending scan: %v", err)
	}

	unavailableObject, err := service.repo.CreateFileObject("availability-unavailable", "uploads/unavailable.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create unavailable object: %v", err)
	}
	unavailableFile, err := service.repo.CreateWithObject(1, "unavailable.txt", unavailableObject)
	if err != nil {
		t.Fatalf("create unavailable file: %v", err)
	}
	setFileScanStatus(t, service.repo, unavailableObject.ID, ScanStatusInfected)

	files, err := service.ListActive(1)
	if err != nil {
		t.Fatalf("list active files: %v", err)
	}
	availability := make(map[int64]string, len(files))
	for _, file := range files {
		availability[file.ID] = file.Availability
	}
	if availability[readyFile.ID] != AvailabilityReady || availability[processingFile.ID] != AvailabilityProcessing || availability[unavailableFile.ID] != AvailabilityUnavailable {
		t.Fatalf("availability = %#v, want ready/processing/unavailable", availability)
	}
}

func TestServiceListDeletedMarksFilesUnavailable(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})
	file, err := service.Upload(1, "deleted.txt", "text/plain", strings.NewReader("deleted"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	files, err := service.ListDeleted(1)
	if err != nil {
		t.Fatalf("list deleted files: %v", err)
	}
	if len(files) != 1 || files[0].Availability != AvailabilityUnavailable {
		t.Fatalf("deleted files = %#v, want one unavailable file", files)
	}
}

func TestServiceListDeletedIncludesScheduledCleanupTime(t *testing.T) {
	service := newTestServiceWithStorageQuotaAndOptions(
		t,
		&fakeStorage{},
		testStorageQuotaBytes,
		WithTrashRetention(30*24*time.Hour),
	)
	file, err := service.Upload(1, "deleted.txt", "text/plain", strings.NewReader("deleted"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := service.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	files, err := service.ListDeleted(1)
	if err != nil {
		t.Fatalf("list deleted files: %v", err)
	}
	if len(files) != 1 || files[0].CleanupAt == nil {
		t.Fatalf("deleted files = %#v, want cleanup time", files)
	}
	if files[0].CleanupAt.Before(time.Now().Add(29*24*time.Hour)) || files[0].CleanupAt.After(time.Now().Add(31*24*time.Hour)) {
		t.Fatalf("cleanup time = %s, want about 30 days from now", files[0].CleanupAt)
	}
}

func TestSupportsInlinePreviewAllowsOnlySupportedImages(t *testing.T) {
	for _, test := range []struct {
		contentType string
		want        bool
	}{
		{contentType: "image/jpeg", want: true},
		{contentType: "image/png", want: true},
		{contentType: "image/webp", want: true},
		{contentType: "image/heic", want: false},
		{contentType: "video/mp4", want: false},
	} {
		if got := SupportsInlinePreview(test.contentType); got != test.want {
			t.Fatalf("supports inline preview %q = %t, want %t", test.contentType, got, test.want)
		}
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
