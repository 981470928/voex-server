package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"voex-server/internal/shared"
)

// Upserts may upgrade read to edit, but must never downgrade an existing editor.
func GrantDocumentAccess(db *gorm.DB, documentID any, userID, permission string) {
	Must(db.Table("document_access").Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{
		"permission": gorm.Expr("IF(document_access.permission = 'edit' OR ? = 'edit', 'edit', 'read')", permission),
	})}).Create(map[string]any{
		"document_id": documentID, "user_id": userID, "permission": permission,
	}))
}

func DocumentAccess(db *gorm.DB, documentID any, userID string) string {
	row := One(db.Table("document_access").Select("permission").Where("document_id=? AND user_id=?", documentID, userID))
	return shared.String(row["permission"])
}

// Parent deletion cannot be used to bypass a child's creator-only deletion rule.
func HasOtherCreatorsInFolders(db *gorm.DB, projectID, rootID any, userID string) bool {
	folders := Rows(db.Table("folders").Select("id,parent_id").Where("project_id=?", projectID))
	selected := map[int64]bool{}
	if rootID != nil {
		selected[shared.Int64(rootID)] = true
	}
	for changed := true; changed; {
		changed = false
		for _, folder := range folders {
			id := shared.Int64(folder["id"])
			if !selected[id] && (rootID == nil || selected[shared.Int64(folder["parent_id"])]) {
				selected[id] = true
				changed = true
			}
		}
	}
	ids := make([]int64, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return false
	}
	return One(db.Table("documents").Select("id").Where("folder_id IN ? AND (creator_id IS NULL OR creator_id<>?)", ids, userID)) != nil
}
