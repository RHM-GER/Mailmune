package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/service"
	"github.com/RHM-GER/Mailmune/internal/store"
)

type Server struct {
	http     *http.Server
	listener net.Listener
	token    string
	service  *service.Service
}

func New(token string, svc *service.Service) (*Server, error) {
	if len(token) < 32 {
		return nil, errors.New("session token must contain at least 32 characters")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{listener: listener, token: token, service: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/summary", s.summary)
	mux.HandleFunc("GET /v1/accounts", s.accounts)
	mux.HandleFunc("POST /v1/accounts", s.saveAccount)
	mux.HandleFunc("POST /v1/accounts/{id}/test", s.testAccount)
	// /scan stays as a compatibility alias for starting a background run.
	mux.HandleFunc("POST /v1/accounts/{id}/scan", s.startScan)
	mux.HandleFunc("POST /v1/accounts/{id}/scans", s.startScan)
	mux.HandleFunc("POST /v1/accounts/{id}/scans/cancel", s.cancelScan)
	mux.HandleFunc("GET /v1/accounts/{id}/scans", s.scanRuns)
	mux.HandleFunc("GET /v1/accounts/{id}/calibration", s.calibration)
	mux.HandleFunc("GET /v1/decisions", s.decisions)
	mux.HandleFunc("POST /v1/reviews", s.review)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("GET /v1/models/recommended", s.recommendedModels)
	mux.HandleFunc("POST /v1/models/capability", s.capabilityTest)
	mux.HandleFunc("POST /v1/accounts/{id}/models", s.setAccountModel)
	mux.HandleFunc("POST /v1/accounts/{id}/models/validate", s.validateAccountModel)
	mux.HandleFunc("GET /v1/events", s.events)
	// The event stream must never be cut off by the global write timeout.
	s.http = &http.Server{Handler: s.security(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
	return s, nil
}

func (s *Server) Address() string { return "http://" + s.listener.Addr().String() }
func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.listener.Addr().String() {
			writeError(w, http.StatusForbidden, "invalid_host", errors.New("invalid host"))
			return
		}
		if r.Header.Get("Origin") != "" {
			writeError(w, http.StatusForbidden, "origin_rejected", errors.New("browser origins are not accepted"))
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", errors.New("unauthorized"))
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": "0.2.0"})
}
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Summary(r.Context())
	respond(w, "summary_failed", value, err)
}
func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Accounts(r.Context())
	respond(w, "accounts_failed", value, err)
}
func (s *Server) saveAccount(w http.ResponseWriter, r *http.Request) {
	var req service.SaveAccountRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	value, err := s.service.SaveAccount(r.Context(), req)
	respond(w, "account_invalid", value, err)
}
func (s *Server) testAccount(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	value, err := s.service.TestAccount(ctx, r.PathValue("id"))
	respond(w, "account_test_failed", value, err)
}
func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resync bool `json:"resync"`
	}
	if r.ContentLength > 0 {
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err)
			return
		}
	}
	value, err := s.service.StartScan(r.Context(), r.PathValue("id"), req.Resync)
	respond(w, "scan_start_failed", value, err)
}
func (s *Server) cancelScan(w http.ResponseWriter, r *http.Request) {
	run, active, err := s.service.CancelScan(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), "scan_cancel_failed", err)
		return
	}
	if !active {
		writeError(w, http.StatusNotFound, "no_active_scan", errors.New("no scan is running for this account"))
		return
	}
	writeJSON(w, http.StatusOK, run)
}
func (s *Server) scanRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	value, err := s.service.ScanRuns(r.Context(), r.PathValue("id"), limit)
	respond(w, "scan_runs_failed", value, err)
}
func (s *Server) calibration(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.CalibrationReport(r.Context(), r.PathValue("id"))
	respond(w, "calibration_failed", value, err)
}
func (s *Server) decisions(w http.ResponseWriter, r *http.Request) {
	filter := store.DecisionFilter{Status: r.URL.Query().Get("status"), AccountID: r.URL.Query().Get("accountId"), Query: r.URL.Query().Get("q")}
	if limit, _ := strconv.Atoi(r.URL.Query().Get("limit")); limit > 0 {
		filter.Limit = limit
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		if value, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.Since = &value
		}
	}
	value, err := s.service.Decisions(r.Context(), filter)
	respond(w, "decisions_failed", value, err)
}
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	var req domain.ReviewRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	err := s.service.Review(r.Context(), req)
	respond(w, "review_failed", map[string]bool{"ok": err == nil}, err)
}
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, err := s.service.Models(ctx)
	respond(w, "models_failed", value, err)
}
func (s *Server) capabilityTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", errors.New("model is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	value, err := s.service.CapabilityTest(ctx, req.Model)
	respond(w, "capability_test_failed", value, err)
}
func (s *Server) recommendedModels(w http.ResponseWriter, r *http.Request) {
	version, models := s.service.RecommendedModels()
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "models": models})
}
func (s *Server) setAccountModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err)
		return
	}
	value, err := s.service.SetAccountModel(r.Context(), r.PathValue("id"), req.Model)
	respond(w, "set_model_failed", value, err)
}
func (s *Server) validateAccountModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	// The model is optional; an empty body validates the account's stored model.
	if r.ContentLength > 0 {
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	report, account, err := s.service.ValidateAccountModel(ctx, r.PathValue("id"), req.Model)
	if err != nil {
		writeError(w, statusFor(err), "validate_model_failed", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report, "account": account})
}

// events streams agent events as server-sent events: scan lifecycle,
// account updates and a periodic heartbeat.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable", errors.New("streaming unavailable"))
		return
	}
	subscription := s.service.Hub().Subscribe()
	defer subscription.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-subscription.Events():
			if !open {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(event.Data))
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, "event: heartbeat\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}

func decode(r *http.Request, target any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("request body exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

// respond writes value or a stable error code with a redacted message.
func respond(w http.ResponseWriter, code string, value any, err error) {
	if err != nil {
		writeError(w, statusFor(err), code, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, secrets.ErrNotFound):
		return http.StatusBadRequest
	case errors.Is(err, mailbox.ErrMoveUnsupported):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeError(w http.ResponseWriter, status int, code string, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSONStatus(w, status, map[string]string{"error": err.Error(), "code": code})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSONStatus(w, status, value)
}
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
