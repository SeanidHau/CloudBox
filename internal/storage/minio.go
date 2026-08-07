package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const minIOPartSize = 10 * 1024 * 1024

type MinIOStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOStorage(
	endpoint string,
	accessKey string,
	secretKey string,
	bucket string,
	useSSL bool,
) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	storage := &MinIOStorage{
		client: client,
		bucket: bucket,
	}

	if err := storage.ensureBucket(context.Background()); err != nil {
		return nil, err
	}

	return storage, nil
}

func (s *MinIOStorage) Save(
	reader io.Reader,
	originalName string,
) (string, int64, string, error) {
	objectName := uuid.NewString() + path.Ext(originalName)
	hasher := sha256.New()

	hashingReader := io.TeeReader(reader, hasher)

	info, err := s.client.PutObject(
		context.Background(),
		s.bucket,
		objectName,
		hashingReader,
		-1,
		minio.PutObjectOptions{
			PartSize: minIOPartSize,
		},
	)
	if err != nil {
		return "", 0, "", err
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))

	return objectName, info.Size, fileHash, nil
}

func (s *MinIOStorage) Open(objectName string) (io.ReadSeekCloser, error) {
	object, err := s.client.GetObject(
		context.Background(),
		s.bucket,
		objectName,
		minio.GetObjectOptions{},
	)
	if err != nil {
		return nil, err
	}

	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, err
	}

	return object, nil
}

func (s *MinIOStorage) Delete(objectName string) error {
	return s.client.RemoveObject(
		context.Background(),
		s.bucket,
		objectName,
		minio.RemoveObjectOptions{},
	)
}

func (s *MinIOStorage) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}
