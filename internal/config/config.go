package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr          string
	DatabasePath  string
	UploadDir     string
	JWTSecret     string
	TokenLifetime time.Duration
}

func Load() Config {
	return Config{
		Addr:          getEnv("CLOUDBOX_ADDR", ":8080"),
		DatabasePath:  getEnv("CLOUDBOX_DB_PATH", "cloudbox.db"),
		UploadDir:     getEnv("CLOUDBOX_UPLOAD_DIR", "uploads"),
		JWTSecret:     getEnv("CLOUDBOX_JWT_SECRET", "dev-secret-change-me"),
		TokenLifetime: time.Duration(getEnvInt("CLOUDBOX_TOKEN_HOURS", 24)) * time.Hour,
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
