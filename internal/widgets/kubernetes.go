package widgets

import (
	"context"
	"fmt"
	"strings"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/discovery"
)

func fetchKubernetes(ctx context.Context, cfg *config.KubernetesWidget) (any, error) {
	token, err := cfg.Token.Resolve()
	if err != nil {
		return nil, err
	}
	discoveryConfig := config.KubernetesDiscovery{
		Enabled:    true,
		API:        cfg.API,
		CAFile:     cfg.CAFile,
		Namespaces: cfg.Namespaces,
		Token:      strings.TrimSpace(token),
	}
	summary, err := discovery.KubernetesSummary(ctx, discoveryConfig)
	if err != nil {
		return nil, fmt.Errorf("Kubernetes API: %w", err)
	}
	return summary, nil
}
