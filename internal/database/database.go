// Package database opens MySQL connections without changing the schema.
package database

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"voex-server/internal/config"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func connection(settings config.Database, create bool) (*gorm.DB, error) {
	cfg := driver.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = settings.Host + ":" + settings.Port
	cfg.User, cfg.Passwd, cfg.DBName = settings.User, settings.Password, settings.Name
	cfg.ParseTime, cfg.Loc = true, time.Local
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
	cfg.Collation = "utf8mb4_unicode_ci"
	open := func(c *driver.Config) (*gorm.DB, error) {
		return gorm.Open(mysql.Open(c.FormatDSN()), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true,
			DisableForeignKeyConstraintWhenMigrating: true,
		})
	}
	db, err := open(cfg)
	if err != nil && create && missingDatabase(err) {
		bootstrap := *cfg
		bootstrap.DBName = ""
		admin, e := open(&bootstrap)
		if e != nil {
			return nil, e
		}
		pool, e := admin.DB()
		if e != nil {
			return nil, e
		}
		e = admin.Exec("CREATE DATABASE IF NOT EXISTS `" + strings.ReplaceAll(cfg.DBName, "`", "``") + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error
		_ = pool.Close()
		if e != nil {
			return nil, e
		}
		db, err = open(cfg)
	}
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

func missingDatabase(err error) bool {
	var mysqlErr *driver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1049
}

// Open only connects and checks compatibility. Run the separate migrate command first.
func Open(settings config.Database) (*gorm.DB, error) {
	db, err := connection(settings, false)
	if err != nil {
		return nil, err
	}
	status, err := inspect(db)
	if err == nil && !status.Compatible {
		err = fmt.Errorf("schema incompatible: %s; run voex-server migrate before serve", status.Error)
	}
	if err != nil {
		if pool, poolErr := db.DB(); poolErr == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	return db, nil
}
