package config

import (
	"os"
	"strconv"
	"strings"
)

const DefaultUserStorageQuotaBytes int64 = 1 << 30

type Config struct {
	HTTPAddr              string
	DBPath                string
	UploadDir             string
	JWTSecret             string
	UserStorageQuotaBytes int64
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
