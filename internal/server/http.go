package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emresahna/heimdall/internal/storage"
	"github.com/emresahna/heimdall/web"
)

type HttpServer struct {
	db *storage.DB
}

func NewHttpServer(db *storage.DB) *HttpServer {
	return &HttpServer{db: db}
}

func (s *HttpServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.Handle("/", http.FileServer(http.FS(web.FS)))
	return mux
}

func (s *HttpServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *HttpServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	now := time.Now()
	from := parseTime(query.Get("from"), now.Add(-15*time.Minute))
	to := parseTime(query.Get("to"), now)
	if from.After(to) {
		from, to = to, from
	}

	limit := parseInt(query.Get("limit"), 200)
	if limit > 1000 {
		limit = 1000
	}
	offset := parseInt(query.Get("offset"), 0)

	var status *uint32
	if raw := query.Get("status"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 32); err == nil {
			val := uint32(parsed)
			status = &val
		}
	}

	filter := storage.QueryFilter{
		From:      from,
		To:        to,
		Limit:     limit,
		Offset:    offset,
		Method:    strings.ToUpper(query.Get("method")),
		Status:    status,
		Namespace: query.Get("namespace"),
		Pod:       query.Get("pod"),
		Path:      query.Get("path"),
	}

	entries, err := s.db.QueryLogs(r.Context(), filter)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}

	response := struct {
		Entries any `json:"entries"`
	}{
		Entries: entries,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func parseTime(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	if num, err := strconv.ParseInt(value, 10, 64); err == nil {
		if num > 1e12 {
			return time.UnixMilli(num)
		}
		return time.Unix(num, 0)
	}
	return fallback
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < 0 {
		return fallback
	}
	return parsed
}
