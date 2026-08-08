package config

import "testing"

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
