package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/galq/blackflame/internal/config"
)

type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	State  string            `json:"State"`
}

func discoverDocker(ctx context.Context, socket string) ([]config.DiscoveredItem, error) {
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	baseURL := "http://docker"
	transport := &http.Transport{IdleConnTimeout: 30 * time.Second}
	switch {
	case strings.HasPrefix(socket, "tcp://"):
		baseURL = "http://" + strings.TrimPrefix(socket, "tcp://")
	case strings.HasPrefix(socket, "http://"), strings.HasPrefix(socket, "https://"):
		baseURL = socket
	default:
		socket = strings.TrimPrefix(socket, "unix://")
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}
	}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/containers/json?all=0", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker socket %s: %w", socket, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker API returned %d", resp.StatusCode)
	}
	var containers []dockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("decode docker containers: %w", err)
	}

	var items []config.DiscoveredItem
	for _, container := range containers {
		items = append(items, dockerItems(container)...)
	}
	return items, nil
}

func dockerItems(container dockerContainer) []config.DiscoveredItem {
	labels := container.Labels
	enabled := parseBool(labels["blackflame.enable"]) || isApplicationType(labels["flame.type"])
	if !enabled {
		return nil
	}
	names := splitLabels(labels["blackflame.name"])
	if len(names) == 0 {
		names = splitLabels(labels["flame.name"])
	}
	urls := splitLabels(labels["blackflame.url"])
	if len(urls) == 0 {
		urls = splitLabels(labels["flame.url"])
	}
	icons := splitLabels(labels["blackflame.icon"])
	if len(icons) == 0 {
		icons = splitLabels(labels["flame.icon"])
	}
	groups := splitLabels(labels["blackflame.group"])
	if len(groups) == 0 {
		groups = []string{"Docker"}
	}
	hidden := parseBool(labels["blackflame.hidden"])
	containerName := ""
	if len(container.Names) > 0 {
		containerName = strings.TrimPrefix(container.Names[0], "/")
	}

	var items []config.DiscoveredItem
	for index, rawURL := range urls {
		rawURL = strings.TrimSpace(rawURL)
		if !validHTTPURL(rawURL) {
			continue
		}
		name := pick(names, index, containerName)
		if name == "" {
			name = containerName
		}
		if name == "" {
			name = container.ID
		}
		icon := pick(icons, index, "docker")
		group := pick(groups, index, "Docker")
		items = append(items, config.DiscoveredItem{
			Source:     config.SourceDocker,
			ExternalID: container.ID + ":" + strconv.Itoa(index),
			Name:       name,
			URL:        rawURL,
			Icon:       icon,
			Group:      group,
			Hidden:     hidden,
		})
	}
	return items
}

func splitLabels(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func pick(values []string, index int, fallback string) string {
	if index < len(values) && values[index] != "" {
		return values[index]
	}
	if len(values) > 0 && values[0] != "" {
		return values[0]
	}
	return fallback
}

func parseBool(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}

func isApplicationType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "application" || value == "app"
}

func validHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
