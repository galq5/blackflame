package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// SecretRef points at a secret in the environment or a file. It deliberately
// does not support inline plaintext secrets in the dashboard settings.
type SecretRef struct {
	Env  string `yaml:"env,omitempty" json:"env,omitempty"`
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

func (s SecretRef) Configured() bool {
	return strings.TrimSpace(s.Env) != "" || strings.TrimSpace(s.File) != ""
}

func (s SecretRef) Resolve() (string, error) {
	if file := strings.TrimSpace(s.File); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if env := strings.TrimSpace(s.Env); env != "" {
		return strings.TrimSpace(os.Getenv(env)), nil
	}
	return "", nil
}

type WidgetBase struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	Title              string `yaml:"title,omitempty" json:"title,omitempty"`
	Public             bool   `yaml:"public,omitempty" json:"public,omitempty"`
	RefreshSeconds     int    `yaml:"refreshSeconds,omitempty" json:"refreshSeconds,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify,omitempty" json:"insecureSkipVerify,omitempty"`
}

func (w WidgetBase) Refresh() time.Duration {
	seconds := w.RefreshSeconds
	if seconds <= 0 {
		seconds = 60
	}
	if seconds < 10 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

type Widgets struct {
	PiHole     *PiHoleWidget     `yaml:"pihole,omitempty" json:"pihole,omitempty"`
	Proxmox    *ProxmoxWidget    `yaml:"proxmox,omitempty" json:"proxmox,omitempty"`
	Kubernetes *KubernetesWidget `yaml:"kubernetes,omitempty" json:"kubernetes,omitempty"`
	Prometheus *PrometheusWidget `yaml:"prometheus,omitempty" json:"prometheus,omitempty"`
	Calendar   *CalendarWidget   `yaml:"calendar,omitempty" json:"calendar,omitempty"`
}

type PiHoleWidget struct {
	WidgetBase `yaml:",inline" json:",inline"`
	URL        string    `yaml:"url,omitempty" json:"url,omitempty"`
	Secret     SecretRef `yaml:"secret,omitempty" json:"secret,omitempty"`
}

type ProxmoxWidget struct {
	WidgetBase  `yaml:",inline" json:",inline"`
	URL         string    `yaml:"url,omitempty" json:"url,omitempty"`
	TokenID     string    `yaml:"tokenId,omitempty" json:"tokenId,omitempty"`
	TokenSecret SecretRef `yaml:"tokenSecret,omitempty" json:"tokenSecret,omitempty"`
	Node        string    `yaml:"node,omitempty" json:"node,omitempty"`
}

type KubernetesWidget struct {
	WidgetBase `yaml:",inline" json:",inline"`
	API        string    `yaml:"api,omitempty" json:"api,omitempty"`
	Token      SecretRef `yaml:"token,omitempty" json:"token,omitempty"`
	CAFile     string    `yaml:"caFile,omitempty" json:"caFile,omitempty"`
	Namespaces []string  `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`
}

type PrometheusWidget struct {
	WidgetBase `yaml:",inline" json:",inline"`
	URL        string            `yaml:"url,omitempty" json:"url,omitempty"`
	Username   SecretRef         `yaml:"username,omitempty" json:"username,omitempty"`
	Password   SecretRef         `yaml:"password,omitempty" json:"password,omitempty"`
	Queries    []PrometheusQuery `yaml:"queries,omitempty" json:"queries,omitempty"`
}

type PrometheusQuery struct {
	Label string `yaml:"label" json:"label"`
	Query string `yaml:"query" json:"query"`
	Unit  string `yaml:"unit,omitempty" json:"unit,omitempty"`
}

type CalendarWidget struct {
	WidgetBase `yaml:",inline" json:",inline"`
	URL        string `yaml:"url,omitempty" json:"url,omitempty"`
	Days       int    `yaml:"days,omitempty" json:"days,omitempty"`
	MaxEvents  int    `yaml:"maxEvents,omitempty" json:"maxEvents,omitempty"`
}

func (w Widgets) validate(add func(field, message string)) {
	if w.PiHole != nil && w.PiHole.Enabled {
		if err := validateHTTPURL(w.PiHole.URL); err != nil {
			add("widgets.pihole.url", err.Error())
		}
		validateWidgetBase("widgets.pihole", w.PiHole.WidgetBase, add)
	}
	if w.Proxmox != nil && w.Proxmox.Enabled {
		if err := validateHTTPURL(w.Proxmox.URL); err != nil {
			add("widgets.proxmox.url", err.Error())
		}
		if strings.TrimSpace(w.Proxmox.TokenID) == "" {
			add("widgets.proxmox.tokenId", "is required")
		}
		if !w.Proxmox.TokenSecret.Configured() {
			add("widgets.proxmox.tokenSecret", "must reference an environment variable or file")
		}
		validateWidgetBase("widgets.proxmox", w.Proxmox.WidgetBase, add)
	}
	if w.Kubernetes != nil && w.Kubernetes.Enabled {
		if strings.TrimSpace(w.Kubernetes.API) != "" {
			if err := validateHTTPURL(w.Kubernetes.API); err != nil {
				add("widgets.kubernetes.api", err.Error())
			}
		}
		validateWidgetBase("widgets.kubernetes", w.Kubernetes.WidgetBase, add)
	}
	if w.Prometheus != nil && w.Prometheus.Enabled {
		if err := validateHTTPURL(w.Prometheus.URL); err != nil {
			add("widgets.prometheus.url", err.Error())
		}
		if len(w.Prometheus.Queries) == 0 {
			add("widgets.prometheus.queries", "at least one query is required")
		}
		for i, query := range w.Prometheus.Queries {
			field := fmt.Sprintf("widgets.prometheus.queries[%d]", i)
			if strings.TrimSpace(query.Label) == "" {
				add(field+".label", "is required")
			}
			if strings.TrimSpace(query.Query) == "" {
				add(field+".query", "is required")
			}
		}
		validateWidgetBase("widgets.prometheus", w.Prometheus.WidgetBase, add)
	}
	if w.Calendar != nil && w.Calendar.Enabled {
		if err := validateHTTPURL(w.Calendar.URL); err != nil {
			add("widgets.calendar.url", err.Error())
		}
		if w.Calendar.Days < 1 || w.Calendar.Days > 365 {
			add("widgets.calendar.days", "must be between 1 and 365")
		}
		if w.Calendar.MaxEvents < 1 || w.Calendar.MaxEvents > 50 {
			add("widgets.calendar.maxEvents", "must be between 1 and 50")
		}
		validateWidgetBase("widgets.calendar", w.Calendar.WidgetBase, add)
	}
}

func validateWidgetBase(field string, base WidgetBase, add func(field, message string)) {
	if base.RefreshSeconds != 0 && base.RefreshSeconds < 10 {
		add(field+".refreshSeconds", "must be at least 10")
	}
	if len(base.Title) > 120 {
		add(field+".title", "must be 120 characters or fewer")
	}
}
