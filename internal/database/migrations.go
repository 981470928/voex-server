package database

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
	"time"
	"voex-server/internal/config"

	"gorm.io/gorm"
)

// Existing SQL remains explicit and immutable once its checksum has been recorded.
//
//go:embed schema.sql
var schema string

//go:embed recent-files.sql
var recentFilesMigration string

//go:embed document-details.sql
var documentDetailsMigration string

//go:embed trash.sql
var trashMigration string

//go:embed ai.sql
var aiMigration string

//go:embed ai-history.sql
var aiHistoryMigration string

//go:embed ai-sessions.sql
var aiSessionsMigration string

//go:embed user-config.sql
var userConfigMigration string

// LatestVersion is the schema required by this binary.
const LatestVersion = "010-user-config"

type migration struct{ version, script, source string }

// 001-003 were historically delivered together in schema.sql. Preserve these
// version identifiers; 002 and 003 verify the same bootstrap instead of inventing
// unsupported upgrade paths from an incomplete pre-bootstrap database.
func migrations() []migration {
	return []migration{
		{"001-workspace", schema, schema},
		{"002-auth", "", schema},
		{"003-teams", "", schema},
		{"004-recent-files", recentFilesMigration, recentFilesMigration},
		{"005-document-details", documentDetailsMigration, documentDetailsMigration},
		{"006-trash", trashMigration, trashMigration},
		{"007-ai-agent", aiMigration, aiMigration},
		{"008-ai-conversation-history", aiHistoryMigration, aiHistoryMigration},
		{"009-ai-conversation-sessions", aiSessionsMigration, aiSessionsMigration},
		{"010-user-config", userConfigMigration, userConfigMigration},
	}
}

func (m migration) checksum() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(m.version+"\n"+m.source)))
}

const registryDDL = `CREATE TABLE schema_migrations (
    version varchar(64) NOT NULL PRIMARY KEY,
    applied_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    checksum char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
    status varchar(16) NOT NULL DEFAULT 'legacy',
    started_at datetime(6) DEFAULT NULL,
    finished_at datetime(6) DEFAULT NULL,
    last_error text DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

const registryUpgrade = `ALTER TABLE schema_migrations
    ADD COLUMN checksum char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
    ADD COLUMN status varchar(16) NOT NULL DEFAULT 'legacy',
    ADD COLUMN started_at datetime(6) DEFAULT NULL,
    ADD COLUMN finished_at datetime(6) DEFAULT NULL,
    ADD COLUMN last_error text DEFAULT NULL`

// Migrate is the only entry point allowed to create a database or execute DDL.
// MySQL DDL is not transactional: a failing migration stays dirty, even if some
// statements committed. An operator must inspect/restore it before retrying.
func Migrate(settings config.Database) error {
	db, err := connection(settings, true)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return db.WithContext(ctx).Connection(func(c *gorm.DB) error {
		c = c.Session(&gorm.Session{NewDB: true})
		var acquired int
		if err := c.Raw("SELECT GET_LOCK('voex-go-schema',30)").Scan(&acquired).Error; err != nil {
			return err
		}
		if acquired != 1 {
			return fmt.Errorf("schema migration lock unavailable")
		}
		defer func() {
			release, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c.WithContext(release).Exec("SELECT RELEASE_LOCK('voex-go-schema')")
		}()
		state, err := readState(c)
		if err != nil {
			return err
		}
		if err := validateState(c, state); err != nil {
			return err
		}
		if err := checkPending(c, state); err != nil {
			return err
		}
		if !state.registry {
			if err := c.Exec(registryDDL).Error; err != nil {
				return err
			}
		} else if !state.metadata {
			if err := c.Exec(registryUpgrade).Error; err != nil {
				return err
			}
		}
		for _, m := range migrations() {
			if record, exists := state.records[m.version]; exists {
				if record.Status == "legacy" {
					// All legacy structures were checked before changing the ledger.
					result := c.Exec(`UPDATE schema_migrations SET checksum=?, status='baseline',
                        started_at=NULL, finished_at=applied_at, last_error=NULL
                        WHERE version=? AND checksum IS NULL AND status='legacy'`, m.checksum(), m.version)
					if result.Error != nil {
						return result.Error
					}
					if result.RowsAffected != 1 {
						return fmt.Errorf("cannot baseline migration %s", m.version)
					}
				}
				continue
			}
			if err := applyMigration(c, m); err != nil {
				return err
			}
		}
		status, err := inspect(c)
		if err != nil {
			return err
		}
		if !status.Compatible {
			return fmt.Errorf("migration verification failed: %s", status.Error)
		}
		return nil
	})
}

func applyMigration(c *gorm.DB, m migration) error {
	if err := c.Exec(`INSERT INTO schema_migrations
        (version,checksum,status,started_at) VALUES (?,?,'dirty',CURRENT_TIMESTAMP(6))`, m.version, m.checksum()).Error; err != nil {
		return err
	}
	// Bootstrap tables have historical forward references. FK checks are disabled
	// only on this pinned connection and only while the complete bootstrap runs.
	if m.version == "001-workspace" {
		if err := c.Exec("SET FOREIGN_KEY_CHECKS=0").Error; err != nil {
			return markDirty(c, m, err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c.WithContext(ctx).Exec("SET FOREIGN_KEY_CHECKS=1")
		}()
	}
	for _, statement := range strings.Split(m.script, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if err := c.Exec(statement).Error; err != nil {
			return markDirty(c, m, err)
		}
	}
	if err := validateMigration(c, m.version); err != nil {
		return markDirty(c, m, err)
	}
	if m.version == "001-workspace" {
		if err := c.Exec("SET FOREIGN_KEY_CHECKS=1").Error; err != nil {
			return markDirty(c, m, err)
		}
	}
	if err := c.Exec(`UPDATE schema_migrations SET status='applied', applied_at=CURRENT_TIMESTAMP,
        finished_at=CURRENT_TIMESTAMP(6),last_error=NULL WHERE version=? AND status='dirty'`, m.version).Error; err != nil {
		return markDirty(c, m, err)
	}
	return nil
}

func markDirty(c *gorm.DB, m migration, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A lost connection may prevent recording the error, but the pre-DDL dirty
	// row already persists and still blocks the next migrate/serve.
	result := c.WithContext(ctx).Exec(`UPDATE schema_migrations SET finished_at=CURRENT_TIMESTAMP(6),
        last_error=? WHERE version=? AND status='dirty'`, cause.Error(), m.version)
	if result.Error != nil {
		return fmt.Errorf("migration %s dirty: %w (could not persist error: %v); restore/repair explicitly before retrying", m.version, cause, result.Error)
	}
	return fmt.Errorf("migration %s dirty: %w; committed DDL was not rolled back; restore/repair explicitly before retrying", m.version, cause)
}
