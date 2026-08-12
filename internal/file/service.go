package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	"github.com/SeanidHau/CloudBox/internal/scanner"
)

var (
	ErrOriginalNameRequired     = errors.New("original name is required")
	ErrContentRequired          = errors.New("file content is required")
	ErrFileHashRequired         = errors.New("file hash is required")
	ErrFolderNameRequired       = errors.New("folder name is required")
	ErrFolderAlreadyExists      = errors.New("folder already exists")
	ErrFolderMoveCycle          = errors.New("folder cannot be moved into itself or its descendant")
	ErrFolderNotEmpty           = errors.New("folder is not empty")
	ErrStorageQuotaExceeded     = errors.New("storage quota exceeded")
	ErrFileIntegrityMismatch    = errors.New("file content does not match stored hash")
	ErrJobQueueUnavailable      = errors.New("background job queue is unavailable")
	ErrVirusScannerUnavailable  = errors.New("virus scanner is unavailable")
	ErrFileScanIncomplete       = errors.New("file is unavailable until virus scan completes")
	ErrFileInfected             = errors.New("file is infected")
	ErrInlinePreviewUnsupported = errors.New("file type does not support inline preview")
)

type Storage interface {
	Save(reader io.Reader, originalName string) (string, int64, string, error)
	Open(storagePath string) (io.ReadSeekCloser, error)
	Delete(storagePath string) error
}

type StorageUsageCache interface {
	Get(userID int64) (usedBytes int64, found bool, err error)
	Set(userID int64, usedBytes int64, ttl time.Duration) error
	Delete(userID int64) error
}

type StorageQuotaProvider interface {
	StorageQuotaBytes(userID int64) (int64, error)
}

type JobEnqueuer interface {
	EnqueueForUser(
		userID int64,
		jobType string,
		payload any,
	) (*jobmodule.Job, error)
}

type ServiceOption func(*Service)

func WithStorageUsageCache(cache StorageUsageCache, ttl time.Duration) ServiceOption {
	return func(service *Service) {
		if cache == nil || ttl <= 0 {
			return
		}

		service.storageUsageCache = cache
		service.storageUsageCacheTTL = ttl
	}
}

func WithStorageQuotaProvider(provider StorageQuotaProvider) ServiceOption {
	return func(service *Service) {
		if provider != nil {
			service.storageQuotaProvider = provider
		}
	}
}

func WithTrashRetention(retention time.Duration) ServiceOption {
	return func(service *Service) {
		if retention > 0 {
			service.trashRetention = retention
		}
	}
}

func WithJobEnqueuer(enqueuer JobEnqueuer) ServiceOption {
	return func(service *Service) {
		if enqueuer != nil {
			service.jobEnqueuer = enqueuer
		}
	}
}

func WithVirusScanner(virusScanner scanner.Scanner) ServiceOption {
	return func(service *Service) {
		if virusScanner != nil {
			service.virusScanner = virusScanner
		}
	}
}

func WithVirusScanTimeout(timeout time.Duration) ServiceOption {
	return func(service *Service) {
		if timeout > 0 {
			service.virusScanTimeout = timeout
		}
	}
}

// WithVideoThumbnailExtractor replaces the ffmpeg-backed frame extractor.
// Tests use it to avoid depending on a local media tool installation.
func WithVideoThumbnailExtractor(extractor VideoThumbnailExtractor) ServiceOption {
	return func(service *Service) {
		if extractor != nil {
			service.videoThumbnailExtractor = extractor
		}
	}
}

type Service struct {
	repo                    *Repository
	storage                 Storage
	storageQuotaBytes       int64
	storageUsageCache       StorageUsageCache
	storageUsageCacheTTL    time.Duration
	storageQuotaProvider    StorageQuotaProvider
	trashRetention          time.Duration
	jobEnqueuer             JobEnqueuer
	virusScanner            scanner.Scanner
	virusScanTimeout        time.Duration
	videoThumbnailExtractor VideoThumbnailExtractor
}

