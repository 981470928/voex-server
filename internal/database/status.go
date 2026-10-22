package database

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"voex-server/internal/config"

	"gorm.io/gorm"
)

type migrationRecord struct{ Version, Checksum, Status string }
type schemaState struct {
	registry, metadata bool
	tableCount         int64
	records            map[string]migrationRecord
}

// SchemaStatus is a read-only report suitable for updater preflight checks.
// Compatible means serve may start; CanMigrate means the fixed forward sequence
// is recognized and safe to attempt. Neither field promises a reversible DDL.
type SchemaStatus struct {
	Exists          bool     `json:"exists"`
	Current         string   `json:"current"`
	Target          string   `json:"target"`
	Compatible      bool     `json:"compatible"`
	CanMigrate      bool     `json:"can_migrate"`
	NeedsMigration  bool     `json:"needs_migration"`
	Legacy          bool     `json:"legacy"`
	Dirty           bool     `json:"dirty"`
	DirtyVersions   []string `json:"dirty_versions"`
	Applied         []string `json:"applied"`
	Pending         []string `json:"pending"`
	UnknownVersions []string `json:"unknown_versions"`
	Error           string   `json:"error,omitempty"`
}

func newStatus() SchemaStatus {
	all := migrations()
	return SchemaStatus{Target: all[len(all)-1].version,
		DirtyVersions: []string{}, Applied: []string{}, Pending: []string{}, UnknownVersions: []string{}}
}

// Inspect never creates a database or upgrades migration metadata. Recognized
// incompatibilities are reported in JSON; connection/query failures are errors.
func Inspect(settings config.Database) (SchemaStatus, error) {
	status := newStatus()
	db, err := connection(settings, false)
	if err != nil {
		if missingDatabase(err) {
			status.NeedsMigration, status.CanMigrate = true, true
			for _, m := range migrations() {
				status.Pending = append(status.Pending, m.version)
			}
			status.Error = "database does not exist; run voex-server migrate"
			return status, nil
		}
		status.Error = err.Error()
		return status, err
	}
	pool, err := db.DB()
	if err != nil {
		status.Error = err.Error()
		return status, err
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return inspect(db.WithContext(ctx))
}

func inspect(db *gorm.DB) (SchemaStatus, error) {
	status := newStatus()
	status.Exists = true
	state, err := readState(db)
	if err != nil {
		status.Error = err.Error()
		return status, err
	}
	known := map[string]bool{}
	gap := false
	for _, m := range migrations() {
		known[m.version] = true
		record, exists := state.records[m.version]
		if !exists {
			status.Pending = append(status.Pending, m.version)
			gap = true
			continue
		}
		complete := record.Status == "applied" || record.Status == "baseline" || record.Status == "legacy"
		if complete {
			status.Applied = append(status.Applied, m.version)
			if !gap {
				status.Current = m.version
			}
		} else {
			gap = true
		}
		if record.Status == "dirty" {
			status.Dirty = true
			status.DirtyVersions = append(status.DirtyVersions, m.version)
		}
		if record.Status == "legacy" {
			status.Legacy = true
		}
	}
	for version := range state.records {
		if !known[version] {
			status.UnknownVersions = append(status.UnknownVersions, version)
		}
	}
	sort.Strings(status.UnknownVersions)
	status.NeedsMigration = !state.metadata || status.Legacy || len(status.Pending) > 0
	if err := validateState(db, state); err != nil {
		status.Error = err.Error()
		return status, nil
	}
	if err := checkPending(db, state); err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.CanMigrate = true
	status.Compatible = !status.NeedsMigration
	if status.NeedsMigration {
		status.Error = "recognized schema requires voex-server migrate"
	}
	return status, nil
}

func readState(db *gorm.DB) (schemaState, error) {
	state := schemaState{records: map[string]migrationRecord{}}
	var tables []string
	if err := db.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&tables).Error; err != nil {
		return state, err
	}
	state.tableCount = int64(len(tables))
	for _, name := range tables {
		if name == "schema_migrations" {
			state.registry = true
		}
	}
	if !state.registry {
		return state, nil
	}
	var columns []string
	if err := db.Raw("SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations'").Scan(&columns).Error; err != nil {
		return state, err
	}
	present := map[string]bool{}
	for _, name := range columns {
		present[name] = true
	}
	if !present["version"] || !present["applied_at"] {
		return state, fmt.Errorf("unrecognized schema_migrations table")
	}
	metadata := []string{"checksum", "status", "started_at", "finished_at", "last_error"}
	count := 0
	for _, name := range metadata {
		if present[name] {
			count++
		}
	}
	if count != 0 && count != len(metadata) {
		return state, fmt.Errorf("schema_migrations metadata is incomplete; inspect/repair explicitly")
	}
	state.metadata = count == len(metadata)
	if err := validateRegistry(db, state.metadata); err != nil {
		return state, err
	}
	query := "SELECT version, '' AS checksum, 'legacy' AS status FROM schema_migrations ORDER BY version"
	if state.metadata {
		query = "SELECT version, COALESCE(checksum,'') AS checksum, status FROM schema_migrations ORDER BY version"
	}
	var records []migrationRecord
	if err := db.Raw(query).Scan(&records).Error; err != nil {
		return state, err
	}
	for _, record := range records {
		if _, duplicate := state.records[record.Version]; duplicate {
			return state, fmt.Errorf("duplicate migration version %s", record.Version)
		}
		state.records[record.Version] = record
	}
	return state, nil
}

