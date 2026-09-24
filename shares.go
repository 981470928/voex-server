package main

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var shareTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var shareAvatarPattern = regexp.MustCompile(`^/api/assets/([a-f0-9-]{36})$`)
var shareAvatarStoragePattern = regexp.MustCompile(`^[a-f0-9-]{36}\.webp$`)

func (s *Server) shared(c DBTX, r *http.Request) (Row, Row) {
	token := r.Header.Get("X-Share-Token")
	if !shareTokenPattern.MatchString(token) {
		fail(404, "分享链接无效或已撤销")
	}
	doc := one(c, "SELECT "+documentColumns+",d.file_content,s.permission "+documentFrom+" JOIN file_shares s ON s.document_id=d.id WHERE s.token_hash=? AND s.revoked_at IS NULL", digest(token))
	if doc == nil {
		fail(404, "分享链接无效或已撤销")
	}
	write := str(doc["permission"]) == "edit"
	if optionalUser(r) != nil {
		permissions := s.projectPermissions(c, r, str(doc["project_key"]))
		write = write || flag(permissions["write"])
	}
	return doc, Row{"read": true, "write": write, "manage": false, "share": false}
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

func (s *Server) shareRoute(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.route(pattern, "optional", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		s.limit(r, "shared-file", 180, time.Minute)
		handler(w, r)
	})
}

