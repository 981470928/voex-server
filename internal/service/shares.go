package service

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"
	"voex-server/internal/filestore"
	"voex-server/internal/identity"
	"voex-server/internal/repository"
	"voex-server/internal/shared"
)

var shareTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var shareAvatarPattern = regexp.MustCompile(`^/api/assets/([a-f0-9-]{36})$`)
var shareAvatarStoragePattern = regexp.MustCompile(`^[a-f0-9-]{36}\.webp$`)

func (s *Service) sharedDocument(db *gorm.DB, ctx context.Context, token string) (shared.Row, shared.Row) {
	if !shareTokenPattern.MatchString(token) {
		shared.Fail(404, "分享链接无效或已撤销")
	}
	doc := repository.ActiveSharedDocument(db, shared.Digest(token))
	if doc == nil {
		shared.Fail(404, "分享链接无效或已撤销")
	}
	write := shared.String(doc["permission"]) == "edit"
	if identity.OptionalUser(ctx) != nil {
		permissions := s.ProjectPermissions(db, ctx, shared.String(doc["project_key"]))
		write = write || shared.Bool(permissions["write"])
	}
	return doc, shared.Row{"read": true, "write": write, "manage": false, "share": false}
}

func shareRevision(value any) (int64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case json.Number:
		parsed, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 1 || number >= 9223372036854775808 || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

func (s *Service) ListDocumentShares(ctx context.Context, rawKey string) []shared.Row {
	key := shared.Key(rawKey, "文件标识")
	return s.tx(ctx, false, func(db *gorm.DB) any {
		doc := s.FindDocument(db, ctx, key, "share", false)
		permissions := s.ProjectPermissions(db, ctx, shared.String(doc["project_key"]))
		rows := repository.ListDocumentShares(db, doc["id"])
		result := make([]shared.Row, 0, len(rows))
		for _, row := range rows {
			var revoked any
			if row["revoked_at"] != nil {
				revoked = shared.Timestamp(row["revoked_at"])
			}
			result = append(result, shared.Row{"id": row["id"], "permission": row["permission"], "created_at": shared.Timestamp(row["created_at"]), "revoked_at": revoked, "created_by": shared.Row{"id": row["creator_id"], "name": row["name"], "avator": row["avator"]}, "can_revoke": shared.Bool(permissions["manage"]) || shared.String(row["creator_id"]) == shared.String(identity.User(ctx)["id"])})
		}
		return result
	}).([]shared.Row)
}

func (s *Service) CreateDocumentShare(ctx context.Context, rawKey string, input shared.Row) shared.Row {
	permission := input["permission"]
	if permission != "read" && permission != "edit" {
		shared.Fail(400, "分享权限只能为只读或编辑")
	}
	key := shared.Key(rawKey, "文件标识")
	return s.tx(ctx, true, func(db *gorm.DB) any {
		doc := s.FindDocument(db, ctx, key, "share", false)
		id, token := shared.UUID(), shared.RandomToken(32)
		repository.CreateDocumentShare(db, shared.Row{"id": id, "document_id": doc["id"], "token_hash": shared.Digest(token), "permission": permission, "created_by": identity.User(ctx)["id"]})
		return shared.Row{"id": id, "token": token, "permission": permission, "created_at": shared.Timestamp(time.Now().UTC())}
	}).(shared.Row)
}

func (s *Service) RevokeDocumentShare(ctx context.Context, rawKey, id string) {
	key := shared.Key(rawKey, "文件标识")
	s.tx(ctx, true, func(db *gorm.DB) any {
		doc := s.FindDocument(db, ctx, key, "share", false)
		share := repository.DocumentShareByID(db, doc["id"], id)
		if share == nil {
			shared.Fail(404, "分享记录不存在")
		}
		permissions := s.ProjectPermissions(db, ctx, shared.String(doc["project_key"]))
		if !shared.Bool(permissions["manage"]) && shared.String(share["created_by"]) != shared.String(identity.User(ctx)["id"]) {
			shared.Fail(403, "只能撤销自己创建的分享")
		}
		repository.RevokeDocumentShare(db, id)
		return nil
	})
}

func (s *Service) GetSharedDocument(ctx context.Context, token string) shared.Row {
	return s.tx(ctx, false, func(db *gorm.DB) any {
		doc, permissions := s.sharedDocument(db, ctx, token)
		rows := repository.ListSharedAttachments(db, shared.String(doc["file_key"]))
		attachments := make([]shared.Row, 0, len(rows))
		for _, file := range rows {
			attachments = append(attachments, shared.Row{"hash": file["hash"], "name": file["name"], "mime": file["mime"], "creator": shared.Row{"id": file["creator_id"], "name": file["creator_name"], "avator": ""}})
		}
		avator := ""
		if shared.String(doc["creator_avator"]) != "" {
			avator = "/api/shared-file/creator-avator"
		}
		// A capability exposes only this document, never parent or contact data.
		return shared.Row{"file_key": doc["file_key"], "file_name": doc["file_name"], "file_content": shared.String(doc["file_content"]), "created_at": shared.Timestamp(doc["created_at"]), "updated_at": shared.Timestamp(doc["updated_at"]), "revision": shared.Int64(doc["revision"]), "privileges": shared.Row{"mode": "inherit"}, "permissions": permissions, "creator": shared.Row{"id": doc["creator_id"], "name": doc["creator_name"], "avator": avator}, "attachments": attachments}
	}).(shared.Row)
}

func (s *Service) UpdateSharedDocument(ctx context.Context, token string, input shared.Row) shared.Row {
	content, contentOK := input["file_content"].(string)
	version, versionOK := shareRevision(input["revision"])
	if !contentOK || !versionOK || len(input) != 2 {
		shared.Fail(400, "分享编辑仅允许提交正文和版本号")
	}
	revision := s.tx(ctx, true, func(db *gorm.DB) any {
		doc, permissions := s.sharedDocument(db, ctx, token)
		if !shared.Bool(permissions["write"]) {
			shared.Fail(403, "此分享链接只有只读权限")
		}
		if version != shared.Int64(doc["revision"]) {
			shared.Fail(409, "文件已被其他人修改，请保留本地内容后重新加载")
		}
		repository.UpdateSharedContent(db, doc["id"], content)
		return shared.Int64(doc["revision"]) + 1
	})
	return shared.Row{"success": true, "revision": revision}
}

func (s *Service) SharedAttachment(ctx context.Context, token, hash string) filestore.Download {
	if !attachmentHashPattern.MatchString(hash) {
		shared.Fail(404, "附件不存在")
	}
	file := s.tx(ctx, false, func(db *gorm.DB) any {
		doc, _ := s.sharedDocument(db, ctx, token)
		file := repository.AttachmentByHash(db, shared.String(doc["file_key"]), hash)
		if file == nil {
			shared.Fail(404, "附件不存在")
		}
		return file
	}).(shared.Row)
	return filestore.Download{Path: s.Files.AttachmentPath(hash), Name: shared.String(file["name"]), MIME: "application/octet-stream", Cache: "no-store", Missing: "文件不存在"}
}

func (s *Service) SharedAvatar(ctx context.Context, token string) filestore.Download {
	asset := s.tx(ctx, false, func(db *gorm.DB) any {
		doc, _ := s.sharedDocument(db, ctx, token)
		match := shareAvatarPattern.FindStringSubmatch(shared.String(doc["creator_avator"]))
		if match == nil {
			shared.Fail(404, "没有头像")
		}
		asset := repository.SharedCreatorAvatar(db, match[1], doc["creator_id"])
		if asset == nil {
			shared.Fail(404, "没有头像")
		}
		return asset
	}).(shared.Row)
	if !shareAvatarStoragePattern.MatchString(shared.String(asset["storage_name"])) {
		shared.Fail(404, "没有头像")
	}
	return filestore.Download{Path: s.Files.AssetPath("avator", shared.String(asset["storage_name"])), Name: "avator.webp", MIME: "image/webp", Inline: true, Cache: "no-store", Missing: "文件不存在"}
}
