package file

import (
	"database/sql"
	"errors"
)

var (
	ErrFileNotFound       = errors.New("file not found")
	ErrFileObjectNotFound = errors.New("file object not found")
	ErrFolderNotFound     = errors.New("folder not found")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Create(userID int64, originalName string, storagePath string, size int64, contentType string) (*UserFile, error) {
	result, err := r.db.Exec(
		`INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status) VALUES (?, ?, ?, ?, ?, ?)`,
		userID,
		originalName,
		storagePath,
		size,
		contentType,
		StatusActive,
	)

	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return r.FindActiveByID(userID, id)
}

func (r *Repository) FindActiveByID(userID int64, fileID int64) (*UserFile, error) {
	return r.findByIDAndStatus(userID, fileID, StatusActive)
}

func (r *Repository) FindDeletedByID(userID int64, fileID int64) (*UserFile, error) {
	return r.findByIDAndStatus(userID, fileID, StatusDeleted)
}

func (r *Repository) ListActive(userID int64) ([]UserFile, error) {
	return r.ListActiveInFolder(userID, nil)
}

func (r *Repository) ListActiveInFolder(
	userID int64,
	parentID *int64,
) ([]UserFile, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if parentID == nil {
		rows, err = r.db.Query(
			`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = ? AND status = ? AND parent_id IS NULL ORDER BY created_at DESC`,
			userID,
			StatusActive,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = ? AND status = ? AND parent_id = ? ORDER BY created_at DESC`,
			userID,
			StatusActive,
			*parentID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]UserFile, 0)
	for rows.Next() {
		file, err := scanUserFile(rows)
		if err != nil {
			return nil, err
		}

		files = append(files, *file)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}

func (r *Repository) ListDeleted(userID int64) ([]UserFile, error) {
	return r.listByStatus(userID, StatusDeleted)
}

func (r *Repository) SoftDelete(userID int64, fileID int64) error {
	result, err := r.db.Exec(
		`UPDATE user_files SET status = ?, deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ? AND status = ?`,
		StatusDeleted,
		fileID,
		userID,
		StatusActive,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrFileNotFound
	}

	return nil
}

func (r *Repository) Restore(userID int64, fileID int64) error {
	result, err := r.db.Exec(
		`UPDATE user_files SET status = ?, deleted_at = NULL WHERE id = ? AND user_id = ? AND status = ?`,
		StatusActive,
		fileID,
		userID,
		StatusDeleted,
	)

	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrFileNotFound
	}

	return nil
}

func (r *Repository) findByIDAndStatus(userID int64, fileID int64, status string) (*UserFile, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE id = ? AND user_id = ? AND status = ?`,
		fileID,
		userID,
		status,
	)

	file, err := scanUserFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	return file, nil
}

func (r *Repository) listByStatus(userID int64, status string) ([]UserFile, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = ? AND status = ? ORDER BY created_at DESC`,
		userID,
		status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]UserFile, 0)

	for rows.Next() {
		file, err := scanUserFile(rows)
		if err != nil {
			return nil, err
		}

		files = append(files, *file)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return files, nil
}

type userFileScanner interface {
	Scan(...any) error
}

func scanUserFile(scanner userFileScanner) (*UserFile, error) {
	var (
		file     UserFile
		parentID sql.NullInt64
	)

	err := scanner.Scan(
		&file.ID,
		&file.UserID,
		&parentID,
		&file.OriginalName,
		&file.StoragePath,
		&file.Size,
		&file.ContentType,
		&file.Status,
		&file.CreatedAt,
		&file.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	if parentID.Valid {
		file.ParentID = &parentID.Int64
	}

	return &file, nil
}

func (r *Repository) CreateFileObject(
	fileHash string,
	storagePath string,
	size int64,
	contentType string,
) (*FileObject, error) {
	_, err := r.db.Exec(
		`INSERT INTO file_objects (file_hash, storage_path, size, content_type, reference_count) VALUES (?, ?, ?, ?, 0)`,
		fileHash,
		storagePath,
		size,
		contentType,
	)
	if err != nil {
		return nil, err
	}

	return r.FindFileObjectByHash(fileHash)
}

func (r *Repository) FindFileObjectByHash(fileHash string) (*FileObject, error) {
	var object FileObject

	err := r.db.QueryRow(
		`SELECT id, file_hash, storage_path, size, content_type, reference_count, created_at FROM file_objects WHERE file_hash = ?`,
		fileHash,
	).Scan(
		&object.ID,
		&object.FileHash,
		&object.StoragePath,
		&object.Size,
		&object.ContentType,
		&object.ReferenceCount,
		&object.CreatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileObjectNotFound
	}

	if err != nil {
		return nil, err
	}

	return &object, nil
}

func (r *Repository) CreateWithObject(
	userID int64,
	originalName string,
	object *FileObject,
) (*UserFile, error) {
	return r.CreateWithObjectInFolder(
		userID,
		nil,
		originalName,
		object,
	)
}

func (r *Repository) CreateWithObjectInFolder(
	userID int64,
	parentID *int64,
	originalName string,
	object *FileObject,
) (*UserFile, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	result, err := tx.Exec(
		`INSERT INTO user_files (user_id, parent_id, object_id, original_name, storage_path, size, content_type, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID,
		parentID,
		object.ID,
		originalName,
		object.StoragePath,
		object.Size,
		object.ContentType,
		StatusActive,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	updateResult, err := tx.Exec(
		`UPDATE file_objects SET reference_count = reference_count + 1 WHERE id = ?`,
		object.ID,
	)

	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	affected, err := updateResult.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if affected == 0 {
		_ = tx.Rollback()
		return nil, ErrFileObjectNotFound
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	fileID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return r.FindActiveByID(userID, fileID)
}

func (r *Repository) CreateFolder(
	userID int64,
	parentID *int64,
	name string,
) (*Folder, error) {
	result, err := r.db.Exec(
		`INSERT INTO folders (user_id, parent_id, name) VALUES (?, ?, ?)`,
		userID,
		parentID,
		name,
	)
	if err != nil {
		return nil, err
	}

	folderId, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return r.FindFolderByID(userID, folderId)
}

func (r *Repository) FindFolderByID(
	userID int64,
	folderID int64,
) (*Folder, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, parent_id, name, created_at, updated_at FROM folders WHERE id = ? AND user_id = ?`,
		folderID,
		userID,
	)

	return scanFolder(row)
}

func (r *Repository) ListFolders(
	userID int64,
	parentID *int64,
) ([]Folder, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if parentID == nil {
		rows, err = r.db.Query(
			`SELECT id, user_id, parent_id, name, created_at, updated_at FROM folders WHERE user_id = ? AND parent_id IS NULL ORDER BY name COLLATE NOCASE, id`,
			userID,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, user_id, parent_id, name, created_at, updated_at FROM folders WHERE user_id = ? AND parent_id = ? ORDER BY name COLLATE NOCASE, id`,
			userID,
			*parentID,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	folders := make([]Folder, 0)

	for rows.Next() {
		folder, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}

		folders = append(folders, *folder)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return folders, nil
}

type folderScanner interface {
	Scan(...any) error
}

func scanFolder(scanner folderScanner) (*Folder, error) {
	var (
		folder   Folder
		parentID sql.NullInt64
	)

	err := scanner.Scan(
		&folder.ID,
		&folder.UserID,
		&parentID,
		&folder.Name,
		&folder.CreatedAt,
		&folder.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, err
	}

	if parentID.Valid {
		folder.ParentID = &parentID.Int64
	}

	return &folder, nil
}

func (r *Repository) MoveActive(
	userID int64,
	fileID int64,
	parentID *int64,
) (*UserFile, error) {
	result, err := r.db.Exec(
		`UPDATE user_files SET parent_id = ? WHERE id = ? AND user_id = ? AND status = ?`,
		parentID,
		fileID,
		userID,
		StatusActive,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrFileNotFound
	}

	return r.FindActiveByID(userID, fileID)
}

func (r *Repository) RenameActive(
	userID int64,
	fileID int64,
	originalName string,
) (*UserFile, error) {
	result, err := r.db.Exec(
		`UPDATE user_files SET original_name = ? WHERE id = ? AND user_id = ? AND status = ?`,
		originalName,
		fileID,
		userID,
		StatusActive,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrFileNotFound
	}

	return r.FindActiveByID(userID, fileID)
}

func (r *Repository) RenameFolder(
	userID int64,
	folderID int64,
	name string,
) (*Folder, error) {
	result, err := r.db.Exec(
		`UPDATE folders SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		name,
		folderID,
		userID,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrFolderNotFound
	}

	return r.FindFolderByID(userID, folderID)
}

func (r *Repository) MoveFolder(
	userID int64,
	folderID int64,
	parentID *int64,
) (*Folder, error) {
	result, err := r.db.Exec(
		`UPDATE folders SET parent_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		parentID,
		folderID,
		userID,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrFolderNotFound
	}

	return r.FindFolderByID(userID, folderID)
}

func (r *Repository) DeleteEmptyFolder(
	userID int64,
	folderID int64,
) (bool, error) {
	result, err := r.db.Exec(
		`DELETE FROM folders WHERE id = ? AND user_id = ? AND NOT EXISTS (SELECT 1 FROM folders AS child WHERE child.user_id = ? AND child.parent_id = ?) AND NOT EXISTS (SELECT 1 FROM user_files WHERE user_id = ? AND parent_id = ?)`,
		folderID,
		userID,
		userID,
		folderID,
		userID,
		folderID,
	)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected == 1, nil
}
