package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/handler"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/database/seeder"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/validator"

	_ "github.com/joho/godotenv/autoload"
	"go.uber.org/zap"
)

// @title FMS Backend API
// @version 1.0
// @description Fleet Management System (FMC150 trackers): live tracking, shifts, ritase reports.
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
	seed := flag.Bool("seed", false, "Seed the database (admin user + default shift templates)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("Failed to load config: %v", err))
	}

	loggerCfg := logger.LoggerConfig{
		Debug:       cfg.App.Debug,
		Format:      cfg.Logger.Format,
		LogToFile:   cfg.Logger.LogToFile,
		LogFilePath: cfg.Logger.LogFilePath,
		MaxSize:     cfg.Logger.MaxSize,
		MaxBackups:  cfg.Logger.MaxBackups,
		MaxAge:      cfg.Logger.MaxAge,
		Compress:    cfg.Logger.Compress,
	}
	if err := logger.InitLogger(loggerCfg); err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	logger.Info("Starting application", zap.String("name", cfg.App.Name), zap.String("env", cfg.App.Env))

	validator.InitValidator()

	// The database may be unreachable for a while (server down, port blocked on the
	// office network): keep retrying instead of exiting, like the demo did.
	var db *database.Database
	for attempt := 1; ; attempt++ {
		db, err = database.NewDatabase(&cfg.Database)
		if err == nil {
			break
		}
		wait := min(60, 5*attempt)
		logger.Warn("Database unreachable, retrying", zap.Error(err), zap.Int("attempt", attempt), zap.Int("wait_s", wait))
		time.Sleep(time.Duration(wait) * time.Second)
	}
	defer db.Close()

	if *seed {
		logger.Info("Running database seeder...")
		if err := seeder.Seed(db.DB, &cfg.Seed); err != nil {
			logger.Fatal("Failed to seed database", zap.Error(err))
		}
		logger.Info("Database seeding completed successfully")
		os.Exit(0)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	router, err := handler.Router(appCtx, cfg, db.DB)
	if err != nil {
		logger.Fatal("Failed to setup router", zap.Error(err))
	}

	srv := &http.Server{
		Addr:           ":" + cfg.App.Port,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   60 * time.Second, // /api/stream lifts this per request
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		logger.Info("Server is running", zap.String("port", cfg.App.Port), zap.String("device_port", cfg.Device.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	appCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}
	logger.Info("Server exited")
}
