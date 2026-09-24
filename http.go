package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

type Row map[string]any
type DBTX interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}
type HTTPError struct {
	Status  int
	Message string
}

func (e HTTPError) Error() string     { return e.Message }
func fail(status int, message string) { panic(HTTPError{status, message}) }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(v)
	}
}
func num(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		n, _ := strconv.ParseInt(str(v), 10, 64)
		return n
	}
}
func flag(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "0"
	default:
		return num(v) != 0
	}
}
func timestamp(v any) string {
	if t, ok := v.(time.Time); ok {
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return str(v)
}
func policy(v any) Row {
	if r, ok := v.(Row); ok {
		return r
	}
	if r, ok := v.(map[string]any); ok {
		return Row(r)
	}
	var r Row
	if json.Unmarshal([]byte(str(v)), &r) != nil || r == nil {
		return Row{"mode": "restricted"}
	}
	return r
}
func query(c DBTX, statement string, args ...any) []Row {
	rows, err := c.Query(statement, args...)
	must(err)
	defer rows.Close()
	names, err := rows.Columns()
	must(err)
	types, err := rows.ColumnTypes()
	must(err)
	result := make([]Row, 0)
	for rows.Next() {
		vals := make([]any, len(names))
		ptr := make([]any, len(names))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		must(rows.Scan(ptr...))
		r := Row{}
		for i, name := range names {
			v := vals[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
				switch types[i].DatabaseTypeName() {
				case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "BIGINT", "YEAR":
					v, err = strconv.ParseInt(string(b), 10, 64)
					must(err)
				case "FLOAT", "DOUBLE", "DECIMAL":
					v, err = strconv.ParseFloat(string(b), 64)
					must(err)
				}
			}
			r[name] = v
		}
		result = append(result, r)
	}
	must(rows.Err())
	return result
}
func one(c DBTX, q string, args ...any) Row {
	rows := query(c, q, args...)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
func execSQL(c DBTX, q string, args ...any) sql.Result { r, e := c.Exec(q, args...); must(e); return r }
func keyValue(v any, label string) string {
	x, ok := v.(string)
	if !ok || strings.TrimSpace(x) == "" {
		fail(400, label+"必须是非空字符串")
	}
	if utf8.RuneCountInString(x) > 64 || x != strings.TrimSpace(x) {
		fail(400, label+"不能超过 64 个字符或包含首尾空白")
	}
	return x
}
func nameValue(v any, label string) string {
	x, ok := v.(string)
	if !ok || strings.TrimSpace(x) == "" {
		fail(400, label+"必须是非空字符串")
	}
	x = strings.TrimSpace(x)
	if utf8.RuneCountInString(x) > 255 {
		fail(400, label+"不能超过 255 个字符")
	}
	return x
}
func body(r *http.Request) Row {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		fail(400, "请求体必须是 JSON 对象")
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
	must(err)
	if len(b) > 2*1024*1024 {
		fail(413, "请求内容超过大小限制")
	}
	if !utf8.Valid(b) || !validJSONUnicode(b) {
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	var obj Row
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if d.Decode(&obj) != nil || obj == nil {
		fail(400, "请求体必须是 JSON 对象")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(400, "请求格式错误，请检查 JSON 和路径参数")
	}
	return obj
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(v)
	}
}
func randomToken(n int) string {
	b := make([]byte, n)
	_, err := rand.Read(b)
	must(err)
	return base64.RawURLEncoding.EncodeToString(b)
}
func uuid() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	must(err)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func publicKey() string      { return strings.ReplaceAll(uuid(), "-", "")[:16] }
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func creator(u Row) Row      { return Row{"id": u["id"], "name": u["name"], "avator": u["avator"]} }
func (s *Server) tx(write bool, fn func(*sql.Tx) any) any {
	tx, err := s.db.BeginTx(nilContext(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	must(err)
	defer tx.Rollback()
	if write {
		r := one(tx, "SELECT id FROM workspace_state WHERE id=1 FOR UPDATE")
		if r == nil {
			panic(errors.New("workspace lock missing"))
		}
	}
	v := fn(tx)
	must(tx.Commit())
	return v
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
		w.ResponseWriter.WriteHeader(n)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func writeError(w http.ResponseWriter, v any) {
	status, msg := 500, "服务器处理失败，请稍后重试"
	switch e := v.(type) {
	case HTTPError:
		status, msg = e.Status, e.Message
	case *HTTPError:
		status, msg = e.Status, e.Message
	case *http.MaxBytesError:
		status, msg = 413, "上传文件超过大小限制"
	case *mysql.MySQLError:
		switch e.Number {
		case 1062:
			status, msg = 409, "资源标识冲突，请重试"
		case 1451, 1452:
			status, msg = 409, "资源关联已发生变化，请刷新后重试"
		case 1205, 1213:
			status, msg = 409, "工作区正在更新，请稍后重试"
		case 1406:
			status, msg = 400, "字段内容超过长度限制"
		}
	}
	if status == 500 {
		log.Printf("[API] request failed (%T)", v)
	}
	jsonResponse(w, status, Row{"error": msg})
}
func (s *Server) route(pattern, auth string, h func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if auth == "required" {
			r = s.authenticate(r, false)
		} else if auth == "optional" {
			r = s.authenticate(r, true)
		}
		h(w, r)
	})
}
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, _ := netip.ParseAddr(host)
	if a.IsLoopback() {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			ip, e := netip.ParseAddr(candidate)
			if e != nil {
				continue
			}
			host = candidate
			if !ip.IsLoopback() {
				break
			}
		}
	}
	return host
}
func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip, _ := netip.ParseAddr(host)
	return ip.IsLoopback() && strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}
