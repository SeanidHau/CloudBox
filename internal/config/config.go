package config

import (
	"os"
	"strconv"
	"strings"
)

const DefaultUserStorageQuotaBytes int64 = 1 << 30

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type Config struct {
	HTTPAddr              string
	DBPath                string
	UploadDir             string
	JWTSecret             string
	UserStorageQuotaBytes int64
	StorageDriver         string
	MinIO                 MinIOConfig
}

func Load() Config {
	return Config{
		HTTPAddr: envOrDefault("HTTP_ADDR", ":8080"),
		DBPath:   envOrDefault("DB_PATH", "cloudbox.db"),
		UploadDir: envOrDefault(
			"UPLOAD_DIR",
			"uploads",
		),
		JWTSecret: envOrDefault(
			"JWT_SECRET",
			"dev-secret-change-me",
		),
		UserStorageQuotaBytes: envInt64OrDefault(
			"USER_STORAGE_QUOTA_BYTES",
			DefaultUserStorageQuotaBytes,
		),
		StorageDriver: envOrDefault("STORAGE_DRIVER", "local"),
		MinIO: MinIOConfig{
			Endpoint:  envOrDefault("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: envOrDefault("MINIO_ACCESS_KEY", ""),
			SecretKey: envOrDefault("MINIO_SECRET_KEY", ""),
			Bucket:    envOrDefault("MINIO_BUCKET", "cloudbox"),
			UseSSL:    envBoolOrDefault("MINIO_USE_SSL", false),
		},
	}
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func envInt64OrDefault(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func envBoolOrDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
}
