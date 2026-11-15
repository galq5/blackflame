package config

import (
	"fmt"
	"strings"
)

const CurrentVersion = 1

// Mode selects the authoritative content store.
type Mode string

const (
	ModeUI     Mode = "ui"
	ModeGitOps Mode = "gitops"
)

func ParseMode(value string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case ModeUI:
		return ModeUI, nil
	case ModeGitOps:
		return ModeGitOps, nil
	default:
		return "", fmt.Errorf("invalid mode %q: must be %q or %q", value, ModeUI, ModeGitOps)
	}
}

// Config is the shared content and settings model used by both storage modes.
type Config struct {
	Version   int       `yaml:"version" json:"version"`
	Branding  Branding  `yaml:"branding,omitempty" json:"branding"`
	Theme     Theme     `yaml:"theme,omitempty" json:"theme"`
	Layout    Layout    `yaml:"layout,omitempty" json:"layout"`
	Widgets   Widgets   `yaml:"widgets,omitempty" json:"widgets"`
	Search    Search    `yaml:"search,omitempty" json:"search"`
	Weather   Weather   `yaml:"weather,omitempty" json:"weather"`
	Status    Status    `yaml:"status,omitempty" json:"status"`
	Discovery Discovery `yaml:"discovery,omitempty" json:"discovery"`
	Icons     Icons     `yaml:"icons,omitempty" json:"icons"`
	Boards    []Board   `yaml:"boards,omitempty" json:"boards"`
}

type Layout struct {
	ShowHero    bool `yaml:"showHero" json:"showHero"`
	ShowSearch  bool `yaml:"showSearch" json:"showSearch"`
	ShowClock   bool `yaml:"showClock" json:"showClock"`
	ShowWeather bool `yaml:"showWeather" json:"showWeather"`
}

type Branding struct {
	Name        string `yaml:"name,omitempty" json:"name"`
	Description string `yaml:"description,omitempty" json:"description"`
	Logo        string `yaml:"logo,omitempty" json:"logo,omitempty"`
}

type Theme struct {
	Preset    string            `yaml:"preset,omitempty" json:"preset"`
	Variables map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`
	CustomCSS string            `yaml:"customCss,omitempty" json:"customCss,omitempty"`
}

type Search struct {
	Providers []SearchProvider `yaml:"providers,omitempty" json:"providers"`
}

type SearchProvider struct {
	Name   string `yaml:"name" json:"name"`
	Prefix string `yaml:"prefix" json:"prefix"`
	URL    string `yaml:"url" json:"url"`
}

type Weather struct {
	Enabled   bool    `yaml:"enabled,omitempty" json:"enabled"`
	Latitude  float64 `yaml:"latitude,omitempty" json:"latitude"`
	Longitude float64 `yaml:"longitude,omitempty" json:"longitude"`
	Units     string  `yaml:"units,omitempty" json:"units"`
	Location  string  `yaml:"location,omitempty" json:"location"`
}

type Status struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	IntervalSeconds int    `yaml:"intervalSeconds,omitempty" json:"intervalSeconds"`
	TimeoutSeconds  int    `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds"`
	ExpectedStatus  string `yaml:"expectedStatus,omitempty" json:"expectedStatus"`
	MaxConcurrency  int    `yaml:"maxConcurrency,omitempty" json:"maxConcurrency"`
}

type Discovery struct {
	Docker     DockerDiscovery     `yaml:"docker,omitempty" json:"docker"`
	Kubernetes KubernetesDiscovery `yaml:"kubernetes,omitempty" json:"kubernetes"`
}

type DockerDiscovery struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Socket          string `yaml:"socket,omitempty" json:"socket"`
	IntervalSeconds int    `yaml:"intervalSeconds,omitempty" json:"intervalSeconds"`
}

type KubernetesDiscovery struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	IntervalSeconds int      `yaml:"intervalSeconds,omitempty" json:"intervalSeconds"`
	Namespaces      []string `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`
	LabelSelector   string   `yaml:"labelSelector,omitempty" json:"labelSelector,omitempty"`
	IngressClass    string   `yaml:"ingressClass,omitempty" json:"ingressClass,omitempty"`
	API             string   `yaml:"api,omitempty" json:"api,omitempty"`
	TokenFile       string   `yaml:"tokenFile,omitempty" json:"tokenFile,omitempty"`
	CAFile          string   `yaml:"caFile,omitempty" json:"caFile,omitempty"`
	Token           string   `yaml:"-" json:"-"`
}

