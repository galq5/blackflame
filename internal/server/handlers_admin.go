package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/galq/blackflame/internal/auth"
	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/flameimport"
	"github.com/galq/blackflame/internal/store"
)

type boardPayload struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Public *bool  `json:"public"`
}

type sectionPayload struct {
	BoardID int64  `json:"boardId"`
	Name    string `json:"name"`
	Public  *bool  `json:"public"`
	Pinned  bool   `json:"pinned"`
}

type itemPayload struct {
	BoardID      int64          `json:"boardId"`
	SectionID    int64          `json:"sectionId"`
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	URL          string         `json:"url"`
	Icon         string         `json:"icon"`
	Description  string         `json:"description"`
	Public       *bool          `json:"public"`
	Pinned       bool           `json:"pinned"`
	Hidden       bool           `json:"hidden"`
	CheckEnabled *bool          `json:"checkEnabled"`
	Config       map[string]any `json:"config"`
}

type reorderPayload struct {
	BoardID   int64   `json:"boardId"`
	SectionID int64   `json:"sectionId"`
	IDs       []int64 `json:"ids"`
}

type settingsPayload struct {
	Branding  *config.Branding  `json:"branding"`
	Theme     *config.Theme     `json:"theme"`
	Layout    *config.Layout    `json:"layout"`
	Widgets   *config.Widgets   `json:"widgets"`
	Search    *config.Search    `json:"search"`
	Weather   *config.Weather   `json:"weather"`
	Status    *config.Status    `json:"status"`
	Discovery *config.Discovery `json:"discovery"`
	Icons     *config.Icons     `json:"icons"`
}

func (s *Server) requireWritableAdminMutation(w http.ResponseWriter, r *http.Request) *auth.Session {
	session := s.requireAdminMutation(w, r)
	if session == nil {
		return nil
	}
	if s.app.Store.ReadOnly() {
		writeError(w, http.StatusConflict, "read_only", "this instance is managed by configuration and is read-only")
		return nil
	}
	return session
}

