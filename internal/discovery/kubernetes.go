package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/galq/blackflame/internal/config"
)

const serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

var errAPINotFound = errors.New("kubernetes API not found")

type k8sClient struct {
	api    string
	token  string
	client *http.Client
}

func discoverKubernetes(ctx context.Context, cfg config.KubernetesDiscovery) ([]config.DiscoveredItem, error) {
	client, err := newK8sClient(cfg)
	if err != nil {
		return nil, err
	}
	var items []config.DiscoveredItem
	ingresses, err := client.listIngresses(ctx, cfg)
	if err != nil && !errors.Is(err, errAPINotFound) {
		return nil, err
	}
	items = append(items, ingresses...)

	routes, err := client.listHTTPRoutes(ctx, cfg)
	if err != nil && !errors.Is(err, errAPINotFound) {
		// Gateway API is optional. A missing CRD is fine; a permission error is
		// reported because it usually means the RBAC manifest is incomplete.
		if !isOptionalAPIError(err) {
			return nil, err
		}
	}
	items = append(items, routes...)
	return items, nil
}

func newK8sClient(cfg config.KubernetesDiscovery) (*k8sClient, error) {
	api := strings.TrimRight(cfg.API, "/")
	tokenFile := cfg.TokenFile
	caFile := cfg.CAFile
	inCluster := false
	if api == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" || port == "" {
			return nil, fmt.Errorf("kubernetes API is not configured; run in-cluster or set discovery.kubernetes.api")
		}
		api = "https://" + net.JoinHostPort(host, port)
		inCluster = true
	}
	if tokenFile == "" && inCluster {
		tokenFile = filepath.Join(serviceAccountDir, "token")
	}
	if caFile == "" && inCluster {
		caFile = filepath.Join(serviceAccountDir, "ca.crt")
	}

	token := strings.TrimSpace(cfg.Token)
	if token == "" && tokenFile != "" {
		raw, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("read kubernetes token: %w", err)
		}
		token = strings.TrimSpace(string(raw))
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		raw, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read kubernetes CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("kubernetes CA file contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        8,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &k8sClient{
		api:   api,
		token: token,
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}, nil
}

func (c *k8sClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("kubernetes request: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errAPINotFound
	case http.StatusForbidden:
		return fmt.Errorf("kubernetes API forbidden for %s; check RBAC permissions", path)
	case http.StatusUnauthorized:
		return fmt.Errorf("kubernetes API rejected the service account token")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("kubernetes API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode kubernetes response: %w", err)
	}
	return nil
}

func (c *k8sClient) listIngresses(ctx context.Context, cfg config.KubernetesDiscovery) ([]config.DiscoveredItem, error) {
	namespaces := cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	var items []config.DiscoveredItem
	for _, namespace := range namespaces {
		path := "/apis/networking.k8s.io/v1/ingresses"
		if namespace != "" {
			path = "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(namespace) + "/ingresses"
		}
		continueToken := ""
		for {
			pathWithQuery := path + query(cfg.LabelSelector, continueToken)
			var list ingressList
			if err := c.get(ctx, pathWithQuery, &list); err != nil {
				if errors.Is(err, errAPINotFound) {
					break
				}
				return nil, err
			}
			for _, ingress := range list.Items {
				items = append(items, ingressItems(ingress, cfg.IngressClass)...)
			}
			continueToken = list.Metadata.Continue
			if continueToken == "" {
				break
			}
		}
	}
	return items, nil
}

func (c *k8sClient) listHTTPRoutes(ctx context.Context, cfg config.KubernetesDiscovery) ([]config.DiscoveredItem, error) {
	namespaces := cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	var items []config.DiscoveredItem
	for _, namespace := range namespaces {
		path := "/apis/gateway.networking.k8s.io/v1/httproutes"
		if namespace != "" {
			path = "/apis/gateway.networking.k8s.io/v1/namespaces/" + url.PathEscape(namespace) + "/httproutes"
		}
		continueToken := ""
		for {
			pathWithQuery := path + query(cfg.LabelSelector, continueToken)
			var list httpRouteList
			if err := c.get(ctx, pathWithQuery, &list); err != nil {
				if errors.Is(err, errAPINotFound) {
					break
				}
				return nil, err
			}
			for _, route := range list.Items {
				items = append(items, httpRouteItems(route)...)
			}
			continueToken = list.Metadata.Continue
			if continueToken == "" {
				break
			}
		}
	}
	return items, nil
}