type Icons struct {
	Source string `yaml:"source,omitempty" json:"source"`
}

type Board struct {
	ID       int64     `yaml:"id,omitempty" json:"id,omitempty"`
	Slug     string    `yaml:"slug" json:"slug"`
	Name     string    `yaml:"name" json:"name"`
	Public   *bool     `yaml:"public,omitempty" json:"public,omitempty"`
	Theme    *Theme    `yaml:"theme,omitempty" json:"theme,omitempty"`
	Sections []Section `yaml:"sections,omitempty" json:"sections"`
}

type Section struct {
	ID        int64  `yaml:"id,omitempty" json:"id,omitempty"`
	Name      string `yaml:"name" json:"name"`
	Public    *bool  `yaml:"public,omitempty" json:"public,omitempty"`
	Pinned    bool   `yaml:"pinned,omitempty" json:"pinned"`
	SortOrder int    `yaml:"-" json:"sortOrder,omitempty"`
	Items     []Item `yaml:"items,omitempty" json:"items"`
}

type Item struct {
	ID           int64          `yaml:"id,omitempty" json:"id,omitempty"`
	Kind         string         `yaml:"kind" json:"kind"`
	Name         string         `yaml:"name" json:"name"`
	URL          string         `yaml:"url,omitempty" json:"url,omitempty"`
	Icon         string         `yaml:"icon,omitempty" json:"icon,omitempty"`
	Description  string         `yaml:"description,omitempty" json:"description,omitempty"`
	Public       *bool          `yaml:"public,omitempty" json:"public,omitempty"`
	Pinned       bool           `yaml:"pinned,omitempty" json:"pinned"`
	Hidden       bool           `yaml:"hidden,omitempty" json:"hidden,omitempty"`
	CheckEnabled *bool          `yaml:"checkEnabled,omitempty" json:"checkEnabled,omitempty"`
	Source       string         `yaml:"source,omitempty" json:"source,omitempty"`
	ExternalID   string         `yaml:"externalId,omitempty" json:"externalId,omitempty"`
	Group        string         `yaml:"group,omitempty" json:"group,omitempty"`
	Override     bool           `yaml:"override,omitempty" json:"override,omitempty"`
	Config       map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
	SortOrder    int            `yaml:"-" json:"sortOrder,omitempty"`
	StatusKey    string         `yaml:"-" json:"statusKey,omitempty"`
}

