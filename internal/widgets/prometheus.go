package widgets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/galq/blackflame/internal/config"
)

type PrometheusData struct {
	Queries []PrometheusValue `json:"queries"`
}

type PrometheusValue struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

func fetchPrometheus(ctx context.Context, cfg *config.PrometheusWidget) (any, error) {
	base := strings.TrimRight(cfg.URL, "/")
	client := newHTTPClient(cfg.InsecureSkipVerify)
	username, err := cfg.Username.Resolve()
	if err != nil {
		return nil, err
	}
	password, err := cfg.Password.Resolve()
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if username != "" || password != "" {
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	}

	data := PrometheusData{}
	for _, query := range cfg.Queries {
		values := url.Values{}
		values.Set("query", query.Query)
		endpoint := base + "/api/v1/query?" + values.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("Prometheus returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var payload struct {
			Status string `json:"status"`
			Data   struct {
				Result []struct {
					Metric map[string]string `json:"metric"`
					Value  []any             `json:"value"`
				} `json:"result"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode Prometheus response: %w", err)
		}
		if payload.Status != "" && payload.Status != "success" {
			return nil, fmt.Errorf("Prometheus query failed: %s", payload.Status)
		}
		value := 0.0
		if len(payload.Data.Result) > 0 && len(payload.Data.Result[0].Value) > 1 {
			if raw, ok := payload.Data.Result[0].Value[1].(string); ok {
				if parsed, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
					value = parsed
				}
			}
		}
		data.Queries = append(data.Queries, PrometheusValue{
			Label: query.Label,
			Value: value,
			Unit:  query.Unit,
		})
	}
	return data, nil
}
