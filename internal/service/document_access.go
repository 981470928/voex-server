package service

import (
	"context"
	"gorm.io/gorm"
	"voex-server/internal/identity"
	"voex-server/internal/repository"
	"voex-server/internal/shared"
)

// Editors have the same file operations as the creator except document deletion.
// A file grant never grants access to unrelated documents or project management.
func (s *Service) documentPermissions(db *gorm.DB, ctx context.Context, doc, project shared.Row) shared.Row {
	userID := shared.String(identity.User(ctx)["id"])
	creator := shared.String(doc["creator_id"]) == userID
	grant := ""
	if !creator && !shared.Bool(project["write"]) {
		grant = repository.DocumentAccess(db, doc["id"], userID)
	}
	write := creator || shared.Bool(project["write"]) || grant == "edit"
	return shared.Row{
		"read": write || shared.Bool(project["read"]) || grant == "read", "write": write,
		"manage": write, "share": write, "remove": creator,
	}
}

func (s *Service) DocumentPermissions(db *gorm.DB, ctx context.Context, doc shared.Row) shared.Row {
	return s.documentPermissions(db, ctx, doc, s.ProjectPermissions(db, ctx, shared.String(doc["project_key"])))
}

func (s *Service) requireFolderDocumentDeletion(db *gorm.DB, ctx context.Context, projectID, rootID any) {
	if repository.HasOtherCreatorsInFolders(db, projectID, rootID, shared.String(identity.User(ctx)["id"])) {
		shared.Fail(403, "包含非本人创建的文件，不能删除")
	}
}
