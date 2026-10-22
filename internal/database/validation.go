package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type columnInfo struct{ TableName, ColumnName, ColumnType, IsNullable, CollationName string }
type indexInfo struct {
	TableName, IndexName, ColumnList string
	NonUnique                        int
}
type foreignKeyInfo struct{ TableName, ConstraintName, ColumnList, ReferencedTableName, ReferencedList, DeleteRule, UpdateRule string }
type tableInfo struct{ TableName, TableCollation, Engine string }
type checkInfo struct{ TableName, ConstraintName, Enforced string }
type schemaCatalog struct {
	columns     map[string]columnInfo
	indexes     map[string]indexInfo
	foreignKeys map[string]foreignKeyInfo
	tables      map[string]tableInfo
	checks      map[string]checkInfo
}

func readCatalog(db *gorm.DB) (schemaCatalog, error) {
	catalog := schemaCatalog{columns: map[string]columnInfo{}, indexes: map[string]indexInfo{},
		foreignKeys: map[string]foreignKeyInfo{}, tables: map[string]tableInfo{}, checks: map[string]checkInfo{}}
	var columns []columnInfo
	if err := db.Raw(`SELECT TABLE_NAME,COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLLATION_NAME,'') AS collation_name
        FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()`).Scan(&columns).Error; err != nil {
		return catalog, err
	}
	for _, column := range columns {
		catalog.columns[column.TableName+"."+column.ColumnName] = column
	}
	var indexes []indexInfo
	if err := db.Raw(`SELECT TABLE_NAME,INDEX_NAME,NON_UNIQUE,
        GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',') AS column_list
        FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE()
        GROUP BY TABLE_NAME,INDEX_NAME,NON_UNIQUE`).Scan(&indexes).Error; err != nil {
		return catalog, err
	}
	for _, index := range indexes {
		catalog.indexes[index.TableName+"."+index.IndexName] = index
	}
	var keys []foreignKeyInfo
	if err := db.Raw(`SELECT k.TABLE_NAME,k.CONSTRAINT_NAME,k.REFERENCED_TABLE_NAME,r.DELETE_RULE,r.UPDATE_RULE,
        GROUP_CONCAT(k.COLUMN_NAME ORDER BY k.ORDINAL_POSITION SEPARATOR ',') AS column_list,
        GROUP_CONCAT(k.REFERENCED_COLUMN_NAME ORDER BY k.ORDINAL_POSITION SEPARATOR ',') AS referenced_list
        FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r
          ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.TABLE_NAME=k.TABLE_NAME AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME
        WHERE k.CONSTRAINT_SCHEMA=DATABASE() AND k.REFERENCED_TABLE_NAME IS NOT NULL AND k.REFERENCED_TABLE_SCHEMA=DATABASE()
        GROUP BY k.TABLE_NAME,k.CONSTRAINT_NAME,k.REFERENCED_TABLE_NAME,r.DELETE_RULE,r.UPDATE_RULE`).Scan(&keys).Error; err != nil {
		return catalog, err
	}
	for _, key := range keys {
		catalog.foreignKeys[key.TableName+"."+key.ConstraintName] = key
	}
	var tables []tableInfo
	if err := db.Raw(`SELECT TABLE_NAME,TABLE_COLLATION,ENGINE FROM information_schema.TABLES
        WHERE TABLE_SCHEMA=DATABASE()`).Scan(&tables).Error; err != nil {
		return catalog, err
	}
	for _, table := range tables {
		catalog.tables[table.TableName] = table
	}
	var checks []checkInfo
	if err := db.Raw(`SELECT TABLE_NAME,CONSTRAINT_NAME,ENFORCED FROM information_schema.TABLE_CONSTRAINTS
        WHERE CONSTRAINT_SCHEMA=DATABASE() AND CONSTRAINT_TYPE='CHECK'`).Scan(&checks).Error; err != nil {
		return catalog, err
	}
	for _, check := range checks {
		catalog.checks[check.TableName+"."+check.ConstraintName] = check
	}
	return catalog, nil
}

func normalizeRule(rule string) string {
	if rule == "NO ACTION" {
		return "RESTRICT"
	}
	return rule
}