func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload boardPayload
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Slug = strings.TrimSpace(payload.Slug)
	payload.Name = strings.TrimSpace(payload.Name)
	public := payload.Public == nil || *payload.Public
	if err := validateBoard(payload.Slug, payload.Name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_board", err.Error())
		return
	}
	id, err := store.CreateBoard(r.Context(), s.app.DB, payload.Slug, payload.Name, public)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var payload boardPayload
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Slug = strings.TrimSpace(payload.Slug)
	payload.Name = strings.TrimSpace(payload.Name)
	public := payload.Public == nil || *payload.Public
	if err := validateBoard(payload.Slug, payload.Name); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_board", err.Error())
		return
	}
	if err := store.UpdateBoard(r.Context(), s.app.DB, id, payload.Slug, payload.Name, public); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := store.DeleteBoard(r.Context(), s.app.DB, id); err != nil {
		if errors.Is(err, store.ErrLastBoard) {
			writeError(w, http.StatusConflict, "last_board", err.Error())
			return
		}
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload sectionPayload
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.BoardID <= 0 || payload.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_section", "boardId and name are required")
		return
	}
	public := payload.Public == nil || *payload.Public
	id, err := store.CreateSection(r.Context(), s.app.DB, payload.BoardID, payload.Name, public, payload.Pinned)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var payload sectionPayload
	if !readJSON(w, r, &payload) {
		return
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_section", "name is required")
		return
	}
	public := payload.Public == nil || *payload.Public
	if err := store.UpdateSection(r.Context(), s.app.DB, id, payload.Name, public, payload.Pinned); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := store.DeleteSection(r.Context(), s.app.DB, id); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleCreateItem(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload itemPayload
	if !readJSON(w, r, &payload) {
		return
	}
	if err := validateItem(payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_item", err.Error())
		return
	}
	id, err := store.CreateItem(r.Context(), s.app.DB, itemInput(payload))
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var payload itemPayload
	if !readJSON(w, r, &payload) {
		return
	}
	if err := validateItem(payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_item", err.Error())
		return
	}
	if err := store.UpdateItem(r.Context(), s.app.DB, id, itemInput(payload)); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := store.DeleteItem(r.Context(), s.app.DB, id); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleReorderItems(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload reorderPayload
	if !readJSON(w, r, &payload) {
		return
	}
	if payload.SectionID <= 0 || len(payload.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_reorder", "sectionId and ids are required")
		return
	}
	if err := store.ReorderItems(r.Context(), s.app.DB, payload.SectionID, payload.IDs); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleReorderSections(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload reorderPayload
	if !readJSON(w, r, &payload) {
		return
	}
	if payload.BoardID <= 0 || len(payload.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_reorder", "boardId and ids are required")
		return
	}
	if err := store.ReorderSections(r.Context(), s.app.DB, payload.BoardID, payload.IDs); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	cfg := s.app.Config(r.Context())
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings": map[string]any{
			"branding":  cfg.Branding,
			"theme":     cfg.Theme,
			"layout":    cfg.Layout,
			"widgets":   cfg.Widgets,
			"search":    cfg.Search,
			"weather":   cfg.Weather,
			"status":    cfg.Status,
			"discovery": cfg.Discovery,
			"icons":     cfg.Icons,
		},
		"readOnly": s.app.Store.ReadOnly(),
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	var payload settingsPayload
	if !readJSON(w, r, &payload) {
		return
	}
	cfg, err := store.LoadConfig(r.Context(), s.app.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings_unavailable", err.Error())
		return
	}
	if payload.Branding != nil {
		cfg.Branding = *payload.Branding
	}
	if payload.Theme != nil {
		cfg.Theme = *payload.Theme
	}
	if payload.Layout != nil {
		cfg.Layout = *payload.Layout
	}
	if payload.Widgets != nil {
		cfg.Widgets = *payload.Widgets
	}
	if payload.Search != nil {
		cfg.Search = *payload.Search
	}
	if payload.Weather != nil {
		cfg.Weather = *payload.Weather
	}
	if payload.Status != nil {
		cfg.Status = *payload.Status
	}
	if payload.Discovery != nil {
		cfg.Discovery = *payload.Discovery
	}
	if payload.Icons != nil {
		cfg.Icons = *payload.Icons
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	if err := store.SaveSettings(r.Context(), s.app.DB, cfg); err != nil {
		s.handleStoreError(w, err)
		return
	}
	s.reloadAfterMutation(r)
	writeNoContent(w)
}

func (s *Server) handleImportFlame(w http.ResponseWriter, r *http.Request) {
	if s.requireWritableAdminMutation(w, r) == nil {
		return
	}
	const maxImport = 64 << 20
	if r.ContentLength > maxImport {
		writeError(w, http.StatusRequestEntityTooLarge, "import_too_large", "Flame database must be smaller than 64 MiB")
		return
	}
	// Bound the body before parsing so oversized uploads are cut off while
	// streaming instead of spooling to disk first.
	r.Body = http.MaxBytesReader(w, r.Body, maxImport+(1<<20))
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "import_too_large", "Flame database must be smaller than 64 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid multipart upload")
		return
	}
	file, _, err := r.FormFile("flame")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_database", "Flame db.sqlite is required")
		return
	}
	defer file.Close()

	temp, err := os.CreateTemp("", "blackflame-flame-*.sqlite")
	if err != nil {
		s.logger.Error("create import temp file", "error", err)
		writeError(w, http.StatusInternalServerError, "import_failed", "import could not be prepared")
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	written, err := io.Copy(temp, io.LimitReader(file, maxImport+1))
	if err != nil {
		temp.Close()
		writeError(w, http.StatusBadRequest, "import_failed", err.Error())
		return
	}
	if written > maxImport {
		temp.Close()
		writeError(w, http.StatusBadRequest, "import_too_large", "Flame database must be smaller than 64 MiB")
		return
	}
	temp.Close()

	replace := strings.EqualFold(r.FormValue("replace"), "true")
	result, err := flameimport.Import(r.Context(), s.app.DB, tempPath, replace)
	if err != nil {
		writeError(w, http.StatusBadRequest, "import_failed", err.Error())
		return
	}
	s.reloadAfterMutation(r)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if s.requireAdmin(w, r) == nil {
		return
	}
	cfg := s.app.Store.Snapshot().Config
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", "configuration is not loaded")
		return
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="blackflame.yaml"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Server) handleDiscoveryRefresh(w http.ResponseWriter, r *http.Request) {
	// The background discovery loop performs the same DB writes in gitops
	// mode (discovered items live in a side table, not the YAML), so the
	// manual trigger only needs auth and CSRF, not the read-only gate.
	if s.requireAdminMutation(w, r) == nil {
		return
	}
	s.app.Discovery.Refresh(r.Context())
	s.reloadAfterMutation(r)
	writeJSON(w, http.StatusOK, s.app.Discovery.Status())
}

func validateBoard(slug, name string) error {
	if slug == "" || name == "" {
		return fmt.Errorf("slug and name are required")
	}
	cfg := config.Default()
	cfg.Boards = []config.Board{{Slug: slug, Name: name}}
	return cfg.Validate()
}

func validateItem(payload itemPayload) error {
	if payload.BoardID <= 0 || payload.SectionID <= 0 {
		return fmt.Errorf("boardId and sectionId are required")
	}
	if strings.TrimSpace(payload.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if payload.Kind != "app" && payload.Kind != "bookmark" {
		return fmt.Errorf("kind must be app or bookmark")
	}
	cfg := config.Default()
	cfg.Boards = []config.Board{{
		Slug: "home",
		Name: "Home",
		Sections: []config.Section{{
			Name: "section",
			Items: []config.Item{{
				Kind: payload.Kind,
				Name: payload.Name,
				URL:  payload.URL,
			}},
		}},
	}}
	return cfg.Validate()
}

func itemInput(payload itemPayload) store.ItemInput {
	public := payload.Public == nil || *payload.Public
	check := payload.CheckEnabled == nil || *payload.CheckEnabled
	return store.ItemInput{
		BoardID:      payload.BoardID,
		SectionID:    payload.SectionID,
		Kind:         payload.Kind,
		Name:         strings.TrimSpace(payload.Name),
		URL:          strings.TrimSpace(payload.URL),
		Icon:         strings.TrimSpace(payload.Icon),
		Description:  strings.TrimSpace(payload.Description),
		Public:       public,
		Pinned:       payload.Pinned,
		Hidden:       payload.Hidden,
		CheckEnabled: check,
		Config:       payload.Config,
	}
}
