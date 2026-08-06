package config

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
		HTTPAddr:              ":8080",
		DBPath:                "cloudbox.db",
		UploadDir:             "uploads",
		JWTSecret:             "dev-secret-change-me",
		UserStorageQuotaBytes: DefaultUserStorageQuotaBytes,
	}
}