// DiscoveredItem is a normalized discovery result before it is merged into content.
type DiscoveredItem struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Icon       string `json:"icon,omitempty"`
	Group      string `json:"group,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
}

const (
	SourceManual     = "manual"
	SourceDocker     = "docker"
	SourceKubernetes = "kubernetes"
)

func Bool(value bool) *bool { return &value }

func (b Board) IsPublic() bool      { return b.Public == nil || *b.Public }
func (s Section) IsPublic() bool    { return s.Public == nil || *s.Public }
func (i Item) IsPublic() bool       { return i.Public == nil || *i.Public }
func (i Item) IsCheckEnabled() bool { return i.CheckEnabled == nil || *i.CheckEnabled }

func (i Item) EffectiveSource() string {
	if i.Source == "" {
		return SourceManual
	}
	return i.Source
}

// Key returns a stable identifier for status checks and SSE events.
func (i Item) Key(boardSlug string) string {
	if i.ExternalID != "" {
		return fmt.Sprintf("discovered:%s:%s", i.EffectiveSource(), i.ExternalID)
	}
	if i.ID > 0 {
		return fmt.Sprintf("item:%d", i.ID)
	}
	return fmt.Sprintf("yaml:%s:%s:%s", boardSlug, i.Kind, slugify(i.Name))
}

func (i Item) Checkable() bool {
	if !i.IsCheckEnabled() || i.Hidden {
		return false
	}
	return i.Kind == "app" && (strings.HasPrefix(i.URL, "http://") || strings.HasPrefix(i.URL, "https://"))
}

func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		Branding: Branding{
			Name:        "Blackflame",
			Description: "Homelab dashboard",
		},
		Theme: DefaultTheme(),
		Layout: Layout{
			ShowHero:    false,
			ShowSearch:  true,
			ShowClock:   true,
			ShowWeather: true,
		},
		Search: DefaultSearch(),
		Weather: Weather{
			Enabled: false,
			Units:   "metric",
		},
		Status: Status{
			Enabled:         true,
			IntervalSeconds: 60,
			TimeoutSeconds:  5,
			ExpectedStatus:  "200-399",
			MaxConcurrency:  16,
		},
		Discovery: Discovery{
			Docker: DockerDiscovery{
				Enabled:         false,
				Socket:          "/var/run/docker.sock",
				IntervalSeconds: 30,
			},
			Kubernetes: KubernetesDiscovery{
				Enabled:         false,
				IntervalSeconds: 30,
			},
		},
		Icons: Icons{
			// Pinned for reproducible builds and offline-friendly caching. Update
			// this revision deliberately with a release.
			Source: "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/d767d7327db7f8ebd3715b552523f61889eea41c/svg",
		},
		Boards: []Board{},
	}
}

func DefaultTheme() Theme {
	// Presets live in the frontend. Keeping the default variables empty means
	// "use the preset" rather than overriding a YAML preset with blackflame
	// colors.
	return Theme{Preset: "blackflame"}
}

func DefaultSearch() Search {
	return Search{
		Providers: []SearchProvider{
			{Name: "Google", Prefix: "/g", URL: "https://www.google.com/search?q=%s"},
			{Name: "YouTube", Prefix: "/yt", URL: "https://www.youtube.com/results?search_query=%s"},
			{Name: "GitHub", Prefix: "/gh", URL: "https://github.com/search?q=%s"},
			{Name: "Wikipedia", Prefix: "/w", URL: "https://en.wikipedia.org/w/index.php?search=%s"},
		},
	}
}

// PublicSnapshot returns a deep-enough copy containing only public content.
func (c *Config) PublicSnapshot() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Theme = cloneTheme(c.Theme)
	out.Widgets = Widgets{}
	out.Search = Search{Providers: append([]SearchProvider(nil), c.Search.Providers...)}
	out.Weather = c.Weather
	out.Status = c.Status
	out.Discovery = Discovery{}
	out.Icons = c.Icons
	out.Boards = make([]Board, 0, len(c.Boards))
	for _, board := range c.Boards {
		if !board.IsPublic() {
			continue
		}
		b := board
		b.Theme = cloneThemePtr(board.Theme)
		b.Sections = make([]Section, 0, len(board.Sections))
		for _, section := range board.Sections {
			if !section.IsPublic() {
				continue
			}
			s := section
			s.Items = make([]Item, 0, len(section.Items))
			for _, item := range section.Items {
				if !item.IsPublic() || item.Hidden {
					continue
				}
				s.Items = append(s.Items, cloneItem(item))
			}
			b.Sections = append(b.Sections, s)
		}
		out.Boards = append(out.Boards, b)
	}
	return &out
}

func cloneTheme(t Theme) Theme {
	out := t
	if t.Variables != nil {
		out.Variables = make(map[string]string, len(t.Variables))
		for k, v := range t.Variables {
			out.Variables[k] = v
		}
	}
	return out
}

func cloneThemePtr(t *Theme) *Theme {
	if t == nil {
		return nil
	}
	out := cloneTheme(*t)
	return &out
}

func cloneItem(i Item) Item {
	out := i
	if i.Config != nil {
		out.Config = make(map[string]any, len(i.Config))
		for k, v := range i.Config {
			out.Config[k] = v
		}
	}
	return out
}

func (c *Config) Counts() (boards, sections, items int) {
	if c == nil {
		return 0, 0, 0
	}
	boards = len(c.Boards)
	for _, b := range c.Boards {
		sections += len(b.Sections)
		for _, s := range b.Sections {
			items += len(s.Items)
		}
	}
	return boards, sections, items
}
