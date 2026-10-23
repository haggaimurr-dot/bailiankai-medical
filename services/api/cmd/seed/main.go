package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"example.com/bailiankai/api/internal/config"
	"example.com/bailiankai/api/internal/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Environment != "development" {
		return errors.New("demo seed is only allowed in development")
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Seed(ctx, db, time.Now()); err != nil {
		return err
	}
	slog.Info("demo seed complete", "clinic", "clinic-demo", "doctors", 2)
	return nil
}
