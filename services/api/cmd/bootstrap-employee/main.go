package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"example.com/bailiankai/api/internal/config"
	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/model"
	"example.com/bailiankai/api/internal/store"
	"gorm.io/gorm"
)

func main() {
	if err := run(); err != nil {
		slog.Error("employee bootstrap failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	path := os.Getenv("EMPLOYEE_CREDENTIALS_FILE")
	if path == "" {
		return errors.New("EMPLOYEE_CREDENTIALS_FILE is required")
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var count int64
	if err := db.WithContext(ctx).Model(&model.EmployeeAccount{}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	password := hex.EncodeToString(secret)
	displayName := "主管"
	if cfg.Environment == "development" {
		displayName = "本地主管"
	}
	_, err = store.New(db).CreateEmployee(ctx, "manager", displayName, password, "manager", "", time.Now())
	if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, store.ErrAccountExists) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("username=manager\npassword=%s\n", password)), 0600); err != nil {
		return err
	}
	slog.Info("manager account created", "credentialsFile", path)
	return nil
}
