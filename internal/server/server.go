package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"notify-me/internal/config"
	"notify-me/internal/dispatcher"
	"notify-me/internal/storage"
)

// HistoryStore is the subset of storage.Storage we need.
type HistoryStore interface {
	Insert(ctx context.Context, r storage.Record) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status string, resolvedAt int64) error
	List(ctx context.Context, f storage.ListFilter) ([]storage.Record, int, error)
	Delete(ctx context.Context, id int64) error
	DeleteAll(ctx context.Context) error
}

type Server struct {
	cfg       *config.Config
	disp      *dispatcher.Dispatcher
	db        HistoryStore
	log       zerolog.Logger
	srv       *http.Server
	OnResolve func() // called after _resolve to close the popup window
}

func New(cfg *config.Config, d *dispatcher.Dispatcher, db HistoryStore, log zerolog.Logger) *Server {
	return &Server{cfg: cfg, disp: d, db: db, log: log}
}

// Handler builds the HTTP routing tree. Endpoint paths, prefix, auth token,
// and behavior (timeout, timeout action) are frozen at construction time;
// changes to those fields require restarting the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	snap := s.cfg.Snapshot()
	prefix := strings.TrimRight(snap.Server.EndpointPrefix, "/")
	for _, ep := range snap.Endpoints {
		mux.Handle(path.Join(prefix, ep.Path), s.endpointHandler(ep))
	}
	// Internal resolve endpoint for popup — no auth, not wrapped in tokenMiddleware.
	mux.Handle(path.Join(prefix, "_resolve"), http.HandlerFunc(s.resolveHandler))
	// Internal history API for main window — no auth, local-only.
	mux.Handle(path.Join(prefix, "_history"), http.HandlerFunc(s.historyHandler))
	mux.Handle(path.Join(prefix, "_history/delete"), http.HandlerFunc(s.historyDeleteHandler))
	mux.Handle(path.Join(prefix, "_history/clear"), http.HandlerFunc(s.historyClearHandler))
	// Wrap only the notification endpoints with auth, not internal endpoints.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, path.Join(prefix, "_")) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			mux.ServeHTTP(w, r)
			return
		}
		tokenMiddleware(snap.Server.AuthToken, mux).ServeHTTP(w, r)
	})
}

// resolveHandler is an internal endpoint used by the popup window to submit
// the user's decision. It bypasses auth since it's called from the local popup.
func (s *Server) resolveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	decision := r.URL.Query().Get("decision")
	if idStr == "" || decision == "" {
		http.Error(w, "missing id or decision", http.StatusBadRequest)
		return
	}
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s.disp.Resolve(id, dispatcher.Result{Decision: decision})
	if s.OnResolve != nil {
		s.OnResolve()
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) historyHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	search := q.Get("search")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	recs, total, err := s.db.List(r.Context(), storage.ListFilter{
		Status: status,
		Search: search,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"records": recs,
		"total":   total,
	})
}

func (s *Server) historyDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.db.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) historyClearHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.db.DeleteAll(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	s.srv = &http.Server{Handler: s.Handler()}
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error().Err(err).Msg("http server stopped")
		}
	}()
	s.log.Info().Str("addr", addr).Msg("http server up")
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func (s *Server) endpointHandler(ep config.EndpointConfig) http.Handler {
	snap := s.cfg.Snapshot()
	defaultTimeoutSeconds := snap.Behavior.DefaultTimeoutSeconds
	timeoutAct := snap.Behavior.TimeoutAction
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.disp.IsPaused() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("paused"))
			return
		}
		// Cap request body at 64KB; oversize -> 413 (loud, not silent truncation).
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		n, err := parseRequest(r, ep, defaultTimeoutSeconds)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		n.TimeoutAct = timeoutAct

		id, err := s.db.Insert(r.Context(), storage.Record{
			Endpoint:     n.Endpoint,
			Title:        n.Title,
			Message:      n.Message,
			SourceIP:     n.SourceIP,
			SourceHeader: n.SourceHdr,
			Status:       "pending",
			CreatedAt:    time.Now().UnixMilli(),
		})
		if err != nil {
			s.log.Error().Err(err).Str("endpoint", n.Endpoint).Msg("storage insert failed")
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		n.ID = id

		if err := s.disp.Enqueue(n); err != nil {
			if err == dispatcher.ErrQueueFull {
				s.log.Warn().Int64("id", id).Msg("queue full, rejecting request")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("queue full"))
				_ = s.db.UpdateStatus(r.Context(), id, "cancelled", time.Now().UnixMilli())
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Block until result, timeout, or client disconnect.
		select {
		case res := <-n.ResultCh:
			_ = s.db.UpdateStatus(r.Context(), id, res.Decision, time.Now().UnixMilli())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(res.Decision))
		case <-r.Context().Done():
			s.disp.Cancel(n.ID)
			// Pull the authoritative decision. Resolve is once-guarded and ResultCh
			// is buffered cap 1, so this read is guaranteed non-blocking.
			res := <-n.ResultCh
			_ = s.db.UpdateStatus(context.Background(), id, res.Decision, time.Now().UnixMilli())
			// No HTTP response — client is gone.
		}
	})
}
