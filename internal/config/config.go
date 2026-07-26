package config

type Config struct {
	HTTPAddr  string
	DBPath    string
	UploadDir string
	JWTSecret string
}

func Load() Config {
	return Config{
		HTTPAddr:  ":8080",
		DBPath:    "cloudbox.db",
		UploadDir: "uploads",
		JWTSecret: "dev-secret-change-me",
	}
}