func NewService(
	repo *Repository,
	storage Storage,
	storageQuotaBytes int64,
	options ...ServiceOption,
) *Service {
	service := &Service{
		repo:                    repo,
		storage:                 storage,
		storageQuotaBytes:       storageQuotaBytes,
		videoThumbnailExtractor: NewFFmpegVideoThumbnailExtractor(),
	}

	for _, option := range options {
		if option != nil {
			option(service)
		}
	}

	return service
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

	if err := s.EnsureStorageQuota(userID, size); err != nil {
		_ = s.storage.Delete(storagePath)
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

	file, err := s.repo.CreateWithObjectInFolder(
		userID,
		parentID,
		originalName,
		object,
	)
	if err != nil {
		return nil, err
	}

	s.invalidateStorageUsageCache(userID)
	if s.virusScanner == nil {
		// 扫描关闭时保持原有行为：图片上传后直接生成缩略图。
		s.enqueueThumbnailIfSupported(userID, file.ID, object)
	} else {
		// 扫描开启时，缩略图必须等到文件被确认 clean 后再生成。
		s.enqueueFileScanIfEnabled(userID, file.ID, object)
	}

	return s.withAvailability(file)
}

func (s *Service) ListActive(userID int64) ([]UserFile, error) {
	return s.ListActiveInFolder(userID, nil)
}

func (s *Service) SearchActive(userID int64, filter SearchFilter) ([]UserFile, error) {
	files, err := s.repo.SearchActive(userID, filter)
	if err != nil {
		return nil, err
	}
	for index := range files {
		if _, err := s.withAvailability(&files[index]); err != nil {
			return nil, err
		}
	}
	return files, nil
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

	files, err := s.repo.ListActiveInFolder(userID, parentID)
	if err != nil {
		return nil, err
	}

	for index := range files {
		if _, err := s.withAvailability(&files[index]); err != nil {
			return nil, err
		}
	}

	return files, nil
}

func (s *Service) ListDeleted(userID int64) ([]UserFile, error) {
	files, err := s.repo.ListDeleted(userID)
	if err != nil {
		return nil, err
	}

	for index := range files {
		if _, err := s.withAvailability(&files[index]); err != nil {
			return nil, err
		}
		if files[index].DeletedAt.Valid && s.trashRetention > 0 {
			cleanupAt := files[index].DeletedAt.Time.Add(s.trashRetention)
			files[index].CleanupAt = &cleanupAt
		}
	}

	return files, nil
}

// withAvailability maps scan internals to the three product states clients
// need. The scan signature and worker details remain server-side only.
func (s *Service) withAvailability(file *UserFile) (*UserFile, error) {
	if file == nil {
		return nil, ErrFileNotFound
	}
	if file.Status != StatusActive {
		file.Availability = AvailabilityUnavailable
		return file, nil
	}
	if s.virusScanner == nil {
		file.Availability = AvailabilityReady
		return file, nil
	}

	object, err := s.repo.FindObjectForActiveFile(file.ID)
	if err != nil {
		return nil, err
	}
	scan, err := s.repo.FindFileScanByObjectID(object.ID)
	if errors.Is(err, ErrFileScanNotFound) {
		file.Availability = AvailabilityProcessing
		return file, nil
	}
	if err != nil {
		return nil, err
	}

	switch scan.Status {
	case ScanStatusClean:
		file.Availability = AvailabilityReady
	case ScanStatusInfected:
		file.Availability = AvailabilityUnavailable
	default:
		file.Availability = AvailabilityProcessing
	}

	return file, nil
}

func (s *Service) OpenForDownload(userID int64, fileID int64) (*UserFile, io.ReadSeekCloser, error) {
	file, err := s.repo.FindActiveByID(userID, fileID)
	if err != nil {
		return nil, nil, err
	}

	object, err := s.repo.FindObjectForActiveFile(fileID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.CheckFileObjectDownload(object.ID); err != nil {
		return nil, nil, err
	}

	reader, err := s.storage.Open(file.StoragePath)
	if err != nil {
		return nil, nil, err
	}

	return file, reader, nil
}

func (s *Service) OpenThumbnailForDownload(
	userID int64,
	fileID int64,
) (*FilePreview, io.ReadSeekCloser, error) {
	preview, err := s.repo.FindFilePreviewForActiveFile(userID, fileID)
	if err != nil {
		return nil, nil, err
	}

	if err := s.CheckFileObjectDownload(preview.FileObjectID); err != nil {
		return nil, nil, err
	}

	reader, err := s.storage.Open(preview.StoragePath)
	if err != nil {
		return nil, nil, err
	}

	return preview, reader, nil
}

// OpenInlinePreview opens the original image for an authenticated owner. It
// deliberately excludes video and unknown binary types so the client cannot
// present an unfinished streaming feature as a preview.
func (s *Service) OpenInlinePreview(userID int64, fileID int64) (*UserFile, io.ReadSeekCloser, error) {
	file, err := s.repo.FindActiveByID(userID, fileID)
	if err != nil {
		return nil, nil, err
	}
	if !SupportsInlinePreview(file.ContentType) {
		return nil, nil, ErrInlinePreviewUnsupported
	}

	object, err := s.repo.FindObjectForActiveFile(fileID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.CheckFileObjectDownload(object.ID); err != nil {
		return nil, nil, err
	}
	reader, err := s.storage.Open(file.StoragePath)
	if err != nil {
		return nil, nil, err
	}

	return file, reader, nil
}

func SupportsInlinePreview(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func (s *Service) CheckFileObjectDownload(fileObjectID int64) error {
	if s.virusScanner == nil {
		return nil
	}

	scan, err := s.repo.FindFileScanByObjectID(fileObjectID)
	if errors.Is(err, ErrFileScanNotFound) {
		return ErrFileScanIncomplete
	}
	if err != nil {
		return err
	}

	switch scan.Status {
	case ScanStatusClean:
		return nil
	case ScanStatusInfected:
		return ErrFileInfected
	default:
		return ErrFileScanIncomplete
	}
}

func (s *Service) VerifyActiveFile(ctx context.Context, fileID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	object, err := s.repo.FindObjectForActiveFile(fileID)
	if err != nil {
		return err
	}

	reader, err := s.storage.Open(object.StoragePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, reader); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != object.FileHash {
		return ErrFileIntegrityMismatch
	}

	return nil
}

func (s *Service) ScanActiveFile(ctx context.Context, userID int64, fileID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.virusScanner == nil {
		return ErrVirusScannerUnavailable
	}

	userFile, err := s.repo.FindActiveByID(userID, fileID)
	if err != nil {
		return err
	}

	object, err := s.repo.FindObjectForActiveFile(fileID)
	if err != nil {
		return err
	}

	if _, _, err := s.repo.CreatePendingFileScan(object.ID); err != nil {
		return fmt.Errorf("create file scan: %w", err)
	}

	_, claimed, err := s.repo.ClaimFileScan(object.ID)
	if err != nil {
		return fmt.Errorf("claim file scan: %w", err)
	}
	if !claimed {
		return nil
	}

	source, err := s.storage.Open(object.StoragePath)
	if err != nil {
		return s.markFileScanFailed(
			object.ID,
			fmt.Errorf("open file for virus scan: %w", err),
		)
	}
	defer source.Close()

	scanContext := ctx
	cancel := func() {}

	if s.virusScanTimeout > 0 {
		scanContext, cancel = context.WithTimeout(ctx, s.virusScanTimeout)
	}
	defer cancel()

	result, err := s.virusScanner.Scan(scanContext, source)
	if err != nil {
		return s.markFileScanFailed(
			object.ID,
			fmt.Errorf("scan file content: %w", err),
		)
	}

	if _, err := s.repo.CompleteFileScan(
		object.ID,
		result.Infected,
		result.Signature,
	); err != nil {
		return fmt.Errorf("complete file scan: %w", err)
	}
	if !result.Infected {
		// 只有通过扫描的图片才允许进入后续缩略图解码流程。
		s.enqueueThumbnailIfSupported(userFile.UserID, userFile.ID, object)
	}

	return nil
}

func (s *Service) markFileScanFailed(fileObjectID int64, cause error) error {
	if _, err := s.repo.FailFileScan(fileObjectID); err != nil {
		return fmt.Errorf("%w; mark file scan failed: %v", cause, err)
	}

	return cause
}

func (s *Service) EnqueueFileVerification(
	userID int64,
	fileID int64,
) (*jobmodule.Job, error) {
	if _, err := s.repo.FindActiveByID(userID, fileID); err != nil {
		return nil, err
	}

	if s.jobEnqueuer == nil {
		return nil, ErrJobQueueUnavailable
	}

	return s.jobEnqueuer.EnqueueForUser(
		userID,
		jobmodule.TypeVerifyFile,
		VerifyFilePayload{FileID: fileID},
	)
}

func (s *Service) SoftDelete(userID int64, fileID int64) error {
	return s.repo.SoftDelete(userID, fileID)
}

func (s *Service) SoftDeleteWithShareOption(userID int64, fileID int64, keepShares bool) error {
	return s.repo.SoftDeleteWithShareOption(userID, fileID, keepShares)
}

func (s *Service) Restore(userID int64, fileID int64) error {
	return s.repo.Restore(userID, fileID)
}

func (s *Service) PermanentlyDelete(userID int64, fileID int64) error {
	unreferenced, err := s.repo.PermanentlyDeleteDeleted(userID, fileID)
	if err != nil {
		return err
	}

	s.invalidateStorageUsageCache(userID)

	if unreferenced == nil {
		return nil
	}

	if err := s.storage.Delete(unreferenced.Object.StoragePath); err != nil {
		slog.Error(
			"delete unreferenced file object failed",
			"object_id", unreferenced.Object.ID,
			"user_id", userID,
			"storage_path", unreferenced.Object.StoragePath,
			"error", err,
		)
	}

	if unreferenced.Preview != nil {
		if err := s.storage.Delete(unreferenced.Preview.StoragePath); err != nil {
			slog.Error(
				"delete unreferenced file preview failed",
				"object_id", unreferenced.Object.ID,
				"user_id", userID,
				"storage_path", unreferenced.Preview.StoragePath,
				"error", err,
			)
		}
	}

	return nil
}

func (s *Service) CleanupDeletedBefore(before time.Time) (int, error) {
	files, err := s.repo.ListDeletedBefore(before)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	for _, file := range files {
		if err := s.PermanentlyDelete(file.UserID, file.ID); err != nil {
			if errors.Is(err, ErrFileNotFound) {
				continue
			}

			return cleaned, err
		}

		cleaned++
	}

	return cleaned, nil
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

	if err := s.EnsureStorageQuota(userID, object.Size); err != nil {
		return nil, err
	}

	file, err := s.repo.CreateWithObjectInFolder(userID, parentID, originalName, object)
	if err != nil {
		return nil, err
	}

	s.invalidateStorageUsageCache(userID)
	if s.virusScanner == nil {
		// 秒传在未启用扫描时沿用原有缩略图流程。
		s.enqueueThumbnailIfSupported(userID, file.ID, object)
	} else {
		// 已启用扫描时，即使对象来自去重也必须先检查扫描状态。
		s.enqueueFileScanIfEnabled(userID, file.ID, object)
	}

	return s.withAvailability(file)
}

// InstantUploadManyIntoFolder saves several references to existing objects in
// a single transaction. It is used for "save all" on a shared collection.
func (s *Service) InstantUploadManyIntoFolder(
	userID int64,
	parentID *int64,
	inputs []InstantUploadInput,
) ([]UserFile, error) {
	if len(inputs) == 0 {
		return nil, ErrFileHashRequired
	}

	objects := make([]*FileObject, 0, len(inputs))
	originalNames := make([]string, 0, len(inputs))
	for _, input := range inputs {
		originalName := strings.TrimSpace(input.OriginalName)
		fileHash := strings.TrimSpace(input.FileHash)
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
		objects = append(objects, object)
		originalNames = append(originalNames, originalName)
	}

	quotaBytes, err := s.storageQuotaForUser(userID)
	if err != nil {
		return nil, err
	}
	files, err := s.repo.CreateManyWithObjectsInFolder(userID, parentID, objects, originalNames, quotaBytes)
	if err != nil {
		return nil, err
	}

	s.invalidateStorageUsageCache(userID)
	for index := range files {
		if s.virusScanner == nil {
			s.enqueueThumbnailIfSupported(userID, files[index].ID, objects[index])
		} else {
			s.enqueueFileScanIfEnabled(userID, files[index].ID, objects[index])
		}
		if _, err = s.withAvailability(&files[index]); err != nil {
			return nil, err
		}
	}
	return files, nil
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

func (s *Service) GetStorageUsage(userID int64) (*StorageUsage, error) {
	quotaBytes, err := s.storageQuotaForUser(userID)
	if err != nil {
		return nil, err
	}
	if s.storageUsageCache != nil {
		usedBytes, found, err := s.storageUsageCache.Get(userID)
		if err == nil && found {
			return s.newStorageUsage(usedBytes, quotaBytes), nil
		}
	}

	usedBytes, err := s.repo.TotalFileSizeByUser(userID)
	if err != nil {
		return nil, err
	}

	if s.storageUsageCache != nil {
		_ = s.storageUsageCache.Set(userID, usedBytes, s.storageUsageCacheTTL)
	}

	return s.newStorageUsage(usedBytes, quotaBytes), nil
}

func (s *Service) newStorageUsage(usedBytes int64, quotaBytes int64) *StorageUsage {
	availableBytes := quotaBytes - usedBytes
	if availableBytes < 0 {
		availableBytes = 0
	}

	return &StorageUsage{
		UsedBytes:      usedBytes,
		QuotaBytes:     quotaBytes,
		AvailableBytes: availableBytes,
	}
}

func (s *Service) invalidateStorageUsageCache(userID int64) {
	if s.storageUsageCache != nil {
		_ = s.storageUsageCache.Delete(userID)
	}
}

func (s *Service) EnsureStorageQuota(
	userID int64,
	additionalBytes int64,
) error {
	quotaBytes, err := s.storageQuotaForUser(userID)
	if err != nil {
		return err
	}
	usedBytes, err := s.repo.TotalFileSizeByUser(userID)
	if err != nil {
		return err
	}

	if usedBytes > quotaBytes ||
		additionalBytes > quotaBytes-usedBytes {
		return ErrStorageQuotaExceeded
	}

	return nil
}

func (s *Service) storageQuotaForUser(userID int64) (int64, error) {
	if s.storageQuotaProvider == nil {
		return s.storageQuotaBytes, nil
	}
	return s.storageQuotaProvider.StorageQuotaBytes(userID)
}

func (s *Service) enqueueFileScanIfEnabled(
	userID int64,
	fileID int64,
	object *FileObject,
) {
	if s.virusScanner == nil || s.jobEnqueuer == nil || object == nil {
		return
	}

	scan, _, err := s.repo.CreatePendingFileScan(object.ID)
	if err != nil {
		slog.Error(
			"create pending file scan",
			"user_id", userID,
			"file_id", fileID,
			"object_id", object.ID,
			"error", err,
		)
		return
	}

	if scan.Status != ScanStatusPending && scan.Status != ScanStatusFailed {
		return
	}

	if _, err := s.jobEnqueuer.EnqueueForUser(
		userID,
		jobmodule.TypeScanFile,
		ScanFilePayload{FileID: fileID},
	); err != nil {
		slog.Error(
			"enqueue file scan job",
			"user_id", userID,
			"file_id", fileID,
			"object_id", object.ID,
			"error", err,
		)
	}
}

func (s *Service) enqueueThumbnailIfSupported(
	userID int64,
	fileID int64,
	object *FileObject,
) {
	if s.jobEnqueuer == nil || object == nil || !SupportsThumbnail(object.ContentType) {
		return
	}

	if _, err := s.repo.FindFilePreviewByObjectID(object.ID); err == nil {
		return
	} else if !errors.Is(err, ErrFilePreviewNotFound) {
		slog.Error(
			"check existing file preview",
			"file_id", fileID,
			"object_id", object.ID,
			"error", err,
		)
		return
	}

	if _, err := s.jobEnqueuer.EnqueueForUser(
		userID,
		jobmodule.TypeGenerateThumbnail,
		ThumbnailPayload{FileID: fileID},
	); err != nil {
		slog.Error(
			"enqueue thumbnail job",
			"file_id", fileID,
			"object_id", object.ID,
			"error", err,
		)
	}
}
