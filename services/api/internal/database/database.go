package database

import (
	"context"
	"fmt"
	"time"
	_ "time/tzdata"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var Shanghai = mustShanghai()

func mustShanghai() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return l
}

func Open(dsn string) (*gorm.DB, error) {
	c, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DB_DSN: %w", err)
	}
	if c.DBName == "" {
		return nil, fmt.Errorf("DB_DSN must select a database")
	}
	c.ParseTime, c.Loc = true, Shanghai
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
	if c.Params == nil {
		c.Params = make(map[string]string)
	}
	c.Params["charset"] = "utf8mb4"
	c.Params["time_zone"] = "'+08:00'"
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: c.FormatDSN()}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(3 * time.Minute)
	return db, nil
}

func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}
