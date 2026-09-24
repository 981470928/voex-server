package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/argon2"
)

type authKey struct{}
type sessionAuth struct {
	User      Row
	SessionID string
}

const accessSeconds = 900

var refreshPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var avatarPattern = regexp.MustCompile(`^/api/assets/([a-f0-9-]{36})$`)
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var phonePattern = regexp.MustCompile(`^\+?[0-9 ()-]{5,32}$`)

func optionalUser(r *http.Request) Row {
	a, ok := r.Context().Value(authKey{}).(sessionAuth)
	if !ok {
		return nil
	}
	return a.User
}
func currentUser(r *http.Request) Row {
	u := optionalUser(r)
	if u == nil {
		fail(401, "请先登录")
	}
	return u
}
func userDTO(row Row) Row {
	v := Row{}
	for _, k := range []string{"id", "account", "name", "avator", "email", "phone"} {
		v[k] = str(row[k])
	}
	return v
}
func loadSecret() []byte {
	path := env("JWT_SECRET_FILE", "/home/server/.secrets/jwt-key")
	must(os.MkdirAll(filepath.Dir(path), 0700))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		b := make([]byte, 64)
		_, err = rand.Read(b)
		must(err)
		_, err = file.Write(b)
		file.Close()
		must(err)
	} else if !os.IsExist(err) {
		must(err)
	}
	secret, err := os.ReadFile(path)
	must(err)
	if len(secret) < 64 {
		panic("JWT signing key is too short")
	}
	return secret
}
func (s *Server) loadOrigins() {
	for _, o := range strings.Split(env("AUTH_ALLOWED_ORIGINS", "https://voex.jmin.site,https://jmin.site,http://218.76.62.176,http://localhost:3000,http://127.0.0.1:3000,http://localhost:3001,http://127.0.0.1:3001"), ",") {
		s.origins[strings.TrimSpace(o)] = true
	}
}
func cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
func sessionCookie(w http.ResponseWriter, r *http.Request, name, path, value string, seconds int) {
	c := &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode, MaxAge: seconds, Expires: time.Now().Add(time.Duration(seconds) * time.Second)}
	if seconds < 0 {
		c.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, c)
}
func clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	sessionCookie(w, r, "voex_access", "/api", "", -1)
	sessionCookie(w, r, "voex_refresh", "/api/auth", "", -1)
}
func (s *Server) signAccess(user Row, sid string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now().Unix()
	b, err := json.Marshal(Row{"sid": sid, "sub": user["id"], "iss": "voex", "aud": "voex-web", "iat": now, "exp": now + accessSeconds})
	must(err)
	payload := header + "." + base64.RawURLEncoding.EncodeToString(b)
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Server) verifyAccess(token string, ignoreExpiry bool) (Row, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var h Row
	if json.Unmarshal(header, &h) != nil || str(h["alg"]) != "HS256" {
		return nil, false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var p Row
	if json.Unmarshal(b, &p) != nil {
		return nil, false
	}
	sub, subOK := p["sub"].(string)
	sid, sidOK := p["sid"].(string)
	if !subOK || !sidOK || sub == "" || sid == "" || str(p["iss"]) != "voex" {
		return nil, false
	}
	aud := str(p["aud"]) == "voex-web"
	if a, ok := p["aud"].([]any); ok {
		for _, v := range a {
			aud = aud || str(v) == "voex-web"
		}
	}
	if !aud {
		return nil, false
	}
	now := time.Now().Unix()
	if nbf, ok := p["nbf"]; ok && num(nbf) > now {
		return nil, false
	}
	if !ignoreExpiry {
		exp, ok := p["exp"].(float64)
		iat, iatOK := p["iat"].(float64)
		if !ok || !iatOK || int64(exp) <= now || int64(iat)+accessSeconds <= now {
			return nil, false
		}
	}
	return p, true
}
func presentedAccess(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return cookie(r, "voex_access")
	}
	parts := strings.Split(header, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.ContainsAny(parts[1], "\t\r\n ") {
		return ""
	}
	return parts[1]
}
func (s *Server) authenticate(r *http.Request, optional bool) *http.Request {
	token := presentedAccess(r)
	if token == "" {
		if optional {
			return r
		}
		fail(401, "请先登录")
	}
	p, ok := s.verifyAccess(token, false)
	if !ok {
		if optional {
			return r
		}
		fail(401, "登录已过期，请重新登录")
	}
	row := one(s.db, "SELECT u.* FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE u.id=? AND s.id=? AND s.expires_at>UTC_TIMESTAMP()", p["sub"], p["sid"])
	if row == nil {
		if optional {
			return r
		}
		fail(401, "登录已失效，请重新登录")
	}
	return r.WithContext(context.WithValue(r.Context(), authKey{}, sessionAuth{userDTO(row), str(p["sid"])}))
}
func (s *Server) issueAccess(w http.ResponseWriter, r *http.Request, user Row, sid string) Row {
	token := s.signAccess(user, sid)
	sessionCookie(w, r, "voex_access", "/api", token, accessSeconds)
	return Row{"accessToken": token, "expiresIn": accessSeconds, "user": user}
}
func (s *Server) createSession(c DBTX, w http.ResponseWriter, r *http.Request, user Row) Row {
	sid, token := uuid(), randomToken(32)
	execSQL(c, "DELETE FROM auth_sessions WHERE expires_at<=UTC_TIMESTAMP()")
	execSQL(c, "INSERT INTO auth_sessions (id,user_id,refresh_hash,expires_at) VALUES (?,?,?,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 14 DAY))", sid, user["id"], digest(token))
	sessionCookie(w, r, "voex_refresh", "/api/auth", token, 14*24*60*60)
	return s.issueAccess(w, r, user, sid)
}
func characters(v any, label string, min, max int) string {
	value, ok := v.(string)
	if !ok || !utf8.ValidString(value) {
		fail(400, label+"格式错误")
	}
	length := utf8.RuneCountInString(value)
	if length < min || length > max {
		fail(400, fmt.Sprintf("%s需要 %d–%d 个字符", label, min, max))
	}
	return value
}
func profileValues(b Row) Row {
	name, ok := b["name"].(string)
	if !ok {
		fail(400, "请填写昵称")
	}
	name = characters(strings.TrimSpace(name), "昵称", 1, 64)
	email, phone := b["email"], b["phone"]
	if email == nil {
		email = ""
	}
	if phone == nil {
		phone = ""
	}
	e := strings.TrimSpace(characters(email, "邮箱", 0, 254))
	p := strings.TrimSpace(characters(phone, "手机号", 0, 32))
	if e != "" && !emailPattern.MatchString(e) {
		fail(400, "请输入有效的邮箱地址")
	}
	if p != "" && !phonePattern.MatchString(p) {
		fail(400, "请输入有效的手机号")
	}
	return Row{"name": name, "email": e, "phone": p}
}
func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, err := rand.Read(salt)
	must(err)
	hash := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return "$argon2id$v=19$m=19456,t=2,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
}
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	// PHC parameters are named; node-argon2 emits m,p,t while other encoders
	// emit m,t,p. Accept either order without weakening the resource bounds.
	params := map[string]uint64{}
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return false
		}
		if key != "m" && key != "t" && key != "p" {
			return false
		}
		if _, exists := params[key]; exists {
			return false
		}
		n, e := strconv.ParseUint(value, 10, 32)
		if e != nil {
			return false
		}
		params[key] = n
	}
	memory, iterations, parallel := params["m"], params["t"], params["p"]
	if len(params) != 3 || memory < 8 || memory > 262144 || iterations < 1 || iterations > 10 || parallel < 1 || parallel > 16 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 128 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallel), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
