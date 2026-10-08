// Package app wires dependencies and manages the HTTP server lifecycle.
package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"voex-server/internal/config"
	"voex-server/internal/database"
	"voex-server/internal/filestore"
	"voex-server/internal/router"
	"voex-server/internal/security"
	"voex-server/internal/service"
)

func Run() (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("initialization failed (%T)", v)
		}
	}()
	log.SetOutput(os.Stderr)
	cfg := config.Read()
	db, err := database.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("database initialization failed (%T)", err)
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	files := filestore.New(cfg.StaticDir, cfg.UploadDir)
	business := service.New(db, files, security.LoadSecret(cfg.SecretFile))
	routes := router.New(business, cfg.Origins, log.New(os.Stdout, "", log.LstdFlags|log.LUTC))
	server := &http.Server{Addr: cfg.Address, Handler: routes, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stopped)
	done := make(chan error, 1)
	go func() { log.Printf("[Server] Gin HTTP listening on %s", server.Addr); done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
		return nil
	case sig := <-stopped:
		log.Printf("[Server] shutdown requested: %s", sig)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		server.Close()
		log.Print("[Server] shutdown deadline reached")
	}
	log.Print("[Server] HTTP listener closed")
	return nil
}
