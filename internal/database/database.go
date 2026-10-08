// Package database configures GORM and applies the explicit bootstrap schema.
package database

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"strings"
	"time"
	"voex-server/internal/config"
)

//go:embed schema.sql
var schema string

func Open(settings config.Database) (*gorm.DB, error) {
	cfg := driver.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = settings.Host + ":" + settings.Port
	cfg.User = settings.User
	cfg.Passwd = settings.Password
	cfg.DBName = settings.Name
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Collation = "utf8mb4_unicode_ci"
	open := func(c *driver.Config) (*gorm.DB, error) {
		return gorm.Open(mysql.Open(c.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true, DisableForeignKeyConstraintWhenMigrating: true})
	}
	db, err := open(cfg)
	if err != nil {
		var d *driver.MySQLError
		if !errors.As(err, &d) || d.Number != 1049 {
			return nil, err
		}
		bootstrap := *cfg
		bootstrap.DBName = ""
		admin, e := open(&bootstrap)
		if e != nil {
			return nil, e
		}
		sqlAdmin, e := admin.DB()
		if e != nil {
			return nil, e
		}
		e = admin.Exec("CREATE DATABASE IF NOT EXISTS `" + strings.ReplaceAll(cfg.DBName, "`", "``") + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci").Error
		sqlAdmin.Close()
		if e != nil {
			return nil, e
		}
		db, err = open(cfg)
		if err != nil {
			return nil, err
		}
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(5 * time.Minute)
	if err = initialize(db); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}
func initialize(db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return db.WithContext(ctx).Connection(func(c *gorm.DB) error {
		// Connection pins the physical connection; NewDB isolates each statement builder.
		c = c.Session(&gorm.Session{NewDB: true})
		var lock int
		if err := c.Raw("SELECT GET_LOCK('voex-go-schema',30)").Scan(&lock).Error; err != nil {
			return err
		}
		if lock != 1 {
			return fmt.Errorf("schema lock unavailable")
		}
		defer func() {
			release, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c.WithContext(release).Exec("SELECT RELEASE_LOCK('voex-go-schema')")
		}()
		var tables int64
		if err := c.Table("information_schema.TABLES").Where("TABLE_SCHEMA=DATABASE()").Count(&tables).Error; err != nil {
			return err
		}
		if tables == 0 {
			if err := c.Exec("SET FOREIGN_KEY_CHECKS=0").Error; err != nil {
				return err
			}
			defer func() {
				restore, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				c.WithContext(restore).Exec("SET FOREIGN_KEY_CHECKS=1")
			}()
			for _, statement := range strings.Split(schema, ";\n\n") {
				if strings.TrimSpace(statement) == "" {
					continue
				}
				if err := c.Exec(statement).Error; err != nil {
					return err
				}
			}
		}
		var versions int64
		if err := c.Table("schema_migrations").Where("version IN ?", []string{"001-workspace", "002-auth", "003-teams"}).Count(&versions).Error; err != nil {
			return err
		}
		if versions != 3 {
			return fmt.Errorf("required schema migrations incomplete")
		}
		var records int64
		if err := c.Table("workspace_state").Where("id=1").Count(&records).Error; err != nil {
			return err
		}
		if records != 1 {
			return fmt.Errorf("workspace lock missing")
		}
		required := map[string][]string{"users": {"id", "account", "password_hash", "name", "avator", "email", "phone"}, "auth_sessions": {"id", "user_id", "refresh_hash", "expires_at"}, "teams": {"team_key", "team_code", "owner_id", "personal_owner_id", "privileges"}, "projects": {"project_key", "team_id", "creator_id", "privileges"}, "folders": {"folder_key", "project_id", "parent_id", "privileges"}, "documents": {"file_key", "folder_id", "creator_id", "revision", "privileges"}, "files": {"file_key", "hash", "creator_id", "privileges"}, "assets": {"directory", "storage_name", "mime"}, "file_shares": {"document_id", "token_hash", "permission", "revoked_at"}}
		for table, columns := range required {
			var rows []map[string]any
			if err := c.Table(table).Select(columns).Where("1=0").Find(&rows).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
