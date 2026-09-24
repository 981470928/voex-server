package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var databaseSchema string

func openDatabase() (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = env("MYSQL_HOST", "127.0.0.1") + ":" + env("MYSQL_PORT", "3306")
	cfg.User = env("MYSQL_USER", "root")
	cfg.Passwd = env("MYSQL_PASSWORD", "")
	cfg.DBName = env("MYSQL_DATABASE", "file_server")
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Collation = "utf8mb4_unicode_ci"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.Ping(); err != nil {
		var dbError *mysql.MySQLError
		if !errors.As(err, &dbError) || dbError.Number != 1049 {
			db.Close()
			return nil, err
		}
		bootstrap := *cfg
		bootstrap.DBName = ""
		admin, e := sql.Open("mysql", bootstrap.FormatDSN())
		if e != nil {
			db.Close()
			return nil, e
		}
		_, e = admin.Exec("CREATE DATABASE IF NOT EXISTS `" + strings.ReplaceAll(cfg.DBName, "`", "``") + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
		admin.Close()
		if e != nil {
			db.Close()
			return nil, e
		}
		if e = db.Ping(); e != nil {
			db.Close()
			return nil, e
		}
	}
	if err = initializeSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func initializeSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	var locked int
	if err = c.QueryRowContext(ctx, "SELECT GET_LOCK('voex-go-schema',30)").Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return fmt.Errorf("schema lock unavailable")
	}
	defer c.ExecContext(context.Background(), "SELECT RELEASE_LOCK('voex-go-schema')")
	var tables int
	if err = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		if _, err = c.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
			return err
		}
		defer c.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=1")
		for _, statement := range strings.Split(databaseSchema, ";\n\n") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = c.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	var versions int
	if err = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version IN ('001-workspace','002-auth','003-teams')").Scan(&versions); err != nil {
		return err
	}
	if versions != 3 {
		return fmt.Errorf("legacy schema migrations incomplete")
	}
	var lockID int
	if err = c.QueryRowContext(ctx, "SELECT id FROM workspace_state WHERE id=1").Scan(&lockID); err != nil {
		return err
	}
	// Existing installations keep their schema and data; incompatible schemas fail closed.
	for _, statement := range []string{"SELECT id,account,password_hash,name,avator,email,phone FROM users LIMIT 0", "SELECT id,user_id,refresh_hash,expires_at FROM auth_sessions LIMIT 0", "SELECT team_key,team_code,owner_id,personal_owner_id,privileges FROM teams LIMIT 0", "SELECT project_key,team_id,creator_id,privileges FROM projects LIMIT 0", "SELECT folder_key,project_id,parent_id,privileges FROM folders LIMIT 0", "SELECT file_key,folder_id,creator_id,revision,privileges FROM documents LIMIT 0", "SELECT file_key,hash,creator_id,privileges FROM files LIMIT 0", "SELECT directory,storage_name,mime FROM assets LIMIT 0", "SELECT document_id,token_hash,permission,revoked_at FROM file_shares LIMIT 0"} {
		rows, e := c.QueryContext(ctx, statement)
		if e != nil {
			return e
		}
		rows.Close()
	}
	return nil
}
