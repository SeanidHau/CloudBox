package upload

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrOriginalNameRequired = errors.New("original name is required")
	ErrFileSizeInvalid      = errors.New("file size must be greater than zero")
	ErrChunkSizeInvalid     = errors.New("chunk size must be greater than zero")
)

type Service struct {
	repo        *Repository
	tempBaseDir string
}

func NewService(repo *Repository, tempBaseDir string) *Service {
	return &Service{
		repo:        repo,
		tempBaseDir: tempBaseDir,
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

	taskID := uuid.NewString()
	task := &Task{
		ID:           taskID,
		UserID:       userID,
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
