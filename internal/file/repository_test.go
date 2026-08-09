package file

import (
	"errors"
	"path/filepath"
	"testing"

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
	if err := repo.SoftDelete(1, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	objectToDelete, err := repo.PermanentlyDeleteDeleted(1, file.ID)
	if err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}
	if objectToDelete == nil || objectToDelete.ID != object.ID {
		t.Fatalf("object to delete = %#v, want object %d", objectToDelete, object.ID)
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
