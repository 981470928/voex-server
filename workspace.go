package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const documentColumns = `d.id,d.file_key,d.file_name,f.project_id,p.project_key,f.folder_key,
 d.created_at,d.updated_at,d.creator_id,d.revision,t.team_key,u.name AS creator_name,u.avator AS creator_avator`
const documentFrom = `FROM documents d JOIN folders f ON f.id=d.folder_id
 JOIN projects p ON p.id=f.project_id JOIN teams t ON t.id=p.team_id LEFT JOIN users u ON u.id=d.creator_id`

func (s *Server) projectPermissions(c DBTX, r *http.Request, key string) Row {
	u := currentUser(r)
	row := one(c, `SELECT t.owner_id,m.user_id,m.role,p.privileges,
 EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=?) AS explicit_member
 FROM projects p JOIN teams t ON t.id=p.team_id
 LEFT JOIN team_members m ON m.team_id=t.id AND m.user_id=? WHERE p.project_key=?`, u["id"], u["id"], key)
	manager := row != nil && str(row["user_id"]) != "" && (str(row["owner_id"]) == str(u["id"]) || str(row["role"]) == "admin")
	edit := row != nil && str(row["user_id"]) != "" && (manager || str(policy(row["privileges"])["mode"]) == "inherit" || flag(row["explicit_member"]))
	return Row{"read": edit, "write": edit, "manage": manager, "share": edit}
}

func (s *Server) requireProject(c DBTX, r *http.Request, key, action string) Row {
	permissions := s.projectPermissions(c, r, key)
	if !flag(permissions[action]) {
		fail(404, "项目不存在或没有访问权限")
	}
	return permissions
}

func workspaceFindProject(c DBTX, key string) Row {
	row := one(c, "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.project_key=?", key)
	if row == nil {
		fail(404, "项目不存在")
	}
	return row
}

func workspaceFindFolder(c DBTX, key string) Row {
	row := one(c, "SELECT * FROM folders WHERE folder_key=?", key)
	if row == nil {
		fail(404, "文件夹不存在")
	}
	return row
}

func workspaceFolderProject(c DBTX, folder Row) Row {
	row := one(c, "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.id=?", folder["project_id"])
	if row == nil {
		fail(404, "项目不存在")
	}
	return row
}

func workspaceInsertProject(c DBTX, name, creatorID string, teamID any) Row {
	key := publicKey()
	execSQL(c, "INSERT INTO projects (project_key,name,creator_id,team_id,privileges) VALUES (?,?,?,?,JSON_OBJECT('mode','inherit'))", key, name, creatorID, teamID)
	return workspaceFindProject(c, key)
}

func workspaceInsertFolder(c DBTX, projectID any, name string) Row {
	key := publicKey()
	execSQL(c, "INSERT INTO folders (folder_key,project_id,parent_id,name,privileges) VALUES (?,?,NULL,?,JSON_OBJECT('mode','inherit'))", key, projectID, name)
	return workspaceFindFolder(c, key)
}

func workspaceEnsureRoot(c DBTX, projectID any) Row {
	row := one(c, `SELECT id,folder_key,project_id,parent_id,name,created_at,updated_at
 FROM folders WHERE project_id=? AND parent_id IS NULL ORDER BY created_at,id LIMIT 1`, projectID)
	if row != nil {
		return row
	}
	return workspaceInsertFolder(c, projectID, "默认文件夹")
}

func projectDTO(row, permissions Row) Row {
	return Row{
		"project_key": row["project_key"], "team_key": row["team_key"], "privileges": policy(row["privileges"]),
		"name": row["name"], "created_at": timestamp(row["created_at"]), "updated_at": timestamp(row["updated_at"]), "permissions": permissions,
	}
}

func folderDTO(row Row, projectKey string) Row {
	return Row{
		"folder_key": row["folder_key"], "privileges": Row{"mode": "inherit"}, "project_key": projectKey,
		"parent_key": nil, "name": row["name"], "created_at": timestamp(row["created_at"]), "updated_at": timestamp(row["updated_at"]),
	}
}

