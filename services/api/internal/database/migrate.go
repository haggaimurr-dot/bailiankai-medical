package database

import (
	"context"
	"embed"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrations embed.FS

// MigrationsFS exposes the embedded migration files for the migrate CLI
// (drills, status checks). The API itself only ever runs Migrate (up).
func MigrationsFS() embed.FS { return migrations }

func Migrate(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, "migrations")
}
