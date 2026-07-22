package main

import (
	"log"

	"github.com/SeanidHau/CloudBox/internal/auth"
	"github.com/SeanidHau/CloudBox/internal/config"
	"github.com/SeanidHau/CloudBox/internal/database"
	cloudfile "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/SeanidHau/CloudBox/internal/storage"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	localStorage, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, cfg.JWTSecret, cfg.TokenLifetime)
	authHandler := auth.NewHandler(authService)

	fileRepo := cloudfile.NewRepository(db)
	fileService := cloudfile.NewService(fileRepo, localStorage)
	fileHandler := cloudfile.NewHandler(fileService)

	router := gin.Default()
	router.MaxMultipartMemory = 32 << 20

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	api := router.Group("/api")
	authHandler.RegisterRoutes(api.Group("/auth"))

	files := api.Group("/files")
	files.Use(middleware.Auth(authService))
	fileHandler.RegisterRoutes(files)

	if err := router.Run(cfg.Addr); err != nil {
		log.Fatalf("run api server: %v", err)
	}
}
