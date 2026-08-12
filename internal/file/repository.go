package file

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrFileNotFound        = errors.New("file not found")
	ErrFileObjectNotFound  = errors.New("file object not found")
	ErrFolderNotFound      = errors.New("folder not found")
	ErrFilePreviewNotFound = errors.New("file preview not found")
	ErrFileScanNotFound    = errors.New("file scan not found")
)

type Repository struct {
	db *sql.DB
}

type UnreferencedFileObject struct {
	Object  FileObject
	Preview *FilePreview
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Create(userID int64, originalName string, storagePath string, size int64, contentType string) (*UserFile, error) {
	var id int64

	err := r.db.QueryRow(
		`INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		userID,
		originalName,
		storagePath,
		size,
		contentType,
		StatusActive,
	).Scan(&id)

	if err != nil {
		return nil, err
	}

	return r.FindActiveByID(userID, id)
}

func (r *Repository) FindActiveByID(userID int64, fileID int64) (*UserFile, error) {
	return r.findByIDAndStatus(userID, fileID, StatusActive)
}

func (r *Repository) FindObjectForActiveFile(fileID int64) (*FileObject, error) {
	var object FileObject

	err := r.db.QueryRow(
		`SELECT fo.id, fo.file_hash, fo.storage_path, fo.size, fo.content_type, fo.reference_count, fo.created_at FROM user_files AS uf JOIN file_objects AS fo ON fo.id = uf.object_id WHERE uf.id = $1 AND uf.status = $2`,
		fileID,
		StatusActive,
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
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	return &object, nil
}

func (r *Repository) FindFilePreviewByObjectID(fileObjectID int64) (*FilePreview, error) {
	row := r.db.QueryRow(
		`SELECT file_object_id, storage_path, size, content_type, width, height, created_at FROM file_previews WHERE file_object_id = $1`,
		fileObjectID,
	)

	return scanFilePreview(row)
}

func (r *Repository) FindFilePreviewForActiveFile(
	userID int64,
	fileID int64,
) (*FilePreview, error) {
	row := r.db.QueryRow(
		`SELECT fp.file_object_id, fp.storage_path, fp.size, fp.content_type, fp.width, fp.height, fp.created_at FROM user_files AS uf JOIN file_previews AS fp ON fp.file_object_id = uf.object_id WHERE uf.id = $1 AND uf.user_id = $2 AND uf.status = $3`,
		fileID,
		userID,
		StatusActive,
	)

	return scanFilePreview(row)
}

func (r *Repository) CreateFilePreview(preview *FilePreview) (bool, error) {
	result, err := r.db.Exec(
		`INSERT INTO file_previews (file_object_id, storage_path, size, content_type, width, height) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (file_object_id) DO NOTHING`,
		preview.FileObjectID,
		preview.StoragePath,
		preview.Size,
		preview.ContentType,
		preview.Width,
		preview.Height,
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

func (r *Repository) FindFileScanByObjectID(fileObjectID int64) (*FileScan, error) {
	row := r.db.QueryRow(
		`SELECT file_object_id, status, signature, scanned_at, created_at, updated_at FROM file_scans WHERE file_object_id = $1`,
		fileObjectID,
	)

	return scanFileScan(row)
}

func (r *Repository) CreatePendingFileScan(
	fileObjectID int64,
) (*FileScan, bool, error) {
	result, err := r.db.Exec(
		`INSERT INTO file_scans (file_object_id, status) VALUES ($1, $2) ON CONFLICT (file_object_id) DO NOTHING`,
		fileObjectID,
		ScanStatusPending,
	)
	if err != nil {
		return nil, false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, false, err
	}

	scan, err := r.FindFileScanByObjectID(fileObjectID)
	if err != nil {
		return nil, false, err
	}

	return scan, affected == 1, nil
}

func (r *Repository) ClaimFileScan(fileObjectID int64) (*FileScan, bool, error) {
	row := r.db.QueryRow(
		`UPDATE file_scans SET status = $1, signature = NULL, scanned_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE file_object_id = $2 AND status IN ($3, $4) RETURNING file_object_id, status, signature, scanned_at, created_at, updated_at`,
		ScanStatusScanning,
		fileObjectID,
		ScanStatusPending,
		ScanStatusFailed,
	)

	scan, err := scanFileScan(row)
	if err == nil {
		return scan, true, nil
	}
	if !errors.Is(err, ErrFileScanNotFound) {
		return nil, false, err
	}

	existing, findErr := r.FindFileScanByObjectID(fileObjectID)
	if findErr != nil {
		return nil, false, findErr
	}

	return existing, false, nil
}

func (r *Repository) CompleteFileScan(
	fileObjectID int64,
	infected bool,
	signature string,
) (*FileScan, error) {
	status := ScanStatusClean
	var storedSignature *string

	if infected {
		status = ScanStatusInfected
		storedSignature = &signature
	}

	return r.updateRunningFileScan(
		fileObjectID,
		status,
		storedSignature,
		true,
	)
}

func (r *Repository) FailFileScan(fileObjectID int64) (*FileScan, error) {
	return r.updateRunningFileScan(
		fileObjectID,
		ScanStatusFailed,
		nil,
		false,
	)
}

func (r *Repository) updateRunningFileScan(
	fileObjectID int64,
	status string,
	signature *string,
	completed bool,
) (*FileScan, error) {
	row := r.db.QueryRow(
		`UPDATE file_scans SET status = $1, signature = $2, scanned_at = CASE WHEN $3 THEN CURRENT_TIMESTAMP ELSE NULL END, updated_at = CURRENT_TIMESTAMP WHERE file_object_id = $4 AND status = $5 RETURNING file_object_id, status, signature, scanned_at, created_at, updated_at`,
		status,
		signature,
		completed,
		fileObjectID,
		ScanStatusScanning,
	)

	return scanFileScan(row)
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
			`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = $1 AND status = $2 AND parent_id IS NULL ORDER BY created_at DESC`,
			userID,
			StatusActive,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = $1 AND status = $2 AND parent_id = $3 ORDER BY created_at DESC`,
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
		`UPDATE user_files SET status = $1, deleted_at = CURRENT_TIMESTAMP WHERE id = $2 AND user_id = $3 AND status = $4`,
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

func (r *Repository) ListDeletedBefore(before time.Time) ([]UserFile, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE status = $1 AND deleted_at IS NOT NULL AND deleted_at < $2 ORDER BY deleted_at ASC`,
		StatusDeleted,
		before,
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

func (r *Repository) Restore(userID int64, fileID int64) error {
	result, err := r.db.Exec(
		`UPDATE user_files SET status = $1, deleted_at = NULL WHERE id = $2 AND user_id = $3 AND status = $4`,
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

func (r *Repository) PermanentlyDeleteDeleted(
	userID int64,
	fileID int64,
) (*UnreferencedFileObject, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var object FileObject
	err = tx.QueryRow(
		`SELECT fo.id, fo.file_hash, fo.storage_path, fo.size, fo.content_type, fo.reference_count, fo.created_at FROM user_files as uf JOIN file_objects AS fo ON fo.id = uf.object_id WHERE uf.id = $1 AND uf.user_id = $2 AND uf.status = $3`,
		fileID,
		userID,
		StatusDeleted,
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
		return nil, ErrFileNotFound
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`DELETE FROM file_shares WHERE user_file_id = $1`, fileID); err != nil {
		return nil, err
	}

	result, err := tx.Exec(
		`DELETE FROM user_files WHERE id = $1 AND user_id = $2 AND status = $3`,
		fileID,
		userID,
		StatusDeleted,
	)
	if err != nil {
		return nil, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, ErrFileNotFound
	}

	var remainingReferences int
	err = tx.QueryRow(
		`UPDATE file_objects SET reference_count = reference_count - 1 WHERE id = $1 AND reference_count > 0 RETURNING reference_count`,
		object.ID,
	).Scan(&remainingReferences)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileObjectNotFound
	}
	if err != nil {
		return nil, err
	}

	if remainingReferences > 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}

		return nil, nil
	}

	preview, err := scanFilePreview(tx.QueryRow(
		`SELECT file_object_id, storage_path, size, content_type, width, height, created_at FROM file_previews WHERE file_object_id = $1`,
		object.ID,
	))
	if errors.Is(err, ErrFilePreviewNotFound) {
		preview = nil
	} else if err != nil {
		return nil, err
	}

	result, err = tx.Exec(
		`DELETE FROM file_objects WHERE id = $1 AND reference_count = 0`,
		object.ID,
	)
	if err != nil {
		return nil, err
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, ErrFileObjectNotFound
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &UnreferencedFileObject{
		Object:  object,
		Preview: preview,
	}, nil
}

func (r *Repository) findByIDAndStatus(userID int64, fileID int64, status string) (*UserFile, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE id = $1 AND user_id = $2 AND status = $3`,
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
		`SELECT id, user_id, parent_id, original_name, storage_path, size, content_type, status, created_at, deleted_at FROM user_files WHERE user_id = $1 AND status = $2 ORDER BY created_at DESC`,
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

type filePreviewScanner interface {
	Scan(...any) error
}

func scanFilePreview(scanner filePreviewScanner) (*FilePreview, error) {
	var preview FilePreview

	err := scanner.Scan(
		&preview.FileObjectID,
		&preview.StoragePath,
		&preview.Size,
		&preview.ContentType,
		&preview.Width,
		&preview.Height,
		&preview.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFilePreviewNotFound
	}
	if err != nil {
		return nil, err
	}

	return &preview, nil
}

type fileScanScanner interface {
	Scan(...any) error
}

func scanFileScan(scanner fileScanScanner) (*FileScan, error) {
	var scan FileScan

	err := scanner.Scan(
		&scan.FileObjectID,
		&scan.Status,
		&scan.Signature,
		&scan.ScannedAt,
		&scan.CreatedAt,
		&scan.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFileScanNotFound
	}
	if err != nil {
		return nil, err
	}

	return &scan, nil
}

func (r *Repository) CreateFileObject(
	fileHash string,
	storagePath string,
	size int64,
	contentType string,
) (*FileObject, error) {
	_, err := r.db.Exec(
		`INSERT INTO file_objects (file_hash, storage_path, size, content_type, reference_count) VALUES ($1, $2, $3, $4, 0)`,
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
		`SELECT id, file_hash, storage_path, size, content_type, reference_count, created_at FROM file_objects WHERE file_hash = $1`,
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

	var fileID int64

	err = tx.QueryRow(
		`INSERT INTO user_files (user_id, parent_id, object_id, original_name, storage_path, size, content_type, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		userID,
		parentID,
		object.ID,
		originalName,
		object.StoragePath,
		object.Size,
		object.ContentType,
		StatusActive,
	).Scan(&fileID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	updateResult, err := tx.Exec(
		`UPDATE file_objects SET reference_count = reference_count + 1 WHERE id = $1`,
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

	return r.FindActiveByID(userID, fileID)
}

func (r *Repository) CreateFolder(
	userID int64,
	parentID *int64,
	name string,
) (*Folder, error) {
	var folderID int64

	err := r.db.QueryRow(
		`INSERT INTO folders (user_id, parent_id, name) VALUES ($1, $2, $3) RETURNING id`,
		userID,
		parentID,
		name,
	).Scan(&folderID)
	if err != nil {
		return nil, err
	}

	return r.FindFolderByID(userID, folderID)
}

func (r *Repository) FindFolderByID(
	userID int64,
	folderID int64,
) (*Folder, error) {
	row := r.db.QueryRow(
		`SELECT id, user_id, parent_id, name, 0 AS size, created_at, updated_at FROM folders WHERE id = $1 AND user_id = $2`,
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

	// descendant_folders expands each listed folder into its full subtree. The
	// grouped sum then gives users a useful folder size without persisting a
	// value that could become stale after moves, restores, or deletions.
	if parentID == nil {
		rows, err = r.db.Query(
			`WITH RECURSIVE listed_folders AS (
				SELECT id, user_id, parent_id, name, created_at, updated_at
				FROM folders
				WHERE user_id = $1 AND parent_id IS NULL
			), descendant_folders(root_id, id) AS (
				SELECT id, id FROM listed_folders
				UNION ALL
				SELECT descendant_folders.root_id, child.id
				FROM descendant_folders
				JOIN folders AS child ON child.parent_id = descendant_folders.id
				WHERE child.user_id = $1
			)
			SELECT lf.id, lf.user_id, lf.parent_id, lf.name,
				COALESCE(SUM(CASE WHEN uf.status = 'active' THEN uf.size ELSE 0 END), 0) AS size,
				lf.created_at, lf.updated_at
			FROM listed_folders AS lf
			JOIN descendant_folders AS df ON df.root_id = lf.id
			LEFT JOIN user_files AS uf ON uf.parent_id = df.id AND uf.user_id = lf.user_id
			GROUP BY lf.id, lf.user_id, lf.parent_id, lf.name, lf.created_at, lf.updated_at
			ORDER BY lower(lf.name), lf.id`,
			userID,
		)
	} else {
		rows, err = r.db.Query(
			`WITH RECURSIVE listed_folders AS (
				SELECT id, user_id, parent_id, name, created_at, updated_at
				FROM folders
				WHERE user_id = $1 AND parent_id = $2
			), descendant_folders(root_id, id) AS (
				SELECT id, id FROM listed_folders
				UNION ALL
				SELECT descendant_folders.root_id, child.id
				FROM descendant_folders
				JOIN folders AS child ON child.parent_id = descendant_folders.id
				WHERE child.user_id = $1
			)
			SELECT lf.id, lf.user_id, lf.parent_id, lf.name,
				COALESCE(SUM(CASE WHEN uf.status = 'active' THEN uf.size ELSE 0 END), 0) AS size,
				lf.created_at, lf.updated_at
			FROM listed_folders AS lf
			JOIN descendant_folders AS df ON df.root_id = lf.id
			LEFT JOIN user_files AS uf ON uf.parent_id = df.id AND uf.user_id = lf.user_id
			GROUP BY lf.id, lf.user_id, lf.parent_id, lf.name, lf.created_at, lf.updated_at
			ORDER BY lower(lf.name), lf.id`,
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
		&folder.Size,
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
		`UPDATE user_files SET parent_id = $1 WHERE id = $2 AND user_id = $3 AND status = $4`,
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
		`UPDATE user_files SET original_name = $1 WHERE id = $2 AND user_id = $3 AND status = $4`,
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
		`UPDATE folders SET name = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND user_id = $3`,
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
		`UPDATE folders SET parent_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND user_id = $3`,
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
		`DELETE FROM folders WHERE id = $1 AND user_id = $2 AND NOT EXISTS (SELECT 1 FROM folders AS child WHERE child.user_id = $3 AND child.parent_id = $4) AND NOT EXISTS (SELECT 1 FROM user_files WHERE user_id = $5 AND parent_id = $6)`,
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

func (r *Repository) TotalFileSizeByUser(userID int64) (int64, error) {
	var total int64

	err := r.db.QueryRow(
		`SELECT COALESCE(SUM(size), 0) FROM user_files WHERE user_id = $1`,
		userID,
	).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}
