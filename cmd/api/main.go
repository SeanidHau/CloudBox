package main

import (
	"log"
	"net/http"

	"github.com/SeanidHau/CloudBox/internal/auth"
	"github.com/SeanidHau/CloudBox/internal/config"
	"github.com/SeanidHau/CloudBox/internal/database"
	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	"github.com/SeanidHau/CloudBox/internal/storage"
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

	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
