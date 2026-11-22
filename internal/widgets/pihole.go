package widgets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/galq/blackflame/internal/config"
)

type PiHoleData struct {
	Queries        int64   `json:"queries"`
	Blocked        int64   `json:"blocked"`
	PercentBlocked float64 `json:"percentBlocked"`
	DomainsBlocked int64   `json:"domainsBlocked"`
	Status         string  `json:"status,omitempty"`
}

func fetchPiHole(ctx context.Context, cfg *config.PiHoleWidget) (any, error) {
	base := strings.TrimRight(cfg.URL, "/")
	secret, err := cfg.Secret.Resolve()
	if err != nil {
		return nil, err
	}
	client := newHTTPClient(cfg.InsecureSkipVerify)

	// Pi-hole v6 uses a session id. If the instance has no password, summary
	// may be readable without authentication.
	sid := ""
	var authErr error
	if secret != "" {
		var auth struct {
			Session struct {
				SID string `json:"sid"`
			} `json:"session"`
		}
		body, _ := json.Marshal(map[string]string{"password": secret})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/auth", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			authErr = fmt.Errorf("v6 login request failed: %w", err)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
					authErr = fmt.Errorf("decode v6 login response: %w", err)
				} else if auth.Session.SID == "" {
					authErr = fmt.Errorf("v6 login returned no session id")
				} else {
					sid = auth.Session.SID
				}
			} else {
				authErr = fmt.Errorf("v6 login returned %d", resp.StatusCode)
			}
		}
	}

	var v6 struct {
		Queries struct {
			Total          int64   `json:"total"`
			Blocked        int64   `json:"blocked"`
			PercentBlocked float64 `json:"percent_blocked"`
		} `json:"queries"`
		Gravity struct {
			DomainsBeingBlocked int64 `json:"domains_being_blocked"`
		} `json:"gravity"`
		Status string `json:"status"`
	}
	headers := map[string]string{}
	if sid != "" {
		headers["X-FTL-SID"] = sid
	}
	if err := getJSON(ctx, client, base+"/api/stats/summary", headers, &v6); err == nil {
		return PiHoleData{
			Queries:        v6.Queries.Total,
			Blocked:        v6.Queries.Blocked,
			PercentBlocked: v6.Queries.PercentBlocked,
			DomainsBlocked: v6.Gravity.DomainsBeingBlocked,
			Status:         v6.Status,
		}, nil
	}

	// Pi-hole v5 API.
	query := url.Values{}
	query.Set("summaryRaw", "")
	if secret != "" {
		query.Set("auth", secret)
	}
	var v5 struct {
		DNSQueriesToday     int64   `json:"dns_queries_today"`
		AdsBlockedToday     int64   `json:"ads_blocked_today"`
		AdsPercentageToday  float64 `json:"ads_percentage_today"`
		DomainsBeingBlocked int64   `json:"domains_being_blocked"`
		Status              string  `json:"status"`
	}
	endpoint := base + "/admin/api.php?" + query.Encode()
	if err := getJSON(ctx, client, endpoint, nil, &v5); err != nil {
		if authErr != nil {
			return nil, fmt.Errorf("Pi-hole API is not reachable (%s): %w", authErr, err)
		}
		return nil, fmt.Errorf("Pi-hole API is not reachable: %w", err)
	}
	return PiHoleData{
		Queries:        v5.DNSQueriesToday,
		Blocked:        v5.AdsBlockedToday,
		PercentBlocked: v5.AdsPercentageToday,
		DomainsBlocked: v5.DomainsBeingBlocked,
		Status:         v5.Status,
	}, nil
}
