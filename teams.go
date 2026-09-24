package main

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

var teamInviteTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var teamCodePattern = regexp.MustCompile(`^[0-9]{10}$`)

func (s *Server) teamRow(c DBTX, r *http.Request, key string, manager bool) Row {
	team := one(c, `SELECT t.*,m.user_id AS member_id,
        CASE WHEN t.owner_id=m.user_id THEN 'owner' ELSE m.role END AS role
        FROM teams t JOIN team_members m ON m.team_id=t.id AND m.user_id=? WHERE t.team_key=?`, currentUser(r)["id"], key)
	if team == nil {
		fail(404, "团队不存在或尚未加入")
	}
	if manager && str(team["role"]) == "member" {
		fail(403, "只有团队所有者和管理员可以操作")
	}
	return team
}

func teamDTO(row Row) Row {
	role, kind := str(row["role"]), str(row["kind"])
	count := int64(1)
	if row["member_count"] != nil {
		count = num(row["member_count"])
	}
	ownerStandard := role == "owner" && kind == "standard"
	return Row{
		"team_key": row["team_key"], "team_code": row["team_code"], "name": row["name"],
		"kind": kind, "role": role, "owner_id": row["owner_id"], "member_count": count,
		"created_at": timestamp(row["created_at"]), "privileges": Row{"mode": "members"},
		"permissions": Row{"read": true, "write": true, "manage": role == "owner" || role == "admin", "transfer": ownerStandard, "delete": ownerStandard},
	}
}

func (s *Server) getTeam(c DBTX, r *http.Request, key string) Row {
	row := s.teamRow(c, r, key, false)
	row["member_count"] = one(c, "SELECT COUNT(*) AS count FROM team_members WHERE team_id=?", row["id"])["count"]
	return teamDTO(row)
}

func (s *Server) listTeams(c DBTX, r *http.Request) []Row {
	userID := currentUser(r)["id"]
	rows := query(c, `SELECT t.*,CASE WHEN t.owner_id=m.user_id THEN 'owner' ELSE m.role END AS role,
        (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id=t.id) AS member_count
        FROM teams t JOIN team_members m ON m.team_id=t.id WHERE m.user_id=?
        ORDER BY (t.personal_owner_id=?) DESC,t.created_at,t.id`, userID, userID)
	result := make([]Row, 0, len(rows))
	for _, row := range rows {
		result = append(result, teamDTO(row))
	}
	return result
}

func teamNumber() string {
	first, err := rand.Int(rand.Reader, big.NewInt(9))
	must(err)
	rest, err := rand.Int(rand.Reader, big.NewInt(1000000000))
	must(err)
	return fmt.Sprintf("%d%09d", first.Int64()+1, rest.Int64())
}

func (s *Server) insertTeam(c DBTX, userID, name string, personal bool) int64 {
	kind := "standard"
	var personalOwner any
	if personal {
		kind, personalOwner = "personal", userID
	}
	for attempt := 0; attempt < 8; attempt++ {
		result, err := c.Exec(`INSERT INTO teams (team_key,team_code,name,kind,owner_id,personal_owner_id,privileges)
            VALUES (?,?,?,?,?,?,JSON_OBJECT('mode','members'))`, publicKey(), teamNumber(), name, kind, userID, personalOwner)
		if err != nil {
			var dbError *mysql.MySQLError
			if !errors.As(err, &dbError) || dbError.Number != 1062 {
				must(err)
			}
			if personal {
				existing := one(c, "SELECT id FROM teams WHERE personal_owner_id=?", userID)
				if existing != nil {
					return num(existing["id"])
				}
			}
			continue
		}
		id, err := result.LastInsertId()
		must(err)
		execSQL(c, "INSERT INTO team_members (team_id,user_id,role) VALUES (?,?,'member')", id, userID)
		return id
	}
	panic("Unable to allocate unique team identifier")
}

func (s *Server) ensurePersonalTeam(c DBTX, userID, name string) int64 {
	row := one(c, "SELECT id FROM teams WHERE personal_owner_id=?", userID)
	if row != nil {
		return num(row["id"])
	}
	return s.insertTeam(c, userID, name+"的个人团队", true)
}

