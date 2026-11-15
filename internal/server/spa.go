package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/galq/blackflame/internal/web"
)

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/api" {
		writeError(w, http.StatusNotFound, "not_found", "API endpoint not found")
		return
	}
	requestPath := strings.TrimPrefix(r.URL.Path, "/")
	if requestPath != "" && !strings.HasSuffix(requestPath, "/") {
		if info, err := fs.Stat(web.FS(), requestPath); err == nil && !info.IsDir() {
			http.FileServer(http.FS(web.FS())).ServeHTTP(w, r)
			return
		}
		if path.Ext(requestPath) != "" {
			http.NotFound(w, r)
			return
		}
	}
	s.serveIndex(w)
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	data, err := web.IndexHTML(s.basePath)
	if err != nil {
		http.Error(w, "dashboard assets are unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
