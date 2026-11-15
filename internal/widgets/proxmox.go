package widgets

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/galq/blackflame/internal/config"
)

type ProxmoxData struct {
	Nodes       int     `json:"nodes"`
	NodesOnline int     `json:"nodesOnline"`
	CPUPercent  float64 `json:"cpuPercent"`
	MemoryUsed  int64   `json:"memoryUsed"`
	MemoryTotal int64   `json:"memoryTotal"`
	VMsRunning  int     `json:"vmsRunning"`
	VMsTotal    int     `json:"vmsTotal"`
}

type proxmoxNode struct {
	Node   string  `json:"node"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"`
	Mem    int64   `json:"mem"`
	MaxMem int64   `json:"maxmem"`
}

func fetchProxmox(ctx context.Context, cfg *config.ProxmoxWidget) (any, error) {
	secret, err := cfg.TokenSecret.Resolve()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.TokenID) == "" || secret == "" {
		return nil, fmt.Errorf("Proxmox token ID and secret are required")
	}
	base := strings.TrimRight(cfg.URL, "/")
	client := newHTTPClient(cfg.InsecureSkipVerify)
	headers := map[string]string{
		"Authorization": "PVEAPIToken=" + cfg.TokenID + "=" + secret,
	}
	var nodeResponse struct {
		Data []proxmoxNode `json:"data"`
	}
	if err := getJSON(ctx, client, base+"/api2/json/nodes", headers, &nodeResponse); err != nil {
		return nil, err
	}
	data := ProxmoxData{}
	for _, node := range nodeResponse.Data {
		if cfg.Node != "" && node.Node != cfg.Node {
			continue
		}
		data.Nodes++
		if node.Status == "online" {
			data.NodesOnline++
		}
		data.CPUPercent += node.CPU * 100
		data.MemoryUsed += node.Mem
		data.MemoryTotal += node.MaxMem

		var vmResponse struct {
			Data []struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		endpoint := base + "/api2/json/nodes/" + url.PathEscape(node.Node) + "/qemu"
		if err := getJSON(ctx, client, endpoint, headers, &vmResponse); err != nil {
			// A node can exist without qemu permissions or without VMs.
			continue
		}
		for _, vm := range vmResponse.Data {
			data.VMsTotal++
			if vm.Status == "running" {
				data.VMsRunning++
			}
		}
	}
	if data.Nodes > 0 {
		data.CPUPercent /= float64(data.Nodes)
	}
	return data, nil
}
