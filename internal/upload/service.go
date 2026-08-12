package upload

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/google/uuid"
)

var (
	ErrOriginalNameRequired = errors.New("original name is required")
	ErrFileSizeInvalid      = errors.New("file size must be greater than zero")
	ErrChunkSizeInvalid     = errors.New("chunk size must be greater than zero")
	ErrChunkContentRequired = errors.New("chunk content is required")
	ErrChunkNumberInvalid   = errors.New("chunk number is invalid")
	ErrChunkSizeMismatch    = errors.New("chunk size does not match expected size")
	ErrTaskNotUploading     = errors.New("upload task is not accepting chunks")
	ErrChunksIncomplete     = errors.New("upload chunks are incomplete")
	ErrChunkHashMismatch    = errors.New("upload chunk hash does not match content")
	ErrFileHashMismatch     = errors.New("file hash does not match uploaded content")
)

type FileService interface {
	ValidateFolder(userID int64, parentID *int64) error

	EnsureStorageQuota(userID int64, additionalBytes int64) error

	UploadIntoFolder(
		userID int64,
		parentID *int64,
		originalName string,
		contentType string,
		reader io.Reader,
	) (*filemodule.UserFile, error)
}

type Service struct {
	repo        *Repository
	tempBaseDir string
	fileService FileService
}

func NewService(
	repo *Repository,
	tempBaseDir string,
	fileService FileService,
) *Service {
	return &Service{
		repo:        repo,
		tempBaseDir: tempBaseDir,
		fileService: fileService,
	}
}

func (s *Service) Init(
	userID int64,
	originalName string,
	contentType string,
	fileSize int64,
	chunkSize int64,
	fileHash string,
) (*Task, error) {
	return s.InitInFolder(
		userID,
		nil,
		originalName,
		contentType,
		fileSize,
		chunkSize,
		fileHash,
	)
}

func (s *Service) ListUploading(userID int64) ([]Task, error) {
	return s.repo.ListUploadingByUser(userID)
}

func (s *Service) InitInFolder(
	userID int64,
	parentID *int64,
	originalName string,
	contentType string,
	fileSize int64,
	chunkSize int64,
	fileHash string,
) (*Task, error) {
	originalName = strings.TrimSpace(originalName)
	contentType = strings.TrimSpace(contentType)
	fileHash = strings.TrimSpace(fileHash)

	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}
	if fileSize <= 0 {
		return nil, ErrFileSizeInvalid
	}
	if chunkSize <= 0 {
		return nil, ErrChunkSizeInvalid
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if err := s.fileService.ValidateFolder(userID, parentID); err != nil {
		return nil, err
	}

	taskID := uuid.NewString()
	task := &Task{
		ID:           taskID,
		UserID:       userID,
		ParentID:     parentID,
		OriginalName: originalName,
		ContentType:  contentType,
		FileSize:     fileSize,
		ChunkSize:    chunkSize,
		TotalChunks:  (fileSize-1)/chunkSize + 1,
		FileHash: sql.NullString{
			String: fileHash,
			Valid:  fileHash != "",
		},
		Status:  StatusUploading,
		TempDir: filepath.Join(s.tempBaseDir, taskID),
	}

	if err := s.fileService.EnsureStorageQuota(userID, fileSize); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(task.TempDir, 0755); err != nil {
		return nil, err
	}

	created, err := s.repo.Create(task)
	if err != nil {
		_ = os.RemoveAll(task.TempDir)
		return nil, err
	}

	return created, nil
}

func (s *Service) UploadChunk(
	userID int64,
	taskID string,
	chunkNumber int64,
	reader io.Reader,
) (*Chunk, error) {
	if reader == nil {
		return nil, ErrChunkContentRequired
	}

	task, err := s.repo.FindByID(userID, taskID)
	if err != nil {
		return nil, err
	}

	if task.Status != StatusUploading {
		return nil, ErrTaskNotUploading
	}

	if chunkNumber < 0 || chunkNumber >= task.TotalChunks {
		return nil, ErrChunkNumberInvalid
	}

	expectedSize := expectedChunkSize(task, chunkNumber)

	chunkPath := filepath.Join(
		task.TempDir,
		fmt.Sprintf("%06d.part", chunkNumber),
	)
	tempPath := chunkPath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return nil, err
	}

	hasher := sha256.New()
	size, copyErr := io.Copy(file, io.TeeReader(reader, hasher))
	closeErr := file.Close()

	if copyErr != nil {
		_ = os.Remove(tempPath)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return nil, closeErr
	}
	if size != expectedSize {
		_ = os.Remove(tempPath)
		return nil, ErrChunkSizeMismatch
	}
	if err := os.Rename(tempPath, chunkPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}

	chunkHash := hex.EncodeToString(hasher.Sum(nil))
	chunk, err := s.repo.UpsertChunk(&Chunk{
		UploadID: task.ID,
		Number:   chunkNumber,
		Size:     size,
		Hash: sql.NullString{
			String: chunkHash,
			Valid:  true,
		},
	})
	if err != nil {
		return nil, err
	}

	touched, err := s.repo.TouchUploading(userID, task.ID)
	if err != nil {
		return nil, err
	}
	if !touched {
		return nil, ErrTaskNotUploading
	}

	return chunk, nil
}

func expectedChunkSize(task *Task, chunkNumber int64) int64 {
	if chunkNumber == task.TotalChunks-1 {
		return task.FileSize - task.ChunkSize*(task.TotalChunks-1)
	}

	return task.ChunkSize
}