func (s *Server) selectedTeam(c DBTX, r *http.Request, key string) Row {
	if key != "" {
		return s.teamRow(c, r, key, false)
	}
	row := one(c, "SELECT team_key FROM teams WHERE personal_owner_id=?", currentUser(r)["id"])
	if row == nil {
		fail(409, "个人团队尚未初始化")
	}
	return s.teamRow(c, r, str(row["team_key"]), false)
}

func teamTimestamps(row Row, fields ...string) Row {
	for _, field := range fields {
		if row[field] != nil {
			row[field] = timestamp(row[field])
		}
	}
	return row
}

func (s *Server) teamMembers(c DBTX, r *http.Request, key string) []Row {
	team := s.teamRow(c, r, key, false)
	rows := query(c, `SELECT u.id AS user_id,u.name,u.avator,CASE WHEN u.id=? THEN 'owner' ELSE m.role END AS role,m.joined_at
        FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=?
        ORDER BY (u.id=?) DESC,(m.role='admin') DESC,m.joined_at,u.id`, team["owner_id"], team["id"], team["owner_id"])
	for _, row := range rows {
		teamTimestamps(row, "joined_at")
	}
	return rows
}

const teamRequestColumns = `r.id,t.team_key,t.name AS team_name,r.user_id,u.name,r.message,r.status,r.created_at,r.reviewed_at`

func teamRequestResult(c DBTX, id any) Row {
	row := one(c, `SELECT `+teamRequestColumns+` FROM team_join_requests r JOIN teams t ON t.id=r.team_id
        JOIN users u ON u.id=r.user_id WHERE r.id=?`, id)
	return teamTimestamps(row, "created_at", "reviewed_at")
}

