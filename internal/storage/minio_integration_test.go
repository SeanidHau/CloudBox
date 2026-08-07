//go:build integration

package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
)

func TestMinIOStorageSaveOpenDelete(t *testing.T) {
	storage := newIntegrationMinIOStorage(t)
	content := []byte("CloudBox MinIO integration test")

	// Save must store the content and return its SHA-256 hash.
	objectName, size, fileHash, err := storage.Save(bytes.NewReader(content), "example.txt")
	if err != nil {
		t.Fatalf("save object: %v", err)
	}
	t.Cleanup(func() {
		if err := storage.Delete(objectName); err != nil {
			t.Errorf("delete test object: %v", err)
		}
	})

	expectedHash := sha256.Sum256(content)
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}
	if fileHash != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("hash = %q, want %q", fileHash, hex.EncodeToString(expectedHash[:]))
	}

	// Open returns a readable object whose content matches the upload.
	object, err := storage.Open(objectName)
	if err != nil {
		t.Fatalf("open object: %v", err)
	}

	got, err := io.ReadAll(object)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
	if err := object.Close(); err != nil {
		t.Fatalf("close object: %v", err)
	}

	// Delete removes the object, so a later Open must fail.
	if err := storage.Delete(objectName); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if _, err := storage.Open(objectName); err == nil {
		t.Fatal("open deleted object: got nil error")
	}
}

func newIntegrationMinIOStorage(t *testing.T) *MinIOStorage {
	t.Helper()

	endpoint := requireIntegrationEnv(t, "MINIO_INTEGRATION_ENDPOINT")
	accessKey := requireIntegrationEnv(t, "MINIO_INTEGRATION_ACCESS_KEY")
	secretKey := requireIntegrationEnv(t, "MINIO_INTEGRATION_SECRET_KEY")
	bucket := requireIntegrationEnv(t, "MINIO_INTEGRATION_BUCKET")

	storage, err := NewMinIOStorage(endpoint, accessKey, secretKey, bucket, false)
	if err != nil {
		t.Fatalf("create MinIO storage: %v", err)
	}

	return storage
}

func requireIntegrationEnv(t *testing.T, name string) string {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		t.Skipf("set %s to run the MinIO integration test", name)
	}

	return value
}
