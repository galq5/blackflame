package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
)

func testApp(t *testing.T) (*App, func()) {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureDefaults(ctx, db); err != nil {
		t.Fatal(err)
	}
	cfgStore, err := store.NewSQLiteStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	authManager, err := auth.New(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := events.NewHub()
	registry := metrics.New()
	app := &App{
		Mode:    config.ModeUI,
		Store:   cfgStore,
		DB:      db,
		Runtime: runtime.NewSQLite(db),
		Auth:    authManager,
		Icons:   icons.New(db, "http://127.0.0.1:1", logger),
		Events:  hub,
		Metrics: registry,
		Logger:  logger,
		Version: version.Get(),
		Started: time.Now(),
	}
	app.Status = status.New(db, hub, logger, registry, func() *config.Config {
		return app.Config(context.Background())
	})
	app.Discovery = discovery.New(nil, logger, registry, func() *config.Config {
		return app.Config(context.Background())
	}, nil)
	app.Weather = weather.New()
	return app, func() {
		cfgStore.Close()
		db.Close()
	}
}

func TestHealthAndBasePath(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "/blackflame", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	response, err := http.Get(ts.URL + "/blackflame/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health returned %d", response.StatusCode)
	}
	response2, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response2.Body.Close()
	if response2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 outside base path, got %d", response2.StatusCode)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response3, err := client.Get(ts.URL + "/blackflame")
	if err != nil {
		t.Fatal(err)
	}
	defer response3.Body.Close()
	if response3.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected redirect, got %d", response3.StatusCode)
	}
}

func TestSetupPrivateContentAndCSRF(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	bootstrap, err := client.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var anonymous bootstrapResponse
	if err := json.NewDecoder(bootstrap.Body).Decode(&anonymous); err != nil {
		t.Fatal(err)
	}
	bootstrap.Body.Close()
	if !anonymous.SetupRequired || anonymous.Authenticated {
		t.Fatalf("unexpected anonymous auth state: %+v", anonymous)
	}

	setupBody := bytes.NewBufferString(`{"password":"testpass123"}`)
	setup, err := client.Post(ts.URL+"/api/v1/auth/setup", "application/json", setupBody)
	if err != nil {
		t.Fatal(err)
	}
	var setupResponse struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(setup.Body).Decode(&setupResponse); err != nil {
		t.Fatal(err)
	}
	setup.Body.Close()
	if setupResponse.CSRF == "" {
		t.Fatal("missing CSRF token")
	}

	payload := map[string]any{
		"boardId": 1, "sectionId": 1, "kind": "app", "name": "Private App",
		"url": "https://private.example.com", "public": false, "checkEnabled": true,
	}
	body, _ := json.Marshal(payload)
	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/items", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected CSRF rejection, got %d", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/items", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", setupResponse.CSRF)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create item returned %d", response.StatusCode)
	}

	anonymousResponse, err := http.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	anonymousBody, _ := io.ReadAll(anonymousResponse.Body)
	anonymousResponse.Body.Close()
	if strings.Contains(string(anonymousBody), "Private App") {
		t.Fatal("private item leaked to anonymous bootstrap")
	}

	authenticated, err := client.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	authenticatedBody, _ := io.ReadAll(authenticated.Body)
	authenticated.Body.Close()
	if !strings.Contains(string(authenticatedBody), "Private App") {
		t.Fatal("authenticated bootstrap is missing private item")
	}

	logout, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	logoutResponse, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected CSRF rejection on logout, got %d", logoutResponse.StatusCode)
	}
	logout, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/logout", nil)
	logout.Header.Set("X-CSRF-Token", setupResponse.CSRF)
	logoutResponse, err = client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("logout returned %d", logoutResponse.StatusCode)
	}
}

func TestGitOpsModeIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "blackflame.yaml")
	yaml := `version: 1
boards:
  - slug: home
    name: Home
    sections:
      - name: Apps
        items:
          - kind: app
            name: Public App
            url: https://public.example.com
          - kind: app
            name: Secret App
            url: https://secret.example.com
            public: false
`
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgStore, err := config.NewYAMLStore(context.Background(), configPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cfgStore.Close()
	db, err := store.OpenSQLite(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	authManager, err := auth.New(context.Background(), db, "gitpass123")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := events.NewHub()
	registry := metrics.New()
	app := &App{
		Mode:    config.ModeGitOps,
		Store:   cfgStore,
		DB:      db,
		Runtime: runtime.NewSQLite(db),
		Auth:    authManager,
		Icons:   icons.New(db, "http://127.0.0.1:1", logger),
		Events:  hub,
		Metrics: registry,
		Logger:  logger,
		Version: version.Get(),
		Started: time.Now(),
	}
	app.Status = status.New(db, hub, logger, registry, func() *config.Config { return app.Config(context.Background()) })
	app.Discovery = discovery.New(nil, logger, registry, func() *config.Config { return app.Config(context.Background()) }, nil)
	app.Weather = weather.New()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	anonymous, err := http.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(anonymous.Body)
	anonymous.Body.Close()
	if strings.Contains(string(body), "Secret App") {
		t.Fatal("private item leaked in GitOps public bootstrap")
	}
	if !strings.Contains(string(body), `"readOnly":true`) {
		t.Fatal("GitOps bootstrap is not marked read-only")
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	login, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", bytes.NewBufferString(`{"password":"gitpass123"}`))
	if err != nil {
		t.Fatal(err)
	}
	var loginResponse struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(login.Body).Decode(&loginResponse); err != nil {
		t.Fatal(err)
	}
	login.Body.Close()

	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/items", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", loginResponse.CSRF)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected read-only conflict, got %d", response.StatusCode)
	}
}

func TestLayoutSettingsPersist(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	setup, err := client.Post(ts.URL+"/api/v1/auth/setup", "application/json", bytes.NewBufferString(`{"password":"testpass123"}`))
	if err != nil {
		t.Fatal(err)
	}
	var setupResponse struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(setup.Body).Decode(&setupResponse); err != nil {
		t.Fatal(err)
	}
	setup.Body.Close()

	body := bytes.NewBufferString(`{"layout":{"showHero":true,"showSearch":false,"showClock":true,"showWeather":false}}`)
	request, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/settings", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", setupResponse.CSRF)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("layout update returned %d", response.StatusCode)
	}

	bootstrap, err := client.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var boot bootstrapResponse
	if err := json.NewDecoder(bootstrap.Body).Decode(&boot); err != nil {
		t.Fatal(err)
	}
	bootstrap.Body.Close()
	layout := boot.Config.Layout
	if !layout.ShowHero || layout.ShowSearch || !layout.ShowClock || layout.ShowWeather {
		t.Fatalf("unexpected layout: %+v", layout)
	}
}

func TestInvalidSettingsDoNotMutateSnapshot(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	setup, err := client.Post(ts.URL+"/api/v1/auth/setup", "application/json", bytes.NewBufferString(`{"password":"testpass123"}`))
	if err != nil {
		t.Fatal(err)
	}
	var setupResponse struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(setup.Body).Decode(&setupResponse); err != nil {
		t.Fatal(err)
	}
	setup.Body.Close()

	before, err := client.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var beforeResponse bootstrapResponse
	if err := json.NewDecoder(before.Body).Decode(&beforeResponse); err != nil {
		t.Fatal(err)
	}
	before.Body.Close()

	body := bytes.NewBufferString(`{
		"theme": {"preset": "nord"},
		"search": {"providers": [{"name": "Bad", "prefix": "/bad", "url": "https://example.com/no-placeholder"}]}
	}`)
	request, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/settings", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", setupResponse.CSRF)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid settings to be rejected, got %d", response.StatusCode)
	}

	after, err := client.Get(ts.URL + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var afterResponse bootstrapResponse
	if err := json.NewDecoder(after.Body).Decode(&afterResponse); err != nil {
		t.Fatal(err)
	}
	after.Body.Close()
	if afterResponse.Config.Theme.Preset != beforeResponse.Config.Theme.Preset {
		t.Fatalf("failed settings update mutated theme: %q -> %q", beforeResponse.Config.Theme.Preset, afterResponse.Config.Theme.Preset)
	}
	if len(afterResponse.Config.Search.Providers) != len(beforeResponse.Config.Search.Providers) {
		t.Fatal("failed settings update mutated search providers")
	}
}

