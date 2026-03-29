package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/robince/somascope/internal/config"
	"github.com/robince/somascope/internal/server"
	"github.com/robince/somascope/internal/store"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = ""
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	if err := cfg.EnsureLayout(); err != nil {
		log.Fatal(err)
	}
	configureLogging(cfg.LogsDir)

	db, err := store.Open(context.Background(), cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	srv, err := server.New(cfg, db, server.VersionInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
	})
	if err != nil {
		log.Fatal(err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	log.Printf("somascope listening on http://%s", addr)
	log.Printf("data dir: %s", cfg.DataDir)
	log.Printf("auth mode: %s", cfg.AuthMode)

	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

func configureLogging(logsDir string) {
	logPath := filepath.Join(logsDir, "somascope.log")
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("warning: failed opening log file %s: %v", logPath, err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stdout, file))
}
