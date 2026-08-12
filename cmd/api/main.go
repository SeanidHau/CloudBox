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
	scannermodule "github.com/SeanidHau/CloudBox/internal/scanner"
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
			"migrations/011_file_scans.sql",
			"migrations/012_user_access.sql",
			"migrations/013_share_access_audit.sql",
			"migrations/014_share_collections.sql",
		}

	case "postgres":
		db, err = database.OpenPostgres(cfg.DatabaseURL)
		migrationPaths = []string{
			"migrations/postgres/001_init.sql",
			"migrations/postgres/002_background_jobs.sql",
			"migrations/postgres/003_background_job_user.sql",
			"migrations/postgres/004_file_preview.sql",
			"migrations/postgres/005_file_scans.sql",
			"migrations/postgres/006_user_access.sql",
			"migrations/postgres/007_share_access_audit.sql",
			"migrations/postgres/008_share_collections.sql",
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
	authService := auth.NewService(authRepo, cfg.JWTSecret, cfg.UserStorageQuotaBytes)
	if _, err := authService.BootstrapAdmin(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("bootstrap administrator: %v", err)
	}
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

	if cfg.ClamAV.Enabled {
		clamAVScanner, err := scannermodule.NewClamAVScanner(cfg.ClamAV.Address)
		if err != nil {
			log.Fatalf("create ClamAV scanner: %v", err)
		}

		fileServiceOptions = append(
			fileServiceOptions,
			filemodule.WithVirusScanner(clamAVScanner),
			filemodule.WithVirusScanTimeout(cfg.ClamAV.Timeout))
	}

	filerepo := filemodule.NewRepository(db)
	fileService := filemodule.NewService(
		filerepo,
		objectStorage,
		cfg.UserStorageQuotaBytes,
		append(fileServiceOptions,
			filemodule.WithStorageQuotaProvider(authRepo),
			filemodule.WithTrashRetention(cfg.TrashRetention),
		)...,
	)
	jobRunner := jobmodule.NewRunner(
		jobRepo,
		map[string]jobmodule.Handler{
			jobmodule.TypeVerifyFile:        filemodule.NewVerifyFileJobHandler(fileService),
			jobmodule.TypeGenerateThumbnail: filemodule.NewThumbnailJobHandler(fileService),
			jobmodule.TypeScanFile:          filemodule.NewScanFileJobHandler(fileService),
		},
		jobmodule.WithWorkerCount(cfg.JobWorkerCount),
		jobmodule.WithPollInterval(cfg.JobPollInterval),
		jobmodule.WithLogger(requestLogger),
	)
	fileHandler := filemodule.NewHandler(fileService)

	shareRepo := sharemodule.NewRepository(db)
	shareService := sharemodule.NewService(
		shareRepo,
		objectStorage,
		sharemodule.WithDownloadPolicy(fileService),
		sharemodule.WithFileSaver(fileService),
	)
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

	api.GET("/shares/:token", shareHandler.PublicInfo)
	api.GET("/shares/:token/preview", shareHandler.PublicPreview)
	api.GET("/share-collections/:token", shareHandler.PublicCollection)

	protected := api.Group("")
	protected.Use(middleware.Auth(cfg.JWTSecret, authService.ValidateSession))

	protected.GET("/me", authHandler.Me)
	protected.POST("/auth/change-password", authHandler.ChangePassword)
	ready := protected.Group("")
	ready.Use(middleware.RequirePasswordChanged())
	ready.GET("/admin/users", authHandler.ListUsers)
	ready.PATCH("/admin/users/:id/quota", authHandler.SetUserQuota)
	ready.PATCH("/admin/users/:id/status", authHandler.SetUserStatus)
	ready.POST("/admin/users/:id/reset-password", authHandler.ResetPassword)
	ready.DELETE("/admin/users/:id/shares", authHandler.RevokeAllUserShares)
	ready.POST("/admin/invitations", authHandler.CreateInvitation)
	ready.GET("/admin/invitations", authHandler.ListInvitations)
	ready.DELETE("/admin/invitations/:id", authHandler.RevokeInvitation)

	ready.POST("/files", fileHandler.Upload)
	ready.POST("/files/instant", fileHandler.InstantUpload)
	ready.GET("/files", fileHandler.ListActive)
	ready.GET("/files/search", fileHandler.Search)
	ready.GET("/files/trash", fileHandler.ListDeleted)
	ready.GET("/files/:id/download", fileHandler.Download)
	ready.GET("/files/:id/preview", fileHandler.Preview)
	ready.GET("/files/:id/thumbnail", fileHandler.DownloadThumbnail)
	ready.DELETE("/files/:id/permanent", fileHandler.PermanentlyDelete)
	ready.DELETE("/files/:id", fileHandler.SoftDelete)
	ready.POST("/files/:id/restore", fileHandler.Restore)
	ready.POST("/files/:id/verify", fileHandler.EnqueueVerification)
	ready.POST("/uploads/init", uploadHandler.Init)
	ready.GET("/uploads", uploadHandler.ListUploading)
	ready.PUT("/uploads/:id/chunks/:number", uploadHandler.UploadChunk)
	ready.POST("/uploads/:id/complete", uploadHandler.Complete)
	ready.GET("/uploads/:id", uploadHandler.GetStatus)
	ready.DELETE("/uploads/:id", uploadHandler.Cancel)
	ready.POST("/folders", fileHandler.CreateFolder)
	ready.GET("/folders", fileHandler.ListFolders)
	ready.PATCH("/files/:id/move", fileHandler.MoveActive)
	ready.PATCH("/files/:id/rename", fileHandler.RenameActive)
	ready.PATCH("/folders/:id/rename", fileHandler.RenameFolder)
	ready.PATCH("/folders/:id/move", fileHandler.MoveFolder)
	ready.DELETE("/folders/:id", fileHandler.DeleteFolder)
	ready.GET("/storage", fileHandler.GetStorageUsage)
	ready.POST("/files/:id/shares", shareHandler.Create)
	ready.POST("/share-collections", shareHandler.CreateCollection)
	ready.POST("/shares/:token/save", shareHandler.Save)
	ready.GET("/shares/:token/download", shareHandler.Download)
	ready.GET("/share-collections/:token/files/:id/download", shareHandler.DownloadCollectionFile)
	ready.GET("/shares", shareHandler.List)
	ready.GET("/share-collections", shareHandler.ListCollections)
	ready.DELETE("/shares/:token", shareHandler.Revoke)
	ready.DELETE("/share-collections/:token", shareHandler.RevokeCollection)
	ready.GET("/jobs/:id", jobHTTPHandler.Get)

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
