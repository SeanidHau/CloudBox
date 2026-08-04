package file

import (
	"errors"
	"io"
	"strings"
)

var (
	ErrOriginalNameRequired = errors.New("original name is required")
	ErrContentRequired      = errors.New("file content is required")
	ErrFileHashRequired     = errors.New("file hash is required")
)

type Storage interface {
	Save(reader io.Reader, originalName string) (string, int64, string, error)
	Open(storagePath string) (io.ReadSeekCloser, error)
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

	storagePath, size, fileHash, err := s.storage.Save(reader, originalName)
	if err != nil {
		return nil, err
	}

	object, err := s.repo.FindFileObjectByHash(fileHash)

	if err == nil {
		if err := s.storage.Delete(storagePath); err != nil {
			return nil, err
		}
	} else if errors.Is(err, ErrFileObjectNotFound) {
		object, err = s.repo.CreateFileObject(
			fileHash,
			storagePath,
			size,
			contentType,
		)
		if err != nil {
			existingObject, findErr := s.repo.FindFileObjectByHash(fileHash)
			if findErr != nil {
				_ = s.storage.Delete(storagePath)
				return nil, err
			}

			object = existingObject

			if err := s.storage.Delete(storagePath); err != nil {
				return nil, err
			}
		}
	} else {
		_ = s.storage.Delete(storagePath)
		return nil, err
	}

	return s.repo.CreateWithObject(userID, originalName, object)
}

func (s *Service) ListActive(userID int64) ([]UserFile, error) {
	return s.repo.ListActive(userID)
}

func (s *Service) ListDeleted(userID int64) ([]UserFile, error) {
	return s.repo.ListDeleted(userID)
}

func (s *Service) OpenForDownload(userID int64, fileID int64) (*UserFile, io.ReadSeekCloser, error) {
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

func (s *Service) InstantUpload(userID int64, originalName string, fileHash string) (*UserFile, error) {
	originalName = strings.TrimSpace(originalName)
	fileHash = strings.TrimSpace(fileHash)

	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}
	if fileHash == "" {
		return nil, ErrFileHashRequired
	}

	object, err := s.repo.FindFileObjectByHash(fileHash)
	if err != nil {
		return nil, err
	}

	return s.repo.CreateWithObject(userID, originalName, object)
}