func (s *Service) Complete(userID int64, taskID string) (*filemodule.UserFile, error) {
	task, err := s.repo.FindByID(userID, taskID)
	if err != nil {
		return nil, err
	}
	if task.Status != StatusUploading {
		return nil, ErrTaskNotUploading
	}

	transitioned, err := s.repo.TransitionStatus(
		userID,
		task.ID,
		StatusUploading,
		StatusCompleting,
	)
	if err != nil {
		return nil, err
	}
	if !transitioned {
		return nil, ErrTaskNotUploading
	}

	shouldRestoreStatus := true
	defer func() {
		if shouldRestoreStatus {
			_, _ = s.repo.TransitionStatus(
				userID,
				taskID,
				StatusCompleting,
				StatusUploading,
			)
		}
	}()

	chunks, err := s.repo.ListChunks(taskID)
	if err != nil {
		return nil, err
	}

	mergedPath, fileHash, err := mergeChunks(task, chunks)
	if err != nil {
		return nil, err
	}
	defer os.Remove(mergedPath)

	if task.FileHash.Valid && task.FileHash.String != fileHash {
		return nil, ErrFileHashMismatch
	}

	mergedFile, err := os.Open(mergedPath)
	if err != nil {
		return nil, err
	}
	defer mergedFile.Close()

	userFile, err := s.fileService.UploadIntoFolder(
		userID,
		task.ParentID,
		task.OriginalName,
		task.ContentType,
		mergedFile,
	)
	if err != nil {
		return nil, err
	}

	completed, err := s.repo.TransitionStatus(
		userID,
		taskID,
		StatusCompleting,
		StatusCompleted,
	)
	if err != nil {
		return nil, err
	}
	if !completed {
		return nil, ErrTaskNotUploading
	}

	shouldRestoreStatus = false
	_ = os.RemoveAll(task.TempDir)

	return userFile, nil
}

func (s *Service) Cancel(userID int64, taskID string) error {
	task, err := s.repo.FindByID(userID, taskID)
	if err != nil {
		return err
	}

	cancelled, err := s.repo.TransitionStatus(
		userID,
		task.ID,
		StatusUploading,
		StatusFailed,
	)
	if err != nil {
		return err
	}
	if !cancelled {
		return ErrTaskNotUploading
	}

	return os.RemoveAll(task.TempDir)
}

func (s *Service) CleanupExpired(before time.Time) (int, error) {
	tasks, err := s.repo.ListExpiredUploading(before)
	if err != nil {
		return 0, err
	}

	cleaned := 0

	for _, task := range tasks {
		expired, err := s.repo.TransitionStatus(
			task.UserID,
			task.ID,
			StatusUploading,
			StatusFailed,
		)
		if err != nil {
			return cleaned, err
		}
		if !expired {
			continue
		}

		if err := os.RemoveAll(task.TempDir); err != nil {
			return cleaned, err
		}

		cleaned++
	}

	return cleaned, nil
}

func mergeChunks(task *Task, chunks []Chunk) (string, string, error) {
	if int64(len(chunks)) != task.TotalChunks {
		return "", "", ErrChunksIncomplete
	}

	mergedFile, err := os.CreateTemp(task.TempDir, "merged-*")
	if err != nil {
		return "", "", err
	}

	mergedPath := mergedFile.Name()
	keepMergedFile := false
	defer func() {
		_ = mergedFile.Close()
		if !keepMergedFile {
			_ = os.Remove(mergedPath)
		}
	}()

	fileHasher := sha256.New()
	var totalSize int64

	for chunkNumber := int64(0); chunkNumber < task.TotalChunks; chunkNumber++ {
		chunk := chunks[chunkNumber]
		expectedSize := expectedChunkSize(task, chunkNumber)

		if chunk.Number != chunkNumber || chunk.Size != expectedSize {
			return "", "", ErrChunksIncomplete
		}
		chunkFile, err := os.Open(
			filepath.Join(task.TempDir, fmt.Sprintf("%06d.part", chunkNumber)),
		)

		if errors.Is(err, os.ErrNotExist) {
			return "", "", ErrChunksIncomplete
		}
		if err != nil {
			return "", "", err
		}

		chunkHasher := sha256.New()
		size, copyErr := io.Copy(
			io.MultiWriter(mergedFile, fileHasher, chunkHasher),
			chunkFile,
		)
		closeErr := chunkFile.Close()

		if copyErr != nil {
			return "", "", copyErr
		}
		if closeErr != nil {
			return "", "", closeErr
		}
		if size != expectedSize {
			return "", "", ErrChunksIncomplete
		}

		chunkHash := hex.EncodeToString(chunkHasher.Sum(nil))
		if !chunk.Hash.Valid || chunk.Hash.String != chunkHash {
			return "", "", ErrChunkHashMismatch
		}

		totalSize += size
	}

	if totalSize != task.FileSize {
		return "", "", ErrChunksIncomplete
	}
	if err := mergedFile.Close(); err != nil {
		return "", "", err
	}

	keepMergedFile = true
	return mergedPath, hex.EncodeToString(fileHasher.Sum(nil)), nil
}

func (s *Service) GetStatus(userID int64, taskID string) (*UploadStatus, error) {
	task, err := s.repo.FindByID(userID, taskID)
	if err != nil {
		return nil, err
	}

	chunks, err := s.repo.ListChunks(task.ID)
	if err != nil {
		return nil, err
	}

	return &UploadStatus{
		Upload: task,
		Chunks: chunks,
	}, nil
}
