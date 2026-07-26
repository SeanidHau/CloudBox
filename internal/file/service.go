package file

import (
	"errors"
	"io"
)

var (
	ErrOriginalNameRequired = errors.New("original name is required")
	ErrContentRequired      = errors.New("file content is required")
)

type Storage interface {
	Save(reader io.Reader, originalName string) (string, int64, error)
	Open(storagePath string) (io.ReadCloser, error)
	Delete(storagePath string) error
}

type Service struct {
	repo    *Repository
	storage Storage
}

func NewService(repo *Repository, storage Storage) *Service {
	return &Service{
		repo:    repo,
		storage: storage,
	}
}

func (s *Service) Upload(userID int64, originalName string, contentType string, reader io.Reader) (*UserFile, error) {
	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}

	if reader == nil {
		return nil, ErrContentRequired
	}

	storagePath, size, err := s.storage.Save(reader, originalName)
	if err != nil {
		return nil, err
	}

	file, err := s.repo.Create(userID, originalName, storagePath, size, contentType)
	if err != nil {
		_ = s.storage.Delete(storagePath)
		return nil, err
	}

	return file, nil
}

func (s *Service) ListActive(userID int64) ([]UserFile, error) {
	return s.repo.ListActive(userID)
}

func (s *Service) ListDeleted(userID int64) ([]UserFile, error) {
	return s.repo.ListDeleted(userID)
}

func (s *Service) OpenForDownload(userID int64, fileID int64) (*UserFile, io.ReadCloser, error) {
	file, err := s.repo.FindActiveByID(userID, fileID)
	if err != nil {
		return nil, nil, err
	}

	reader, err := s.storage.Open(file.StoragePath)
	if err != nil {
		return nil, nil, err
	}

	return file, reader, nil
}

func (s *Service) SoftDelete(userID int64, fileID int64) error {
	return s.repo.SoftDelete(userID, fileID)
}

func (s *Service) Restore(userID int64, fileID int64) error {
	return s.repo.Restore(userID, fileID)
}
