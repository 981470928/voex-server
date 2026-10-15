package service

import (
	"context"
	"voex-server/internal/repository"
	"voex-server/internal/shared"

	"gorm.io/gorm"
)

// RestoreFolder 将未到期的文件夹恢复到原项目。
func (s *Service) RestoreFolder(ctx context.Context, key string) {
	s.tx(ctx, true, func(db *gorm.DB) any {
		folder := repository.TrashFolderByKey(db, key)
		if folder == nil {
			shared.Fail(404, "文件夹不存在")
		}

		s.RequireProject(db, ctx, shared.String(folder["project_key"]), "write")

		if folder["deleted_at"] == nil {
			shared.Fail(409, "文件夹不在回收站中")
		}

		if !repository.TrashRestoreFolder(db, folder["id"]) {
			shared.Fail(409, "文件夹已过保留期限，无法恢复")
		}

		return nil
	})
}

// RestoreDocument 将未到期的文档恢复到原文件夹。
func (s *Service) RestoreDocument(ctx context.Context, key string) {
	s.tx(ctx, true, func(db *gorm.DB) any {
		document := repository.TrashDocumentByKey(db, key)
		if document == nil {
			shared.Fail(404, "文档不存在")
		}

		s.RequireProject(db, ctx, shared.String(document["project_key"]), "write")

		if document["deleted_at"] == nil {
			shared.Fail(409, "文档不在回收站中")
		}

		if document["folder_deleted_at"] != nil {
			shared.Fail(409, "请先恢复原文件夹")
		}

		if !repository.TrashRestoreDocument(db, document["id"]) {
			shared.Fail(409, "文档已过保留期限，无法恢复")
		}

		return nil
	})
}

// ListTrash 查询当前用户有项目写权限的回收站内容。
func (s *Service) ListTrash(
	ctx context.Context,
	teamKey, projectKey string,
	limit, offset int,
) shared.Row {
	if limit < 1 || limit > 200 {
		shared.Fail(400, "limit 必须在 1 到 200 之间")
	}
	if offset < 0 {
		shared.Fail(400, "offset 必须是非负整数")
	}

	return s.tx(ctx, false, func(db *gorm.DB) any {
		team := s.SelectedTeam(db, ctx, teamKey)
		projects := repository.WorkspaceProjects(db, team["id"])
		projectIDs := make([]int64, 0, len(projects))

		for _, project := range projects {
			key := shared.String(project["project_key"])
			if projectKey != "" && key != projectKey {
				continue
			}

			permissions := s.ProjectPermissions(db, ctx, key)
			if shared.Bool(permissions["write"]) {
				projectIDs = append(projectIDs, shared.Int64(project["id"]))
			}
		}

		if projectKey != "" && len(projectIDs) == 0 {
			shared.Fail(404, "项目不存在或没有访问权限")
		}

		rows, total := repository.TrashList(db, projectIDs, limit, offset)
		items := make([]shared.Row, 0, len(rows))

		for _, row := range rows {
			var deletedBy any
			if id := shared.String(row["deleted_by"]); id != "" {
				deletedBy = shared.Row{
					"id":   id,
					"name": shared.String(row["deleted_by_name"]),
				}
			}

			item := shared.Row{
				"type":        row["item_type"],
				"key":         row["resource_key"],
				"name":        row["name"],
				"team_key":    row["team_key"],
				"project_key": row["project_key"],
				"folder_key":  row["folder_key"],
				"deleted_by":  deletedBy,
				"deleted_at":  shared.Timestamp(row["deleted_at"]),
				"expires_at":  shared.Timestamp(row["expires_at"]),
				"can_restore": shared.Bool(row["can_restore"]),
			}

			if shared.String(row["item_type"]) == "folder" {
				item["document_count"] = shared.Int64(row["document_count"])
			}

			items = append(items, item)
		}

		return shared.Row{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		}
	}).(shared.Row)
}

// purgeDocument 彻底删除回收站中的文档
func (s *Service) PurgeDocument(ctx context.Context, key string) {
	s.tx(ctx, true, func(db *gorm.DB) any {
		document := repository.TrashDocumentByKey(db, key)

		if document == nil {
			shared.Fail(404, "文档不存在")
		}
		s.RequireProject(db, ctx, shared.String(document["project_key"]), "write")
		if document["deleted_at"] == nil {
			shared.Fail(409, "文档不在回收站中")
		}
		repository.WorkspaceDeleteDocument(db, document["id"], shared.String(document["file_key"]))
		return nil
	})
}

// PurgeFolder 彻底删除回收站中 的文件夹及其全部文档

func (s *Service) PurgeFolder(ctx context.Context, key string) {
	s.tx(ctx, true, func(db *gorm.DB) any {
		folder := repository.TrashFolderByKey(db, key)
		if folder == nil {
			shared.Fail(404, "文件夹不存在")
		}
		s.RequireProject(db, ctx, shared.String(folder["project_key"]), "write")
		if folder["deleted_at"] == nil {
			shared.Fail(409, "文件夹不在回收站中")
		}

		repository.WorkspaceDeleteFolder(db, folder["project_id"], folder["id"])
		return nil
	})
}