func (s *Server) registerShareRoutes() {
	s.route("GET /api/document/{key}/shares", "required", func(w http.ResponseWriter, r *http.Request) {
		shares := s.tx(false, func(c *sql.Tx) any {
			doc := s.findDocument(c, r, keyValue(r.PathValue("key"), "文件标识"), "share", false)
			permissions := s.projectPermissions(c, r, str(doc["project_key"]))
			rows := query(c, "SELECT s.id,s.permission,s.created_at,s.revoked_at,u.id AS creator_id,u.name,u.avator FROM file_shares s JOIN users u ON u.id=s.created_by WHERE s.document_id=? ORDER BY s.created_at DESC,s.id", doc["id"])
			result := make([]Row, 0, len(rows))
			for _, row := range rows {
				var revoked any
				if row["revoked_at"] != nil {
					revoked = timestamp(row["revoked_at"])
				}
				result = append(result, Row{"id": row["id"], "permission": row["permission"], "created_at": timestamp(row["created_at"]), "revoked_at": revoked, "created_by": Row{"id": row["creator_id"], "name": row["name"], "avator": row["avator"]}, "can_revoke": flag(permissions["manage"]) || str(row["creator_id"]) == str(currentUser(r)["id"])})
			}
			return result
		})
		jsonResponse(w, 200, shares)
	})
	s.route("POST /api/document/{key}/shares", "required", func(w http.ResponseWriter, r *http.Request) {
		permission := body(r)["permission"]
		if permission != "read" && permission != "edit" {
			fail(400, "分享权限只能为只读或编辑")
		}
		share := s.tx(true, func(c *sql.Tx) any {
			doc := s.findDocument(c, r, keyValue(r.PathValue("key"), "文件标识"), "share", false)
			id, token := uuid(), randomToken(32)
			execSQL(c, "INSERT INTO file_shares (id,document_id,token_hash,permission,created_by) VALUES (?,?,?,?,?)", id, doc["id"], digest(token), permission, currentUser(r)["id"])
			return Row{"id": id, "token": token, "permission": permission, "created_at": timestamp(time.Now().UTC())}
		})
		jsonResponse(w, 201, share)
	})
	s.route("DELETE /api/document/{key}/shares/{id}", "required", func(w http.ResponseWriter, r *http.Request) {
		s.tx(true, func(c *sql.Tx) any {
			doc := s.findDocument(c, r, keyValue(r.PathValue("key"), "文件标识"), "share", false)
			share := one(c, "SELECT created_by FROM file_shares WHERE id=? AND document_id=?", r.PathValue("id"), doc["id"])
			if share == nil {
				fail(404, "分享记录不存在")
			}
			permissions := s.projectPermissions(c, r, str(doc["project_key"]))
			if !flag(permissions["manage"]) && str(share["created_by"]) != str(currentUser(r)["id"]) {
				fail(403, "只能撤销自己创建的分享")
			}
			execSQL(c, "UPDATE file_shares SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP()) WHERE id=?", r.PathValue("id"))
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.shareRoute("GET /api/shared-file", func(w http.ResponseWriter, r *http.Request) {
		result := s.tx(false, func(c *sql.Tx) any {
			doc, permissions := s.shared(c, r)
			rows := query(c, "SELECT f.hash,f.name,f.mime,u.id AS creator_id,u.name AS creator_name FROM files f LEFT JOIN users u ON u.id=f.creator_id WHERE f.file_key=? ORDER BY f.id", doc["file_key"])
			attachments := make([]Row, 0, len(rows))
			for _, file := range rows {
				attachments = append(attachments, Row{"hash": file["hash"], "name": file["name"], "mime": file["mime"], "creator": Row{"id": file["creator_id"], "name": file["creator_name"], "avator": ""}})
			}
			avator := ""
			if str(doc["creator_avator"]) != "" {
				avator = "/api/shared-file/creator-avator"
			}
			// Public capabilities only expose this document and its attachments, never
			// parent team/project/folder identifiers or members' contact information.
			return Row{"file_key": doc["file_key"], "file_name": doc["file_name"], "file_content": str(doc["file_content"]), "created_at": timestamp(doc["created_at"]), "updated_at": timestamp(doc["updated_at"]), "revision": num(doc["revision"]), "privileges": Row{"mode": "inherit"}, "permissions": permissions, "creator": Row{"id": doc["creator_id"], "name": doc["creator_name"], "avator": avator}, "attachments": attachments}
		})
		jsonResponse(w, 200, result)
	})
	s.shareRoute("PUT /api/shared-file", func(w http.ResponseWriter, r *http.Request) {
		input := body(r)
		content, contentOK := input["file_content"].(string)
		version, versionOK := shareRevision(input["revision"])
		if !contentOK || !versionOK || len(input) != 2 {
			fail(400, "分享编辑仅允许提交正文和版本号")
		}
		revision := s.tx(true, func(c *sql.Tx) any {
			doc, permissions := s.shared(c, r)
			if !flag(permissions["write"]) {
				fail(403, "此分享链接只有只读权限")
			}
			if version != num(doc["revision"]) {
				fail(409, "文件已被其他人修改，请保留本地内容后重新加载")
			}
			execSQL(c, "UPDATE documents SET file_content=?,revision=revision+1 WHERE id=?", content, doc["id"])
			return num(doc["revision"]) + 1
		})
		jsonResponse(w, 200, Row{"success": true, "revision": revision})
	})
	s.shareRoute("GET /api/shared-file/attachments/{hash}", func(w http.ResponseWriter, r *http.Request) {
		hash := r.PathValue("hash")
		if !attachmentHashPattern.MatchString(hash) {
			fail(404, "附件不存在")
		}
		file := s.tx(false, func(c *sql.Tx) any {
			doc, _ := s.shared(c, r)
			file := one(c, "SELECT name,mime FROM files WHERE file_key=? AND hash=?", doc["file_key"], hash)
			if file == nil {
				fail(404, "附件不存在")
			}
			return file
		}).(Row)
		storageFile(w, r, filepath.Join(staticDir, hash), str(file["name"]), "application/octet-stream", false, "no-store", "文件不存在")
	})
	s.shareRoute("GET /api/shared-file/creator-avator", func(w http.ResponseWriter, r *http.Request) {
		asset := s.tx(false, func(c *sql.Tx) any {
			doc, _ := s.shared(c, r)
			match := shareAvatarPattern.FindStringSubmatch(str(doc["creator_avator"]))
			if match == nil {
				fail(404, "没有头像")
			}
			asset := one(c, "SELECT * FROM assets WHERE id=? AND creator_id=? AND directory='avator' AND mime='image/webp'", match[1], doc["creator_id"])
			if asset == nil {
				fail(404, "没有头像")
			}
			return asset
		}).(Row)
		if !shareAvatarStoragePattern.MatchString(str(asset["storage_name"])) {
			fail(404, "没有头像")
		}
		storageFile(w, r, filepath.Join(storageDirectory("avator"), str(asset["storage_name"])), "avator.webp", "image/webp", true, "no-store", "文件不存在")
	})
}