func validateState(db *gorm.DB, state schemaState) error {
	if !state.registry {
		if state.tableCount != 0 {
			return fmt.Errorf("unrecognized database: tables exist without schema_migrations; no automatic baseline")
		}
		return nil
	}
	if len(state.records) == 0 {
		if state.tableCount != 1 {
			return fmt.Errorf("unrecognized database: empty ledger with existing tables; no automatic baseline")
		}
		return nil
	}
	known := map[string]migration{}
	for _, m := range migrations() {
		known[m.version] = m
	}
	versions := make([]string, 0, len(state.records))
	for version := range state.records {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	for _, version := range versions {
		record := state.records[version]
		m, recognized := known[version]
		if !recognized {
			return fmt.Errorf("unknown migration %s; this binary cannot serve or migrate a newer/unrecognized schema", version)
		}
		if record.Status == "dirty" {
			return fmt.Errorf("migration %s is dirty; committed MySQL DDL was not rolled back; restore/repair explicitly before retrying", version)
		}
		if record.Status == "legacy" {
			if record.Checksum != "" {
				return fmt.Errorf("migration %s has inconsistent legacy metadata", version)
			}
			for _, base := range []string{"001-workspace", "002-auth", "003-teams"} {
				if _, exists := state.records[base]; !exists {
					return fmt.Errorf("legacy bootstrap incomplete: missing %s; no automatic baseline", base)
				}
			}
		} else if record.Status != "applied" && record.Status != "baseline" {
			return fmt.Errorf("migration %s has unsupported status %q", version, record.Status)
		} else if record.Checksum != m.checksum() {
			return fmt.Errorf("migration %s checksum mismatch: applied SQL has changed; restore the original script or restore/repair the database explicitly", version)
		}
	}
	catalog, err := readCatalog(db)
	if err != nil {
		return err
	}
	missing := ""
	for _, m := range migrations() {
		if _, exists := state.records[m.version]; !exists {
			if missing == "" {
				missing = m.version
			}
			continue
		}
		if missing != "" {
			return fmt.Errorf("migration history is not a fixed prefix: %s is missing before %s", missing, m.version)
		}
		if err := validateRequirements(catalog, m.version); err != nil {
			return fmt.Errorf("migration %s schema verification failed: %w; no automatic baseline or repair", m.version, err)
		}
	}
	return validateWorkspace(db)
}

// An unrecorded partial migration is never silently adopted. This also catches
// historical crashes that occurred before the new dirty bookkeeping existed.
func checkPending(db *gorm.DB, state schemaState) error {
	expected := map[string][]string{
		"004-recent-files":             {"recent_files.*"},
		"005-document-details":         {"documents.updated_by", "documents.updated_by_anonymous"},
		"006-trash":                    {"documents.deleted_at", "documents.deleted_by", "documents.expires_at", "folders.deleted_at", "folders.deleted_by", "folders.expires_at"},
		"007-ai-agent":                 {"ai_models.*", "ai_review_applications.*"},
		"008-ai-conversation-history":  {"ai_conversation_turns.*"},
		"009-ai-conversation-sessions": {"ai_conversation_sessions.*", "ai_conversation_turns.session_id"},
		"010-user-config":              {"users.user_config"},
	}
	for _, m := range migrations() {
		if _, applied := state.records[m.version]; applied {
			continue
		}
		for _, object := range expected[m.version] {
			table, column, _ := strings.Cut(object, ".")
			var count int64
			query := db.Table("information_schema.COLUMNS").Where("TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table)
			if column != "*" {
				query = query.Where("COLUMN_NAME=?", column)
			}
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("unrecorded migration %s already has %s; partial/unknown schema requires explicit inspection, not automatic replay", m.version, object)
			}
		}
	}
	return nil
}
