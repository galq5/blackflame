package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Registry struct {
	mu              sync.Mutex
	httpRequests    map[string]uint64
	statusChecks    uint64
	statusErrors    uint64
	discoveryErrors uint64
}

func New() *Registry {
	return &Registry{httpRequests: map[string]uint64{}}
}

func (r *Registry) IncHTTP(method string, code int) {
	r.mu.Lock()
	r.httpRequests[method+"\x00"+strconv.Itoa(code)]++
	r.mu.Unlock()
}

func (r *Registry) IncStatus(up bool) {
	r.mu.Lock()
	r.statusChecks++
	if !up {
		r.statusErrors++
	}
	r.mu.Unlock()
}

func (r *Registry) IncDiscoveryError() {
	r.mu.Lock()
	r.discoveryErrors++
	r.mu.Unlock()
}

func (r *Registry) WriteBase(w io.Writer, version, mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Fprintln(w, "# HELP blackflame_build_info Build information.")
	fmt.Fprintln(w, "# TYPE blackflame_build_info gauge")
	fmt.Fprintf(w, "blackflame_build_info{version=%q,mode=%q} 1\n", version, mode)

	fmt.Fprintln(w, "# HELP blackflame_http_requests_total HTTP requests by method and status.")
	fmt.Fprintln(w, "# TYPE blackflame_http_requests_total counter")
	keys := make([]string, 0, len(r.httpRequests))
	for key := range r.httpRequests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		fmt.Fprintf(w, "blackflame_http_requests_total{method=%q,code=%q} %d\n", parts[0], parts[1], r.httpRequests[key])
	}

	fmt.Fprintln(w, "# HELP blackflame_status_checks_total Status checks performed.")
	fmt.Fprintln(w, "# TYPE blackflame_status_checks_total counter")
	fmt.Fprintf(w, "blackflame_status_checks_total %d\n", r.statusChecks)

	fmt.Fprintln(w, "# HELP blackflame_status_check_errors_total Failed status checks.")
	fmt.Fprintln(w, "# TYPE blackflame_status_check_errors_total counter")
	fmt.Fprintf(w, "blackflame_status_check_errors_total %d\n", r.statusErrors)

	fmt.Fprintln(w, "# HELP blackflame_discovery_errors_total Discovery errors.")
	fmt.Fprintln(w, "# TYPE blackflame_discovery_errors_total counter")
	fmt.Fprintf(w, "blackflame_discovery_errors_total %d\n", r.discoveryErrors)

}
