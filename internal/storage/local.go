package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type LocalStorage struct {
	baseDir string
}

func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{
		baseDir: baseDir,
	}
}

func (s *LocalStorage) Save(reader io.Reader, originalName string) (string, int64, error) {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return "", 0, err
	}

	storageName := fmt.Sprintf("%s%s", uuid.NewString(), filepath.Ext(originalName))
	storagePath := filepath.Join(s.baseDir, storageName)

	file, err := os.Create(storagePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	size, err := io.Copy(file, reader)
	if err != nil {
		return "", 0, err
	}

	return storagePath, size, nil
}

func (s *LocalStorage) Open(storagePath string) (io.ReadSeekCloser, error) {
	return os.Open(storagePath)
}

func (s *LocalStorage) Delete(storagePath string) error {
	return os.Remove(storagePath)
}
