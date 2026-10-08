package repository

import (
	"voex-server/internal/shared"

	"gorm.io/gorm"
)

const DocumentColumns = `d.id,d.file_key,d.file_name,f.project_id,p.project_key,f.folder_key,
 d.created_at,d.updated_at,d.creator_id,d.revision,t.team_key,u.name AS creator_name,u.avator AS creator_avator`
const DocumentFrom = `FROM documents d JOIN folders f ON f.id=d.folder_id
 JOIN projects p ON p.id=f.project_id JOIN teams t ON t.id=p.team_id LEFT JOIN users u ON u.id=d.creator_id`

// DocumentQuery centralizes document joins for workspace and sharing repositories.
func DocumentQuery(db *gorm.DB, content bool) *gorm.DB {
	columns := DocumentColumns
	if content {
		columns += ",d.file_content"
	}
	return db.Table("documents AS d").Select(columns).
		Joins("JOIN folders f ON f.id=d.folder_id").
		Joins("JOIN projects p ON p.id=f.project_id").
		Joins("JOIN teams t ON t.id=p.team_id").
		Joins("LEFT JOIN users u ON u.id=d.creator_id")
}

func DocumentByKey(db *gorm.DB, key string, content bool) shared.Row {
	return One(DocumentQuery(db, content).Where("d.file_key=?", key))
}

func WorkspaceProjectPermission(db *gorm.DB, userID, key string) shared.Row {
	return One(db.Table("projects AS p").Select(`t.owner_id,m.user_id,m.role,p.privileges,
 EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=?) AS explicit_member`, userID).
		Joins("JOIN teams t ON t.id=p.team_id").
		Joins("LEFT JOIN team_members m ON m.team_id=t.id AND m.user_id=?", userID).
		Where("p.project_key=?", key))
}

func workspaceProjectQuery(db *gorm.DB) *gorm.DB {
	return db.Table("projects AS p").Select("p.*,t.team_key").Joins("JOIN teams t ON t.id=p.team_id")
}

func WorkspaceProjectByKey(db *gorm.DB, key string) shared.Row {
	return One(workspaceProjectQuery(db).Where("p.project_key=?", key))
}

func WorkspaceProjectByID(db *gorm.DB, id any) shared.Row {
	return One(workspaceProjectQuery(db).Where("p.id=?", id))
}

func WorkspaceProjects(db *gorm.DB, teamID any) []shared.Row {
	return Rows(workspaceProjectQuery(db).Where("p.team_id=?", teamID).Order("p.created_at,p.id"))
}

func WorkspaceFolderByKey(db *gorm.DB, key string) shared.Row {
	return One(db.Table("folders").Where("folder_key=?", key))
}

func WorkspaceFolders(db *gorm.DB, projectID any) []shared.Row {
	return Rows(db.Table("folders").Where("project_id=?", projectID).Order("created_at,id"))
}

func WorkspaceFirstRoot(db *gorm.DB, projectID any) shared.Row {
	return One(db.Table("folders").Where("project_id=? AND parent_id IS NULL", projectID).Order("created_at,id"))
}

func WorkspaceInsertProject(db *gorm.DB, key, name, creatorID string, teamID any) shared.Row {
	Must(db.Table("projects").Create(map[string]any{
		"project_key": key, "name": name, "creator_id": creatorID,
		"team_id": teamID, "privileges": `{"mode":"inherit"}`,
	}))
	return WorkspaceProjectByKey(db, key)
}

func WorkspaceInsertFolder(db *gorm.DB, key string, projectID any, name string) shared.Row {
	Must(db.Table("folders").Create(map[string]any{
		"folder_key": key, "project_id": projectID, "parent_id": nil,
		"name": name, "privileges": `{"mode":"inherit"}`,
	}))
	return WorkspaceFolderByKey(db, key)
}

func WorkspaceRenameProject(db *gorm.DB, id any, name string) {
	Must(db.Table("projects").Where("id=?", id).Update("name", name))
}

func WorkspaceProjectHasFolders(db *gorm.DB, id any) bool {
	return One(db.Table("folders").Select("id").Where("project_id=?", id)) != nil
}

func WorkspaceDeleteProject(db *gorm.DB, id any) {
	Must(db.Table("projects").Where("id=?", id).Delete(&map[string]any{}))
}

func WorkspaceRenameFolder(db *gorm.DB, id any, name string) {
	Must(db.Table("folders").Where("id=?", id).Update("name", name))
}

func WorkspaceFolderHasDocuments(db *gorm.DB, id any) bool {
	return One(db.Table("documents").Select("id").Where("folder_id=?", id)) != nil
}

func WorkspaceDeleteFolder(db *gorm.DB, id any) {
	Must(db.Table("folders").Where("id=?", id).Delete(&map[string]any{}))
}

func WorkspaceProjectDocuments(db *gorm.DB, projectID any) []shared.Row {
	return Rows(DocumentQuery(db, false).Where("f.project_id=?", projectID).Order("d.created_at,d.id"))
}

func WorkspaceDocuments(db *gorm.DB, userID, code string) []shared.Row {
	query := DocumentQuery(db, false).Where("EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id=p.team_id AND tm.user_id=?)", userID)
	if code != "" {
		query = query.Where("d.file_name LIKE ?", "%"+code+"%")
	}
	return Rows(query.Order("d.created_at,d.id"))
}

func WorkspaceProjectMembers(db *gorm.DB, projectID any) []shared.Row {
	return Rows(db.Table("project_members").Select("user_id").Where("project_id=?", projectID).Order("user_id"))
}

func WorkspaceTeamMembers(db *gorm.DB, teamID any) []shared.Row {
	return Rows(db.Table("team_members").Select("user_id").Where("team_id=?", teamID))
}

func WorkspaceSetProjectPrivileges(db *gorm.DB, projectID any, encoded string, members []string) {
	Must(db.Table("projects").Where("id=?", projectID).Update("privileges", encoded))
	Must(db.Table("project_members").Where("project_id=?", projectID).Delete(&map[string]any{}))
	for _, id := range members {
		Must(db.Table("project_members").Create(map[string]any{"project_id": projectID, "user_id": id}))
	}
}

func WorkspaceInsertDocument(db *gorm.DB, key, fileName string, folderID any, creatorID string) {
	Must(db.Table("documents").Create(map[string]any{
		"file_key": key, "file_name": fileName, "folder_id": folderID,
		"creator_id": creatorID, "privileges": `{"mode":"inherit"}`,
	}))
}

func WorkspaceUpdateDocument(db *gorm.DB, key string, changes shared.Row, incrementRevision bool) {
	values := map[string]any{}
	for key, value := range changes {
		values[key] = value
	}
	if incrementRevision {
		values["revision"] = gorm.Expr("revision + 1")
	}
	Must(db.Table("documents").Where("file_key=?", key).Updates(values))
}

func WorkspaceDeleteDocument(db *gorm.DB, id any, key string) {
	Must(db.Table("files").Where("file_key=?", key).Delete(&map[string]any{}))
	Must(db.Table("documents").Where("id=?", id).Delete(&map[string]any{}))
}
