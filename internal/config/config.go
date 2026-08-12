package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultUserStorageQuotaBytes int64 = 1 << 30
	DefaultRedisUsageCacheTTL          = time.Minute
	DefaultLogLevel                    = "info"
	DefaultTrashRetentionHours         = 0
	DefaultTraceExporter               = "none"
	DefaultJobWorkerCount              = 1
	DefaultJobPollInterval             = time.Second
	DefaultClamAVTimeout               = time.Minute
)

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type RedisConfig struct {
	Enabled       bool
	Addr          string
	Password      string
	DB            int
	UsageCacheTTL time.Duration
}

type ClamAVConfig struct {
	Enabled bool
	Address string
	Timeout time.Duration
}

type Config struct {
	HTTPAddr              string
	DBPath                string
	DatabaseDriver        string
	DatabaseURL           string
	UploadDir             string
	JWTSecret             string
	UserStorageQuotaBytes int64
	StorageDriver         string
	MinIO                 MinIOConfig
	Redis                 RedisConfig
	LogLevel              string
	TrashRetention        time.Duration
	TraceExporter         string
	JobWorkerCount        int
	JobPollInterval       time.Duration
	ClamAV                ClamAVConfig
	AdminUsername         string
	AdminPassword         string
}

func Load() Config {
	return Config{
		HTTPAddr:       envOrDefault("HTTP_ADDR", ":8080"),
		DBPath:         envOrDefault("DB_PATH", "cloudbox.db"),
		DatabaseDriver: envOrDefault("DATABASE_DRIVER", "sqlite"),
		DatabaseURL:    envOrDefault("DATABASE_URL", ""),
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
		Redis: RedisConfig{
			Enabled: envBoolOrDefault("REDIS_ENABLED", false),
			Addr:    envOrDefault("REDIS_ADDR", "localhost:6379"),
			Password: envOrDefault(
				"REDIS_PASSWORD",
				"",
			),
			DB: envNonNegativeIntOrDefault("REDIS_DB", 0),
			UsageCacheTTL: time.Duration(
				envInt64OrDefault("REDIS_USAGE_CACHE_TTL_SECONDS", int64(DefaultRedisUsageCacheTTL/time.Second)),
			) * time.Second,
		},
		ClamAV: ClamAVConfig{
			Enabled: envBoolOrDefault("CLAMAV_ENABLED", false),
			Address: envOrDefault("CLAMAV_ADDRESS", "127.0.0.1:3310"),
			Timeout: time.Duration(
				envInt64OrDefault(
					"CLAMAV_TIMEOUT_SECONDS",
					int64(DefaultClamAVTimeout/time.Second),
				),
			) * time.Second,
		},
		LogLevel: envLogLevel("LOG_LEVEL", DefaultLogLevel),
		TrashRetention: time.Duration(
			envNonNegativeIntOrDefault(
				"TRASH_RETENTION_HOURS",
				DefaultTrashRetentionHours,
			),
		) * time.Hour,
		TraceExporter: envTraceExporter("TRACE_EXPORTER", DefaultTraceExporter),
		JobWorkerCount: envNonNegativeIntOrDefault(
			"JOB_WORKER_COUNT",
			DefaultJobWorkerCount,
		),
		JobPollInterval: time.Duration(
			envInt64OrDefault(
				"JOB_POLL_INTERVAL_MILLISECONDS",
				int64(DefaultJobPollInterval/time.Millisecond),
			),
		) * time.Millisecond,
		AdminUsername: envOrDefault("ADMIN_USERNAME", ""),
		AdminPassword: envOrDefault("ADMIN_PASSWORD", ""),
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

func envNonNegativeIntOrDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}

	return parsed
}

func envLogLevel(name string, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))

	switch value {
	case "debug", "info", "warn", "error":
		return value
	default:
		return fallback
	}
}

func envTraceExporter(name string, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))

	switch value {
	case "none", "stdout":
		return value
	default:
		return fallback
	}
}
