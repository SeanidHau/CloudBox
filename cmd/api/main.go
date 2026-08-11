package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SeanidHau/CloudBox/internal/auth"
	cachemodule "github.com/SeanidHau/CloudBox/internal/cache"
	"github.com/SeanidHau/CloudBox/internal/config"
	"github.com/SeanidHau/CloudBox/internal/database"
	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	jobmodule "github.com/SeanidHau/CloudBox/internal/job"
	metricsmodule "github.com/SeanidHau/CloudBox/internal/metrics"
	"github.com/SeanidHau/CloudBox/internal/middleware"
	sharemodule "github.com/SeanidHau/CloudBox/internal/share"
	"github.com/SeanidHau/CloudBox/internal/storage"
	telemetrymodule "github.com/SeanidHau/CloudBox/internal/telemetry"
	uploadmodule "github.com/SeanidHau/CloudBox/internal/upload"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	var (
		db             *sql.DB
		err            error
		migrationPaths []string
	)

	// Each database backend uses migrations written for its SQL dialect.
	switch cfg.DatabaseDriver {
	case "sqlite":
		db, err = database.Open(cfg.DBPath)
		migrationPaths = []string{
			"migrations/001_init.sql",
			"migrations/002_file_objects.sql",
			"migrations/003_upload_tasks.sql",
			"migrations/004_fix_upload_chunks.sql",
			"migrations/005_folders.sql",
			"migrations/006_upload_task_parent.sql",
			"migrations/007_file_shares.sql",
			"migrations/008_background_jobs.sql",
			"migrations/009_background_job_user.sql",
			"migrations/010_file_preview.sql",
		}

	case "postgres":
		db, err = database.OpenPostgres(cfg.DatabaseURL)
		migrationPaths = []string{
			"migrations/postgres/001_init.sql",
			"migrations/postgres/002_background_jobs.sql",
			"migrations/postgres/003_background_job_user.sql",
			"migrations/postgres/004_file_preview.sql",
		}

	default:
		log.Fatalf("unsupported database driver: %s", cfg.DatabaseDriver)
	}

	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := database.Migrate(db, migrationPaths...); err != nil {
		log.Fatal(err)
	}

	authRepo := auth.NewRepository(db)
	authService := auth.NewService(authRepo, cfg.JWTSecret)
	authHandler := auth.NewHandler(authService)

	logLevel := new(slog.LevelVar)
	if err := logLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		log.Fatalf("parse log level: %v", err)
	}

	requestLogger := slog.New(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		}),
	)

	slog.SetDefault(requestLogger)

	shutdownTracing, err := telemetrymodule.SetupTracing(
		"cloudbox",
		cfg.TraceExporter,
	)
	if err != nil {
		log.Fatalf("setup tracing: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := shutdownTracing(ctx); err != nil {
			slog.Error("shutdown tracing", "error", err)
		}
	}()

	httpMetrics := metricsmodule.NewHTTPMetrics(prometheus.DefaultRegisterer)

	r := gin.New()
	r.Use(
		middleware.RequestID(),
		telemetrymodule.HTTPTracing(),
		middleware.RequestLogger(requestLogger),
		httpMetrics.Middleware(),
		gin.Recovery(),
	)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := r.Group("/api")

	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	var objectStorage filemodule.Storage

	switch cfg.StorageDriver {
	case "local":
		objectStorage = storage.NewLocalStorage(cfg.UploadDir)

	case "minio":
		minIOStorage, err := storage.NewMinIOStorage(
			cfg.MinIO.Endpoint,
			cfg.MinIO.AccessKey,
			cfg.MinIO.SecretKey,
			cfg.MinIO.Bucket,
			cfg.MinIO.UseSSL,
		)
		if err != nil {
			log.Fatalf("open MinIO storage: %v", err)
		}

		objectStorage = minIOStorage

	default:
		log.Fatalf("unsupported storage driver: %s", cfg.StorageDriver)
	}

	var fileServiceOptions []filemodule.ServiceOption

	if cfg.Redis.Enabled {
		redisClient := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := redisClient.Ping(ctx).Err(); err != nil {
			_ = redisClient.Close()
			log.Fatalf("connect redis: %v", err)
		}
		defer redisClient.Close()

		fileServiceOptions = append(
			fileServiceOptions,
			filemodule.WithStorageUsageCache(
				cachemodule.NewRedisStorageUsageCache(redisClient),
				cfg.Redis.UsageCacheTTL,
			),
		)
	}

	jobRepo := jobmodule.NewRepository(db)
	jobService := jobmodule.NewService(jobRepo)
	jobHTTPHandler := jobmodule.NewHTTPHandler(jobService)

	fileServiceOptions = append(
		fileServiceOptions,
		filemodule.WithJobEnqueuer(jobService),
	)

	filerepo := filemodule.NewRepository(db)
	fileService := filemodule.NewService(
		filerepo,
		objectStorage,
		cfg.UserStorageQuotaBytes,
		fileServiceOptions...,
	)
	jobRunner := jobmodule.NewRunner(
		jobRepo,
		map[string]jobmodule.Handler{
			jobmodule.TypeVerifyFile:        filemodule.NewVerifyFileJobHandler(fileService),
			jobmodule.TypeGenerateThumbnail: filemodule.NewThumbnailJobHandler(fileService),
		},
		jobmodule.WithWorkerCount(cfg.JobWorkerCount),
		jobmodule.WithPollInterval(cfg.JobPollInterval),
		jobmodule.WithLogger(requestLogger),
	)
	fileHandler := filemodule.NewHandler(fileService)

	shareRepo := sharemodule.NewRepository(db)
	shareService := sharemodule.NewService(shareRepo, objectStorage)
	shareHandler := sharemodule.NewHandler(shareService)

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
			log.Printf("cleaned %d expired upload tasks", cleaned)
		}
	}

	cleanupExpiredTrash := func() {
		if cfg.TrashRetention <= 0 {
			return
		}

		before := time.Now().Add(-cfg.TrashRetention)

		cleaned, err := fileService.CleanupDeletedBefore(before)
		if err != nil {
			log.Printf("cleanup expired trash: %v", err)
			return
		}
		if cleaned > 0 {
			log.Printf("cleaned %d expired trash files", cleaned)
		}
	}

	runCleanupJobs := func() {
		cleanupExpiredUploads()
		cleanupExpiredTrash()
	}

	runCleanupJobs()

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			runCleanupJobs()
		}
	}()

	api.GET("/shares/:token/download", shareHandler.Download)

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
	protected.GET("/files/:id/thumbnail", fileHandler.DownloadThumbnail)
	protected.DELETE("/files/:id/permanent", fileHandler.PermanentlyDelete)
	protected.DELETE("/files/:id", fileHandler.SoftDelete)
	protected.POST("/files/:id/restore", fileHandler.Restore)
	protected.POST("/files/:id/verify", fileHandler.EnqueueVerification)
	protected.POST("/uploads/init", uploadHandler.Init)
	protected.PUT("/uploads/:id/chunks/:number", uploadHandler.UploadChunk)
	protected.POST("/uploads/:id/complete", uploadHandler.Complete)
	protected.GET("/uploads/:id", uploadHandler.GetStatus)
	protected.DELETE("/uploads/:id", uploadHandler.Cancel)
	protected.POST("/folders", fileHandler.CreateFolder)
	protected.GET("/folders", fileHandler.ListFolders)
	protected.PATCH("/files/:id/move", fileHandler.MoveActive)
	protected.PATCH("/files/:id/rename", fileHandler.RenameActive)
	protected.PATCH("/folders/:id/rename", fileHandler.RenameFolder)
	protected.PATCH("/folders/:id/move", fileHandler.MoveFolder)
	protected.DELETE("/folders/:id", fileHandler.DeleteFolder)
	protected.GET("/storage", fileHandler.GetStorageUsage)
	protected.POST("/files/:id/shares", shareHandler.Create)
	protected.GET("/shares", shareHandler.List)
	protected.DELETE("/shares/:token", shareHandler.Revoke)
	protected.GET("/jobs/:id", jobHTTPHandler.Get)

	runContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	workerDone := make(chan struct{})

	go func() {
		defer close(workerDone)
		jobRunner.Run(runContext)
	}()

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: r,
	}

	go func() {
		<-runContext.Done()

		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownContext); err != nil {
			slog.Error("shutdown HTTP server:", "error", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve HTTP", "error", err)
		stop()
	}

	<-workerDone
}
