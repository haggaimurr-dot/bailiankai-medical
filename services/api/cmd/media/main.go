// media is an operations command, never exposed as a patient or staff endpoint.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/bailiankai/api/internal/database"
	"example.com/bailiankai/api/internal/mediaconfig"
	"example.com/bailiankai/api/internal/store"
)

func main() {
	if err := run(); err != nil {
		// SDK errors can contain signed URLs; do not print their raw contents.
		slog.Error("media command failed; verify storage permissions, key, checksums and database/snapshot compatibility")
		os.Exit(1)
	}
}

func run() error {
	operation := flag.String("operation", "check", "check, cleanup, backup, restore or migrate-local")
	snapshot := flag.String("snapshot", "", "snapshot id (required for restore)")
	apply := flag.Bool("apply", false, "perform cleanup/recovery writes; otherwise verify only")
	sourceRoot := flag.String("source-root", "", "existing local private-media directory for migrate-local")
	flag.Parse()
	cfg, err := mediaconfig.Load()
	if err != nil {
		return err
	}
	primary, err := cfg.Open()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	if err := primary.Ready(ctx); err != nil {
		return err
	}
	if *operation == "check" {
		fmt.Println("private storage checks passed")
		return nil
	}
	db, err := database.Open(os.Getenv("DB_DSN"))
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	repo := store.New(db)
	if err := repo.Ready(ctx); err != nil {
		return err
	}
	if *operation == "cleanup" {
		result, err := store.CleanupMedia(ctx, primary, repo, time.Now(), 24*time.Hour, *apply)
		fmt.Printf("candidates=%d deleted=%d failures=%d apply=%t\n", result.Candidates, result.Deleted, result.Failures, *apply)
		return err
	}
	if *operation == "migrate-local" {
		if cfg.Driver != "cos" || *sourceRoot == "" {
			return fmt.Errorf("migrate-local requires COS and source-root")
		}
		if info, err := os.Stat(*sourceRoot); err != nil || !info.IsDir() {
			return fmt.Errorf("source directory must already exist")
		}
		source, err := store.NewPrivateMedia(*sourceRoot)
		if err != nil {
			return err
		}
		refs, err := repo.MediaReferences(ctx)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			data, err := source.Read(ctx, ref.ID)
			if err != nil {
				return err
			}
			if err := store.VerifyMedia(data, ref); err != nil {
				return err
			}
			if !*apply {
				continue
			}
			if err := primary.Save(ctx, ref.ID, data); err != nil {
				return err
			}
			check, err := primary.Read(ctx, ref.ID)
			if err != nil {
				return err
			}
			if err := store.VerifyMedia(check, ref); err != nil {
				return err
			}
		}
		fmt.Printf("local migration verified objects=%d apply=%t\n", len(refs), *apply)
		return nil
	}
	if *operation != "backup" && *operation != "restore" {
		return fmt.Errorf("unknown operation")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("MEDIA_BACKUP_KEY"))
	if err != nil || len(key) != 32 {
		return fmt.Errorf("MEDIA_BACKUP_KEY must contain base64 of 32 random bytes")
	}
	if *operation == "backup" {
		if *snapshot != "" {
			return fmt.Errorf("backup snapshot id is generated automatically")
		}
		*snapshot = store.NewMediaSnapshotID()
	}
	backupCfg, err := mediaconfig.Backup(cfg, *snapshot)
	if err != nil {
		return err
	}
	backup, err := backupCfg.Open()
	if err != nil {
		return err
	}
	// Emit id before copying so operators can locate and remove failed snapshots.
	fmt.Printf("snapshot=%s\n", *snapshot)
	var count int
	if *operation == "backup" {
		count, err = store.BackupMedia(ctx, primary, backup, repo, *snapshot, key)
	} else {
		count, err = store.RestoreMedia(ctx, backup, primary, repo, *snapshot, key, *apply)
	}
	if err == nil {
		fmt.Printf("%s verified objects=%d apply=%t\n", *operation, count, *operation == "backup" || *apply)
	}
	return err
}
