package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	for _, name := range []string{
		"HTTP_ADDR",
		"DB_PATH",
		"UPLOAD_DIR",
		"JWT_SECRET",
		"USER_STORAGE_QUOTA_BYTES",
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
	if cfg.UploadDir != "uploads" {
		t.Fatalf("upload directory = %q, want uploads", cfg.UploadDir)
	}
	if cfg.JWTSecret != "dev-secret-change-me" {
		t.Fatalf("JWT secret = %q, want development default", cfg.JWTSecret)
	}
	if cfg.UserStorageQuotaBytes != DefaultUserStorageQuotaBytes {
		t.Fatalf("quota = %d, want %d", cfg.UserStorageQuotaBytes, DefaultUserStorageQuotaBytes)
	}
}

func TestLoadUsesEnvironmentValues(t *testing.T) {
	t.Setenv("HTTP_ADDR", "  :9090  ")
	t.Setenv("DB_PATH", " /data/cloudbox.db ")
	t.Setenv("UPLOAD_DIR", " /data/uploads ")
	t.Setenv("JWT_SECRET", " production-secret ")
	t.Setenv("USER_STORAGE_QUOTA_BYTES", "2048")

	cfg := Load()

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTP address = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.DBPath != "/data/cloudbox.db" {
		t.Fatalf("DB path = %q, want /data/cloudbox.db", cfg.DBPath)
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