func (s *Server) registerTeamRoutes() {
	s.route("POST /api/team-invites/inspect", "public", func(w http.ResponseWriter, r *http.Request) {
		s.limit(r, "team-invite", 60, time.Minute)
		token, ok := body(r)["token"].(string)
		if !ok || !teamInviteTokenPattern.MatchString(token) {
			fail(404, "邀请链接无效或已撤销")
		}
		row := one(s.db, `SELECT t.team_key,t.team_code,t.name,t.kind FROM teams t JOIN team_invites i ON i.team_id=t.id
            WHERE i.token_hash=? AND i.revoked_at IS NULL`, digest(token))
		if row == nil {
			fail(404, "邀请链接无效或已撤销")
		}
		jsonResponse(w, 200, row)
	})
	s.route("GET /api/teams", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any { return s.listTeams(c, r) }))
	})
	s.route("POST /api/teams", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		jsonResponse(w, 201, s.tx(true, func(c *sql.Tx) any {
			name := nameValue(b["name"], "团队名称")
			if utf8.RuneCountInString(name) > 128 {
				fail(400, "团队名称最多128个字符")
			}
			id := s.insertTeam(c, str(currentUser(r)["id"]), name, false)
			row := one(c, "SELECT team_key FROM teams WHERE id=?", id)
			return s.getTeam(c, r, str(row["team_key"]))
		}))
	})
	s.route("GET /api/teams/{key}", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			return s.getTeam(c, r, keyValue(r.PathValue("key"), "团队标识"))
		}))
	})
	s.route("PATCH /api/teams/{key}", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			name := nameValue(b["name"], "团队名称")
			if utf8.RuneCountInString(name) > 128 {
				fail(400, "团队名称最多128个字符")
			}
			execSQL(c, "UPDATE teams SET name=? WHERE id=?", name, t["id"])
			return s.getTeam(c, r, str(t["team_key"]))
		}))
	})
	s.route("DELETE /api/teams/{key}", "required", func(w http.ResponseWriter, r *http.Request) {
		s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			if str(t["role"]) != "owner" {
				fail(403, "只有所有者可以删除团队")
			}
			if str(t["kind"]) == "personal" {
				fail(403, "个人团队无法删除")
			}
			if one(c, "SELECT id FROM projects WHERE team_id=? LIMIT 1", t["id"]) != nil {
				fail(409, "请先清空团队中的项目")
			}
			execSQL(c, "DELETE FROM teams WHERE id=?", t["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("POST /api/teams/{key}/transfer", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			if str(t["role"]) != "owner" || str(t["kind"]) == "personal" {
				fail(403, "仅普通团队的所有者可以转让团队")
			}
			userID := keyValue(b["user_id"], "成员标识")
			if userID == str(t["owner_id"]) {
				fail(400, "请选择其他团队成员")
			}
			if one(c, "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?", t["id"], userID) == nil {
				fail(400, "新所有者必须是当前团队成员")
			}
			execSQL(c, "UPDATE team_members SET role='admin' WHERE team_id=? AND user_id=?", t["id"], t["owner_id"])
			execSQL(c, "UPDATE teams SET owner_id=? WHERE id=?", userID, t["id"])
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/teams/{key}/members", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any { return s.teamMembers(c, r, r.PathValue("key")) }))
	})
	s.route("PATCH /api/teams/{key}/members/{userId}", "required", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			if str(t["role"]) != "owner" {
				fail(403, "只有所有者可以设置管理员")
			}
			userID := r.PathValue("userId")
			if userID == str(t["owner_id"]) {
				fail(403, "所有者身份只能通过团队转让变更")
			}
			role := str(b["role"])
			if role != "admin" && role != "member" {
				fail(400, "角色只能为管理员或成员")
			}
			if one(c, "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?", t["id"], userID) == nil {
				fail(404, "成员不存在")
			}
			execSQL(c, "UPDATE team_members SET role=? WHERE team_id=? AND user_id=?", role, t["id"], userID)
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("DELETE /api/teams/{key}/members/{userId}", "required", func(w http.ResponseWriter, r *http.Request) {
		s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), false)
			userID, actorID := r.PathValue("userId"), str(currentUser(r)["id"])
			if userID == "me" {
				userID = actorID
			}
			member := one(c, "SELECT role FROM team_members WHERE team_id=? AND user_id=?", t["id"], userID)
			if member == nil {
				fail(404, "成员不存在")
			}
			if userID == str(t["owner_id"]) {
				fail(403, "所有者不能退出或被移除，请先转让普通团队")
			}
			role := str(t["role"])
			if userID != actorID && (role == "member" || (role == "admin" && str(member["role"]) == "admin")) {
				fail(403, "没有移除此成员的权限")
			}
			execSQL(c, "DELETE pm FROM project_members pm JOIN projects p ON p.id=pm.project_id WHERE p.team_id=? AND pm.user_id=?", t["id"], userID)
			execSQL(c, "DELETE FROM team_members WHERE team_id=? AND user_id=?", t["id"], userID)
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/teams/{key}/invites", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			rows := query(c, "SELECT id,created_at,revoked_at FROM team_invites WHERE team_id=? ORDER BY created_at DESC", t["id"])
			for _, row := range rows {
				teamTimestamps(row, "created_at", "revoked_at")
			}
			return rows
		}))
	})
	s.route("POST /api/teams/{key}/invites", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 201, s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			id, token := uuid(), randomToken(32)
			execSQL(c, "INSERT INTO team_invites (id,team_id,token_hash,created_by) VALUES (?,?,?,?)", id, t["id"], digest(token), currentUser(r)["id"])
			return Row{"id": id, "token": token, "created_at": timestamp(time.Now().UTC())}
		}))
	})
	s.route("DELETE /api/teams/{key}/invites/{id}", "required", func(w http.ResponseWriter, r *http.Request) {
		s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			id := r.PathValue("id")
			if one(c, "SELECT id FROM team_invites WHERE id=? AND team_id=?", id, t["id"]) == nil {
				fail(404, "邀请不存在")
			}
			execSQL(c, "UPDATE team_invites SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP()) WHERE id=?", id)
			return nil
		})
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("POST /api/team-join-requests", "required", func(w http.ResponseWriter, r *http.Request) {
		s.limit(r, "team-invite", 60, time.Minute)
		b, user := body(r), currentUser(r)
		message, ok := b["message"].(string)
		if !ok || strings.TrimSpace(message) == "" {
			fail(400, "请填写申请信息")
		}
		message = strings.TrimSpace(message)
		if utf8.RuneCountInString(message) > 1000 {
			fail(400, "申请信息最多1000个字符")
		}
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			var team Row
			token, isToken := b["invite_token"].(string)
			code, isCode := b["team_code"].(string)
			if isToken && teamInviteTokenPattern.MatchString(token) {
				team = one(c, "SELECT t.id FROM teams t JOIN team_invites i ON i.team_id=t.id WHERE i.token_hash=? AND i.revoked_at IS NULL", digest(token))
			} else if isCode && teamCodePattern.MatchString(code) {
				team = one(c, "SELECT id FROM teams WHERE team_code=?", code)
			} else {
				fail(400, "请提供有效的团队编号或邀请链接")
			}
			if team == nil {
				fail(404, "团队或邀请链接不存在")
			}
			teamID := team["id"]
			if one(c, "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?", teamID, user["id"]) != nil {
				fail(409, "你已经是该团队成员")
			}
			prior := one(c, "SELECT id,status FROM team_join_requests WHERE team_id=? AND user_id=?", teamID, user["id"])
			if prior != nil && str(prior["status"]) == "pending" {
				return teamRequestResult(c, prior["id"])
			}
			id := uuid()
			if prior != nil {
				execSQL(c, "UPDATE team_join_requests SET id=?,message=?,status='pending',created_at=CURRENT_TIMESTAMP(),reviewed_at=NULL,reviewed_by=NULL WHERE id=?", id, message, prior["id"])
			} else {
				execSQL(c, "INSERT INTO team_join_requests (id,team_id,user_id,message) VALUES (?,?,?,?)", id, teamID, user["id"], message)
			}
			return teamRequestResult(c, id)
		}))
	})
	s.route("GET /api/team-join-requests", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			rows := query(c, `SELECT `+teamRequestColumns+` FROM team_join_requests r JOIN teams t ON t.id=r.team_id
                JOIN users u ON u.id=r.user_id WHERE r.user_id=? ORDER BY r.created_at DESC`, currentUser(r)["id"])
			for _, row := range rows {
				teamTimestamps(row, "created_at", "reviewed_at")
			}
			return rows
		}))
	})
	s.route("GET /api/teams/{key}/join-requests", "required", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, s.tx(false, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			rows := query(c, `SELECT `+teamRequestColumns+` FROM team_join_requests r JOIN teams t ON t.id=r.team_id
                JOIN users u ON u.id=r.user_id WHERE r.team_id=? ORDER BY (r.status='pending') DESC,r.created_at DESC`, t["id"])
			for _, row := range rows {
				teamTimestamps(row, "created_at", "reviewed_at")
			}
			return rows
		}))
	})
	s.route("PATCH /api/teams/{key}/join-requests/{id}", "required", func(w http.ResponseWriter, r *http.Request) {
		status := str(body(r)["status"])
		if status != "approved" && status != "rejected" {
			fail(400, "审核结果不合法")
		}
		jsonResponse(w, 200, s.tx(true, func(c *sql.Tx) any {
			t := s.teamRow(c, r, r.PathValue("key"), true)
			request := one(c, "SELECT * FROM team_join_requests WHERE id=? AND team_id=?", r.PathValue("id"), t["id"])
			if request == nil {
				fail(404, "申请不存在")
			}
			if str(request["status"]) != "pending" {
				if str(request["status"]) == status {
					return teamRequestResult(c, request["id"])
				}
				fail(409, "申请已经处理")
			}
			if status == "approved" {
				execSQL(c, "INSERT IGNORE INTO team_members (team_id,user_id,role) VALUES (?,?,'member')", t["id"], request["user_id"])
			}
			execSQL(c, "UPDATE team_join_requests SET status=?,reviewed_by=?,reviewed_at=CURRENT_TIMESTAMP() WHERE id=?", status, currentUser(r)["id"], request["id"])
			return teamRequestResult(c, request["id"])
		}))
	})
}
