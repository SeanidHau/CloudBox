package config

import (
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	for _, name := range []string{
		"HTTP_ADDR",
		"DB_PATH",
		"DATABASE_DRIVER",
		"DATABASE_URL",
		"UPLOAD_DIR",
		"JWT_SECRET",
		"USER_STORAGE_QUOTA_BYTES",
		"STORAGE_DRIVER",
		"MINIO_ENDPOINT",
		"MINIO_ACCESS_KEY",
		"MINIO_SECRET_KEY",
		"MINIO_BUCKET",
		"MINIO_USE_SSL",
		"REDIS_ENABLED",
		"REDIS_ADDR",
		"REDIS_PASSWORD",
		"REDIS_DB",
		"REDIS_USAGE_CACHE_TTL_SECONDS",
		"LOG_LEVEL",
		"TRASH_RETENTION_HOURS",
		"TRACE_EXPORTER",
	} {
		t.Setenv(name, "")
	}

	cfg := Load()

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTP address = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DBPath != "cloudbox.db" {
		t.Fatalf("DB path = %q, want cloudbox.db", cfg.DBPath)
	}
	if cfg.DatabaseDriver != "sqlite" {
		t.Fatalf("database driver = %q, want sqlite", cfg.DatabaseDriver)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("database URL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.UploadDir != "uploads" {
		t.Fatalf("upload directory = %q, want uploads", cfg.UploadDir)
	}
	if cfg.JWTSecret != "dev-secret-change-me" {
		t.Fatalf("JWT secret = %q, want development default", cfg.JWTSecret)
	}
	if cfg.UserStorageQuotaBytes != DefaultUserStorageQuotaBytes {
		t.Fatalf("quota = %d, want %d", cfg.UserStorageQuotaBytes, DefaultUserStorageQuotaBytes)
	}
	if cfg.StorageDriver != "local" {
		t.Fatalf("storage driver = %q, want local", cfg.StorageDriver)
	}
	if cfg.MinIO.Endpoint != "localhost:9000" {
		t.Fatalf("MinIO endpoint = %q, want localhost:9000", cfg.MinIO.Endpoint)
	}
	if cfg.MinIO.Bucket != "cloudbox" {
		t.Fatalf("MinIO bucket = %q, want cloudbox", cfg.MinIO.Bucket)
	}
	if cfg.MinIO.UseSSL {
		t.Fatal("MinIO SSL should default to false")
	}
	if cfg.Redis.Enabled {
		t.Fatal("Redis should default to disabled")
	}
	if cfg.Redis.Addr != "localhost:6379" {
		t.Fatalf("Redis address = %q, want localhost:6379", cfg.Redis.Addr)
	}
	if cfg.Redis.DB != 0 {
		t.Fatalf("Redis DB = %d, want 0", cfg.Redis.DB)
	}
	if cfg.Redis.UsageCacheTTL != DefaultRedisUsageCacheTTL {
		t.Fatalf("Redis usage cache TTL = %s, want %s", cfg.Redis.UsageCacheTTL, DefaultRedisUsageCacheTTL)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Fatalf("log level = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.TrashRetention != 0 {
		t.Fatalf("trash retention = %s, want disabled", cfg.TrashRetention)
	}
	if cfg.TraceExporter != DefaultTraceExporter {
		t.Fatalf("trace exporter = %q, want %q", cfg.TraceExporter, DefaultTraceExporter)
	}
}

func TestLoadUsesEnvironmentValues(t *testing.T) {
	t.Setenv("HTTP_ADDR", "  :9090  ")
	t.Setenv("DB_PATH", " /data/cloudbox.db ")
	t.Setenv("DATABASE_DRIVER", " postgres ")
	t.Setenv("DATABASE_URL", " postgres://cloudbox:password@db:5432/cloudbox?sslmode=disable ")
	t.Setenv("UPLOAD_DIR", " /data/uploads ")
	t.Setenv("JWT_SECRET", " production-secret ")
	t.Setenv("USER_STORAGE_QUOTA_BYTES", "2048")
	t.Setenv("STORAGE_DRIVER", " minio ")
	t.Setenv("MINIO_ENDPOINT", " minio:9000 ")
	t.Setenv("MINIO_ACCESS_KEY", " minio-user ")
	t.Setenv("MINIO_SECRET_KEY", " minio-password ")
	t.Setenv("MINIO_BUCKET", " cloudbox-files ")
	t.Setenv("MINIO_USE_SSL", "true")
	t.Setenv("REDIS_ENABLED", "true")
	t.Setenv("REDIS_ADDR", " redis:6379 ")
	t.Setenv("REDIS_PASSWORD", " redis-password ")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("REDIS_USAGE_CACHE_TTL_SECONDS", "120")
	t.Setenv("LOG_LEVEL", " WARN ")
	t.Setenv("TRASH_RETENTION_HOURS", "72")
	t.Setenv("TRACE_EXPORTER", " STDOUT ")

	cfg := Load()

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTP address = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.DBPath != "/data/cloudbox.db" {
		t.Fatalf("DB path = %q, want /data/cloudbox.db", cfg.DBPath)
	}
	if cfg.DatabaseDriver != "postgres" {
		t.Fatalf("database driver = %q, want postgres", cfg.DatabaseDriver)
	}
	if cfg.DatabaseURL != "postgres://cloudbox:password@db:5432/cloudbox?sslmode=disable" {
		t.Fatalf("database URL = %q, want configured value", cfg.DatabaseURL)
	}
	if cfg.UploadDir != "/data/uploads" {
		t.Fatalf("upload directory = %q, want /data/uploads", cfg.UploadDir)
	}
	if cfg.JWTSecret != "production-secret" {
		t.Fatalf("JWT secret = %q, want production-secret", cfg.JWTSecret)
	}
	if cfg.UserStorageQuotaBytes != 2048 {
		t.Fatalf("quota = %d, want 2048", cfg.UserStorageQuotaBytes)
	}
	if cfg.StorageDriver != "minio" {
		t.Fatalf("storage driver = %q, want minio", cfg.StorageDriver)
	}
	if cfg.MinIO.Endpoint != "minio:9000" {
		t.Fatalf("MinIO endpoint = %q, want minio:9000", cfg.MinIO.Endpoint)
	}
	if cfg.MinIO.AccessKey != "minio-user" || cfg.MinIO.SecretKey != "minio-password" {
		t.Fatalf("MinIO credentials = %#v, want configured values", cfg.MinIO)
	}
	if cfg.MinIO.Bucket != "cloudbox-files" {
		t.Fatalf("MinIO bucket = %q, want cloudbox-files", cfg.MinIO.Bucket)
	}
	if !cfg.MinIO.UseSSL {
		t.Fatal("MinIO SSL = false, want true")
	}
	if !cfg.Redis.Enabled {
		t.Fatal("Redis enabled = false, want true")
	}
	if cfg.Redis.Addr != "redis:6379" || cfg.Redis.Password != "redis-password" {
		t.Fatalf("Redis connection = %#v, want configured values", cfg.Redis)
	}
	if cfg.Redis.DB != 2 {
		t.Fatalf("Redis DB = %d, want 2", cfg.Redis.DB)
	}
	if cfg.Redis.UsageCacheTTL != 2*time.Minute {
		t.Fatalf("Redis usage cache TTL = %s, want 2m0s", cfg.Redis.UsageCacheTTL)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("log level = %q, want warn", cfg.LogLevel)
	}
	if cfg.TrashRetention != 72*time.Hour {
		t.Fatalf("trash retention = %s, want 72h0m0s", cfg.TrashRetention)
	}
	if cfg.TraceExporter != "stdout" {
		t.Fatalf("trace exporter = %q, want stdout", cfg.TraceExporter)
	}
}

func TestLoadFallsBackForInvalidMinIOUseSSL(t *testing.T) {
	t.Setenv("MINIO_USE_SSL", "not-a-bool")

	cfg := Load()
	if cfg.MinIO.UseSSL {
		t.Fatal("invalid MinIO SSL value should fall back to false")
	}
}

func TestLoadFallsBackForInvalidQuota(t *testing.T) {
	for _, value := range []string{"not-a-number", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("USER_STORAGE_QUOTA_BYTES", value)

			cfg := Load()
			if cfg.UserStorageQuotaBytes != DefaultUserStorageQuotaBytes {
				t.Fatalf("quota for %q = %d, want %d", value, cfg.UserStorageQuotaBytes, DefaultUserStorageQuotaBytes)
			}
		})
	}
}

