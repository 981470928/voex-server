// Package updater runs the isolated, privileged offline release installer.
package updater

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Release struct {
	FormatVersion  int       `json:"formatVersion"`
	Version        string    `json:"version"`
	FromVersions   []string  `json:"fromVersions"`
	SchemaVersion  string    `json:"schemaVersion"`
	CreatedAt      time.Time `json:"createdAt"`
	Notes          string    `json:"notes"`
	APIImage       string    `json:"apiImage"`
	ImagesSHA256   string    `json:"imagesSHA256"`
	FrontendSHA256 string    `json:"frontendSHA256"`
	APIImageID     string    `json:"apiImageID"`
}

type Job struct {
	ID                      string     `json:"id"`
	RequestID               string     `json:"requestId"`
	Version                 string     `json:"version"`
	FromVersion             string     `json:"previousVersion"`
	Status                  string     `json:"status"`
	Step                    string     `json:"step"`
	Message                 string     `json:"message"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
	StartedAt               *time.Time `json:"startedAt,omitempty"`
	FinishedAt              *time.Time `json:"finishedAt,omitempty"`
	Backup                  string     `json:"backup,omitempty"`
	PreviousFrontendVersion string     `json:"previousFrontendVersion,omitempty"`
}

type storedJob struct {
	Job        Job    `json:"job"`
	WatchToken string `json:"watchToken"`
}

type Server struct {
	instanceLock                           *os.File
	mu                                     sync.Mutex
	busy                                   bool
	releases                               map[string]Release
	jobs                                   map[string]*storedJob
	requests                               map[string]string
	current                                string
	deploy, data, static, token, publicKey string
	client                                 *http.Client
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func Run() error {
	server, err := New()
	if err != nil {
		return err
	}
	defer server.instanceLock.Close()
	httpServer := &http.Server{
		Addr: env("VOEX_UPDATER_ADDR", "0.0.0.0:9090"), Handler: server.Handler(),
		ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	// An in-flight job remains durably marked running if the container is killed;
	// the next process marks it interrupted and never replays database changes.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	log.Print("updater listening")
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func New() (*Server, error) {
	deploy := filepath.Clean(env("VOEX_DEPLOY_DIR", "/home/voex-deploy"))
	if !filepath.IsAbs(deploy) || deploy == "/" {
		return nil, errors.New("invalid deployment directory")
	}
	token := env("VOEX_UPDATER_TOKEN", "")
	if len(token) < 32 {
		return nil, errors.New("VOEX_UPDATER_TOKEN must contain at least 32 characters")
	}
	s := &Server{
		deploy: deploy, data: deploy,
		static: filepath.Clean(env("VOEX_STATIC_ROOT", "/home/static")), token: token,
		publicKey: env("VOEX_RELEASE_PUBLIC_KEY", filepath.Join(deploy, "release.pub")),
		releases:  make(map[string]Release), jobs: make(map[string]*storedJob), requests: make(map[string]string),
		client: &http.Client{Timeout: 10 * time.Second},
	}
	if !filepath.IsAbs(s.static) || s.static == "/" {
		return nil, errors.New("invalid static directory")
	}
	for _, name := range []string{"releases", "jobs", "backups", "imports", "state"} {
		if err := privateDir(filepath.Join(s.data, name)); err != nil {
			return nil, errors.New("cannot initialize updater directories")
		}
	}
	if err := os.Chmod(filepath.Join(s.data, "state"), 0755); err != nil {
		return nil, errors.New("cannot expose maintenance marker")
	}
	if _, err := s.readPublicKey(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(s.data, "state", "updater.lock"), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, errors.New("cannot open updater instance lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("another updater instance owns this deployment")
	}
	s.instanceLock = lock
	if err := s.loadState(); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return s, nil
}

func privateDir(name string) error {
	if err := os.MkdirAll(name, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("not a real directory")
	}
	return os.Chmod(name, 0700)
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func atomicWrite(name string, data []byte) error { return atomicWriteMode(name, data, 0600) }
func atomicWriteMode(name string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(name), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), name)
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s *Server) maintenance() string { return filepath.Join(s.data, "state", "maintenance") }
func (s *Server) runtime() string     { return filepath.Join(s.data, "runtime.env") }
func (s *Server) saveJobLocked(record *storedJob) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.data, "jobs", record.Job.ID+".json"), data)
}

func (s *Server) loadState() error {
	entries, err := os.ReadDir(filepath.Join(s.data, "releases"))
	if err != nil {
		return errors.New("cannot read release state")
	}
	for _, entry := range entries {
		if !entry.IsDir() || !versionPattern.MatchString(entry.Name()) {
			continue
		}
		release, err := s.readRelease(filepath.Join(s.data, "releases", entry.Name()))
		if err != nil || release.Version != entry.Name() {
			return errors.New("stored release validation failed")
		}
		s.releases[release.Version] = release
	}
	entries, err = os.ReadDir(filepath.Join(s.data, "jobs"))
	if err != nil {
		return errors.New("cannot read upgrade state")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.data, "jobs", entry.Name()))
		if err != nil {
			return errors.New("cannot read upgrade job")
		}
		var record storedJob
		if err := json.Unmarshal(data, &record); err != nil || len(record.Job.ID) != 32 || len(record.WatchToken) != 64 || entry.Name() != record.Job.ID+".json" {
			return errors.New("invalid persisted upgrade state")
		}
		marker, markerErr := os.ReadFile(s.maintenance())
		completionPending := record.Job.Status == "succeeded" && markerErr == nil && strings.TrimSpace(string(marker)) == record.Job.ID
		if record.Job.Status == "queued" || record.Job.Status == "running" || completionPending {
			now := time.Now().UTC()
			record.Job.Status, record.Job.Step = "interrupted", "interrupted"
			record.Job.Message = "更新器重启，升级已中断；维护模式保留，需要人工恢复"
			record.Job.UpdatedAt, record.Job.FinishedAt = now, &now
			if err := atomicWriteMode(s.maintenance(), []byte(record.Job.ID+"\n"), 0644); err != nil {
				return errors.New("cannot retain interrupted maintenance")
			}
			if err := s.saveJobLocked(&record); err != nil {
				return errors.New("cannot persist interrupted upgrade")
			}
		}
		s.jobs[record.Job.ID] = &record
		s.requests[record.Job.RequestID] = record.Job.ID
		if current := s.jobs[s.current]; current == nil || record.Job.CreatedAt.After(current.Job.CreatedAt) {
			s.current = record.Job.ID
		}
	}
	return nil
}

func equalSecret(actual, expected string) bool {
	a, b := sha256.Sum256([]byte(actual)), sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failHTTP(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) })
	private := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			value := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !equalSecret(value, s.token) {
				failHTTP(w, 401, "更新服务未授权")
				return
			}
			handler(w, r)
		}
	}
	mux.HandleFunc("GET /releases", private(s.listReleases))
	mux.HandleFunc("POST /packages", private(s.importPackage))
	mux.HandleFunc("POST /upgrades", private(s.startUpgrade))
	mux.HandleFunc("GET /upgrades/current", private(s.currentUpgrade))
	mux.HandleFunc("GET /api/upgrade-status/{jobID}", s.watchUpgrade)
	return mux
}

func (s *Server) listReleases(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	items := make([]Release, 0, len(s.releases))
	for _, release := range s.releases {
		items = append(items, release)
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) currentUpgrade(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	record := s.jobs[s.current]
	if record == nil {
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"job": nil})
		return
	}
	job, token := record.Job, record.WatchToken
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"job": job, "watchToken": token})
}
func (s *Server) watchUpgrade(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	record := s.jobs[r.PathValue("jobID")]
	if record == nil || !equalSecret(r.Header.Get("X-Upgrade-Watch-Token"), record.WatchToken) {
		s.mu.Unlock()
		failHTTP(w, 404, "升级任务不存在或令牌无效")
		return
	}
	job := record.Job
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"job": job})
}

func readJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("unexpected JSON suffix")
	}
	return nil
}
