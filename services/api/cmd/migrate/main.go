// Command migrate runs Goose migrations for the Bailiankai API using the same
// embedded migration files as the API itself.
//
// Usage:
//
//	migrate up                  # apply pending migrations (same as API startup)
//	migrate status              # show applied/pending migrations
//	migrate down -steps 1       # roll back N migrations (drills only)
//
// Safety: down/status/up refuse any DSN whose database is not the test
// schema (bailiankai_web) unless -allow-prod is passed explicitly, so a
// drill can never touch production by accident. The DSN comes from the
// same config as the API (DB_DSN).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"example.com/bailiankai/api/internal/config"
	"example.com/bailiankai/api/internal/database"
	"github.com/pressly/goose/v3"
)

func main() {
	cmd := flag.String("cmd", "up", "migration command: status | up | down")
	steps := flag.Int("steps", 1, "number of migrations to roll back (down only)")
	allowProd := flag.Bool("allow-prod", false, "allow running against a non-test database (explicit opt-in)")
	flag.Parse()
	if err := run(*cmd, *steps, *allowProd); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run(cmd string, steps int, allowProd bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !allowProd && !strings.Contains(cfg.DSN, "/bailiankai_web?") && !strings.HasSuffix(cfg.DSN, "/bailiankai_web") {
		return fmt.Errorf("refusing: DSN database is not bailiankai_web (test schema); pass -allow-prod to override")
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	switch cmd {
	case "status":
		goose.SetBaseFS(database.MigrationsFS())
		if err := goose.SetDialect("mysql"); err != nil {
			return err
		}
		return goose.StatusContext(ctx, sqlDB, "migrations")
	case "up":
		return database.Migrate(ctx, db)
	case "down":
		if steps < 1 {
			return fmt.Errorf("down requires -steps >= 1")
		}
		goose.SetBaseFS(database.MigrationsFS())
		if err := goose.SetDialect("mysql"); err != nil {
			return err
		}
		for i := 0; i < steps; i++ {
			if err := goose.DownContext(ctx, sqlDB, "migrations"); err != nil {
				return err
			}
		}
		fmt.Println("migrate down: ok")
		return nil
	default:
		return fmt.Errorf("unknown -cmd: %s", cmd)
	}
}
