package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}
	return &Local{root: root}, nil
}

func (s *Local) Save(ctx context.Context, userID int64, originalName string, reader io.Reader) (string, error) {
	userDir := filepath.Join(s.root, fmt.Sprintf("user-%d", userID))
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", fmt.Errorf("create user upload dir: %w", err)
	}

	ext := filepath.Ext(originalName)
	storageName := uuid.NewString() + ext
	absolutePath := filepath.Join(userDir, storageName)

	file, err := os.Create(absolutePath)
	if err != nil {
		return "", fmt.Errorf("create stored file: %w", err)
	}
	defer file.Close()

	if _, err := copyWithContext(ctx, file, reader); err != nil {
		os.Remove(absolutePath)
		return "", fmt.Errorf("copy uploaded file: %w", err)
	}

	return filepath.ToSlash(filepath.Join(fmt.Sprintf("user-%d", userID), storageName)), nil
}

func (s *Local) Open(storagePath string) (io.ReadCloser, string, error) {
	cleanPath := filepath.Clean(storagePath)
	if filepath.IsAbs(cleanPath) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("invalid storage path")
	}

	absolutePath := filepath.Join(s.root, cleanPath)
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, "", fmt.Errorf("open stored file: %w", err)
	}

	return file, absolutePath, nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var written int64

	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, readErr := src.Read(buffer)
		if nr > 0 {
			nw, writeErr := dst.Write(buffer[:nr])
			written += int64(nw)
			if writeErr != nil {
				return written, writeErr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}