func (s *Server) hashSlot(fn func()) {
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
		fn()
	default:
		fail(429, "登录服务繁忙，请稍后重试")
	}
}
func (s *Server) registerAuthRoutes() {
	s.route("POST /api/auth/account-availability", "public", func(w http.ResponseWriter, r *http.Request) {
		s.limit(r, "auth-availability", 60, time.Minute)
		account := characters(body(r)["account"], "账号", 8, 64)
		jsonResponse(w, 200, Row{"available": one(s.db, "SELECT id FROM users WHERE account=?", []byte(account)) == nil})
	})
	s.route("POST /api/auth/register", "public", func(w http.ResponseWriter, r *http.Request) {
		s.limit(r, "auth-registration", 15, time.Hour)
		b := body(r)
		account := characters(b["account"], "账号", 8, 64)
		password := characters(b["password"], "密码", 10, 128)
		profile := profileValues(b)
		id := uuid()
		var hash string
		s.hashSlot(func() { hash = hashPassword(password) })
		defer func() {
			if e := recover(); e != nil {
				clearSessionCookies(w, r)
				if m, ok := e.(*mysql.MySQLError); ok && m.Number == 1062 {
					fail(409, "账号已被使用，请更换账号")
				}
				panic(e)
			}
		}()
		result := s.tx(true, func(tx *sql.Tx) any {
			execSQL(tx, "INSERT INTO users (id,account,password_hash,name,email,phone) VALUES (?,?,?,?,?,?)", id, []byte(account), hash, profile["name"], profile["email"], profile["phone"])
			s.ensurePersonalTeam(tx, id, str(profile["name"]))
			u := Row{"id": id, "account": account, "name": profile["name"], "email": profile["email"], "phone": profile["phone"], "avator": ""}
			return s.createSession(tx, w, r, u)
		})
		jsonResponse(w, 201, result)
	})
	s.route("POST /api/auth/login", "public", func(w http.ResponseWriter, r *http.Request) {
		refundIP := s.limitKey("auth-login-ip:"+rateIP(r), 30, 15*time.Minute)
		b := body(r)
		accountKey := rateIP(r)
		if a, ok := b["account"].(string); ok {
			accountKey = digest(a)
		}
		refundAccount := s.limitKey("auth-login-account:"+accountKey, 10, 15*time.Minute)
		account := characters(b["account"], "账号", 8, 64)
		password := characters(b["password"], "密码", 10, 128)
		row := one(s.db, "SELECT * FROM users WHERE account=?", []byte(account))
		hash := s.dummyHash
		if row != nil {
			hash = str(row["password_hash"])
		}
		valid := false
		s.hashSlot(func() { valid = verifyPassword(hash, password) })
		if row == nil || !valid {
			fail(401, "账号或密码不正确")
		}
		previous := cookie(r, "voex_refresh")
		if previous != "" {
			execSQL(s.db, "DELETE FROM auth_sessions WHERE refresh_hash=?", digest(previous))
		}
		session := s.createSession(s.db, w, r, userDTO(row))
		refundIP()
		refundAccount()
		jsonResponse(w, 200, session)
	})
	s.route("POST /api/auth/refresh", "public", func(w http.ResponseWriter, r *http.Request) {
		s.limit(r, "auth-availability", 60, time.Minute)
		token := cookie(r, "voex_refresh")
		if !refreshPattern.MatchString(token) {
			fail(401, "请重新登录")
		}
		row := one(s.db, "SELECT u.*,s.id AS session_id FROM users u JOIN auth_sessions s ON s.user_id=u.id WHERE s.refresh_hash=? AND s.expires_at>UTC_TIMESTAMP()", digest(token))
		if row == nil {
			clearSessionCookies(w, r)
			fail(401, "登录已过期，请重新登录")
		}
		jsonResponse(w, 200, s.issueAccess(w, r, userDTO(row), str(row["session_id"])))
	})
	s.route("POST /api/auth/logout", "public", func(w http.ResponseWriter, r *http.Request) {
		if p, ok := s.verifyAccess(presentedAccess(r), true); ok {
			execSQL(s.db, "DELETE FROM auth_sessions WHERE id=? AND user_id=?", p["sid"], p["sub"])
		}
		if token := cookie(r, "voex_refresh"); token != "" {
			execSQL(s.db, "DELETE FROM auth_sessions WHERE refresh_hash=?", digest(token))
		}
		clearSessionCookies(w, r)
		jsonResponse(w, 200, Row{"success": true})
	})
	s.route("GET /api/auth/me", "required", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, currentUser(r)) })
	s.route("PATCH /api/auth/me", "required", func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		b := body(r)
		merged := Row{}
		for k, v := range u {
			merged[k] = v
		}
		for k, v := range b {
			merged[k] = v
		}
		profile := profileValues(merged)
		avatar := str(u["avator"])
		if v, exists := b["avator"]; exists {
			var ok bool
			avatar, ok = v.(string)
			if !ok {
				fail(400, "头像格式错误")
			}
			if avatar != "" {
				match := avatarPattern.FindStringSubmatch(avatar)
				if match == nil {
					fail(400, "请先上传头像")
				}
				if one(s.db, "SELECT id FROM assets WHERE id=? AND creator_id=? AND directory='avator' AND mime IN ('image/png','image/jpeg','image/webp')", match[1], u["id"]) == nil {
					fail(400, "头像文件不存在或不属于当前账号")
				}
			}
		}
		execSQL(s.db, "UPDATE users SET name=?,email=?,phone=?,avator=? WHERE id=?", profile["name"], profile["email"], profile["phone"], avatar, u["id"])
		for k, v := range profile {
			u[k] = v
		}
		u["avator"] = avatar
		jsonResponse(w, 200, u)
	})
}

// Keep strict numeric parsing separate from forgiving SQL conversion helpers.
func strictPositiveInt(v any) (int64, bool) {
	switch x := v.(type) {
	case json.Number:
		n, e := strconv.ParseInt(string(x), 10, 64)
		return n, e == nil && n > 0
	case float64:
		return int64(x), x > 0 && x == float64(int64(x))
	}
	return 0, false
}
