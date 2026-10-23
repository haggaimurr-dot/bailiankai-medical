package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/bailiankai/api/internal/config"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/httpapi"
	"example.com/bailiankai/api/internal/store"
	"example.com/bailiankai/api/internal/wechat"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(cfg.DSN)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	repo := store.New(db)
	var messageSender store.MessageSender
	if cfg.WechatAppID != "" {
		client := wechat.New(cfg.WechatAppID, cfg.WechatAppSecret)
		repo.SetPatientProvider(client)
		messageSender = client
	}
	if err := repo.SetPendingConfirmation(cfg.PendingConfirmation); err != nil {
		return err
	}
	media, err := cfg.Media.Open()
	if err != nil {
		return err
	}
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 15*time.Second)
	err = media.Ready(probeCtx)
	cancelProbe()
	if err != nil {
		return err
	}
	provider, assessmentConfig, err := store.LoadAssessmentGateway()
	if err != nil {
		return err
	}
	assessments := store.NewAssessmentService(repo, media, provider, assessmentConfig)
	handler := httpapi.NewWithAssessmentService(repo, media, assessments)
	srv := &http.Server{Addr: cfg.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); repo.RunExpiryWorker(workerCtx, 15*time.Second) }()
	mediaWorkerDone := make(chan struct{})
	go func() { defer close(mediaWorkerDone); repo.RunMediaCleanupWorker(workerCtx, media) }()
	notificationDone := make(chan struct{})
	go func() { defer close(notificationDone); repo.RunNotificationWorker(workerCtx, messageSender) }()
	handoffDone := make(chan struct{})
	go func() { defer close(handoffDone); repo.RunHandoffNotificationWorker(workerCtx) }()
	assessmentDone := make(chan struct{})
	go func() { defer close(assessmentDone); assessments.Run(workerCtx) }()
	exportDone := make(chan struct{})
	go func() { defer close(exportDone); repo.RunRecordExportWorker(workerCtx) }()
	defer func() {
		cancelWorker()
		<-workerDone
		<-mediaWorkerDone
		<-notificationDone
		<-exportDone
		<-handoffDone
		<-assessmentDone
	}()
	serverError := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.Address, "mode", cfg.Environment)
		serverError <- srv.ListenAndServe()
	}()
	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
