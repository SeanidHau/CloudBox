package main

import (
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/SeanidHau/CloudBox/internal/auth"
	"github.com/SeanidHau/CloudBox/internal/config"
	"github.com/SeanidHau/CloudBox/internal/database"
	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/SeanidHau/CloudBox/internal/storage"
	uploadmodule "github.com/SeanidHau/CloudBox/internal/upload"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := database.Migrate(
		db,
		"migrations/001_init.sql",
		"migrations/002_file_objects.sql",
		"migrations/003_upload_tasks.sql",
		"migrations/004_fix_upload_chunks.sql",
	); err != nil {
		log.Fatal(err)
	}

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, cfg.JWTSecret)
	authHandler := auth.NewHandler(authService)

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	api := r.Group("/api")

	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	localStorage := storage.NewLocalStorage(cfg.UploadDir)
	filerepo := filemodule.NewRepository(db)
	fileService := filemodule.NewService(filerepo, localStorage)
	fileHandler := filemodule.NewHandler(fileService)

	uploadRepo := uploadmodule.NewRepository(db)
	uploadService := uploadmodule.NewService(
		uploadRepo,
		filepath.Join(cfg.UploadDir, "tmp"),
		fileService,
	)
	uploadHandler := uploadmodule.NewHandler(uploadService)

	cleanupExpiredUploads := func() {
		before := time.Now().Add(-24 * time.Hour)

		cleaned, err := uploadService.CleanupExpired(before)
		if err != nil {
			log.Printf("cleanup expired uploads: %v", err)
			return
		}
		if cleaned > 0 {
			log.Printf("cleanup %d expired uploads tasks", cleaned)
		}
	}

	cleanupExpiredUploads()

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cleanupExpiredUploads()
		}
	}()

	protected := api.Group("")
	protected.Use(middleware.Auth(cfg.JWTSecret))

	protected.GET("/me", func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user_id": userID,
		})
	})

	protected.POST("/files", fileHandler.Upload)
	protected.POST("/files/instant", fileHandler.InstantUpload)
	protected.GET("/files", fileHandler.ListActive)
	protected.GET("/files/trash", fileHandler.ListDeleted)
	protected.GET("/files/:id/download", fileHandler.Download)
	protected.DELETE("/files/:id", fileHandler.SoftDelete)
	protected.POST("/files/:id/restore", fileHandler.Restore)
	protected.POST("/uploads/init", uploadHandler.Init)
	protected.PUT("/uploads/:id/chunks/:number", uploadHandler.UploadChunk)
	protected.POST("/uploads/:id/complete", uploadHandler.Complete)
	protected.GET("/uploads/:id", uploadHandler.GetStatus)
	protected.DELETE("/uploads/:id", uploadHandler.Cancel)

	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
