package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/robince/somascope/internal/config"
	"github.com/robince/somascope/internal/oura"
	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/settings"
	"github.com/robince/somascope/internal/store"
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
	settings   *settings.Store
	store      *store.Store
	oura       *oura.Client
	syncs      *providersync.Manager
	mux        *http.ServeMux
}

func New(cfg config.Config, appStore *store.Store, version VersionInfo) (*Server, error) {
	dist, err := web.Assets()
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:        cfg,
		version:    version,
		spaFS:      dist,
		spaHandler: http.FileServerFS(dist),
		settings:   settings.NewStore(appStore),
		store:      appStore,
		oura:       oura.NewClient(nil),
		mux:        http.NewServeMux(),
	}
	syncs, err := providersync.NewManager(appStore)
	if err != nil {
		return nil, err
	}
	s.syncs = syncs
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
	s.mux.HandleFunc("GET /api/v1/dashboard/overview", s.handleDashboardOverview)
	s.mux.HandleFunc("GET /api/v1/export/formats", s.handleExportFormats)
	s.mux.HandleFunc("GET /api/v1/export/canonical", s.handleExportCanonical)
	s.mux.HandleFunc("GET /api/v1/export/raw", s.handleExportRaw)
	s.mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/v1/settings", s.handlePutSettings)
	s.mux.HandleFunc("GET /api/v1/providers/oura/status", s.handleOuraStatus)
	s.mux.HandleFunc("GET /api/v1/providers/oura/recent", s.handleOuraRecent)
	s.mux.HandleFunc("POST /api/v1/providers/oura/auth/start", s.handleOuraAuthStart)
	s.mux.HandleFunc("GET /oauth/oura/callback", s.handleOuraCallback)
	s.mux.HandleFunc("POST /api/v1/providers/oura/sync", s.handleOuraSync)
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
		"name":           "somascope",
		"auth_mode":      s.cfg.AuthMode,
		"data_dir":       s.cfg.DataDir,
		"db_path":        s.cfg.DBPath,
		"schema_version": s.mustSchemaVersion(),
		"version":        s.version,
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
			"daily_records",
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

func (s *Server) handleExportCanonical(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("local store unavailable"))
		return
	}

	rows, err := s.store.CanonicalExportRows(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	format := r.URL.Query().Get("format")
	switch format {
	case "", "jsonl":
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		for _, row := range rows {
			if err := json.NewEncoder(w).Encode(row); err != nil {
				log.Printf("warning: failed writing canonical JSONL row: %v", err)
				return
			}
		}
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(w)
		if err := writer.Write([]string{
			"record_type", "provider", "record_kind", "local_date", "zone_offset", "source_device",
			"external_id", "start_time", "end_time", "duration_minutes", "time_in_bed_minutes",
			"efficiency_percent", "is_nap", "summary_json", "stages_json", "metrics_json", "raw_document_id",
		}); err != nil {
			log.Printf("warning: failed writing canonical CSV header: %v", err)
			return
		}
		for _, row := range rows {
			if err := writer.Write([]string{
				row.RecordType,
				row.Provider,
				row.RecordKind,
				row.LocalDate,
				row.ZoneOffset,
				row.SourceDevice,
				row.ExternalID,
				row.StartTime,
				row.EndTime,
				intPtrString(row.DurationMinutes),
				intPtrString(row.TimeInBedMinutes),
				floatPtrString(row.EfficiencyPercent),
				boolString(row.IsNap),
				string(row.Summary),
				string(row.Stages),
				string(row.Metrics),
				int64PtrString(row.RawDocumentID),
			}); err != nil {
				log.Printf("warning: failed writing canonical CSV row: %v", err)
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			log.Printf("warning: failed flushing canonical CSV: %v", err)
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported export format %q", format))
	}
}

func (s *Server) handleExportRaw(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, fmt.Errorf("local store unavailable"))
		return
	}

	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing provider"))
		return
	}

	startDate, err := exportDateParam(r, "start_date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	endDate, err := exportDateParam(r, "end_date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if startDate != "" && endDate != "" && startDate > endDate {
		writeError(w, http.StatusBadRequest, fmt.Errorf("start_date must be on or before end_date"))
		return
	}

	rows, err := s.store.RawExportRows(r.Context(), provider, store.RawExportFilter{
		StartDate:     startDate,
		EndDate:       endDate,
		DocumentKinds: exportListParam(r, "document_kind"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	format := r.URL.Query().Get("format")
	switch format {
	case "", "jsonl":
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		for _, row := range rows {
			if err := json.NewEncoder(w).Encode(row); err != nil {
				log.Printf("warning: failed writing raw JSONL row: %v", err)
				return
			}
		}
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(w)
		if err := writer.Write([]string{
			"record_type", "provider", "document_kind", "local_date", "zone_offset",
			"external_id", "request_path", "request_query", "request_start", "request_end",
			"fetched_at", "document_key", "raw_document_id", "payload_json",
		}); err != nil {
			log.Printf("warning: failed writing raw CSV header: %v", err)
			return
		}
		for _, row := range rows {
			if err := writer.Write([]string{
				row.RecordType,
				row.Provider,
				row.DocumentKind,
				row.LocalDate,
				row.ZoneOffset,
				row.ExternalID,
				row.RequestPath,
				row.RequestQuery,
				row.RequestStart,
				row.RequestEnd,
				row.FetchedAt,
				row.DocumentKey,
				fmt.Sprintf("%d", row.RawDocumentID),
				string(row.Payload),
			}); err != nil {
				log.Printf("warning: failed writing raw CSV row: %v", err)
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			log.Printf("warning: failed flushing raw CSV: %v", err)
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported export format %q", format))
	}
}

func exportDateParam(r *http.Request, key string) (string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("%s must use YYYY-MM-DD", key)
	}
	return value, nil
}

func exportListParam(r *http.Request, key string) []string {
	values := r.URL.Query()[key]
	if len(values) == 0 {
		return nil
	}

	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	value, err := s.settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var next settings.Settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	value, err := s.settings.Update(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
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
	name := "index.html"
	if err != nil {
		file, err = s.spaFS.Open("stub.html")
		name = "stub.html"
	}
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
	if _, err := w.Write(data); err != nil {
		log.Printf("warning: failed writing %s response: %v", name, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("warning: failed encoding JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"ok":    false,
		"error": err.Error(),
	})
}

func (s *Server) mustSchemaVersion() int {
	if s.store == nil {
		return 0
	}
	version, err := s.store.SchemaVersion(context.Background())
	if err != nil {
		log.Printf("warning: failed reading schema version: %v", err)
		return 0
	}
	return version
}

func intPtrString(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func int64PtrString(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func floatPtrString(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%.3f", *value)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
