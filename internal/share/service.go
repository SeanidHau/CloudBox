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
	ErrSharePasswordLocked    = errors.New("share password attempts are temporarily locked")
	ErrShareDownloadRateLimit = errors.New("share download rate limit reached")
)

const DefaultShareLifetime = 7 * 24 * time.Hour

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

func WithAccessControl(control *AccessControl) ServiceOption {
	return func(service *Service) {
		if control != nil {
			service.accessControl = control
		}
	}
}

func WithAccessAuditor(auditor AccessAuditor) ServiceOption {
	return func(service *Service) {
		if auditor != nil {
			service.auditor = auditor
		}
	}
}

type Service struct {
	repo           *Repository
	storage        Storage
	downloadPolicy DownloadPolicy
	fileSaver      FileSaver
	accessControl  *AccessControl
	auditor        AccessAuditor
}

func NewService(repo *Repository, storage Storage, options ...ServiceOption) *Service {
	service := &Service{
		repo:          repo,
		storage:       storage,
		accessControl: NewAccessControl(),
		auditor:       repo,
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

	if expiresAt == nil {
		defaultExpiry := time.Now().UTC().Add(DefaultShareLifetime)
		expiresAt = &defaultExpiry
	}

	if !expiresAt.After(time.Now()) {
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

// GetPublicFile verifies that a visitor may view share information. Unlike a
// download, it never reserves a download slot, so opening an image preview
// cannot exhaust a limited share.
func (s *Service) GetPublicFile(token string, password string) (*PublicFile, error) {
	return s.GetPublicFileFromIP(token, password, "")
}

func (s *Service) GetPublicFileFromIP(token string, password string, ipHash string) (*PublicFile, error) {
	sharedFile, err := s.findAccessibleFile(token, password, false, ipHash)
	if err != nil {
		_ = s.audit(token, ipHash, AccessInfo, accessResult(err))
		return nil, err
	}

	_, err = s.repo.FindPreviewByShareToken(token)
	hasPreview := err == nil
	if err != nil && !errors.Is(err, ErrFileNotFound) {
		_ = s.audit(token, ipHash, AccessInfo, AccessDenied)
		return nil, err
	}

	result := &PublicFile{
		OriginalName: sharedFile.OriginalName,
		Size:         sharedFile.Size,
		ContentType:  sharedFile.ContentType,
		HasPreview:   hasPreview,
	}
	if err := s.audit(token, ipHash, AccessInfo, AccessAllowed); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) OpenPublicPreview(token string, password string) (*SharedPreview, io.ReadSeekCloser, error) {
	return s.OpenPublicPreviewFromIP(token, password, "")
}

func (s *Service) OpenPublicPreviewFromIP(token string, password string, ipHash string) (*SharedPreview, io.ReadSeekCloser, error) {
	if _, err := s.findAccessibleFile(token, password, false, ipHash); err != nil {
		_ = s.audit(token, ipHash, AccessPreview, accessResult(err))
		return nil, nil, err
	}

	preview, err := s.repo.FindPreviewByShareToken(token)
	if err != nil {
		_ = s.audit(token, ipHash, AccessPreview, AccessDenied)
		return nil, nil, err
	}
	reader, err := s.storage.Open(preview.StoragePath)
	if err != nil {
		_ = s.audit(token, ipHash, AccessPreview, AccessDenied)
		return nil, nil, err
	}

	if err := s.audit(token, ipHash, AccessPreview, AccessAllowed); err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	return preview, reader, nil
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
	return s.OpenForDownloadFromIP(token, password, "")
}

func (s *Service) OpenForDownloadFromIP(
	token string,
	password string,
	ipHash string,
) (*SharedFile, io.ReadSeekCloser, error) {
	file, err := s.findAccessibleFile(token, password, true, ipHash)
	if err != nil {
		_ = s.audit(token, ipHash, AccessDownload, accessResult(err))
		return nil, nil, err
	}
	if !s.accessControl.AllowDownload(token, ipHash) {
		_ = s.audit(token, ipHash, AccessDownload, AccessRateLimited)
		return nil, nil, ErrShareDownloadRateLimit
	}

	reader, err := s.storage.Open(file.StoragePath)
	if err != nil {
		_ = s.audit(token, ipHash, AccessDownload, AccessDenied)
		return nil, nil, err
	}

	reserved, err := s.repo.ReserveDownload(token)
	if err != nil {
		_ = reader.Close()
		_ = s.audit(token, ipHash, AccessDownload, AccessDenied)
		return nil, nil, err
	}
	if !reserved {
		_ = reader.Close()

		latest, err := s.repo.FindByToken(token)
		if err != nil {
			_ = s.audit(token, ipHash, AccessDownload, AccessDenied)
			return nil, nil, err
		}
		if latest.ExpiresAt != nil && !latest.ExpiresAt.After(time.Now()) {
			_ = s.audit(token, ipHash, AccessDownload, AccessDenied)
			return nil, nil, ErrShareExpired
		}

		_ = s.audit(token, ipHash, AccessDownload, AccessDenied)
		return nil, nil, ErrDownloadLimitReached
	}

	if err := s.audit(token, ipHash, AccessDownload, AccessAllowed); err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	return file, reader, nil
}

func (s *Service) findAccessibleFile(token string, password string, requireDownloadSlot bool, ipHash string) (*SharedFile, error) {
	if s.accessControl.PasswordLocked(token, ipHash) {
		return nil, ErrSharePasswordLocked
	}
	share, err := s.repo.FindByToken(token)
	if err != nil {
		return nil, err
	}

	if err := validateShareAccess(share, password, requireDownloadSlot); err != nil {
		if errors.Is(err, ErrSharePasswordInvalid) {
			s.accessControl.RecordPasswordFailure(token, ipHash)
		}
		return nil, err
	}
	s.accessControl.ClearPasswordFailures(token, ipHash)

	file, err := s.repo.FindActiveFileByShareToken(token)
	if err != nil {
		return nil, err
	}
	if s.downloadPolicy != nil {
		if err := s.downloadPolicy.CheckFileObjectDownload(file.ObjectID); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSharedFileUnavailable, err)
		}
	}

	return file, nil
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
	return s.SaveToUserFilesFromIP(userID, token, password, parentID, "")
}

func (s *Service) SaveToUserFilesFromIP(
	userID int64,
	token string,
	password string,
	parentID *int64,
	ipHash string,
) (*filemodule.UserFile, error) {
	if s.fileSaver == nil {
		return nil, ErrShareSaveUnavailable
	}

	share, err := s.repo.FindByToken(token)
	if err != nil {
		_ = s.audit(token, ipHash, AccessSave, AccessDenied)
		return nil, err
	}
	if s.accessControl.PasswordLocked(token, ipHash) {
		_ = s.audit(token, ipHash, AccessSave, AccessLocked)
		return nil, ErrSharePasswordLocked
	}
	if err := validateShareAccess(share, password, true); err != nil {
		if errors.Is(err, ErrSharePasswordInvalid) {
			s.accessControl.RecordPasswordFailure(token, ipHash)
		}
		_ = s.audit(token, ipHash, AccessSave, accessResult(err))
		return nil, err
	}
	s.accessControl.ClearPasswordFailures(token, ipHash)

	sharedFile, err := s.repo.FindActiveFileByShareToken(token)
	if err != nil {
		_ = s.audit(token, ipHash, AccessSave, AccessDenied)
		return nil, err
	}
	if s.downloadPolicy != nil {
		if err := s.downloadPolicy.CheckFileObjectDownload(sharedFile.ObjectID); err != nil {
			_ = s.audit(token, ipHash, AccessSave, AccessDenied)
			return nil, fmt.Errorf("%w: %v", ErrSharedFileUnavailable, err)
		}
	}
	if !s.accessControl.AllowDownload(token, ipHash) {
		_ = s.audit(token, ipHash, AccessSave, AccessRateLimited)
		return nil, ErrShareDownloadRateLimit
	}

	reserved, err := s.repo.ReserveDownload(token)
	if err != nil {
		_ = s.audit(token, ipHash, AccessSave, AccessDenied)
		return nil, err
	}
	if !reserved {
		_ = s.audit(token, ipHash, AccessSave, AccessDenied)
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
			_ = s.audit(token, ipHash, AccessSave, AccessDenied)
			return nil, fmt.Errorf("save shared file: %w (release reservation: %v)", err, releaseErr)
		}
		_ = s.audit(token, ipHash, AccessSave, AccessDenied)
		return nil, err
	}

	if err := s.audit(token, ipHash, AccessSave, AccessAllowed); err != nil {
		return nil, err
	}
	return file, nil
}

func (s *Service) audit(token string, ipHash string, action AccessAction, result AccessResult) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.RecordShareAccess(AccessAudit{Token: token, IPHash: ipHash, Action: action, Result: result})
}

func accessResult(err error) AccessResult {
	if errors.Is(err, ErrSharePasswordLocked) {
		return AccessLocked
	}
	if errors.Is(err, ErrShareDownloadRateLimit) {
		return AccessRateLimited
	}
	return AccessDenied
}

func validateShareAccess(share *Share, password string, requireDownloadSlot bool) error {
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

	if requireDownloadSlot && share.MaxDownloads != nil && share.DownloadCount >= *share.MaxDownloads {
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