func TestWidgetsEndpointWithNoIntegrations(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	response, err := http.Get(ts.URL + "/api/v1/widgets")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("widgets endpoint returned %d", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `"widgets":[]`) {
		t.Fatalf("expected empty widgets, got %s", body)
	}
}

func TestCustomCSSIsEmptyButValid(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	server := New(app, "127.0.0.1:0", "", time.Second)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	response, err := http.Get(ts.URL + "/custom.css")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("custom.css returned %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
		t.Fatalf("unexpected content type %q", contentType)
	}
}

func TestLiveStatusAndMetricsRespectVisibilityChanges(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	board := app.Store.Snapshot().Config.Boards[0]
	item := store.ItemInput{
		BoardID: board.ID, SectionID: board.Sections[0].ID, Kind: "app",
		Name: "Service", URL: target.URL, Public: true, CheckEnabled: true,
	}
	id, err := store.CreateItem(ctx, app.DB, item)
	if err != nil {
		t.Fatal(err)
	}
	publicID, err := store.CreateItem(ctx, app.DB, item)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	privateKey := (config.Item{ID: id}).Key(board.Slug)
	publicKey := (config.Item{ID: publicID}).Key(board.Slug)
	decorated := app.Config(ctx).Boards[0].Sections[0].Items[0].StatusKey
	raw := app.Store.Snapshot().Config.Boards[0].Sections[0].Items[0].StatusKey
	if decorated != privateKey || raw != "" {
		t.Fatalf("expected a decorated copy and an unchanged snapshot, got %q and %q", decorated, raw)
	}

	updates, unsubscribe := app.Events.Subscribe()
	defer unsubscribe()
	statusCtx, stopStatus := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		app.Status.Start(statusCtx)
	}()
	defer func() {
		stopStatus()
		<-done
	}()
	for range 2 {
		select {
		case <-updates:
		case <-ctx.Done():
			t.Fatal("status checks did not complete")
		}
	}

	handler := New(app, "127.0.0.1:0", "", time.Second).Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	initial, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(initial, privateKey) {
		t.Fatalf("initial events omitted the visible item: %q %v", initial, err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	item.Public = false
	if err := store.UpdateItem(ctx, app.DB, id, item); err != nil {
		t.Fatal(err)
	}
	if err := app.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	app.Events.Publish("status", status.Result{Key: privateKey, Up: true})
	app.Events.Publish("status", status.Result{Key: publicKey, Up: true})
	event, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(event, `"key":"`+publicKey+`"`) {
		t.Fatalf("expected only the still-public item in the stream: %q %v", event, err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || strings.Contains(body, privateKey+`"`) || !strings.Contains(body, publicKey+`"`) {
		t.Fatalf("metrics did not respect item visibility: %d\n%s", recorder.Code, body)
	}
}

func TestHandleStoreErrorMapping(t *testing.T) {
	app, cleanup := testApp(t)
	defer cleanup()
	srv := &Server{app: app, logger: app.Logger}

	recorder := httptest.NewRecorder()
	srv.handleStoreError(recorder, store.ErrNotFound)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for ErrNotFound, got %d", recorder.Code)
	}

	// A duplicate board slug is a driver unique violation.
	_, err := store.CreateBoard(context.Background(), app.DB, "home", "Duplicate", true)
	if err == nil {
		t.Fatal("expected duplicate slug insert to fail")
	}
	recorder = httptest.NewRecorder()
	srv.handleStoreError(recorder, err)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409 for unique violation, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "UNIQUE constraint failed") {
		t.Fatalf("driver error text leaked to the client: %s", recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	srv.handleStoreError(recorder, errors.New("database is locked: /data/blackflame.db"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown store errors, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "database is locked") {
		t.Fatalf("internal error text leaked to the client: %s", recorder.Body.String())
	}
}