func documentDTO(row Row) Row {
	return Row{
		"revision": num(row["revision"]), "privileges": Row{"mode": "inherit"},
		"creator": Row{"id": row["creator_id"], "name": row["creator_name"], "avator": row["creator_avator"]},
		"id":      row["id"], "file_key": row["file_key"], "file_name": row["file_name"],
		"project_key": row["project_key"], "folder_key": row["folder_key"],
		"created_at": timestamp(row["created_at"]), "updated_at": timestamp(row["updated_at"]),
	}
}

func (s *Server) workspaceListProjects(c DBTX, r *http.Request, teamKey string) []Row {
	team := s.selectedTeam(c, r, teamKey)
	rows := query(c, "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.team_id=? ORDER BY p.created_at,p.id", team["id"])
	result := make([]Row, 0, len(rows))
	for _, row := range rows {
		permissions := s.projectPermissions(c, r, str(row["project_key"]))
		if flag(permissions["read"]) {
			result = append(result, projectDTO(row, permissions))
		}
	}
	return result
}

func (s *Server) workspaceDocumentLocation(c DBTX, r *http.Request, projectKey, folderKey, teamKey string) (Row, Row) {
	var project, folder Row
	if folderKey != "" {
		folder = workspaceFindFolder(c, folderKey)
		project = workspaceFolderProject(c, folder)
		if projectKey != "" && projectKey != str(project["project_key"]) {
			fail(400, "文件夹不属于指定项目")
		}
		s.requireProject(c, r, str(project["project_key"]), "write")
	} else {
		if projectKey != "" {
			project = workspaceFindProject(c, projectKey)
		} else {
			team := s.selectedTeam(c, r, teamKey)
			visible := s.workspaceListProjects(c, r, str(team["team_key"]))
			if len(visible) != 0 {
				project = workspaceFindProject(c, str(visible[0]["project_key"]))
			} else {
				project = workspaceInsertProject(c, "默认项目", str(currentUser(r)["id"]), team["id"])
			}
		}
		s.requireProject(c, r, str(project["project_key"]), "write")
		folder = workspaceEnsureRoot(c, project["id"])
	}
	if teamKey != "" && teamKey != str(project["team_key"]) {
		fail(400, "项目不属于指定团队")
	}
	return project, folder
}

func (s *Server) findDocument(c DBTX, r *http.Request, key, action string, content bool) Row {
	columns := documentColumns
	if content {
		columns += ",d.file_content"
	}
	row := one(c, "SELECT "+columns+" "+documentFrom+" WHERE d.file_key=?", key)
	if row == nil {
		fail(404, "文档不存在")
	}
	s.requireProject(c, r, str(row["project_key"]), action)
	return row
}

func workspaceOptionalKey(b Row, field, label string) string {
	value, present := b[field]
	if !present {
		return ""
	}
	return keyValue(value, label)
}

func workspaceQueryValue(r *http.Request, field string) (any, bool) {
	values := r.URL.Query()
	if entries, present := values[field]; present {
		if len(entries) != 1 {
			return entries, true
		}
		return entries[0], true
	}
	for key := range values {
		if strings.HasPrefix(key, field+"[") {
			return []any{}, true
		}
	}
	return nil, false
}

