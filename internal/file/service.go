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
	ErrFolderNameRequired   = errors.New("folder name is required")
	ErrFolderAlreadyExists  = errors.New("folder already exists")
	ErrFolderMoveCycle      = errors.New("folder cannot be moved into itself or its descendant")
	ErrFolderNotEmpty       = errors.New("folder is not empty")
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
	return s.UploadIntoFolder(
		userID,
		nil,
		originalName,
		contentType,
		reader,
	)
}

func (s *Service) UploadIntoFolder(
	userID int64,
	parentID *int64,
	originalName string,
	contentType string,
	reader io.Reader,
) (*UserFile, error) {
	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}
	if reader == nil {
		return nil, ErrContentRequired
	}

	if parentID != nil {
		if _, err := s.repo.FindFolderByID(userID, *parentID); err != nil {
			return nil, err
		}
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

	return s.repo.CreateWithObjectInFolder(
		userID,
		parentID,
		originalName,
		object,
	)
}

func (s *Service) ListActive(userID int64) ([]UserFile, error) {
	return s.ListActiveInFolder(userID, nil)
}

func (s *Service) ListActiveInFolder(
	userID int64,
	parentID *int64,
) ([]UserFile, error) {
	if parentID != nil {
		if _, err := s.repo.FindFolderByID(userID, *parentID); err != nil {
			return nil, err
		}
	}

	return s.repo.ListActiveInFolder(userID, parentID)
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
	return s.InstantUploadIntoFolder(userID, nil, originalName, fileHash)
}

func (s *Service) InstantUploadIntoFolder(
	userID int64,
	parentID *int64,
	originalName string,
	fileHash string,
) (*UserFile, error) {
	originalName = strings.TrimSpace(originalName)
	fileHash = strings.TrimSpace(fileHash)

	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}
	if fileHash == "" {
		return nil, ErrFileHashRequired
	}

	if parentID != nil {
		if _, err := s.repo.FindFolderByID(userID, *parentID); err != nil {
			return nil, err
		}
	}

	object, err := s.repo.FindFileObjectByHash(fileHash)
	if err != nil {
		return nil, err
	}

	return s.repo.CreateWithObjectInFolder(userID, parentID, originalName, object)
}

func (s *Service) CreateFolder(
	userID int64,
	parentID *int64,
	name string,
) (*Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrFolderNameRequired
	}

	if parentID != nil {
		if _, err := s.repo.FindFolderByID(userID, *parentID); err != nil {
			return nil, err
		}
	}

	siblings, err := s.repo.ListFolders(userID, parentID)
	if err != nil {
		return nil, err
	}
	for _, sibling := range siblings {
		if sibling.Name == name {
			return nil, ErrFolderAlreadyExists
		}
	}

	return s.repo.CreateFolder(userID, parentID, name)
}

func (s *Service) ListFolders(
	userID int64,
	parentID *int64,
) ([]Folder, error) {
	if parentID != nil {
		if _, err := s.repo.FindFolderByID(userID, *parentID); err != nil {
			return nil, err
		}
	}

	return s.repo.ListFolders(userID, parentID)
}

func (s *Service) ValidateFolder(userID int64, parentID *int64) error {
	if parentID == nil {
		return nil
	}

	_, err := s.repo.FindFolderByID(userID, *parentID)
	return err
}

func (s *Service) MoveActive(
	userID int64,
	fileID int64,
	parentID *int64,
) (*UserFile, error) {
	if err := s.ValidateFolder(userID, parentID); err != nil {
		return nil, err
	}

	return s.repo.MoveActive(userID, fileID, parentID)
}

func (s *Service) RenameActive(
	userID int64,
	fileID int64,
	originalName string,
) (*UserFile, error) {
	originalName = strings.TrimSpace(originalName)
	if originalName == "" {
		return nil, ErrOriginalNameRequired
	}

	return s.repo.RenameActive(userID, fileID, originalName)
}

func (s *Service) RenameFolder(
	userID int64,
	folderID int64,
	name string,
) (*Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrFolderNameRequired
	}

	folder, err := s.repo.FindFolderByID(userID, folderID)
	if err != nil {
		return nil, err
	}

	siblings, err := s.repo.ListFolders(userID, folder.ParentID)
	if err != nil {
		return nil, err
	}
	for _, sibling := range siblings {
		if sibling.ID != folder.ID && sibling.Name == name {
			return nil, ErrFolderAlreadyExists
		}
	}

	return s.repo.RenameFolder(userID, folderID, name)
}

func (s *Service) MoveFolder(
	userID int64,
	folderID int64,
	parentID *int64,
) (*Folder, error) {
	folder, err := s.repo.FindFolderByID(userID, folderID)
	if err != nil {
		return nil, err
	}

	if parentID != nil {
		if *parentID == folderID {
			return nil, ErrFolderMoveCycle
		}

		currentParentID := parentID
		for currentParentID != nil {
			current, err := s.repo.FindFolderByID(userID, *currentParentID)
			if err != nil {
				return nil, err
			}
			if current.ID == folderID {
				return nil, ErrFolderMoveCycle
			}

			currentParentID = current.ParentID
		}
	}

	siblings, err := s.repo.ListFolders(userID, parentID)
	if err != nil {
		return nil, err
	}
	for _, sibling := range siblings {
		if sibling.ID != folder.ID && sibling.Name == folder.Name {
			return nil, ErrFolderAlreadyExists
		}
	}

	return s.repo.MoveFolder(userID, folderID, parentID)
}

func (s *Service) DeleteFolder(userID int64, folderID int64) error {
	if _, err := s.repo.FindFolderByID(userID, folderID); err != nil {
		return err
	}

	deleted, err := s.repo.DeleteEmptyFolder(userID, folderID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrFolderNotEmpty
	}

	return nil
}