func TestLoadFallsBackForInvalidRedisDB(t *testing.T) {
	for _, value := range []string{"not-a-number", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("REDIS_DB", value)

			cfg := Load()
			if cfg.Redis.DB != 0 {
				t.Fatalf("Redis DB for %q = %d, want 0", value, cfg.Redis.DB)
			}
		})
	}
}

func TestLoadFallsBackForInvalidRedisUsageCacheTTL(t *testing.T) {
	for _, value := range []string{"not-a-number", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("REDIS_USAGE_CACHE_TTL_SECONDS", value)

			cfg := Load()
			if cfg.Redis.UsageCacheTTL != DefaultRedisUsageCacheTTL {
				t.Fatalf("Redis usage cache TTL for %q = %s, want %s", value, cfg.Redis.UsageCacheTTL, DefaultRedisUsageCacheTTL)
			}
		})
	}
}

func TestLoadAcceptsValidLogLevels(t *testing.T) {
	for _, value := range []string{"debug", "info", "warn", "error"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", value)

			cfg := Load()
			if cfg.LogLevel != value {
				t.Fatalf("log level for %q = %q, want %q", value, cfg.LogLevel, value)
			}
		})
	}
}

func TestLoadFallsBackForInvalidLogLevel(t *testing.T) {
	for _, value := range []string{"trace", "warning", "fatal"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", value)

			cfg := Load()
			if cfg.LogLevel != DefaultLogLevel {
				t.Fatalf("log level for %q = %q, want %q", value, cfg.LogLevel, DefaultLogLevel)
			}
		})
	}
}

func TestLoadFallsBackForInvalidTrashRetention(t *testing.T) {
	for _, value := range []string{"not-a-number", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRASH_RETENTION_HOURS", value)

			cfg := Load()
			if cfg.TrashRetention != 0 {
				t.Fatalf("trash retention for %q = %s, want disabled", value, cfg.TrashRetention)
			}
		})
	}
}

func TestLoadFallsBackForInvalidTraceExporter(t *testing.T) {
	for _, value := range []string{"otlp", "console", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRACE_EXPORTER", value)

			cfg := Load()
			if cfg.TraceExporter != DefaultTraceExporter {
				t.Fatalf("trace exporter for %q = %q, want %q", value, cfg.TraceExporter, DefaultTraceExporter)
			}
		})
	}
}
