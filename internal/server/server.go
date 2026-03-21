package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/robince/somascope/internal/config"
	"github.com/robince/somascope/internal/web"
)

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

type Server struct {
	cfg        config.Config
	version    VersionInfo
	spaFS      fs.FS
	spaHandler http.Handler
	mux        *http.ServeMux
}

func New(cfg config.Config, version VersionInfo) (*Server, error) {
	dist, err := web.Assets()
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:        cfg,
		version:    version,
		spaFS:      dist,
		spaHandler: http.FileServerFS(dist),
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/v1/app", s.handleApp)
	s.mux.HandleFunc("GET /api/v1/spec", s.handleSpec)
	s.mux.HandleFunc("GET /api/v1/export/formats", s.handleExportFormats)
	s.mux.Handle("/", http.HandlerFunc(s.handleSPA))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "somascope",
	})
}

func (s *Server) handleApp(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      "somascope",
		"auth_mode": s.cfg.AuthMode,
		"data_dir":  s.cfg.DataDir,
		"db_path":   s.cfg.DBPath,
		"version":   s.version,
	})
}

func (s *Server) handleSpec(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"v1_scope": []string{
			"single-user local app",
			"fitbit and oura",
			"daily summary first",
			"raw and structured export",
			"embedded frontend for distribution",
		},
		"planned_auth_modes": []string{
			string(config.AuthModeBYO),
			string(config.AuthModeBrokered),
		},
		"canonical_entities": []string{
			"connections",
			"provider_credentials",
			"sync_state",
			"daily_facts",
			"sleep_sessions",
			"metric_definitions",
			"raw_documents",
		},
	})
}

func (s *Server) handleExportFormats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{
			{
				"id":          "raw-json",
				"label":       "Raw JSON",
				"description": "Provider-native payloads as fetched",
				"status":      "planned",
			},
			{
				"id":          "canonical-jsonl",
				"label":       "Canonical JSONL",
				"description": "Structured export with normalized metrics and provenance",
				"status":      "planned",
			},
			{
				"id":          "canonical-csv",
				"label":       "Canonical CSV",
				"description": "Spreadsheet-friendly daily and session exports",
				"status":      "planned",
			},
		},
	})
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		s.serveIndex(w)
		return
	}

	if _, err := fs.Stat(s.spaFS, path); err == nil {
		s.spaHandler.ServeHTTP(w, r)
		return
	}

	s.serveIndex(w)
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	file, err := s.spaFS.Open("index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read index.html", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
