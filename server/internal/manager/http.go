package manager

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jeni0101/vpn/server/internal/model"
	"github.com/jeni0101/vpn/server/internal/store"
)

type HTTPServer struct {
	service *Service
}

func NewHTTPServer(service *Service) http.Handler {
	server := &HTTPServer{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/devices", server.devices)
	mux.HandleFunc("POST /v1/devices/invite", server.createInvite)
	mux.HandleFunc("POST /v1/devices/standard", server.createStandard)
	mux.HandleFunc("POST /v1/devices/{id}/revoke", server.revoke)
	mux.HandleFunc("POST /v1/devices/{id}/rotate", server.rotate)
	mux.HandleFunc("POST /v1/enrollments/claim", server.claim)
	mux.HandleFunc("POST /v1/import", server.importDevices)
	mux.HandleFunc("GET /v1/usage", server.usage)
	mux.HandleFunc("GET /v1/audit", server.audit)
	mux.HandleFunc("POST /v1/audit", server.addAudit)
	return http.MaxBytesHandler(mux, 128*1024)
}

func (s *HTTPServer) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "personal-vpn-managerd"})
}

func (s *HTTPServer) devices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.service.Devices(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *HTTPServer) createInvite(w http.ResponseWriter, r *http.Request) {
	var request CreateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.CreateInvite(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *HTTPServer) createStandard(w http.ResponseWriter, r *http.Request) {
	var request CreateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := s.service.CreateStandard(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *HTTPServer) claim(w http.ResponseWriter, r *http.Request) {
	var request ClaimRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	device, err := s.service.Claim(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ClaimResult{
		Device: device, Configuration: s.service.Settings(),
	})
}

func (s *HTTPServer) revoke(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing device id"})
		return
	}
	if err := s.service.Revoke(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) rotate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing device id"})
		return
	}
	result, err := s.service.CreateRotationInvite(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *HTTPServer) importDevices(w http.ResponseWriter, r *http.Request) {
	var request ImportRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Devices) == 0 || len(request.Devices) > 245 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid import count"})
		return
	}
	if err := s.service.Import(r.Context(), request); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) usage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	bucket := query.Get("bucket")
	if bucket == "" {
		bucket = "hour"
	}
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now.Add(time.Hour)
	var err error
	if value := query.Get("from"); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from"})
			return
		}
	}
	if value := query.Get("to"); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to"})
			return
		}
	}
	if !to.After(from) || to.Sub(from) > 6*365*24*time.Hour {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid range"})
		return
	}
	points, err := s.service.Usage(r.Context(), query.Get("device_id"), bucket, from, to)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": points, "bucket": bucket})
}

func (s *HTTPServer) audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.service.Audit(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *HTTPServer) addAudit(w http.ResponseWriter, r *http.Request) {
	var event model.AuditEvent
	if !decodeJSON(w, r, &event) {
		return
	}
	if len(event.Action) > 80 || len(event.Detail) > 500 || event.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid audit event"})
		return
	}
	if err := s.service.AddAudit(r.Context(), event); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request must contain one JSON object"})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "internal error"
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, message = http.StatusNotFound, "not found"
	case errors.Is(err, store.ErrConflict):
		status, message = http.StatusConflict, "conflict"
	case errors.Is(err, store.ErrExpired):
		status, message = http.StatusGone, "expired or already used"
	default:
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "invalid") || strings.Contains(lower, "unsupported") ||
			strings.Contains(lower, "must be") || strings.Contains(lower, "only active") {
			status, message = http.StatusBadRequest, err.Error()
		}
		if strings.Contains(lower, "read-only") {
			status, message = http.StatusServiceUnavailable, "manager is in read-only mode"
		}
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
