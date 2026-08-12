package file

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SeanidHau/CloudBox/internal/database"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/005_folders.sql",
		"../../migrations/007_file_shares.sql",
		"../../migrations/010_file_preview.sql",
		"../../migrations/011_file_scans.sql",
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'user-1', 'hash-1')`); err != nil {
		t.Fatalf("insert user 1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (2, 'user-2', 'hash-2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	return NewRepository(db)
}

func TestRepositoryCreateListSoftDeleteAndRestore(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.Create(1, "test.txt", "uploads/test.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if created.Status != StatusActive {
		t.Fatalf("status = %q, want %q", created.Status, StatusActive)
	}
	if created.ParentID != nil {
		t.Fatalf("root file parent ID = %v, want nil", *created.ParentID)
	}

	active, err := repo.ListActive(1)
	if err != nil {
		t.Fatalf("list active files: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active file count = %d, want 1", len(active))
	}

	if _, err := repo.FindActiveByID(2, created.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("other user find error = %v, want %v", err, ErrFileNotFound)
	}

	if err := repo.SoftDelete(1, created.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	active, err = repo.ListActive(1)
	if err != nil {
		t.Fatalf("list active files after delete: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active file count after delete = %d, want 0", len(active))
	}

	deleted, err := repo.ListDeleted(1)
	if err != nil {
		t.Fatalf("list deleted files: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted file count = %d, want 1", len(deleted))
	}
	if !deleted[0].DeletedAt.Valid {
		t.Fatal("expected deleted_at to be set")
	}

	if err := repo.Restore(1, created.ID); err != nil {
		t.Fatalf("restore file: %v", err)
	}

	restored, err := repo.FindActiveByID(1, created.ID)
	if err != nil {
		t.Fatalf("find restored file: %v", err)
	}
	if restored.Status != StatusActive {
		t.Fatalf("restored status = %q, want %q", restored.Status, StatusActive)
	}
}

func TestRepositoryCreatesAndSharesFilePreview(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject(
		"preview-source-hash",
		"uploads/source.png",
		100,
		"image/png",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}

	firstFile, err := repo.CreateWithObject(1, "first.png", object)
	if err != nil {
		t.Fatalf("create first user file: %v", err)
	}
	secondFile, err := repo.CreateWithObject(2, "second.png", object)
	if err != nil {
		t.Fatalf("create second user file: %v", err)
	}

	preview := &FilePreview{
		FileObjectID: object.ID,
		StoragePath:  "uploads/source-preview.jpg",
		Size:         50,
		ContentType:  "image/jpeg",
		Width:        320,
		Height:       240,
	}
	created, err := repo.CreateFilePreview(preview)
	if err != nil {
		t.Fatalf("create file preview: %v", err)
	}
	if !created {
		t.Fatal("first preview insert should create a record")
	}

	created, err = repo.CreateFilePreview(preview)
	if err != nil {
		t.Fatalf("create duplicate file preview: %v", err)
	}
	if created {
		t.Fatal("duplicate preview insert should reuse the existing record")
	}

	byObject, err := repo.FindFilePreviewByObjectID(object.ID)
	if err != nil {
		t.Fatalf("find preview by object: %v", err)
	}
	if byObject.StoragePath != preview.StoragePath || byObject.Width != 320 || byObject.Height != 240 {
		t.Fatalf("preview by object = %#v, want stored preview", byObject)
	}

	for _, file := range []UserFile{*firstFile, *secondFile} {
		byFile, err := repo.FindFilePreviewForActiveFile(file.UserID, file.ID)
		if err != nil {
			t.Fatalf("find preview for user %d file %d: %v", file.UserID, file.ID, err)
		}
		if byFile.FileObjectID != object.ID {
			t.Fatalf("preview object ID = %d, want %d", byFile.FileObjectID, object.ID)
		}
	}

	if _, err := repo.FindFilePreviewForActiveFile(2, firstFile.ID); !errors.Is(err, ErrFilePreviewNotFound) {
		t.Fatalf("other user preview error = %v, want %v", err, ErrFilePreviewNotFound)
	}

	if err := repo.SoftDelete(1, firstFile.ID); err != nil {
		t.Fatalf("soft delete first file: %v", err)
	}
	if _, err := repo.FindFilePreviewForActiveFile(1, firstFile.ID); !errors.Is(err, ErrFilePreviewNotFound) {
		t.Fatalf("deleted file preview error = %v, want %v", err, ErrFilePreviewNotFound)
	}
}

func TestRepositoryCreatesAndReusesPendingFileScan(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject(
		"scan-source-hash",
		"uploads/source.bin",
		100,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}

	scan, created, err := repo.CreatePendingFileScan(object.ID)
	if err != nil {
		t.Fatalf("create pending scan: %v", err)
	}
	if !created {
		t.Fatal("first pending scan insert should create a record")
	}
	if scan.FileObjectID != object.ID || scan.Status != ScanStatusPending {
		t.Fatalf("scan = %#v, want pending state for object %d", scan, object.ID)
	}
	if scan.Signature.Valid || scan.ScannedAt.Valid {
		t.Fatalf("new scan signature/time = %#v/%#v, want null", scan.Signature, scan.ScannedAt)
	}

	reused, created, err := repo.CreatePendingFileScan(object.ID)
	if err != nil {
		t.Fatalf("create duplicate pending scan: %v", err)
	}
	if created {
		t.Fatal("duplicate pending scan insert should reuse the existing record")
	}
	if reused.FileObjectID != scan.FileObjectID || reused.Status != ScanStatusPending {
		t.Fatalf("reused scan = %#v, want existing pending scan", reused)
	}

	if _, err := repo.FindFileScanByObjectID(object.ID + 1); !errors.Is(err, ErrFileScanNotFound) {
		t.Fatalf("find missing scan error = %v, want %v", err, ErrFileScanNotFound)
	}
}

func TestRepositoryTransitionsFileScanStates(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject(
		"clean-scan-source-hash",
		"uploads/clean-source.bin",
		100,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("create clean file object: %v", err)
	}
	if _, _, err := repo.CreatePendingFileScan(object.ID); err != nil {
		t.Fatalf("create clean pending scan: %v", err)
	}

	// The conditional UPDATE makes the first worker the only worker that can claim this scan.
	claimedScan, claimed, err := repo.ClaimFileScan(object.ID)
	if err != nil {
		t.Fatalf("claim pending scan: %v", err)
	}
	if !claimed || claimedScan.Status != ScanStatusScanning {
		t.Fatalf("claimed scan = %#v, claimed = %t, want scanning/true", claimedScan, claimed)
	}

	// A second worker sees the current state but cannot take over an in-progress scan.
	repeatedClaim, claimed, err := repo.ClaimFileScan(object.ID)
	if err != nil {
		t.Fatalf("claim already running scan: %v", err)
	}
	if claimed || repeatedClaim.Status != ScanStatusScanning {
		t.Fatalf("repeated claim = %#v, claimed = %t, want scanning/false", repeatedClaim, claimed)
	}

	cleanScan, err := repo.CompleteFileScan(object.ID, false, "")
	if err != nil {
		t.Fatalf("complete clean scan: %v", err)
	}
	if cleanScan.Status != ScanStatusClean || cleanScan.Signature.Valid || !cleanScan.ScannedAt.Valid {
		t.Fatalf("clean scan = %#v, want clean status without signature and with scanned time", cleanScan)
	}

	terminalClaim, claimed, err := repo.ClaimFileScan(object.ID)
	if err != nil {
		t.Fatalf("claim terminal scan: %v", err)
	}
	if claimed || terminalClaim.Status != ScanStatusClean {
		t.Fatalf("terminal claim = %#v, claimed = %t, want clean/false", terminalClaim, claimed)
	}

	infectedObject, err := repo.CreateFileObject(
		"infected-scan-source-hash",
		"uploads/infected-source.bin",
		100,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("create infected file object: %v", err)
	}
	if _, _, err := repo.CreatePendingFileScan(infectedObject.ID); err != nil {
		t.Fatalf("create infected pending scan: %v", err)
	}
	if _, claimed, err := repo.ClaimFileScan(infectedObject.ID); err != nil || !claimed {
		t.Fatalf("claim infected scan = claimed:%t err:%v, want true/nil", claimed, err)
	}

	// An infected result persists the signature that ClamAV reports to the application.
	infectedScan, err := repo.CompleteFileScan(infectedObject.ID, true, "Eicar-Test-Signature")
	if err != nil {
		t.Fatalf("complete infected scan: %v", err)
	}
	if infectedScan.Status != ScanStatusInfected || !infectedScan.Signature.Valid || infectedScan.Signature.String != "Eicar-Test-Signature" || !infectedScan.ScannedAt.Valid {
		t.Fatalf("infected scan = %#v, want infected state with signature and scanned time", infectedScan)
	}

	retryObject, err := repo.CreateFileObject(
		"retry-scan-source-hash",
		"uploads/retry-source.bin",
		100,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("create retry file object: %v", err)
	}
	if _, _, err := repo.CreatePendingFileScan(retryObject.ID); err != nil {
		t.Fatalf("create retry pending scan: %v", err)
	}
	if _, claimed, err := repo.ClaimFileScan(retryObject.ID); err != nil || !claimed {
		t.Fatalf("claim retry scan = claimed:%t err:%v, want true/nil", claimed, err)
	}

	// Failed scans keep no completion time and are eligible for a later retry.
	failedScan, err := repo.FailFileScan(retryObject.ID)
	if err != nil {
		t.Fatalf("fail running scan: %v", err)
	}
	if failedScan.Status != ScanStatusFailed || failedScan.Signature.Valid || failedScan.ScannedAt.Valid {
		t.Fatalf("failed scan = %#v, want failed status without signature or scanned time", failedScan)
	}

	retriedScan, claimed, err := repo.ClaimFileScan(retryObject.ID)
	if err != nil {
		t.Fatalf("claim failed scan for retry: %v", err)
	}
	if !claimed || retriedScan.Status != ScanStatusScanning {
		t.Fatalf("retried scan = %#v, claimed = %t, want scanning/true", retriedScan, claimed)
	}
}

func TestRepositoryPermanentlyDeleteDeletedFile(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject("permanent-delete-hash", "uploads/permanent.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	file, err := repo.CreateWithObject(1, "permanent.txt", object)
	if err != nil {
		t.Fatalf("create user file: %v", err)
	}
	if _, err := repo.db.Exec(`INSERT INTO file_shares (token, user_file_id) VALUES ($1, $2)`, "permanent-share", file.ID); err != nil {
		t.Fatalf("create file share: %v", err)
	}
	if _, err := repo.CreateFilePreview(&FilePreview{
		FileObjectID: object.ID,
		StoragePath:  "uploads/permanent-preview.png",
		Size:         50,
		ContentType:  "image/png",
		Width:        320,
		Height:       160,
	}); err != nil {
		t.Fatalf("create file preview: %v", err)
	}
	if err := repo.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	objectToDelete, err := repo.PermanentlyDeleteDeleted(1, file.ID)
	if err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}
	if objectToDelete == nil || objectToDelete.Object.ID != object.ID {
		t.Fatalf("object to delete = %#v, want object %d", objectToDelete, object.ID)
	}
	if objectToDelete.Preview == nil || objectToDelete.Preview.StoragePath != "uploads/permanent-preview.png" {
		t.Fatalf("preview to delete = %#v, want stored preview", objectToDelete.Preview)
	}
	if _, err := repo.FindDeletedByID(1, file.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("find permanently deleted file error = %v, want %v", err, ErrFileNotFound)
	}
	if _, err := repo.FindFileObjectByHash(object.FileHash); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("find deleted object error = %v, want %v", err, ErrFileObjectNotFound)
	}

	var shareCount int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM file_shares WHERE user_file_id = $1`, file.ID).Scan(&shareCount); err != nil {
		t.Fatalf("count file shares: %v", err)
	}
	if shareCount != 0 {
		t.Fatalf("share count = %d, want 0", shareCount)
	}
}

