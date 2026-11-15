package discovery

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/galq/blackflame/internal/config"
)

func TestDiscoverDockerLabels(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]dockerContainer{{
			ID:    "container-1",
			Names: []string{"/immich"},
			Labels: map[string]string{
				"blackflame.enable": "true",
				"blackflame.name":   "Immich;Photos",
				"blackflame.url":    "https://photos.example.com;https://photos2.example.com",
				"blackflame.icon":   "immich;photoprism",
				"blackflame.group":  "Media",
			},
		}, {
			ID:    "container-2",
			Names: []string{"/ignored"},
			Labels: map[string]string{
				"flame.type": "application",
				"flame.name": "Legacy",
				"flame.url":  "https://legacy.example.com",
			},
		}})
	})}
	go server.Serve(listener)
	defer server.Close()

	items, err := discoverDocker(context.Background(), socket)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %+v", items)
	}
	if items[0].Name != "Immich" || items[0].Group != "Media" || items[0].Source != config.SourceDocker {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[2].Name != "Legacy" || items[2].URL != "https://legacy.example.com" {
		t.Fatalf("unexpected legacy item: %+v", items[2])
	}
}

func TestWildcardIngressHostIsSkipped(t *testing.T) {
	var ing ingress
	if err := json.Unmarshal([]byte(`{
		"metadata": {"name": "wildcard", "namespace": "default", "annotations": {"blackflame.dev/type": "application"}},
		"spec": {"rules": [{"host": "*.example.com", "http": {"paths": [{"path": "/"}]}}]}
	}`), &ing); err != nil {
		t.Fatal(err)
	}
	if items := ingressItems(ing, ""); len(items) != 0 {
		t.Fatalf("wildcard ingress should be skipped: %+v", items)
	}
}

func TestDiscoverKubernetesIngress(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/networking.k8s.io/v1/ingresses":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{map[string]any{
					"metadata": map[string]any{
						"name":      "photos",
						"namespace": "media",
						"annotations": map[string]string{
							"flame.pawelmalak/type": "application",
							"flame.pawelmalak/name": "Photos",
							"flame.pawelmalak/icon": "immich",
						},
					},
					"spec": map[string]any{
						"tls": []any{map[string]any{"hosts": []string{"photos.example.com"}}},
						"rules": []any{map[string]any{
							"host": "photos.example.com",
							"http": map[string]any{"paths": []any{map[string]any{"path": "/"}}},
						}},
					},
				}},
			})
		case "/apis/gateway.networking.k8s.io/v1/httproutes":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cert := server.Certificate()
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := discoverKubernetes(context.Background(), config.KubernetesDiscovery{
		Enabled:         true,
		IntervalSeconds: 30,
		API:             server.URL,
		TokenFile:       tokenPath,
		CAFile:          caPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	item := items[0]
	if item.Name != "Photos" || item.URL != "https://photos.example.com/" || item.Group != "media" {
		t.Fatalf("unexpected ingress item: %+v", item)
	}
	if item.Source != config.SourceKubernetes {
		t.Fatalf("unexpected source: %+v", item)
	}
}