func validateRequirements(catalog schemaCatalog, version string) error {
	requirements := requirementsFor(version)
	if requirements == nil {
		return fmt.Errorf("no structural requirements for migration %s", version)
	}
	for _, required := range requirements {
		table, exists := catalog.tables[required.name]
		if !exists || table.Engine != "InnoDB" {
			return fmt.Errorf("required InnoDB table %s is missing or incompatible", required.name)
		}
		if required.collation != "" && table.TableCollation != required.collation {
			return fmt.Errorf("table %s collation is %s, expected %s", required.name, table.TableCollation, required.collation)
		}
		for _, column := range required.columns {
			actual, exists := catalog.columns[required.name+"."+column.name]
			if !exists || strings.ToLower(actual.ColumnType) != column.typ || (actual.IsNullable == "YES") != column.nullable ||
				(column.collation != "" && actual.CollationName != column.collation) {
				return fmt.Errorf("column %s.%s is missing or incompatible (expected %s nullable=%t collation=%s)", required.name, column.name, column.typ, column.nullable, column.collation)
			}
		}
		for _, index := range required.indexes {
			actual, exists := catalog.indexes[required.name+"."+index.name]
			if !exists || actual.ColumnList != index.columns || (actual.NonUnique == 0) != index.unique {
				return fmt.Errorf("index %s.%s is missing or incompatible", required.name, index.name)
			}
		}
		for _, key := range required.foreignKeys {
			actual, exists := catalog.foreignKeys[required.name+"."+key.name]
			if !exists || actual.ColumnList != key.columns || actual.ReferencedTableName != key.table || actual.ReferencedList != key.referenced ||
				normalizeRule(actual.DeleteRule) != key.deleteRule || normalizeRule(actual.UpdateRule) != key.updateRule {
				return fmt.Errorf("foreign key %s.%s is missing or incompatible", required.name, key.name)
			}
		}
		for _, name := range required.checks {
			if check, exists := catalog.checks[required.name+"."+name]; !exists || check.Enforced != "YES" {
				return fmt.Errorf("enforced check %s.%s is missing", required.name, name)
			}
		}
	}
	return nil
}

func validateWorkspace(db *gorm.DB) error {
	var count int64
	if err := db.Table("workspace_state").Where("id=1").Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("workspace lock row is missing")
	}
	return nil
}

func validateMigration(db *gorm.DB, version string) error {
	catalog, err := readCatalog(db)
	if err != nil {
		return err
	}
	if err := validateRequirements(catalog, version); err != nil {
		return err
	}
	if version == "001-workspace" || version == "002-auth" || version == "003-teams" {
		return validateWorkspace(db)
	}
	return nil
}

// Validate performs only SELECT statements, including checksum/dirty checks.
func Validate(db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := inspect(db.WithContext(ctx))
	if err != nil {
		return err
	}
	if !status.Compatible {
		return fmt.Errorf("schema incompatible: %s", status.Error)
	}
	return nil
}

func validateRegistry(db *gorm.DB, metadata bool) error {
	required := []columnRequirement{{"version", "varchar(64)", false, ""}, {"applied_at", "datetime", false, ""}}
	if metadata {
		required = append(required,
			columnRequirement{"checksum", "char(64)", true, "ascii_bin"},
			columnRequirement{"status", "varchar(16)", false, ""},
			columnRequirement{"started_at", "datetime(6)", true, ""},
			columnRequirement{"finished_at", "datetime(6)", true, ""},
			columnRequirement{"last_error", "text", true, ""})
	}
	var columns []columnInfo
	if err := db.Raw(`SELECT TABLE_NAME,COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLLATION_NAME,'') AS collation_name
        FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations'`).Scan(&columns).Error; err != nil {
		return err
	}
	present := map[string]columnInfo{}
	for _, column := range columns {
		present[column.ColumnName] = column
	}
	for _, column := range required {
		actual, exists := present[column.name]
		if !exists || strings.ToLower(actual.ColumnType) != column.typ || (actual.IsNullable == "YES") != column.nullable ||
			(column.collation != "" && actual.CollationName != column.collation) {
			return fmt.Errorf("unrecognized schema_migrations column %s", column.name)
		}
	}
	var primary string
	if err := db.Raw(`SELECT COALESCE(GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ','),'')
        FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations' AND INDEX_NAME='PRIMARY'`).Scan(&primary).Error; err != nil {
		return err
	}
	if primary != "version" {
		return fmt.Errorf("unrecognized schema_migrations primary key")
	}
	var engine string
	if err := db.Raw(`SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations'`).Scan(&engine).Error; err != nil {
		return err
	}
	if engine != "InnoDB" {
		return fmt.Errorf("schema_migrations must use InnoDB")
	}
	return nil
}
