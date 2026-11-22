package widgets

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/galq/blackflame/internal/config"
)

func TestPiHoleV5(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stats/summary" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/admin/api.php" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"dns_queries_today":100,"ads_blocked_today":20,"ads_percentage_today":20,"domains_being_blocked":1000,"status":"enabled"}`)
	}))
	defer server.Close()

	data, err := fetchPiHole(context.Background(), &config.PiHoleWidget{
		URL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	pi := data.(PiHoleData)
	if pi.Queries != 100 || pi.Blocked != 20 || pi.PercentBlocked != 20 || pi.DomainsBlocked != 1000 {
		t.Fatalf("unexpected Pi-hole data: %+v", pi)
	}
}

func TestPrometheusAndCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"status":"success","data":{"result":[{"metric":{},"value":[123,"42.5"]}]}}`)
	}))
	defer server.Close()

	cfg := config.Widgets{Prometheus: &config.PrometheusWidget{
		WidgetBase: config.WidgetBase{Enabled: true, RefreshSeconds: 60},
		URL:        server.URL,
		Queries:    []config.PrometheusQuery{{Label: "Test", Query: "up"}},
	}}
	manager := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := manager.Cards(context.Background(), cfg, false)
	if len(first) != 1 || first[0].Error != "" {
		t.Fatalf("unexpected card: %+v", first)
	}
	data := first[0].Data.(PrometheusData)
	if len(data.Queries) != 1 || data.Queries[0].Value != 42.5 {
		t.Fatalf("unexpected Prometheus data: %+v", data)
	}
	_ = manager.Cards(context.Background(), cfg, false)
	if requests.Load() != 1 {
		t.Fatalf("expected one cached request, got %d", requests.Load())
	}
	public := *cfg.Prometheus
	public.Public = true
	public.Queries = []config.PrometheusQuery{{Label: "Public", Query: "up"}}
	cards := manager.Cards(context.Background(), config.Widgets{Prometheus: &public}, true)
	if len(cards) != 1 || cards[0].Error != "" {
		t.Fatalf("unexpected public cards: %+v", cards)
	}
	if queries := cards[0].Data.(PrometheusData).Queries; len(queries) != 1 || queries[0].Label != "Public" {
		t.Fatalf("public widget reused private data: %+v", queries)
	}
	if requests.Load() != 2 {
		t.Fatalf("expected a fresh request for changed widget settings, got %d", requests.Load())
	}
}

func TestCalendar(t *testing.T) {
	start := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	ics := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:test",
		"DTSTAMP:" + start.Format("20060102T150405Z"),
		"DTSTART:" + start.Format("20060102T150405Z"),
		"DTEND:" + start.Add(time.Hour).Format("20060102T150405Z"),
		"SUMMARY:Test event",
		"LOCATION:Home",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, ics)
	}))
	defer server.Close()

	data, err := fetchCalendar(context.Background(), &config.CalendarWidget{
		URL:       server.URL,
		Days:      7,
		MaxEvents: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	calendar := data.(CalendarData)
	if len(calendar.Events) != 1 || calendar.Events[0].Summary != "Test event" || calendar.Events[0].Location != "Home" {
		t.Fatalf("unexpected calendar data: %+v", calendar)
	}
}

func TestWidgetValidation(t *testing.T) {
	cfg := config.Default()
	cfg.Widgets.Prometheus = &config.PrometheusWidget{
		WidgetBase: config.WidgetBase{Enabled: true},
		URL:        "not-a-url",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid Prometheus widget to fail validation")
	}
	cfg.Widgets.Prometheus = nil
	cfg.Widgets.Calendar = &config.CalendarWidget{
		WidgetBase: config.WidgetBase{Enabled: true},
		URL:        "https://example.com/calendar.ics",
		Days:       0,
		MaxEvents:  0,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid calendar widget to fail validation")
	}
}

func TestRedactURLHidesSecrets(t *testing.T) {
	redacted := redactURL("http://pihole.local/admin/api.php?summaryRaw=&auth=supersecret")
	if strings.Contains(redacted, "supersecret") {
		t.Fatalf("secret leaked into error URL: %s", redacted)
	}
	if !strings.Contains(redacted, "auth=REDACTED") {
		t.Fatalf("expected the auth parameter to be masked: %s", redacted)
	}
	unchanged := redactURL("http://prometheus.local/api/v1/query?query=up")
	if unchanged != "http://prometheus.local/api/v1/query?query=up" {
		t.Fatalf("non-sensitive URL was rewritten: %s", unchanged)
	}
}