func query(labelSelector, continueToken string) string {
	values := url.Values{}
	if strings.TrimSpace(labelSelector) != "" {
		values.Set("labelSelector", labelSelector)
	}
	if continueToken != "" {
		values.Set("continue", continueToken)
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

type listMeta struct {
	Continue string `json:"continue"`
}

type ingressList struct {
	Metadata listMeta  `json:"metadata"`
	Items    []ingress `json:"items"`
}

type ingress struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		IngressClassName *string `json:"ingressClassName"`
		TLS              []struct {
			Hosts []string `json:"hosts"`
		} `json:"tls"`
		Rules []struct {
			Host string `json:"host"`
			HTTP struct {
				Paths []struct {
					Path string `json:"path"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
	} `json:"spec"`
}

func ingressItems(ingress ingress, classFilter string) []config.DiscoveredItem {
	annotations := ingress.Metadata.Annotations
	if classFilter != "" {
		class := ""
		if ingress.Spec.IngressClassName != nil {
			class = *ingress.Spec.IngressClassName
		}
		if class == "" {
			class = annotations["kubernetes.io/ingress.class"]
		}
		if class != classFilter {
			return nil
		}
	}
	if !applicationEnabled(annotations) {
		return nil
	}
	names := splitLabels(annotation(annotations, "blackflame.dev/name", "flame.pawelmalak/name"))
	urls := splitLabels(annotation(annotations, "blackflame.dev/url", "flame.pawelmalak/url"))
	icons := splitLabels(annotation(annotations, "blackflame.dev/icon", "flame.pawelmalak/icon"))
	groups := splitLabels(annotation(annotations, "blackflame.dev/group"))
	hidden := parseBool(annotations["blackflame.dev/hidden"])

	derived := deriveIngressURL(ingress)
	if len(urls) == 0 && derived != "" {
		urls = []string{derived}
	}
	if len(urls) == 0 {
		return nil
	}
	group := pick(groups, 0, ingress.Metadata.Namespace)
	if group == "" {
		group = "Kubernetes"
	}
	var items []config.DiscoveredItem
	for index, rawURL := range urls {
		if !validHTTPURL(rawURL) {
			continue
		}
		name := pick(names, index, ingress.Metadata.Name)
		items = append(items, config.DiscoveredItem{
			Source:     config.SourceKubernetes,
			ExternalID: ingress.Metadata.Namespace + "/" + ingress.Metadata.Name + ":" + strconv.Itoa(index),
			Name:       name,
			URL:        rawURL,
			Icon:       pick(icons, index, "kubernetes"),
			Group:      group,
			Hidden:     hidden,
		})
	}
	return items
}

func deriveIngressURL(ingress ingress) string {
	if len(ingress.Spec.Rules) == 0 {
		return ""
	}
	rule := ingress.Spec.Rules[0]
	if rule.Host == "" || strings.Contains(rule.Host, "*") {
		return ""
	}
	scheme := "http"
	if len(ingress.Spec.TLS) > 0 {
		scheme = "https"
	}
	path := "/"
	if len(rule.HTTP.Paths) > 0 && rule.HTTP.Paths[0].Path != "" {
		path = rule.HTTP.Paths[0].Path
	}
	return scheme + "://" + rule.Host + path
}

type httpRouteList struct {
	Metadata listMeta    `json:"metadata"`
	Items    []httpRoute `json:"items"`
}

type httpRoute struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Hostnames []string `json:"hostnames"`
		Rules     []struct {
			Matches []struct {
				Path struct {
					Value string `json:"value"`
				} `json:"path"`
			} `json:"matches"`
		} `json:"rules"`
	} `json:"spec"`
}

func httpRouteItems(route httpRoute) []config.DiscoveredItem {
	annotations := route.Metadata.Annotations
	if !applicationEnabled(annotations) {
		return nil
	}
	names := splitLabels(annotation(annotations, "blackflame.dev/name", "flame.pawelmalak/name"))
	urls := splitLabels(annotation(annotations, "blackflame.dev/url", "flame.pawelmalak/url"))
	icons := splitLabels(annotation(annotations, "blackflame.dev/icon", "flame.pawelmalak/icon"))
	groups := splitLabels(annotation(annotations, "blackflame.dev/group"))
	hidden := parseBool(annotations["blackflame.dev/hidden"])

	if len(urls) == 0 {
		if derived := deriveHTTPRouteURL(route); derived != "" {
			urls = []string{derived}
		}
	}
	if len(urls) == 0 {
		return nil
	}
	group := pick(groups, 0, route.Metadata.Namespace)
	if group == "" {
		group = "Kubernetes"
	}
	var items []config.DiscoveredItem
	for index, rawURL := range urls {
		if !validHTTPURL(rawURL) {
			continue
		}
		items = append(items, config.DiscoveredItem{
			Source:     config.SourceKubernetes,
			ExternalID: route.Metadata.Namespace + "/" + route.Metadata.Name + ":" + strconv.Itoa(index),
			Name:       pick(names, index, route.Metadata.Name),
			URL:        rawURL,
			Icon:       pick(icons, index, "kubernetes"),
			Group:      group,
			Hidden:     hidden,
		})
	}
	return items
}

func deriveHTTPRouteURL(route httpRoute) string {
	if len(route.Spec.Hostnames) == 0 || strings.Contains(route.Spec.Hostnames[0], "*") {
		return ""
	}
	scheme := "https"
	if value := strings.TrimSpace(route.Metadata.Annotations["blackflame.dev/scheme"]); value == "http" || value == "https" {
		scheme = value
	}
	path := "/"
	if len(route.Spec.Rules) > 0 && len(route.Spec.Rules[0].Matches) > 0 {
		if value := route.Spec.Rules[0].Matches[0].Path.Value; value != "" {
			path = value
		}
	}
	return scheme + "://" + route.Spec.Hostnames[0] + path
}

func annotation(annotations map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(annotations[key]); value != "" {
			return value
		}
	}
	return ""
}

func applicationEnabled(annotations map[string]string) bool {
	if isApplicationType(annotation(annotations, "blackflame.dev/type", "flame.pawelmalak/type")) {
		return true
	}
	return parseBool(annotations["blackflame.dev/enabled"])
}

func isOptionalAPIError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") || strings.Contains(text, "the server could not find the requested resource")
}

type KubernetesHealth struct {
	NodesReady  int `json:"nodesReady"`
	NodesTotal  int `json:"nodesTotal"`
	PodsRunning int `json:"podsRunning"`
	PodsFailing int `json:"podsFailing"`
	PodsTotal   int `json:"podsTotal"`
}

// KubernetesSummary reads cluster health for the optional Kubernetes widget.
func KubernetesSummary(ctx context.Context, cfg config.KubernetesDiscovery) (KubernetesHealth, error) {
	client, err := newK8sClient(cfg)
	if err != nil {
		return KubernetesHealth{}, err
	}
	var summary KubernetesHealth

	var nodeList struct {
		Items []struct {
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := client.get(ctx, "/api/v1/nodes", &nodeList); err != nil {
		return summary, err
	}
	summary.NodesTotal = len(nodeList.Items)
	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				summary.NodesReady++
				break
			}
		}
	}

	namespaces := cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		path := "/api/v1/pods"
		if namespace != "" {
			path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods"
		}
		var podList struct {
			Items []struct {
				Status struct {
					Phase             string `json:"phase"`
					ContainerStatuses []struct {
						State struct {
							Waiting *struct {
								Reason string `json:"reason"`
							} `json:"waiting"`
						} `json:"state"`
					} `json:"containerStatuses"`
				} `json:"status"`
			} `json:"items"`
		}
		if err := client.get(ctx, path, &podList); err != nil {
			return summary, err
		}
		for _, pod := range podList.Items {
			summary.PodsTotal++
			if pod.Status.Phase == "Running" {
				summary.PodsRunning++
			}
			failing := pod.Status.Phase == "Failed"
			for _, container := range pod.Status.ContainerStatuses {
				if container.State.Waiting == nil {
					continue
				}
				switch container.State.Waiting.Reason {
				case "CrashLoopBackOff", "Error", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError":
					failing = true
				}
			}
			if failing {
				summary.PodsFailing++
			}
		}
	}
	return summary, nil
}
