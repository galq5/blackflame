package icons

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const maxIconBytes = 1 << 20 // 1 MiB

var iconNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

type Content struct {
	Name        string
	ContentType string
	Bytes       []byte
	ETag        string
}

type Service struct {
	db      *sql.DB
	client  *http.Client
	baseURL string
	logger  *slog.Logger
}

func New(db *sql.DB, baseURL string, logger *slog.Logger) *Service {
	return &Service{
		db:      db,
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		logger:  logger,
	}
}

func (s *Service) Get(ctx context.Context, name string) (Content, error) {
	name = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(name), ".svg"), ".png")
	if !iconNameRE.MatchString(name) {
		return Content{}, fmt.Errorf("invalid icon name")
	}
	if content, err := s.fromDB(ctx, name); err == nil {
		return content, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Content{}, err
	}
	if strings.HasPrefix(name, "custom-") {
		return Content{}, sql.ErrNoRows
	}

	for _, ext := range []string{".svg", ".png"} {
		url := s.baseURL + "/" + name + ext
		content, err := s.fetch(ctx, name, url)
		if err == nil {
			return content, nil
		}
		if !errors.Is(err, errNotFound) {
			s.logger.Debug("icon fetch failed", "name", name, "url", url, "error", err)
		}
	}
	return Content{}, sql.ErrNoRows
}

func (s *Service) Upload(ctx context.Context, filename string, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxIconBytes+1))
	if err != nil {
		return "", fmt.Errorf("read icon: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("icon is empty")
	}
	if len(data) > maxIconBytes {
		return "", fmt.Errorf("icon exceeds %d bytes", maxIconBytes)
	}
	contentType := http.DetectContentType(data)
	extension := strings.ToLower(filepathExt(filename))
	if declared := mime.TypeByExtension(extension); declared != "" {
		contentType = strings.TrimSpace(strings.Split(declared, ";")[0])
	}
	if extension == ".svg" && (contentType == "text/xml" || contentType == "application/xml" || contentType == "text/plain") {
		contentType = "image/svg+xml"
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/webp", "image/svg+xml":
	default:
		return "", fmt.Errorf("unsupported icon type %q", contentType)
	}
	if contentType == "image/svg+xml" {
		data, err = sanitizeSVG(data)
		if err != nil {
			return "", err
		}
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate icon name: %w", err)
	}
	name := "custom-" + hex.EncodeToString(suffix)
	if err := s.store(ctx, name, contentType, data, "", ""); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	content, err := s.Get(r.Context(), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", content.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", content.ETag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	if match := r.Header.Get("If-None-Match"); match != "" && match == content.ETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(content.Bytes)
}

func (s *Service) fromDB(ctx context.Context, name string) (Content, error) {
	var content Content
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT content_type, bytes FROM icons WHERE name = ?`, name).Scan(&content.ContentType, &data)
	if err != nil {
		return Content{}, err
	}
	content.Name = name
	content.Bytes = data
	content.ETag = etag(data)
	return content, nil
}

var errNotFound = errors.New("icon not found")

func (s *Service) fetch(ctx context.Context, name, rawURL string) (Content, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Content{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Content{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Content{}, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return Content{}, fmt.Errorf("icon server returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return Content{}, err
	}
	if len(data) > maxIconBytes {
		return Content{}, fmt.Errorf("icon exceeds %d bytes", maxIconBytes)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType == "image/svg+xml" {
		data, err = sanitizeSVG(data)
		if err != nil {
			return Content{}, err
		}
	}
	if contentType != "image/svg+xml" && contentType != "image/png" {
		return Content{}, fmt.Errorf("unsupported icon type %q", contentType)
	}
	if err := s.store(ctx, name, contentType, data, rawURL, resp.Header.Get("ETag")); err != nil {
		return Content{}, err
	}
	return Content{Name: name, ContentType: contentType, Bytes: data, ETag: etag(data)}, nil
}

func (s *Service) store(ctx context.Context, name, contentType string, data []byte, sourceURL, sourceETag string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO icons(name, content_type, bytes, source_url, etag, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			content_type = excluded.content_type,
			bytes = excluded.bytes,
			source_url = excluded.source_url,
			etag = excluded.etag,
			fetched_at = excluded.fetched_at`,
		name, contentType, data, nullString(sourceURL), nullString(sourceETag), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("store icon: %w", err)
	}
	return nil
}

func sanitizeSVG(data []byte) ([]byte, error) {
	lower := strings.ToLower(string(data))
	for _, bad := range []string{
		"<script", "javascript:", "<foreignobject", "<iframe", "<embed", "<object",
		"onload=", "onerror=", "onclick=", "onmouseover=", "onfocus=",
	} {
		if strings.Contains(lower, bad) {
			return nil, fmt.Errorf("unsafe SVG icon")
		}
	}
	return data, nil
}

func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func filepathExt(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index < 0 {
		return ""
	}
	return name[index:]
}

// Curated names for the icon picker. Arbitrary dashboard-icons slugs are also
// accepted by Get.
func Curated() []string {
	return []string{
		"adguard-home", "audiobookshelf", "authentik", "caddy", "calibre-web",
		"cloudflare", "deluge", "docker", "duplicati", "gitea",
		"grafana", "heimdall", "home-assistant", "immich", "jellyfin",
		"jellyseerr", "kanboard", "kavita", "kubernetes", "lidarr", "mariadb",
		"mealie", "minio", "mongodb", "navidrome", "nextcloud", "nginx",
		"node-red", "openmediavault", "paperless-ngx", "photoprism",
		"pi-hole", "plex", "portainer", "postgresql", "prometheus", "proxmox",
		"qbittorrent", "radarr", "redis", "sonarr", "syncthing", "traefik",
		"trilium", "truenas", "unraid", "uptime-kuma", "vaultwarden", "zigbee2mqtt",
	}
}
