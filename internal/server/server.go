package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/galq/blackflame/internal/auth"
	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/discovery"
	"github.com/galq/blackflame/internal/events"
	"github.com/galq/blackflame/internal/icons"
	"github.com/galq/blackflame/internal/metrics"
	"github.com/galq/blackflame/internal/runtime"
	"github.com/galq/blackflame/internal/status"
	"github.com/galq/blackflame/internal/store"
	"github.com/galq/blackflame/internal/version"
	"github.com/galq/blackflame/internal/weather"
	"github.com/galq/blackflame/internal/widgets"
)

type App struct {
	Mode      config.Mode
	Store     config.Store
	DB        *sql.DB
	Runtime   runtime.Store
	Auth      *auth.Manager
	Icons     *icons.Service
	Status    *status.Manager
	Discovery *discovery.Manager
	Weather   *weather.Client
	Widgets   *widgets.Manager
	Events    *events.Hub
	Metrics   *metrics.Registry
	Logger    *slog.Logger
	Version   version.Info
	Started   time.Time

	configCacheMu      sync.Mutex
	configCache        *config.Config
	configCacheSource  *config.Config
	configCacheExpires time.Time
}

func (a *App) Config(ctx context.Context) *config.Config {
	a.configCacheMu.Lock()
	defer a.configCacheMu.Unlock()

	source := a.Store.Snapshot().Config
	if source == nil {
		return nil
	}
	mergeDiscovery := a.Mode == config.ModeGitOps && a.DB != nil
	now := time.Now()
	if a.configCacheSource == source && (!mergeDiscovery || now.Before(a.configCacheExpires)) {
		return a.configCache
	}

	cfg := source
	if mergeDiscovery {
		items, err := store.LoadDiscoveredItems(ctx, a.DB)
		if err != nil {
			a.Logger.Warn("load discovered items", "error", err)
			return decorateKeys(source)
		}
		cfg = store.MergeDiscovered(source, items)
	}

	a.configCache = decorateKeys(cfg)
	a.configCacheSource = source
	a.configCacheExpires = now.Add(time.Second)
	return a.configCache
}

func decorateKeys(cfg *config.Config) *config.Config {
	decorated := *cfg
	decorated.Boards = slices.Clone(cfg.Boards)
	for bi := range decorated.Boards {
		board := &decorated.Boards[bi]
		board.Sections = slices.Clone(board.Sections)
		for si := range board.Sections {
			section := &board.Sections[si]
			section.Items = slices.Clone(section.Items)
			for ii := range section.Items {
				item := &section.Items[ii]
				item.StatusKey = item.Key(board.Slug)
			}
		}
	}
	return &decorated
}

func (a *App) Reload(ctx context.Context) error {
	a.configCacheMu.Lock()
	defer a.configCacheMu.Unlock()
	a.configCache = nil
	a.configCacheSource = nil
	return a.Store.Reload(ctx)
}

type Server struct {
	app             *App
	basePath        string
	httpServer      *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
	baseCancel      context.CancelFunc
}

func New(app *App, listen, basePath string, shutdownTimeout time.Duration) *Server {
	server := &Server{
		app:             app,
		basePath:        normalizeBasePath(basePath),
		shutdownTimeout: shutdownTimeout,
		logger:          app.Logger,
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	server.baseCancel = baseCancel
	server.httpServer = &http.Server{
		Addr:              listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE connections are long-lived.
		IdleTimeout:       120 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return baseCtx
		},
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	handler := s.securityHeaders(s.recoverer(s.logging(s.metricsMiddleware(mux))))
	return withBasePath(s.basePath, handler)
}

func (s *Server) Run(ctx context.Context) error {
	defer s.baseCancel()
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", "address", s.httpServer.Addr, "basePath", s.basePath)
		errCh <- s.httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		// Cancel request contexts first so long-lived SSE handlers return
		// instead of holding graceful shutdown open until the deadline.
		s.baseCancel()
		s.logger.Info("shutting down http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Warn("graceful shutdown timed out; forcing close", "error", err)
			_ = s.httpServer.Close()
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /custom.css", s.handleCustomCSS)

	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)
	mux.HandleFunc("GET /api/v1/weather", s.handleWeather)
	mux.HandleFunc("GET /api/v1/widgets", s.handleWidgets)
	mux.HandleFunc("GET /api/v1/icons", s.handleIconList)
	mux.HandleFunc("POST /api/v1/icons", s.handleIconUpload)
	mux.HandleFunc("GET /icons/{name}", s.app.Icons.ServeHTTP)

	mux.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	mux.HandleFunc("POST /api/v1/auth/setup", s.handleSetup)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)

	mux.HandleFunc("POST /api/v1/boards", s.handleCreateBoard)
	mux.HandleFunc("PATCH /api/v1/boards/{id}", s.handleUpdateBoard)
	mux.HandleFunc("DELETE /api/v1/boards/{id}", s.handleDeleteBoard)
	mux.HandleFunc("POST /api/v1/sections", s.handleCreateSection)
	mux.HandleFunc("PATCH /api/v1/sections/{id}", s.handleUpdateSection)
	mux.HandleFunc("DELETE /api/v1/sections/{id}", s.handleDeleteSection)
	mux.HandleFunc("POST /api/v1/items", s.handleCreateItem)
	mux.HandleFunc("PATCH /api/v1/items/{id}", s.handleUpdateItem)
	mux.HandleFunc("DELETE /api/v1/items/{id}", s.handleDeleteItem)
	mux.HandleFunc("POST /api/v1/reorder/items", s.handleReorderItems)
	mux.HandleFunc("POST /api/v1/reorder/sections", s.handleReorderSections)
	mux.HandleFunc("PATCH /api/v1/settings", s.handleUpdateSettings)
	mux.HandleFunc("GET /api/v1/admin/settings", s.handleAdminSettings)
	mux.HandleFunc("POST /api/v1/import/flame", s.handleImportFlame)
	mux.HandleFunc("GET /api/v1/export", s.handleExport)
	mux.HandleFunc("POST /api/v1/discovery/refresh", s.handleDiscoveryRefresh)

	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		mux.HandleFunc(method+" /api/", func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "API endpoint not found")
		})
	}
	mux.HandleFunc("GET /", s.handleSPA)
}

func normalizeBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return strings.TrimRight(value, "/")
}

func withBasePath(base string, next http.Handler) http.Handler {
	if base == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == base {
			http.Redirect(w, r, base+"/", http.StatusMovedPermanently)
			return
		}
		if !strings.HasPrefix(r.URL.Path, base+"/") {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = strings.TrimPrefix(r.URL.Path, base)
		if clone.URL.Path == "" {
			clone.URL.Path = "/"
		}
		next.ServeHTTP(w, clone)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := requestID()
		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		s.logger.Info("http request",
			"requestId", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"durationMs", time.Since(start).Milliseconds(),
		)
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		s.app.Metrics.IncHTTP(r.Method, recorder.status)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic serving request", "panic", recovered, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		if isSecureRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func requestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
