package metrics

import (
	"net/http"
	"strings"
	"testing"
)

func TestIncHTTPBoundsLabelCardinality(t *testing.T) {
	registry := New()
	// Unknown methods must collapse into one label instead of minting a
	// new map entry per attacker-chosen request method.
	for _, method := range []string{"X1", "X2", "X3", "garbage"} {
		registry.IncHTTP(method, http.StatusNotFound)
	}
	registry.IncHTTP(http.MethodGet, http.StatusOK)

	var builder strings.Builder
	registry.WriteBase(&builder, "test", "ui")
	output := builder.String()

	if strings.Contains(output, "X1") || strings.Contains(output, "garbage") {
		t.Fatalf("unknown methods leaked into metrics output: %s", output)
	}
	if !strings.Contains(output, `blackflame_http_requests_total{method="other",code="404"} 4`) {
		t.Fatalf("expected 4 requests collapsed into method=other: %s", output)
	}
	if !strings.Contains(output, `blackflame_http_requests_total{method="GET",code="200"} 1`) {
		t.Fatalf("expected GET 200 counter: %s", output)
	}
}
