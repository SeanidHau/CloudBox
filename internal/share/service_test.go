package share

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type testReadSeekCloser struct {
	*bytes.Reader
	closed bool
}

func (r *testReadSeekCloser) Close() error {
	r.closed = true
	return nil
}

type fakeStorage struct {
	content    map[string][]byte
	lastReader *testReadSeekCloser
}

type fakeDownloadPolicy struct {
	err          error
	calls        int
	fileObjectID int64
}

func (p *fakeDownloadPolicy) CheckFileObjectDownload(fileObjectID int64) error {
	p.calls++
	p.fileObjectID = fileObjectID
	return p.err
}

func (s *fakeStorage) Open(storagePath string) (io.ReadSeekCloser, error) {
	data, ok := s.content[storagePath]
	if !ok {
		return nil, errors.New("storage file not found")
	}

	reader := &testReadSeekCloser{
		Reader: bytes.NewReader(data),
	}
	s.lastReader = reader

	return reader, nil
}

func TestServiceCreateShare(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo, nil)
	fileID := createTestFile(t, repo, 1, "active")
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	maxDownloads := int64(3)

	created, err := service.Create(
		1,
		fileID,
		"share-password",
		&expiresAt,
		&maxDownloads,
	)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if created.Token == "" {
		t.Fatal("share token should not be empty")
	}
	decodedToken, err := base64.RawURLEncoding.DecodeString(created.Token)
	if err != nil {
		t.Fatalf("decode share token: %v", err)
	}
	if len(decodedToken) != 32 {
		t.Fatalf("token byte length = %d, want 32", len(decodedToken))
	}
	if created.PasswordHash == "share-password" {
		t.Fatal("password must not be stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(created.PasswordHash),
		[]byte("share-password"),
	); err != nil {
		t.Fatalf("compare password hash: %v", err)
	}
	if created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expires at = %v, want %v", created.ExpiresAt, expiresAt)
	}
	if created.MaxDownloads == nil || *created.MaxDownloads != maxDownloads {
		t.Fatalf("max downloads = %v, want %d", created.MaxDownloads, maxDownloads)
	}
}

func TestServiceCreateValidatesInput(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo, nil)
	activeFileID := createTestFile(t, repo, 1, "active")
	deletedFileID := createTestFile(t, repo, 1, "deleted")
	past := time.Now().Add(-time.Second)
	zero := int64(0)

	tests := []struct {
		name         string
		userID       int64
		fileID       int64
		expiresAt    *time.Time
		maxDownloads *int64
		wantErr      error
	}{
		{
			name:    "missing file",
			userID:  1,
			fileID:  999,
			wantErr: ErrFileNotFound,
		},
		{
			name:    "other user file",
			userID:  2,
			fileID:  activeFileID,
			wantErr: ErrFileNotFound,
		},
		{
			name:    "deleted file",
			userID:  1,
			fileID:  deletedFileID,
			wantErr: ErrFileNotFound,
		},
		{
			name:      "past expiration",
			userID:    1,
			fileID:    activeFileID,
			expiresAt: &past,
			wantErr:   ErrShareExpirationInvalid,
		},
		{
			name:         "zero download limit",
			userID:       1,
			fileID:       activeFileID,
			maxDownloads: &zero,
			wantErr:      ErrDownloadLimitInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Create(
				test.userID,
				test.fileID,
				"",
				test.expiresAt,
				test.maxDownloads,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("create error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestServiceOpenForDownload(t *testing.T) {
	repo := newTestRepository(t)
	storage := &fakeStorage{
		content: map[string][]byte{
			"uploads/document.txt": []byte("shared content"),
		},
	}
	service := NewService(repo, storage)
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(1)

	share, err := service.Create(
		1,
		fileID,
		"share-password",
		nil,
		&maxDownloads,
	)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if _, _, err := service.OpenForDownload(share.Token, ""); !errors.Is(err, ErrSharePasswordRequired) {
		t.Fatalf("missing password error = %v, want %v", err, ErrSharePasswordRequired)
	}
	if _, _, err := service.OpenForDownload(share.Token, "wrong-password"); !errors.Is(err, ErrSharePasswordInvalid) {
		t.Fatalf("wrong password error = %v, want %v", err, ErrSharePasswordInvalid)
	}

	file, reader, err := service.OpenForDownload(share.Token, "share-password")
	if err != nil {
		t.Fatalf("open shared file: %v", err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read shared file: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close shared file: %v", err)
	}
	if file.OriginalName != "document.txt" {
		t.Fatalf("original name = %q, want document.txt", file.OriginalName)
	}
	if string(content) != "shared content" {
		t.Fatalf("shared content = %q, want shared content", content)
	}

	if _, _, err := service.OpenForDownload(share.Token, "share-password"); !errors.Is(err, ErrDownloadLimitReached) {
		t.Fatalf("over-limit error = %v, want %v", err, ErrDownloadLimitReached)
	}
	if storage.lastReader == nil || !storage.lastReader.closed {
		t.Fatal("reader should be closed after failing to reserve a download")
	}
}

func TestServiceOpenForDownloadAppliesDownloadPolicy(t *testing.T) {
	repo := newTestRepository(t)
	storage := &fakeStorage{
		content: map[string][]byte{
			"uploads/document.txt": []byte("shared content"),
		},
	}
	policy := &fakeDownloadPolicy{err: errors.New("scan is incomplete")}
	service := NewService(repo, storage, WithDownloadPolicy(policy))
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(1)

	share, err := service.Create(1, fileID, "", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	// Reject before opening storage or consuming the limited download count.
	if _, _, err := service.OpenForDownload(share.Token, ""); !errors.Is(err, ErrSharedFileUnavailable) {
		t.Fatalf("blocked shared download error = %v, want %v", err, ErrSharedFileUnavailable)
	}
	if policy.calls != 1 || policy.fileObjectID <= 0 {
		t.Fatalf("policy calls/object ID = %d/%d, want 1/positive", policy.calls, policy.fileObjectID)
	}
	if storage.lastReader != nil {
		t.Fatal("storage must not open when the download policy rejects the file")
	}

	current, err := repo.FindByToken(share.Token)
	if err != nil {
		t.Fatalf("find blocked share: %v", err)
	}
	if current.DownloadCount != 0 {
		t.Fatalf("download count after policy rejection = %d, want 0", current.DownloadCount)
	}

	policy.err = nil
	_, reader, err := service.OpenForDownload(share.Token, "")
	if err != nil {
		t.Fatalf("open allowed shared file: %v", err)
	}
	defer reader.Close()

	current, err = repo.FindByToken(share.Token)
	if err != nil {
		t.Fatalf("find allowed share: %v", err)
	}
	if current.DownloadCount != 1 {
		t.Fatalf("download count after allowed download = %d, want 1", current.DownloadCount)
	}
}

func TestServiceOpenForDownloadRejectsExpiredShare(t *testing.T) {
	repo := newTestRepository(t)
	storage := &fakeStorage{
		content: map[string][]byte{
			"uploads/document.txt": []byte("shared content"),
		},
	}
	service := NewService(repo, storage)
	fileID := createTestFile(t, repo, 1, "active")
	expiresAt := time.Now().UTC().Add(-time.Hour)

	if _, err := repo.Create(&Share{
		Token:      "expired-share",
		UserFileID: fileID,
		ExpiresAt:  &expiresAt,
	}); err != nil {
		t.Fatalf("create expired share: %v", err)
	}

	if _, _, err := service.OpenForDownload("expired-share", ""); !errors.Is(err, ErrShareExpired) {
		t.Fatalf("expired share error = %v, want %v", err, ErrShareExpired)
	}
}
