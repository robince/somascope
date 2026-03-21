package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/robince/somascope/internal/config"
	"github.com/robince/somascope/internal/server"
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

	srv, err := server.New(cfg, server.VersionInfo{
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
