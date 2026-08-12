package share

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrShareExpirationInvalid = errors.New("share expiration must be in the future")
	ErrDownloadLimitInvalid   = errors.New("download limit must be greater than zero")
	ErrShareExpired           = errors.New("share has expired")
	ErrSharePasswordRequired  = errors.New("share password is required")
	ErrSharePasswordInvalid   = errors.New("share password is invalid")
	ErrDownloadLimitReached   = errors.New("share download limit reached")
	ErrSharedFileUnavailable  = errors.New("shared file is unavailable")
	ErrShareSaveUnavailable   = errors.New("shared file save is unavailable")
)

type Storage interface {
	Open(storagePath string) (io.ReadSeekCloser, error)
}

type DownloadPolicy interface {
	CheckFileObjectDownload(fileObject int64) error
}

// FileSaver is implemented by file.Service. Saving a shared file creates a
// new user-file reference to the existing object instead of copying its bytes.
type FileSaver interface {
	InstantUploadIntoFolder(
		userID int64,
		parentID *int64,
		originalName string,
		fileHash string,
	) (*filemodule.UserFile, error)
}

type ServiceOption func(*Service)

func WithDownloadPolicy(policy DownloadPolicy) ServiceOption {
	return func(service *Service) {
		if policy != nil {
			service.downloadPolicy = policy
		}
	}
}

func WithFileSaver(saver FileSaver) ServiceOption {
	return func(service *Service) {
		if saver != nil {
			service.fileSaver = saver
		}
	}
}

type Service struct {
	repo           *Repository
	storage        Storage
	downloadPolicy DownloadPolicy
	fileSaver      FileSaver
}

func NewService(repo *Repository, storage Storage, options ...ServiceOption) *Service {
	service := &Service{
		repo:    repo,
		storage: storage,
	}

	for _, option := range options {
		if option != nil {
			option(service)
		}
	}

	return service
}

func (s *Service) Create(
	userID int64,
	fileID int64,
	password string,
	expiresAt *time.Time,
	maxDownloads *int64,
) (*Share, error) {
	hasFile, err := s.repo.HasActiveFile(userID, fileID)
	if err != nil {
		return nil, err
	}
	if !hasFile {
		return nil, ErrFileNotFound
	}

	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, ErrShareExpirationInvalid
	}

	if maxDownloads != nil && *maxDownloads <= 0 {
		return nil, ErrDownloadLimitInvalid
	}

	var passwordHash string
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword(
			[]byte(password),
			bcrypt.DefaultCost,
		)
		if err != nil {
			return nil, err
		}

		passwordHash = string(hash)
	}

	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	return s.repo.Create(&Share{
		Token:        token,
		UserFileID:   fileID,
		PasswordHash: passwordHash,
		ExpiresAt:    expiresAt,
		MaxDownloads: maxDownloads,
	})
}

func generateToken() (string, error) {
	tokenBytes := make([]byte, 32)

	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(tokenBytes), nil
}

func (s *Service) OpenForDownload(
	token string,
	password string,
) (*SharedFile, io.ReadSeekCloser, error) {
	share, err := s.repo.FindByToken(token)
	if err != nil {
		return nil, nil, err
	}

	if err := validateShareAccess(share, password); err != nil {
		return nil, nil, err
	}

	file, err := s.repo.FindActiveFileByShareToken(token)
	if err != nil {
		return nil, nil, err
	}

	if s.downloadPolicy != nil {
		if err := s.downloadPolicy.CheckFileObjectDownload(file.ObjectID); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrSharedFileUnavailable, err)
		}
	}

	reader, err := s.storage.Open(file.StoragePath)
	if err != nil {
		return nil, nil, err
	}

	reserved, err := s.repo.ReserveDownload(token)
	if err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	if !reserved {
		_ = reader.Close()

		latest, err := s.repo.FindByToken(token)
		if err != nil {
			return nil, nil, err
		}
		if latest.ExpiresAt != nil && !latest.ExpiresAt.After(time.Now()) {
			return nil, nil, ErrShareExpired
		}

		return nil, nil, ErrDownloadLimitReached
	}

	return file, reader, nil
}

// SaveToUserFiles adds the shared object to a user's workspace. It applies
// the same access and scan checks as a public download, while the file module
// remains responsible for target-folder ownership and storage quota checks.
func (s *Service) SaveToUserFiles(
	userID int64,
	token string,
	password string,
	parentID *int64,
) (*filemodule.UserFile, error) {
	if s.fileSaver == nil {
		return nil, ErrShareSaveUnavailable
	}

	share, err := s.repo.FindByToken(token)
	if err != nil {
		return nil, err
	}
	if err := validateShareAccess(share, password); err != nil {
		return nil, err
	}

	sharedFile, err := s.repo.FindActiveFileByShareToken(token)
	if err != nil {
		return nil, err
	}
	if s.downloadPolicy != nil {
		if err := s.downloadPolicy.CheckFileObjectDownload(sharedFile.ObjectID); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSharedFileUnavailable, err)
		}
	}

	reserved, err := s.repo.ReserveDownload(token)
	if err != nil {
		return nil, err
	}
	if !reserved {
		return nil, ErrDownloadLimitReached
	}

	file, err := s.fileSaver.InstantUploadIntoFolder(
		userID,
		parentID,
		sharedFile.OriginalName,
		sharedFile.FileHash,
	)
	if err != nil {
		// A failed save must not consume an available download slot.
		if releaseErr := s.repo.ReleaseDownloadReservation(token); releaseErr != nil {
			return nil, fmt.Errorf("save shared file: %w (release reservation: %v)", err, releaseErr)
		}
		return nil, err
	}

	return file, nil
}

func validateShareAccess(share *Share, password string) error {
	if share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now()) {
		return ErrShareExpired
	}

	if share.PasswordHash != "" {
		if password == "" {
			return ErrSharePasswordRequired
		}

		if err := bcrypt.CompareHashAndPassword(
			[]byte(share.PasswordHash),
			[]byte(password),
		); err != nil {
			return ErrSharePasswordInvalid
		}
	}

	if share.MaxDownloads != nil && share.DownloadCount >= *share.MaxDownloads {
		return ErrDownloadLimitReached
	}

	return nil
}

func (s *Service) List(userID int64) ([]Share, error) {
	return s.repo.ListByUser(userID)
}

func (s *Service) Revoke(userID int64, token string) error {
	return s.repo.DeleteByToken(userID, token)
}