// Initialization historically accepts an absent body or a JSON array and only
// reads team_key from JSON objects; other workspace writes require an object.
func workspaceInitializeTeamKey(r *http.Request) string {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return ""
	}
	if r.Body == nil || r.Body == http.NoBody {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	must(err)
	if len(data) > 2*1024*1024 {
		fail(413, "请求内容超过大小限制")
	}
	if len(data) == 0 {
		return ""
	}
	if !utf8.Valid(data) {
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	switch parsed := value.(type) {
	case map[string]any:
		return workspaceOptionalKey(Row(parsed), "team_key", "团队标识")
	case []any:
		return ""
	default:
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	return ""
}

func workspaceRevision(value any) (float64, bool) {
	var number float64
	switch v := value.(type) {
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case float64:
		number = v
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) {
		return 0, false
	}
	return number, true
}

func (s *Server) registerWorkspaceRoutes() {
	s.route("GET /api/projects", "required", func(w http.ResponseWriter, r *http.Request) {
		teamKey := ""
		if value, present := workspaceQueryValue(r, "team_key"); present {
			teamKey = keyValue(value, "团队标识")
		}
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any { return s.workspaceListProjects(c, r, teamKey) }))
	})
	s.route("POST /api/workspace/initialize", "required", func(w http.ResponseWriter, r *http.Request) {
		teamKey := workspaceInitializeTeamKey(r)
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any { return s.workspaceListProjects(c, r, teamKey) }))
	})
	s.route("POST /api/project", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		name := nameValue(b["name"], "项目名称")
		teamKey := workspaceOptionalKey(b, "team_key", "团队标识")
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			team := s.selectedTeam(c, r, teamKey)
			project := workspaceInsertProject(c, name, str(currentUser(r)["id"]), team["id"])
			workspaceEnsureRoot(c, project["id"])
			return projectDTO(project, s.requireProject(c, r, str(project["project_key"]), "write"))
		}))
	})
	s.route("PUT /api/project/{projectKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("projectKey"), "项目标识")
		name := nameValue(body(r)["name"], "项目名称")
		s.tx(true, func(c *sql.Tx) any {
			project := workspaceFindProject(c, key)
			s.requireProject(c, r, key, "write")
			execSQL(c, "UPDATE projects SET name=? WHERE id=?", name, project["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("DELETE /api/project/{projectKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("projectKey"), "项目标识")
		s.tx(true, func(c *sql.Tx) any {
			project := workspaceFindProject(c, key)
			s.requireProject(c, r, key, "write")
			if one(c, "SELECT id FROM folders WHERE project_id=? LIMIT 1", project["id"]) != nil {
				fail(409, "项目中仍有文件夹，请先清空")
			}
			execSQL(c, "DELETE FROM projects WHERE id=?", project["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/project/{projectKey}/tree", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("projectKey"), "项目标识")
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			project := workspaceFindProject(c, key)
			permissions := s.requireProject(c, r, key, "read")
			folders := query(c, "SELECT * FROM folders WHERE project_id=? ORDER BY created_at,id", project["id"])
			documents := query(c, "SELECT "+documentColumns+" "+documentFrom+" WHERE f.project_id=? ORDER BY d.created_at,d.id", project["id"])
			folderList, documentList := make([]Row, 0, len(folders)), make([]Row, 0, len(documents))
			for _, folder := range folders {
				folderList = append(folderList, folderDTO(folder, key))
			}
			for _, document := range documents {
				documentList = append(documentList, documentDTO(document))
			}
			return Row{"project": projectDTO(project, permissions), "folders": folderList, "documents": documentList}
		}))
	})
	s.route("POST /api/folder", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		projectKey := keyValue(b["project_key"], "项目标识")
		parentKey := ""
		if b["parent_key"] != nil {
			parentKey = keyValue(b["parent_key"], "父文件夹标识")
		}
		name := nameValue(b["name"], "文件夹名称")
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			project := workspaceFindProject(c, projectKey)
			s.requireProject(c, r, projectKey, "write")
			if parentKey != "" {
				fail(400, "项目仅允许一层文件夹，文件夹下只能创建文件")
			}
			return folderDTO(workspaceInsertFolder(c, project["id"], name), projectKey)
		}))
	})
	s.route("PUT /api/folder/{folderKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("folderKey"), "文件夹标识")
		name := nameValue(body(r)["name"], "文件夹名称")
		s.tx(true, func(c *sql.Tx) any {
			folder := workspaceFindFolder(c, key)
			project := workspaceFolderProject(c, folder)
			s.requireProject(c, r, str(project["project_key"]), "write")
			execSQL(c, "UPDATE folders SET name=? WHERE id=?", name, folder["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("DELETE /api/folder/{folderKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("folderKey"), "文件夹标识")
		s.tx(true, func(c *sql.Tx) any {
			folder := workspaceFindFolder(c, key)
			project := workspaceFolderProject(c, folder)
			s.requireProject(c, r, str(project["project_key"]), "write")
			if one(c, "SELECT id FROM documents WHERE folder_id=? LIMIT 1", folder["id"]) != nil {
				fail(409, "文件夹中仍有文件，请先清空")
			}
			execSQL(c, "DELETE FROM folders WHERE id=?", folder["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/project/{projectKey}/privileges", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("projectKey"), "项目标识")
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			project := workspaceFindProject(c, key)
			s.requireProject(c, r, key, "manage")
			members := query(c, "SELECT user_id FROM project_members WHERE project_id=? ORDER BY user_id", project["id"])
			ids := make([]string, 0, len(members))
			for _, member := range members {
				ids = append(ids, str(member["user_id"]))
			}
			result := policy(project["privileges"])
			result["user_ids"] = ids
			return result
		}))
	})
	s.route("PUT /api/project/{projectKey}/privileges", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		key := keyValue(r.PathValue("projectKey"), "项目标识")
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			project := workspaceFindProject(c, key)
			s.requireProject(c, r, key, "manage")
			mode, isString := b["mode"].(string)
			if !isString || (mode != "inherit" && mode != "restricted") {
				fail(400, "请选择继承团队或指定成员")
			}
			ids, valid := b["user_ids"].([]any)
			if !valid || len(ids) > 1000 {
				fail(400, "项目成员列表不合法")
			}
			selected := make([]string, 0, len(ids))
			seen := map[string]bool{}
			for _, value := range ids {
				id, valid := value.(string)
				if !valid || len(utf16.Encode([]rune(id))) > 64 {
					fail(400, "项目成员列表不合法")
				}
				if !seen[id] {
					seen[id] = true
					selected = append(selected, id)
				}
			}
			if len(selected) != 0 {
				members := query(c, "SELECT user_id FROM team_members WHERE team_id=?", project["team_id"])
				allowed := map[string]bool{}
				for _, member := range members {
					allowed[str(member["user_id"])] = true
				}
				for _, id := range selected {
					if !allowed[id] {
						fail(400, "只能授权给当前团队成员")
					}
				}
			}
			encoded, err := json.Marshal(Row{"mode": mode})
			must(err)
			execSQL(c, "UPDATE projects SET privileges=? WHERE id=?", string(encoded), project["id"])
			execSQL(c, "DELETE FROM project_members WHERE project_id=?", project["id"])
			if mode == "restricted" {
				for _, id := range selected {
					execSQL(c, "INSERT INTO project_members (project_id,user_id) VALUES (?,?)", project["id"], id)
				}
			} else {
				selected = []string{}
			}
			updated := workspaceFindProject(c, key)
			result := projectDTO(updated, s.requireProject(c, r, key, "read"))
			result["privileges"] = Row{"mode": mode, "user_ids": selected}
			return result
		}))
	})
	s.route("POST /api/document", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		fileName := "untitled.md"
		if value, present := b["file_name"]; present {
			fileName = nameValue(value, "文档名称")
		}
		projectKey := workspaceOptionalKey(b, "project_key", "项目标识")
		folderKey := workspaceOptionalKey(b, "folder_key", "文件夹标识")
		teamKey := workspaceOptionalKey(b, "team_key", "团队标识")
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			project, folder := s.workspaceDocumentLocation(c, r, projectKey, folderKey, teamKey)
			key := publicKey()
			execSQL(c, "INSERT INTO documents (file_key,file_name,folder_id,creator_id,privileges) VALUES (?,?,?,?,JSON_OBJECT('mode','inherit'))", key, fileName, folder["id"], currentUser(r)["id"])
			return Row{
				"revision": 1, "privileges": Row{"mode": "inherit"}, "creator": creator(currentUser(r)),
				"file_key": key, "file_name": fileName, "project_key": project["project_key"], "folder_key": folder["folder_key"],
				"project_name": project["name"], "folder_path": []any{folder["name"]},
			}
		}))
	})
	s.route("GET /api/document/{fileKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("fileKey"), "文档标识")
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			row := s.findDocument(c, r, key, "read", true)
			result := documentDTO(row)
			result["file_content"] = row["file_content"]
			result["permissions"] = s.projectPermissions(c, r, str(row["project_key"]))
			return result
		}))
	})
	s.route("PUT /api/document/{fileKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("fileKey"), "文档标识")
		b := body(r)
		sets, values := []string{}, []any{}
		if value, present := b["file_name"]; present {
			sets = append(sets, "file_name=?")
			values = append(values, nameValue(value, "文档名称"))
		}
		content, hasContent := b["file_content"]
		if hasContent {
			if content != nil {
				if _, valid := content.(string); !valid {
					fail(400, "文档内容必须是字符串或 null")
				}
			}
			sets = append(sets, "file_content=?")
			values = append(values, content)
		}
		if len(sets) == 0 {
			fail(400, "请提供文档名称或文档内容")
		}
		revision := s.tx(true, func(c *sql.Tx) any {
			document := s.findDocument(c, r, key, "write", false)
			next := num(document["revision"])
			if hasContent {
				requested, valid := workspaceRevision(b["revision"])
				if !valid {
					fail(400, "保存正文需要版本号")
				}
				if requested != float64(next) {
					fail(409, "文件已被其他人修改，请保留本地内容后重新加载")
				}
				sets = append(sets, "revision=revision+1")
				next++
			}
			values = append(values, document["file_key"])
			execSQL(c, "UPDATE documents SET "+strings.Join(sets, ",")+" WHERE file_key=?", values...)
			return next
		})
		jsonResponse(w, 200, Row{"success": true, "revision": revision})
	})
	s.route("DELETE /api/document/{fileKey}", "required", func(w http.ResponseWriter, r *http.Request) {
		key := keyValue(r.PathValue("fileKey"), "文档标识")
		s.tx(true, func(c *sql.Tx) any {
			document := s.findDocument(c, r, key, "write", false)
			execSQL(c, "DELETE FROM files WHERE file_key=?", document["file_key"])
			execSQL(c, "DELETE FROM documents WHERE id=?", document["id"])
			return nil
		})
		// Content-addressed blobs remain until a separate orphan sweep.
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/documents", "required", func(w http.ResponseWriter, r *http.Request) {
		code := ""
		if value, present := workspaceQueryValue(r, "code"); present {
			var valid bool
			code, valid = value.(string)
			if !valid {
				fail(400, "搜索关键字必须是字符串")
			}
		}
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			statement := "SELECT " + documentColumns + " " + documentFrom + " WHERE EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id=p.team_id AND tm.user_id=?)"
			args := []any{currentUser(r)["id"]}
			if code != "" {
				statement += " AND d.file_name LIKE ?"
				args = append(args, "%"+code+"%")
			}
			statement += " ORDER BY d.created_at ASC,d.id ASC"
			documents := query(c, statement, args...)
			readable := map[string]bool{}
			visible := make([]Row, 0, len(documents))
			for _, document := range documents {
				projectKey := str(document["project_key"])
				allowed, cached := readable[projectKey]
				if !cached {
					allowed = flag(s.projectPermissions(c, r, projectKey)["read"])
					readable[projectKey] = allowed
				}
				if allowed {
					visible = append(visible, documentDTO(document))
				}
			}
			return visible
		}))
	})
}
