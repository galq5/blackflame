package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	boardSlugRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	statusRE    = regexp.MustCompile(`^[1-5][0-9]{2}(?:-[1-5][0-9]{2})?$`)
	prefixRE    = regexp.MustCompile(`^/[a-zA-Z0-9_-]+$`)
)

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Error())
	}
	return "invalid config: " + strings.Join(parts, "; ")
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	var errs ValidationErrors
	add := func(field, message string) {
		errs = append(errs, ValidationError{Field: field, Message: message})
	}

	if c.Version != CurrentVersion {
		add("version", fmt.Sprintf("must be %d", CurrentVersion))
	}
	if strings.TrimSpace(c.Branding.Name) == "" {
		add("branding.name", "is required")
	}
	if len(c.Branding.Name) > 80 {
		add("branding.name", "must be 80 characters or fewer")
	}
	if len(c.Branding.Description) > 200 {
		add("branding.description", "must be 200 characters or fewer")
	}
	if len(c.Branding.Logo) > 200 {
		add("branding.logo", "must be 200 characters or fewer")
	}

	boardSlugs := map[string]int{}
	for bi := range c.Boards {
		board := &c.Boards[bi]
		field := fmt.Sprintf("boards[%d]", bi)
		if strings.TrimSpace(board.Name) == "" {
			add(field+".name", "is required")
		}
		if board.Slug == "" {
			add(field+".slug", "is required")
		} else if !boardSlugRE.MatchString(board.Slug) {
			add(field+".slug", "must contain only lowercase letters, digits, and hyphens")
		} else if previous, ok := boardSlugs[board.Slug]; ok {
			add(field+".slug", fmt.Sprintf("duplicates boards[%d].slug", previous))
		} else {
			boardSlugs[board.Slug] = bi
		}

		sectionNames := map[string]int{}
		itemKeys := map[string]string{}
		for si := range board.Sections {
			section := &board.Sections[si]
			sfield := fmt.Sprintf("%s.sections[%d]", field, si)
			if strings.TrimSpace(section.Name) == "" {
				add(sfield+".name", "is required")
			} else if previous, ok := sectionNames[section.Name]; ok {
				add(sfield+".name", fmt.Sprintf("duplicates sections[%d].name", previous))
			} else {
				sectionNames[section.Name] = si
			}

			for ii := range section.Items {
				item := &section.Items[ii]
				ifield := fmt.Sprintf("%s.items[%d]", sfield, ii)
				if strings.TrimSpace(item.Name) == "" {
					add(ifield+".name", "is required")
				}
				switch item.Kind {
				case "app", "bookmark":
					if err := validateHTTPURL(item.URL); err != nil {
						add(ifield+".url", err.Error())
					}
				case "weather":
					// Weather items are reserved for a future release. They are
					// accepted so configs do not break when the widget arrives.
				default:
					add(ifield+".kind", "must be app, bookmark, or weather")
				}
				if item.ID < 0 {
					add(ifield+".id", "must not be negative")
				}
				key := item.Key(board.Slug)
				if previous, ok := itemKeys[key]; ok {
					add(ifield, fmt.Sprintf("duplicate item key %q (also used by %s)", key, previous))
				} else {
					itemKeys[key] = ifield
				}
			}
		}
	}

	searchPrefixes := map[string]int{}
	for si := range c.Search.Providers {
		provider := &c.Search.Providers[si]
		field := fmt.Sprintf("search.providers[%d]", si)
		if strings.TrimSpace(provider.Name) == "" {
			add(field+".name", "is required")
		}
		if !prefixRE.MatchString(provider.Prefix) {
			add(field+".prefix", "must start with / and contain only letters, digits, _ or -")
		} else if previous, ok := searchPrefixes[provider.Prefix]; ok {
			add(field+".prefix", fmt.Sprintf("duplicates providers[%d].prefix", previous))
		} else {
			searchPrefixes[provider.Prefix] = si
		}
		if !strings.Contains(provider.URL, "%s") {
			add(field+".url", "must contain %s")
		} else if err := validateHTTPURL(strings.ReplaceAll(provider.URL, "%s", "test")); err != nil {
			add(field+".url", err.Error())
		}
	}

	if c.Weather.Enabled {
		if c.Weather.Latitude < -90 || c.Weather.Latitude > 90 {
			add("weather.latitude", "must be between -90 and 90")
		}
		if c.Weather.Longitude < -180 || c.Weather.Longitude > 180 {
			add("weather.longitude", "must be between -180 and 180")
		}
		if c.Weather.Units != "" && c.Weather.Units != "metric" && c.Weather.Units != "imperial" {
			add("weather.units", "must be metric or imperial")
		}
	}

	if c.Status.Enabled {
		if c.Status.IntervalSeconds < 5 {
			add("status.intervalSeconds", "must be at least 5")
		}
		if c.Status.TimeoutSeconds < 1 {
			add("status.timeoutSeconds", "must be at least 1")
		}
		if c.Status.MaxConcurrency < 1 || c.Status.MaxConcurrency > 128 {
			add("status.maxConcurrency", "must be between 1 and 128")
		}
		if c.Status.ExpectedStatus != "" && !statusRE.MatchString(c.Status.ExpectedStatus) {
			add("status.expectedStatus", "must be a status code or range such as 200-399")
		} else if _, _, err := ParseStatusRange(c.Status.ExpectedStatus); err != nil {
			add("status.expectedStatus", err.Error())
		}
	}

	if c.Discovery.Docker.Enabled && c.Discovery.Docker.IntervalSeconds < 5 {
		add("discovery.docker.intervalSeconds", "must be at least 5")
	}
	if c.Discovery.Kubernetes.Enabled && c.Discovery.Kubernetes.IntervalSeconds < 5 {
		add("discovery.kubernetes.intervalSeconds", "must be at least 5")
	}

	if c.Icons.Source != "" {
		if err := validateHTTPURL(c.Icons.Source); err != nil {
			add("icons.source", err.Error())
		}
	}

	c.Widgets.validate(add)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateHTTPURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("must include a host")
	}
	return nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.' || r == '/':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

func ParseStatusRange(value string) (int, int, error) {
	if value == "" {
		return 200, 399, nil
	}
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, err
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, err
		}
		if start > end {
			// An inverted range is never satisfiable and would silently
			// mark every check as down.
			return 0, 0, fmt.Errorf("status range start %d is greater than end %d", start, end)
		}
		return start, end, nil
	}
	code, err := strconv.Atoi(value)
	if err != nil {
		return 0, 0, err
	}
	return code, code, nil
}