func TestRepositoryPermanentlyDeleteKeepsSharedObject(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject("shared-permanent-delete-hash", "uploads/shared.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	first, err := repo.CreateWithObject(1, "first.txt", object)
	if err != nil {
		t.Fatalf("create first user file: %v", err)
	}
	second, err := repo.CreateWithObject(2, "second.txt", object)
	if err != nil {
		t.Fatalf("create second user file: %v", err)
	}
	if err := repo.SoftDelete(1, first.ID); err != nil {
		t.Fatalf("soft delete first file: %v", err)
	}
	if _, err := repo.CreateFilePreview(&FilePreview{
		FileObjectID: object.ID,
		StoragePath:  "uploads/shared-preview.png",
		Size:         50,
		ContentType:  "image/png",
		Width:        320,
		Height:       160,
	}); err != nil {
		t.Fatalf("create shared preview: %v", err)
	}

	objectToDelete, err := repo.PermanentlyDeleteDeleted(1, first.ID)
	if err != nil {
		t.Fatalf("permanently delete first file: %v", err)
	}
	if objectToDelete != nil {
		t.Fatalf("object to delete = %#v, want nil while another file references it", objectToDelete)
	}

	remainingObject, err := repo.FindFileObjectByHash(object.FileHash)
	if err != nil {
		t.Fatalf("find shared object: %v", err)
	}
	if remainingObject.ReferenceCount != 1 {
		t.Fatalf("reference count = %d, want 1", remainingObject.ReferenceCount)
	}
	if _, err := repo.FindActiveByID(2, second.ID); err != nil {
		t.Fatalf("find remaining user file: %v", err)
	}
	if _, err := repo.FindFilePreviewByObjectID(object.ID); err != nil {
		t.Fatalf("find preview for shared object: %v", err)
	}
}

func TestRepositoryPermanentlyDeleteRequiresTrashFile(t *testing.T) {
	repo := newTestRepository(t)

	object, err := repo.CreateFileObject("active-permanent-delete-hash", "uploads/active.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	file, err := repo.CreateWithObject(1, "active.txt", object)
	if err != nil {
		t.Fatalf("create user file: %v", err)
	}

	if _, err := repo.PermanentlyDeleteDeleted(1, file.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("permanently delete active file error = %v, want %v", err, ErrFileNotFound)
	}
	if _, err := repo.FindActiveByID(1, file.ID); err != nil {
		t.Fatalf("find active file after rejected delete: %v", err)
	}
}

func TestRepositoryListDeletedBefore(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC().Truncate(time.Second)

	oldFile, err := repo.Create(1, "old.txt", "uploads/old.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create old file: %v", err)
	}
	recentFile, err := repo.Create(2, "recent.txt", "uploads/recent.txt", 1, "text/plain")
	if err != nil {
		t.Fatalf("create recent file: %v", err)
	}
	if err := repo.SoftDelete(1, oldFile.ID); err != nil {
		t.Fatalf("soft delete old file: %v", err)
	}
	if err := repo.SoftDelete(2, recentFile.ID); err != nil {
		t.Fatalf("soft delete recent file: %v", err)
	}

	if _, err := repo.db.Exec(`UPDATE user_files SET deleted_at = $1 WHERE id = $2`, now.Add(-2*time.Hour), oldFile.ID); err != nil {
		t.Fatalf("set old deleted time: %v", err)
	}
	if _, err := repo.db.Exec(`UPDATE user_files SET deleted_at = $1 WHERE id = $2`, now.Add(-30*time.Minute), recentFile.ID); err != nil {
		t.Fatalf("set recent deleted time: %v", err)
	}

	files, err := repo.ListDeletedBefore(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("list expired deleted files: %v", err)
	}
	if len(files) != 1 || files[0].ID != oldFile.ID {
		t.Fatalf("expired deleted files = %#v, want old file", files)
	}
}

func TestRepositoryListActiveInFolder(t *testing.T) {
	repo := newTestRepository(t)

	folder, err := repo.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	object, err := repo.CreateFileObject(
		"folder-list-hash",
		"uploads/shared.txt",
		15,
		"text/plain",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}

	rootFile, err := repo.CreateWithObject(1, "root.txt", object)
	if err != nil {
		t.Fatalf("create root file: %v", err)
	}
	folderFile, err := repo.CreateWithObjectInFolder(1, &folder.ID, "report.txt", object)
	if err != nil {
		t.Fatalf("create folder file: %v", err)
	}

	rootFiles, err := repo.ListActive(1)
	if err != nil {
		t.Fatalf("list root files: %v", err)
	}
	if len(rootFiles) != 1 || rootFiles[0].ID != rootFile.ID {
		t.Fatalf("root files = %#v, want root.txt", rootFiles)
	}

	folderFiles, err := repo.ListActiveInFolder(1, &folder.ID)
	if err != nil {
		t.Fatalf("list folder files: %v", err)
	}
	if len(folderFiles) != 1 || folderFiles[0].ID != folderFile.ID {
		t.Fatalf("folder files = %#v, want report.txt", folderFiles)
	}
}

func TestRepositoryMoveActiveFile(t *testing.T) {
	repo := newTestRepository(t)

	folder, err := repo.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	created, err := repo.Create(1, "report.txt", "uploads/report.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	moved, err := repo.MoveActive(1, created.ID, &folder.ID)
	if err != nil {
		t.Fatalf("move file into folder: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != folder.ID {
		t.Fatalf("moved parent ID = %v, want %d", moved.ParentID, folder.ID)
	}

	movedToRoot, err := repo.MoveActive(1, created.ID, nil)
	if err != nil {
		t.Fatalf("move file to root: %v", err)
	}
	if movedToRoot.ParentID != nil {
		t.Fatalf("root parent ID = %v, want nil", movedToRoot.ParentID)
	}

	if err := repo.SoftDelete(1, created.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	if _, err := repo.MoveActive(1, created.ID, &folder.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("move deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestRepositoryRenameActiveFile(t *testing.T) {
	repo := newTestRepository(t)
	created, err := repo.Create(1, "draft.txt", "uploads/object.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	renamed, err := repo.RenameActive(1, created.ID, "final.txt")
	if err != nil {
		t.Fatalf("rename file: %v", err)
	}
	if renamed.OriginalName != "final.txt" {
		t.Fatalf("original name = %q, want %q", renamed.OriginalName, "final.txt")
	}
	if renamed.StoragePath != created.StoragePath {
		t.Fatalf("storage path = %q, want unchanged %q", renamed.StoragePath, created.StoragePath)
	}

	if err := repo.SoftDelete(1, created.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	if _, err := repo.RenameActive(1, created.ID, "deleted.txt"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("rename deleted file error = %v, want %v", err, ErrFileNotFound)
	}
}

func TestRepositoryRenameFolder(t *testing.T) {
	repo := newTestRepository(t)
	folder, err := repo.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	renamed, err := repo.RenameFolder(1, folder.ID, "work")
	if err != nil {
		t.Fatalf("rename folder: %v", err)
	}
	if renamed.Name != "work" {
		t.Fatalf("folder name = %q, want %q", renamed.Name, "work")
	}

	if _, err := repo.RenameFolder(2, folder.ID, "private"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user rename error = %v, want %v", err, ErrFolderNotFound)
	}
}

func TestRepositoryMoveFolder(t *testing.T) {
	repo := newTestRepository(t)

	documents, err := repo.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create documents folder: %v", err)
	}
	photos, err := repo.CreateFolder(1, &documents.ID, "photos")
	if err != nil {
		t.Fatalf("create photos folder: %v", err)
	}
	archive, err := repo.CreateFolder(1, nil, "archive")
	if err != nil {
		t.Fatalf("create archive folder: %v", err)
	}

	moved, err := repo.MoveFolder(1, documents.ID, &archive.ID)
	if err != nil {
		t.Fatalf("move documents folder: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != archive.ID {
		t.Fatalf("moved parent ID = %v, want %d", moved.ParentID, archive.ID)
	}

	// 子文件夹仍引用 documents，因此整棵子树会自然随父目录移动。
	foundPhotos, err := repo.FindFolderByID(1, photos.ID)
	if err != nil {
		t.Fatalf("find child folder: %v", err)
	}
	if foundPhotos.ParentID == nil || *foundPhotos.ParentID != documents.ID {
		t.Fatalf("child parent ID = %v, want %d", foundPhotos.ParentID, documents.ID)
	}

	movedToRoot, err := repo.MoveFolder(1, documents.ID, nil)
	if err != nil {
		t.Fatalf("move folder to root: %v", err)
	}
	if movedToRoot.ParentID != nil {
		t.Fatalf("root parent ID = %v, want nil", movedToRoot.ParentID)
	}
}

func TestRepositoryDeleteEmptyFolder(t *testing.T) {
	repo := newTestRepository(t)

	empty, err := repo.CreateFolder(1, nil, "empty")
	if err != nil {
		t.Fatalf("create empty folder: %v", err)
	}
	deleted, err := repo.DeleteEmptyFolder(1, empty.ID)
	if err != nil {
		t.Fatalf("delete empty folder: %v", err)
	}
	if !deleted {
		t.Fatal("expected empty folder to be deleted")
	}

	parent, err := repo.CreateFolder(1, nil, "parent")
	if err != nil {
		t.Fatalf("create parent folder: %v", err)
	}
	if _, err := repo.CreateFolder(1, &parent.ID, "child"); err != nil {
		t.Fatalf("create child folder: %v", err)
	}
	deleted, err = repo.DeleteEmptyFolder(1, parent.ID)
	if err != nil {
		t.Fatalf("delete folder with child: %v", err)
	}
	if deleted {
		t.Fatal("folder with child should not be deleted")
	}

	withFile, err := repo.CreateFolder(1, nil, "with-file")
	if err != nil {
		t.Fatalf("create file folder: %v", err)
	}
	object, err := repo.CreateFileObject("delete-folder-hash", "uploads/object.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	file, err := repo.CreateWithObjectInFolder(1, &withFile.ID, "report.txt", object)
	if err != nil {
		t.Fatalf("create folder file: %v", err)
	}
	if err := repo.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}
	deleted, err = repo.DeleteEmptyFolder(1, withFile.ID)
	if err != nil {
		t.Fatalf("delete folder with trashed file: %v", err)
	}
	if deleted {
		t.Fatal("folder with trashed file should not be deleted")
	}
}

func TestRepositoryTotalFileSizeByUser(t *testing.T) {
	repo := newTestRepository(t)

	first, err := repo.Create(1, "first.txt", "uploads/first.txt", 5, "text/plain")
	if err != nil {
		t.Fatalf("create first file: %v", err)
	}
	if _, err := repo.Create(1, "second.txt", "uploads/second.txt", 6, "text/plain"); err != nil {
		t.Fatalf("create second file: %v", err)
	}
	if _, err := repo.Create(2, "private.txt", "uploads/private.txt", 100, "text/plain"); err != nil {
		t.Fatalf("create other user file: %v", err)
	}
	if err := repo.SoftDelete(1, first.ID); err != nil {
		t.Fatalf("soft delete first file: %v", err)
	}

	total, err := repo.TotalFileSizeByUser(1)
	if err != nil {
		t.Fatalf("get user file size: %v", err)
	}
	if total != 11 {
		t.Fatalf("total size = %d, want 11", total)
	}
}

func TestRepositoryCreateAndFindFileObject(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.CreateFileObject(
		"a9c2a8c997d2a80c4756e14b6c80e7a5ed8f0262ba1e430ac0c0e751ea0b3abe",
		"uploads/object.txt",
		15,
		"text/plain",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected file object ID")
	}
	if created.ReferenceCount != 0 {
		t.Fatalf("reference count = %d, want 0", created.ReferenceCount)
	}

	found, err := repo.FindFileObjectByHash(created.FileHash)
	if err != nil {
		t.Fatalf("find file object: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("found object ID = %d, want %d", found.ID, created.ID)
	}
	if found.StoragePath != "uploads/object.txt" {
		t.Fatalf("storage path = %q, want %q", found.StoragePath, "uploads/object.txt")
	}
	if found.Size != 15 {
		t.Fatalf("size = %d, want %d", found.Size, 15)
	}

	if _, err := repo.FindFileObjectByHash("missing-hash"); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("missing object error = %v, want %v", err, ErrFileObjectNotFound)
	}
}

func TestRepositoryCreateFindAndListFolders(t *testing.T) {
	repo := newTestRepository(t)

	documents, err := repo.CreateFolder(1, nil, "documents")
	if err != nil {
		t.Fatalf("create documents folder: %v", err)
	}
	if documents.ParentID != nil {
		t.Fatalf("root folder parent ID = %v, want nil", *documents.ParentID)
	}

	_, err = repo.CreateFolder(1, nil, "music")
	if err != nil {
		t.Fatalf("create music folder: %v", err)
	}

	photos, err := repo.CreateFolder(1, &documents.ID, "photos")
	if err != nil {
		t.Fatalf("create child folder: %v", err)
	}
	if photos.ParentID == nil || *photos.ParentID != documents.ID {
		t.Fatalf("child parent ID = %v, want %d", photos.ParentID, documents.ID)
	}

	rootFolders, err := repo.ListFolders(1, nil)
	if err != nil {
		t.Fatalf("list root folders: %v", err)
	}
	if len(rootFolders) != 2 || rootFolders[0].Name != "documents" || rootFolders[1].Name != "music" {
		t.Fatalf("root folders = %#v, want documents and music", rootFolders)
	}

	childFolders, err := repo.ListFolders(1, &documents.ID)
	if err != nil {
		t.Fatalf("list child folders: %v", err)
	}
	if len(childFolders) != 1 || childFolders[0].ID != photos.ID {
		t.Fatalf("child folders = %#v, want photos", childFolders)
	}

	if _, err := repo.FindFolderByID(2, documents.ID); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("other user find error = %v, want %v", err, ErrFolderNotFound)
	}
	if _, err := repo.CreateFolder(1, nil, "documents"); err == nil {
		t.Fatal("expected duplicate root folder name to fail")
	}
}

func TestRepositoryListFoldersIncludesRecursiveActiveFileSize(t *testing.T) {
	repo := newTestRepository(t)

	projects, err := repo.CreateFolder(1, nil, "projects")
	if err != nil {
		t.Fatalf("create projects folder: %v", err)
	}
	archive, err := repo.CreateFolder(1, &projects.ID, "archive")
	if err != nil {
		t.Fatalf("create archive folder: %v", err)
	}

	directFile, err := repo.Create(1, "brief.txt", "uploads/brief.txt", 12, "text/plain")
	if err != nil {
		t.Fatalf("create direct file: %v", err)
	}
	if _, err := repo.MoveActive(1, directFile.ID, &projects.ID); err != nil {
		t.Fatalf("move direct file: %v", err)
	}

	nestedFile, err := repo.Create(1, "history.txt", "uploads/history.txt", 20, "text/plain")
	if err != nil {
		t.Fatalf("create nested file: %v", err)
	}
	if _, err := repo.MoveActive(1, nestedFile.ID, &archive.ID); err != nil {
		t.Fatalf("move nested file: %v", err)
	}

	deletedFile, err := repo.Create(1, "removed.txt", "uploads/removed.txt", 30, "text/plain")
	if err != nil {
		t.Fatalf("create deleted file: %v", err)
	}
	if _, err := repo.MoveActive(1, deletedFile.ID, &archive.ID); err != nil {
		t.Fatalf("move deleted file: %v", err)
	}
	if err := repo.SoftDelete(1, deletedFile.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	rootFile, err := repo.Create(1, "root.txt", "uploads/root.txt", 99, "text/plain")
	if err != nil {
		t.Fatalf("create root file: %v", err)
	}
	if rootFile.ParentID != nil {
		t.Fatal("root file should not belong to projects")
	}

	rootFolders, err := repo.ListFolders(1, nil)
	if err != nil {
		t.Fatalf("list root folders: %v", err)
	}
	if len(rootFolders) != 1 {
		t.Fatalf("root folders = %#v, want one folder", rootFolders)
	}
	if rootFolders[0].Size != 32 {
		t.Fatalf("projects size = %d, want 32", rootFolders[0].Size)
	}

	childFolders, err := repo.ListFolders(1, &projects.ID)
	if err != nil {
		t.Fatalf("list child folders: %v", err)
	}
	if len(childFolders) != 1 || childFolders[0].Size != 20 {
		t.Fatalf("archive folders = %#v, want nested active size 20", childFolders)
	}
}
