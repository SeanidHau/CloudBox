package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestLocalStorageSaveReturnsContentHash(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())

	const content = "hello cloudbox"
	storagePath, size, fileHash, err := storage.Save(strings.NewReader(content), "hello.txt")
	if err != nil {
		t.Fatalf("save file: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}

	expectedHash := sha256.Sum256([]byte(content))
	if fileHash != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("file hash = %q, want %q", fileHash, hex.EncodeToString(expectedHash[:]))
	}

	savedContent, err := os.ReadFile(storagePath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(savedContent) != content {
		t.Fatalf("saved content = %q, want %q", string(savedContent), content)
	}
}
