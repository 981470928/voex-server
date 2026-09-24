package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Server struct {
	db        *sql.DB
	secret    []byte
	mux       *http.ServeMux
	origins   map[string]bool
	limits    limiter
	hashSlots chan struct{}
	dummyHash string
	accessLog *log.Logger
}

func nilContext() context.Context { return context.Background() }
func main() {
	log.SetOutput(os.Stderr)
	loadConfig()
	db, err := openDatabase()
	if err != nil {
		log.Fatalf("[DB] initialization failed (%T); check configuration and schema", err)
	}
	defer db.Close()
	initializeStorage()
	s := &Server{db: db, secret: loadSecret(), mux: http.NewServeMux(), origins: map[string]bool{}, limits: limiter{entries: map[string]limitEntry{}}, hashSlots: make(chan struct{}, 4), accessLog: log.New(os.Stdout, "", log.LstdFlags|log.LUTC)}
	s.loadOrigins()
	s.dummyHash = hashPassword(randomToken(32))
	s.registerAuthRoutes()
	s.registerTeamRoutes()
	s.registerWorkspaceRoutes()
	s.registerStorageRoutes()
	s.registerShareRoutes()
	s.route("/api/", "required", func(w http.ResponseWriter, r *http.Request) { fail(404, "接口不存在") })
	s.route("/api", "required", func(w http.ResponseWriter, r *http.Request) { fail(404, "接口不存在") })
	server := &http.Server{Addr: env("VOEX_ADDR", "127.0.0.1:8090"), Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { log.Printf("[Server] Go HTTP listening on %s", server.Addr); done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			log.Fatalf("[Server] listener failed: %v", err)
		}
		return
	case signal := <-stopped:
		log.Printf("[Server] shutdown requested: %s", signal)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		log.Print("[Server] shutdown deadline reached")
	}
	log.Print("[Server] HTTP listener closed")
}
