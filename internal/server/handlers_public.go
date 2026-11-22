package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/discovery"
	"github.com/galq/blackflame/internal/events"
	"github.com/galq/blackflame/internal/icons"
	statuspkg "github.com/galq/blackflame/internal/status"
	"github.com/galq/blackflame/internal/version"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	snapshot := s.app.Store.Snapshot()
	response := map[string]any{
		"status": "ok",
	}
	code := http.StatusOK
	if snapshot.Config == nil {
		response["status"] = "unavailable"
		response["error"] = "config is not loaded"
		code = http.StatusServiceUnavailable
	}
	if snapshot.Err != nil {
		// Load errors can contain filesystem paths; keep the detail for
		// authenticated callers only.
		if s.sessionFromRequest(r.Context(), r) != nil {
			response["configError"] = snapshot.Err.Error()
		} else {
			response["configError"] = true
		}
		if code == http.StatusOK {
			response["status"] = "degraded"
		}
	}
	if err := s.app.Runtime.Ping(r.Context()); err != nil {
		response["status"] = "unavailable"
		response["error"] = "runtime store is unavailable"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, response)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.app.Metrics.WriteBase(w, s.app.Version.Version, string(s.app.Mode))
	discoveryStatus := s.app.Discovery.Status()
	fmt.Fprintln(w, "# HELP blackflame_discovery_last_success_timestamp Last successful discovery sync.")
	fmt.Fprintln(w, "# TYPE blackflame_discovery_last_success_timestamp gauge")
	fmt.Fprintln(w, "# HELP blackflame_discovery_items Items returned by the last discovery sync.")
	fmt.Fprintln(w, "# TYPE blackflame_discovery_items gauge")
	for _, entry := range []struct {
		source string
		status discovery.SourceStatus
	}{
		{"docker", discoveryStatus.Docker},
		{"kubernetes", discoveryStatus.Kubernetes},
	} {
		if !entry.status.LastSync.IsZero() {
			fmt.Fprintf(w, "blackflame_discovery_last_success_timestamp{source=%q} %d\n", entry.source, entry.status.LastSync.Unix())
		}
		fmt.Fprintf(w, "blackflame_discovery_items{source=%q} %d\n", entry.source, entry.status.Count)
	}
	results := s.app.Status.ResultsFor(s.app.Config(r.Context()).PublicSnapshot())
	keys := make([]string, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, "# HELP blackflame_item_up Whether the last check succeeded.")
	fmt.Fprintln(w, "# TYPE blackflame_item_up gauge")
	for _, key := range keys {
		up := 0
		if results[key].Up {
			up = 1
		}
		fmt.Fprintf(w, "blackflame_item_up{item=%q} %d\n", key, up)
	}
	fmt.Fprintln(w, "# HELP blackflame_item_latency_seconds Last observed latency.")
	fmt.Fprintln(w, "# TYPE blackflame_item_latency_seconds gauge")
	for _, key := range keys {
		fmt.Fprintf(w, "blackflame_item_latency_seconds{item=%q} %f\n", key, float64(results[key].LatencyMS)/1000)
	}
	fmt.Fprintln(w, "# HELP blackflame_item_cert_expiry_timestamp TLS certificate expiry as a Unix timestamp.")
	fmt.Fprintln(w, "# TYPE blackflame_item_cert_expiry_timestamp gauge")
	for _, key := range keys {
		if results[key].CertExpiry != nil {
			fmt.Fprintf(w, "blackflame_item_cert_expiry_timestamp{item=%q} %d\n", key, results[key].CertExpiry.Unix())
		}
	}
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.app.Version,
		"mode":    s.app.Mode,
	})
}

