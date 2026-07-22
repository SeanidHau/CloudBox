package file

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var ErrFileDeleted = errors.New("file is deleted")

type Storage interface {
	Save(ctx context.Context, userID int64, originalName string, reader io.Reader) (string, error)
	Open(storagePath string) (io.ReadCloser, string, error)
}

type Service struct {
	repo    *Repository
	storage Storage
}

func NewService(repo *Repository, storage Storage) *Service {
	return &Service{repo: repo, storage: storage}
}

func (s *Service) Upload(ctx context.Context, params CreateFileParams, reader io.Reader) (UserFile, error) {
	storagePath, err := s.storage.Save(ctx, params.UserID, params.OriginalName, reader)
	if err != nil {
		return UserFile{}, err
	}

	params.StoragePath = storagePath
	userFile, err := s.repo.Create(ctx, params)
	if err != nil {
		return UserFile{}, fmt.Errorf("save file metadata: %w", err)
	}

	return userFile, nil
}

func (s *Service) ListActive(ctx context.Context, userID int64) ([]UserFile, error) {
	return s.repo.ListByStatus(ctx, userID, StatusActive)
}

func (s *Service) ListTrash(ctx context.Context, userID int64) ([]UserFile, error) {
	return s.repo.ListByStatus(ctx, userID, StatusDeleted)
}

func (s *Service) OpenDownload(ctx context.Context, userID, fileID int64) (UserFile, io.ReadCloser, error) {
	userFile, err := s.repo.FindByID(ctx, userID, fileID)
	if err != nil {
		return UserFile{}, nil, err
	}
	if userFile.Status == StatusDeleted {
		return UserFile{}, nil, ErrFileDeleted
	}

	reader, _, err := s.storage.Open(userFile.StoragePath)
	if err != nil {
		return UserFile{}, nil, err
	}

	return userFile, reader, nil
}

func (s *Service) Delete(ctx context.Context, userID, fileID int64) error {
	return s.repo.SoftDelete(ctx, userID, fileID)
}

func (s *Service) Restore(ctx context.Context, userID, fileID int64) error {
	return s.repo.Restore(ctx, userID, fileID)
}
