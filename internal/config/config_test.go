package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadYAMLValid(t *testing.T) {
	cfg, err := LoadYAML(strings.NewReader(`
version: 1
boards:
  - slug: home
    name: Home
    sections:
      - name: Apps
        items:
          - kind: app
            name: Immich
            url: https://photos.example.com
            icon: immich
`))
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if !cfg.Boards[0].IsPublic() || !cfg.Boards[0].Sections[0].IsPublic() {
		t.Fatal("public should default to true")
	}
	if got := cfg.Boards[0].Sections[0].Items[0].Key("home"); got != "yaml:home:app:immich" {
		t.Fatalf("unexpected key %q", got)
	}
}

func TestLoadYAMLRejectsUnknownFields(t *testing.T) {
	_, err := LoadYAML(strings.NewReader(`
version: 1
boards: []
unknown: true
`))
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestValidateRejectsBadURLAndDuplicateSlugs(t *testing.T) {
	cfg := Default()
	cfg.Boards = []Board{
		{Slug: "home", Name: "Home", Sections: []Section{{Name: "Apps", Items: []Item{{Kind: "app", Name: "Bad", URL: "javascript:alert(1)"}}}}},
		{Slug: "home", Name: "Duplicate"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	text := err.Error()
	if !strings.Contains(text, "must use http or https") || !strings.Contains(text, "duplicates") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestPublicSnapshotFiltersPrivateContent(t *testing.T) {
	cfg := Default()
	cfg.Boards = []Board{
		{
			Slug: "public", Name: "Public",
			Sections: []Section{
				{Name: "Visible", Items: []Item{{Kind: "app", Name: "A", URL: "https://a.example"}, {Kind: "app", Name: "Private", URL: "https://p.example", Public: Bool(false)}}},
				{Name: "Hidden section", Public: Bool(false), Items: []Item{{Kind: "app", Name: "B", URL: "https://b.example"}}},
			},
		},
		{Slug: "private", Name: "Private", Public: Bool(false), Sections: []Section{{Name: "X", Items: []Item{{Kind: "app", Name: "C", URL: "https://c.example"}}}}},
	}
	public := cfg.PublicSnapshot()
	if len(public.Boards) != 1 || public.Boards[0].Slug != "public" {
		t.Fatalf("unexpected boards: %+v", public.Boards)
	}
	if len(public.Boards[0].Sections) != 1 || len(public.Boards[0].Sections[0].Items) != 1 {
		t.Fatalf("private content leaked: %+v", public.Boards[0])
	}
	if public.Boards[0].Sections[0].Items[0].Name != "A" {
		t.Fatalf("wrong item survived: %+v", public.Boards[0].Sections[0].Items)
	}
}

func TestYAMLStoreKeepsLastGoodConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blackflame.yaml")
	valid := `
version: 1
boards:
  - slug: home
    name: Home
`
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewYAMLStore(context.Background(), path)
	if err != nil {
		t.Fatalf("NewYAMLStore: %v", err)
	}
	defer store.Close()
	if store.Snapshot().Config == nil {
		t.Fatal("expected config")
	}
	if err := os.WriteFile(path, []byte("version: 1\nunknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(context.Background()); err == nil {
		t.Fatal("expected invalid reload to fail")
	}
	snapshot := store.Snapshot()
	if snapshot.Config == nil {
		t.Fatal("last good config was lost")
	}
	if snapshot.Err == nil {
		t.Fatal("expected snapshot error")
	}
}

func TestParseStatusRange(t *testing.T) {
	start, end, err := ParseStatusRange("200-399")
	if err != nil || start != 200 || end != 399 {
		t.Fatalf("unexpected range: %d %d %v", start, end, err)
	}
	if _, _, err := ParseStatusRange("399-200"); err == nil {
		t.Fatal("expected an inverted range to be rejected")
	}
	cfg := Default()
	cfg.Status.ExpectedStatus = "399-200"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to reject an inverted status range")
	}
}