type bootstrapResponse struct {
	Mode          config.Mode                 `json:"mode"`
	Version       version.Info                `json:"version"`
	ReadOnly      bool                        `json:"readOnly"`
	Editable      bool                        `json:"editable"`
	Authenticated bool                        `json:"authenticated"`
	SetupRequired bool                        `json:"setupRequired"`
	CSRF          string                      `json:"csrf,omitempty"`
	Config        *config.Config              `json:"config"`
	ConfigError   string                      `json:"configError,omitempty"`
	LoadedAt      time.Time                   `json:"loadedAt"`
	Status        map[string]statuspkg.Result `json:"status"`
	Discovery     discovery.Status            `json:"discovery,omitempty"`
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	snapshot := s.app.Store.Snapshot()
	cfg := s.app.Config(r.Context())
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	session, authenticated, setupRequired := s.authState(r.Context(), r)
	visible := cfg
	if !authenticated {
		visible = cfg.PublicSnapshot()
	}
	response := bootstrapResponse{
		Mode:          s.app.Mode,
		Version:       s.app.Version,
		ReadOnly:      s.app.Store.ReadOnly(),
		Editable:      authenticated && !s.app.Store.ReadOnly(),
		Authenticated: authenticated,
		SetupRequired: setupRequired,
		Config:        visible,
		LoadedAt:      snapshot.LoadedAt,
		Status:        s.app.Status.ResultsFor(visible),
	}
	if session != nil {
		response.CSRF = session.CSRF
	}
	if snapshot.Err != nil {
		// Load errors can contain filesystem paths; keep the detail for
		// authenticated callers only.
		if authenticated {
			response.ConfigError = snapshot.Err.Error()
		} else {
			response.ConfigError = "configuration failed to load"
		}
	}
	if authenticated {
		response.Discovery = s.app.Discovery.Status()
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, authenticated, _ := s.authState(r.Context(), r)
	cfg := s.app.Config(r.Context())
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	if !authenticated {
		cfg = cfg.PublicSnapshot()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": s.app.Status.ResultsFor(cfg),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unsupported", "streaming is not supported")
		return
	}
	ctx := r.Context()
	authenticated := s.sessionFromRequest(ctx, r) != nil
	cfg := s.app.Config(ctx)
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	if !authenticated {
		cfg = cfg.PublicSnapshot()
	}

	ch, cancel := s.app.Events.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	initial, _ := json.Marshal(events.Event{Type: "status", Data: s.app.Status.ResultsFor(cfg)})
	fmt.Fprintf(w, "data: %s\n\n", initial)
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case payload, ok := <-ch:
			if !ok {
				return
			}
			var event struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(payload, &event); err != nil {
				continue
			}
			if authenticated {
				authenticated = s.sessionFromRequest(ctx, r) != nil
			}
			if event.Type == "status" {
				var result statuspkg.Result
				if err := json.Unmarshal(event.Data, &result); err != nil {
					continue
				}
				if !statusVisible(s.app.Config(ctx), result.Key, authenticated) {
					continue
				}
			} else if !authenticated {
				// Default-deny: no other event type has a visibility
				// filter today, so anonymous streams only ever receive
				// status events rather than whatever a future publisher
				// emits.
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func statusVisible(cfg *config.Config, key string, authenticated bool) bool {
	if cfg == nil {
		return false
	}
	for _, board := range cfg.Boards {
		if !authenticated && !board.IsPublic() {
			continue
		}
		for _, section := range board.Sections {
			if !authenticated && !section.IsPublic() {
				continue
			}
			for _, item := range section.Items {
				if (authenticated || (item.IsPublic() && !item.Hidden)) && item.Key(board.Slug) == key {
					return true
				}
			}
		}
	}
	return false
}

func (s *Server) handleWeather(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config(r.Context())
	if cfg == nil || !cfg.Weather.Enabled {
		writeError(w, http.StatusNotFound, "weather_disabled", "weather is not configured")
		return
	}
	data, err := s.app.Weather.Get(r.Context(), cfg.Weather.Latitude, cfg.Weather.Longitude, cfg.Weather.Units, cfg.Weather.Location)
	if err != nil {
		writeError(w, http.StatusBadGateway, "weather_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleWidgets(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config(r.Context())
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	_, authenticated, _ := s.authState(r.Context(), r)
	if s.app.Widgets == nil {
		writeJSON(w, http.StatusOK, map[string]any{"widgets": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"widgets": s.app.Widgets.Cards(r.Context(), cfg.Widgets, !authenticated),
	})
}

func (s *Server) handleIconList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"icons": icons.Curated()})
}

func (s *Server) handleIconUpload(w http.ResponseWriter, r *http.Request) {
	session := s.requireAdminMutation(w, r)
	if session == nil {
		return
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid multipart upload")
		return
	}
	file, header, err := r.FormFile("icon")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_icon", "icon file is required")
		return
	}
	defer file.Close()
	name, err := s.app.Icons.Upload(r.Context(), header.Filename, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_icon", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (s *Server) handleCustomCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	cfg := s.app.Config(r.Context())
	if cfg != nil {
		_, _ = w.Write([]byte(cfg.Theme.CustomCSS))
	}
}
