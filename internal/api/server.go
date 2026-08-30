package api

import (
	"bytes"
	"context"
	"crypto/subtle"
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
	mux.HandleFunc("POST /v1/accounts/{id}/scan", s.scan)
	mux.HandleFunc("GET /v1/decisions", s.decisions)
	mux.HandleFunc("POST /v1/reviews", s.review)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("GET /v1/events", s.events)
	s.http = &http.Server{Handler: s.security(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
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
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not accepted", http.StatusForbidden)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": "0.1.0"})
}
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Summary(r.Context())
	respond(w, value, err)
}
func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.Accounts(r.Context())
	respond(w, value, err)
}
func (s *Server) saveAccount(w http.ResponseWriter, r *http.Request) {
	var req service.SaveAccountRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	value, err := s.service.SaveAccount(r.Context(), req)
	respond(w, value, err)
}
func (s *Server) testAccount(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	value, err := s.service.TestAccount(ctx, r.PathValue("id"))
	respond(w, value, err)
}
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	value, err := s.service.Scan(ctx, r.PathValue("id"))
	respond(w, value, err)
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
	respond(w, value, err)
}
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	var req domain.ReviewRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	err := s.service.Review(r.Context(), req)
	respond(w, map[string]bool{"ok": err == nil}, err)
}
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, err := s.service.Models(ctx)
	respond(w, value, err)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
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
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSONStatus(w, status, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, status int, value any) { writeJSONStatus(w, status, value) }
func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
