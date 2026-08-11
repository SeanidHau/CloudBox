//go:build integration

package file

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/database"
	"github.com/google/uuid"
)

func TestRepositoryPermanentlyDeleteDeletedPostgres(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_INTEGRATION_URL")
	if databaseURL == "" {
		t.Skip("set POSTGRES_INTEGRATION_URL to run the PostgreSQL integration test")
	}

	db, err := database.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	migrationPaths := []string{
		filepath.Join("..", "..", "migrations", "postgres", "001_init.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "004_file_preview.sql"),
	}
	if err := database.Migrate(db, migrationPaths...); err != nil {
		t.Fatalf("apply Postgres migrations: %v", err)
	}

	repo := NewRepository(db)
	fileHash := "postgres-permanent-delete-" + uuid.NewString()

	var userID int64
	err = db.QueryRow(
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		"postgres-delete-"+uuid.NewString(),
		"hash",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		// 测试失败时也尽量移除遗留的关联数据。
		_, _ = db.Exec(`DELETE FROM file_shares WHERE user_file_id IN (SELECT id FROM user_files WHERE user_id = $1)`, userID)
		_, _ = db.Exec(`DELETE FROM user_files WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM file_objects WHERE file_hash = $1`, fileHash)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})

	object, err := repo.CreateFileObject(fileHash, "uploads/postgres-delete.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	file, err := repo.CreateWithObject(userID, "postgres-delete.txt", object)
	if err != nil {
		t.Fatalf("create user file: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO file_shares (token, user_file_id) VALUES ($1, $2)`, uuid.NewString(), file.ID); err != nil {
		t.Fatalf("create file share: %v", err)
	}
	if err := repo.SoftDelete(userID, file.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	deletedObject, err := repo.PermanentlyDeleteDeleted(userID, file.ID)
	if err != nil {
		t.Fatalf("permanently delete file: %v", err)
	}
	if deletedObject == nil || deletedObject.Object.ID != object.ID {
		t.Fatalf("deleted object = %#v, want object %d", deletedObject, object.ID)
	}
	if _, err := repo.FindFileObjectByHash(fileHash); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("find deleted object error = %v, want %v", err, ErrFileObjectNotFound)
	}

	var shareCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_shares WHERE user_file_id = $1`, file.ID).Scan(&shareCount); err != nil {
		t.Fatalf("count file shares: %v", err)
	}
	if shareCount != 0 {
		t.Fatalf("share count = %d, want 0", shareCount)
	}
}
