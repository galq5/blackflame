package status

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/events"
	"github.com/galq/blackflame/internal/metrics"
	"github.com/galq/blackflame/internal/store"
)

func TestBlockedTarget(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data",
		// IPv4-mapped IPv6 spellings of the same address.
		"http://[::ffff:169.254.169.254]/latest",
		"http://[::ffff:a9fe:a9fe]/latest",
		// Other link-local addresses in the same range.
		"http://169.254.100.100/latest",
		// Known metadata endpoints by name or address.
		"http://metadata.google.internal/computeMetadata/v1",
		"http://metadata/",
		"http://[fd00:ec2::254]/latest",
		"http://100.100.100.200/latest/meta-data",
	}
	for _, target := range blocked {
		if !blockedTarget(target) {
			t.Fatalf("expected %q to be blocked", target)
		}
	}
	allowed := []string{
		"https://example.com",
		"http://192.168.1.10:8080/healthz",
		"http://[fd12::5]:9090/status",
	}
	for _, target := range allowed {
		if blockedTarget(target) {
			t.Fatalf("expected %q to be allowed", target)
		}
	}
}

func TestCheckDueRecordsStatus(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()

	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Status = config.Status{Enabled: true, IntervalSeconds: 5, TimeoutSeconds: 2, ExpectedStatus: "200-399", MaxConcurrency: 2}
	cfg.Boards = []config.Board{{
		Slug: "home", Name: "Home",
		Sections: []config.Section{{
			Name: "Apps",
			Items: []config.Item{
				{ID: 1, Kind: "app", Name: "Up", URL: up.URL, CheckEnabled: config.Bool(true)},
				{ID: 2, Kind: "app", Name: "Down", URL: down.URL, CheckEnabled: config.Bool(true)},
			},
		}},
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := New(db, events.NewHub(), logger, metrics.New(), func() *config.Config { return cfg })
	manager.checkDue(context.Background())

	results := manager.Results()
	if !results["item:1"].Up || results["item:1"].StatusCode != 200 {
		t.Fatalf("unexpected up result: %+v", results["item:1"])
	}
	if results["item:2"].Up || results["item:2"].StatusCode != 500 {
		t.Fatalf("unexpected down result: %+v", results["item:2"])
	}
	if len(manager.ResultsFor(cfg)) != 2 {
		t.Fatalf("expected two visible results: %+v", manager.ResultsFor(cfg))
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM status_latest`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected two status rows, got %d", count)
	}
}
