package share

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"testing"
	"time"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
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

type fakeFileSaver struct {
	err          error
	userID       int64
	parentID     *int64
	originalName string
	fileHash     string
	inputs       []filemodule.InstantUploadInput
}

func (s *fakeFileSaver) InstantUploadIntoFolder(
	userID int64,
	parentID *int64,
	originalName string,
	fileHash string,
) (*filemodule.UserFile, error) {
	s.userID = userID
	s.parentID = parentID
	s.originalName = originalName
	s.fileHash = fileHash
	if s.err != nil {
		return nil, s.err
	}

	return &filemodule.UserFile{
		ID:           99,
		UserID:       userID,
		OriginalName: originalName,
	}, nil
}

func (s *fakeFileSaver) InstantUploadManyIntoFolder(
	userID int64,
	parentID *int64,
	inputs []filemodule.InstantUploadInput,
) ([]filemodule.UserFile, error) {
	s.userID = userID
	s.parentID = parentID
	s.inputs = append([]filemodule.InstantUploadInput(nil), inputs...)
	if s.err != nil {
		return nil, s.err
	}
	files := make([]filemodule.UserFile, 0, len(inputs))
	for index, input := range inputs {
		files = append(files, filemodule.UserFile{ID: int64(100 + index), UserID: userID, OriginalName: input.OriginalName})
	}
	return files, nil
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

func TestServiceCreateShareUsesSevenDayDefaultExpiry(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo, nil)
	fileID := createTestFile(t, repo, 1, "active")
	before := time.Now().UTC().Add(DefaultShareLifetime - time.Second)

	created, err := service.Create(1, fileID, "", nil, nil)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if created.ExpiresAt == nil {
		t.Fatal("default share expiry should be set")
	}
	if created.ExpiresAt.Before(before) || created.ExpiresAt.After(time.Now().UTC().Add(DefaultShareLifetime+time.Second)) {
		t.Fatalf("default expiry = %v, want roughly seven days from now", created.ExpiresAt)
	}
}

func TestServicePublicInfoDoesNotConsumeDownloadLimit(t *testing.T) {
	repo := newTestRepository(t)
	service := NewService(repo, nil)
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(1)
	share, err := service.Create(1, fileID, "share-password", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if _, err := service.GetPublicFile(share.Token, ""); !errors.Is(err, ErrSharePasswordRequired) {
		t.Fatalf("unverified public info error = %v, want %v", err, ErrSharePasswordRequired)
	}

	info, err := service.GetPublicFile(share.Token, "share-password")
	if err != nil {
		t.Fatalf("get verified public info: %v", err)
	}
	if info.OriginalName != "document.txt" || info.Size != 15 || info.ContentType != "text/plain" {
		t.Fatalf("public info = %#v, want document metadata", info)
	}

	current, err := repo.FindByToken(share.Token)
	if err != nil {
		t.Fatalf("find share: %v", err)
	}
	if current.DownloadCount != 0 {
		t.Fatalf("download count after public info = %d, want 0", current.DownloadCount)
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

func TestServiceSaveToUserFilesCreatesObjectReference(t *testing.T) {
	repo := newTestRepository(t)
	saver := &fakeFileSaver{}
	service := NewService(repo, nil, WithFileSaver(saver))
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(2)

	share, err := service.Create(1, fileID, "share-password", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	saved, err := service.SaveToUserFiles(2, share.Token, "share-password", nil)
	if err != nil {
		t.Fatalf("save shared file: %v", err)
	}
	if saved.UserID != 2 || saved.OriginalName != "document.txt" {
		t.Fatalf("saved file = %#v, want target user and source name", saved)
	}
	if saver.userID != 2 || saver.originalName != "document.txt" || saver.fileHash == "" {
		t.Fatalf("saver input = %#v, want target user, source name, and object hash", saver)
	}

	current, err := repo.FindByToken(share.Token)
	if err != nil {
		t.Fatalf("find share: %v", err)
	}
	if current.DownloadCount != 1 {
		t.Fatalf("download count = %d, want 1", current.DownloadCount)
	}
}

func TestServiceSaveToUserFilesReleasesReservationOnFailure(t *testing.T) {
	repo := newTestRepository(t)
	saver := &fakeFileSaver{err: errors.New("storage quota exceeded")}
	service := NewService(repo, nil, WithFileSaver(saver))
	fileID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(1)

	share, err := service.Create(1, fileID, "", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	if _, err := service.SaveToUserFiles(2, share.Token, "", nil); err == nil {
		t.Fatal("save error = nil, want saver error")
	}
	current, err := repo.FindByToken(share.Token)
	if err != nil {
		t.Fatalf("find share: %v", err)
	}
	if current.DownloadCount != 0 {
		t.Fatalf("download count after failed save = %d, want 0", current.DownloadCount)
	}
}

func TestServiceSaveCollectionToUserFilesCreatesEveryReference(t *testing.T) {
	repo := newTestRepository(t)
	saver := &fakeFileSaver{}
	service := NewService(repo, nil, WithFileSaver(saver))
	firstID := createTestFile(t, repo, 1, "active")
	secondID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(3)

	collection, err := service.CreateCollection(1, []int64{firstID, secondID}, "collection-password", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	saved, err := service.SaveCollectionToUserFilesFromIP(2, collection.Token, "collection-password", nil, "test-ip")
	if err != nil {
		t.Fatalf("save collection: %v", err)
	}
	if len(saved) != 2 || len(saver.inputs) != 2 || saver.userID != 2 {
		t.Fatalf("saved collection = %#v, saver = %#v", saved, saver)
	}
	current, err := repo.FindCollectionByToken(collection.Token)
	if err != nil {
		t.Fatalf("find collection: %v", err)
	}
	if current.DownloadCount != 2 {
		t.Fatalf("download count = %d, want 2", current.DownloadCount)
	}
}

func TestServiceSaveCollectionReleasesAllReservationsOnFailure(t *testing.T) {
	repo := newTestRepository(t)
	saver := &fakeFileSaver{err: errors.New("storage quota exceeded")}
	service := NewService(repo, nil, WithFileSaver(saver))
	firstID := createTestFile(t, repo, 1, "active")
	secondID := createTestFile(t, repo, 1, "active")
	maxDownloads := int64(2)

	collection, err := service.CreateCollection(1, []int64{firstID, secondID}, "", nil, &maxDownloads)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if _, err := service.SaveCollectionToUserFilesFromIP(2, collection.Token, "", nil, "test-ip"); err == nil {
		t.Fatal("save collection error = nil, want saver error")
	}
	current, err := repo.FindCollectionByToken(collection.Token)
	if err != nil {
		t.Fatalf("find collection: %v", err)
	}
	if current.DownloadCount != 0 {
		t.Fatalf("download count after failed save = %d, want 0", current.DownloadCount)
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