func (s *Server) trustedOrigin(r *http.Request, origin string) bool {
	scheme := "http"
	if secureRequest(r) {
		scheme = "https"
	}
	if origin == scheme+"://"+r.Host {
		return true
	}
	return s.origins[origin]
}
func (s *Server) ServeHTTP(base http.ResponseWriter, r *http.Request) {
	w := &statusWriter{ResponseWriter: base}
	start := time.Now()
	defer func() {
		if v := recover(); v != nil && w.status == 0 {
			writeError(w, v)
		}
		status := w.status
		if status == 0 {
			status = 200
		}
		s.accessLog.Printf("%s %s %s %d %.3f ms", remoteIP(r), r.Method, r.URL.EscapedPath(), status, float64(time.Since(start).Microseconds())/1000)
	}()
	if strings.HasPrefix(r.URL.Path, "/api") {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}
	origin := r.Header.Get("Origin")
	w.Header().Add("Vary", "Origin")
	if origin != "" && s.trustedOrigin(r, origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,PATCH,POST,DELETE")
		w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		w.WriteHeader(204)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		if (origin != "" && !s.trustedOrigin(r, origin)) || (origin == "" && r.Header.Get("Sec-Fetch-Site") == "cross-site") {
			fail(403, "请求来源不受信任")
		}
	}
	if len(r.URL.Path) > 1 {
		r.URL.Path = strings.TrimRight(r.URL.Path, "/")
	}
	s.mux.ServeHTTP(w, r)
}

type limitEntry struct {
	Count   int
	Expires time.Time
}
type limiter struct {
	sync.Mutex
	entries map[string]limitEntry
	nextGC  time.Time
}

func (s *Server) limitKey(key string, max int, window time.Duration) func() {
	now := time.Now()
	s.limits.Lock()
	defer s.limits.Unlock()
	if now.After(s.limits.nextGC) {
		for k, e := range s.limits.entries {
			if !now.Before(e.Expires) {
				delete(s.limits.entries, k)
			}
		}
		s.limits.nextGC = now.Add(time.Minute)
	}
	e := s.limits.entries[key]
	if !now.Before(e.Expires) {
		e = limitEntry{Expires: now.Add(window)}
	}
	e.Count++
	s.limits.entries[key] = e
	if e.Count > max {
		fail(429, "尝试次数过多，请稍后重试")
	}
	return func() {
		s.limits.Lock()
		defer s.limits.Unlock()
		v := s.limits.entries[key]
		if v.Expires.Equal(e.Expires) && v.Count > 0 {
			v.Count--
			s.limits.entries[key] = v
		}
	}
}
func (s *Server) limit(r *http.Request, bucket string, max int, window time.Duration) {
	defer func() {
		if v := recover(); v != nil {
			if e, ok := v.(HTTPError); ok && e.Status == 429 {
				switch bucket {
				case "shared-file":
					e.Message = "分享访问过于频繁，请稍后重试"
				case "auth-registration":
					e.Message = "注册次数过多，请 1 小时后重试"
				case "auth-availability":
					e.Message = "检测过于频繁，请稍后重试"
				}
				panic(e)
			}
			panic(v)
		}
	}()
	s.limitKey(bucket+":"+rateIP(r), max, window)
}
func rateIP(r *http.Request) string {
	raw := remoteIP(r)
	a, e := netip.ParseAddr(raw)
	if e != nil {
		return raw
	}
	a = a.Unmap()
	if a.Is6() {
		return netip.PrefixFrom(a, 56).Masked().String()
	}
	return a.String()
}

// encoding/json replaces unpaired UTF-16 escapes. Reject them before decoding
// so malformed account/password strings cannot become a different identity.
func validJSONUnicode(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		v, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
