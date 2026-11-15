package widgets

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"

	"github.com/galq/blackflame/internal/config"
)

type CalendarData struct {
	Events []CalendarEvent `json:"events"`
}

type CalendarEvent struct {
	Summary  string    `json:"summary"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end,omitempty"`
	Location string    `json:"location,omitempty"`
}

func fetchCalendar(ctx context.Context, cfg *config.CalendarWidget) (any, error) {
	client := newHTTPClient(cfg.InsecureSkipVerify)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("calendar returned %d", resp.StatusCode)
	}
	calendar, err := ics.ParseCalendar(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("parse calendar: %w", err)
	}

	days := cfg.Days
	if days <= 0 {
		days = 7
	}
	maxEvents := cfg.MaxEvents
	if maxEvents <= 0 {
		maxEvents = 5
	}
	now := time.Now()
	until := now.AddDate(0, 0, days)
	events := make([]CalendarEvent, 0, maxEvents)
	for _, event := range calendar.Events() {
		start, err := event.GetStartAt()
		if err != nil || start.After(until) {
			continue
		}
		end, _ := event.GetEndAt()
		if end.IsZero() {
			end = start
		}
		if end.Before(now) {
			continue
		}
		summary := strings.TrimSpace(propertyValue(event.GetProperty(ics.ComponentPropertySummary)))
		if summary == "" {
			summary = "Untitled event"
		}
		location := strings.TrimSpace(propertyValue(event.GetProperty(ics.ComponentPropertyLocation)))
		events = append(events, CalendarEvent{
			Summary:  summary,
			Start:    start,
			End:      end,
			Location: location,
		})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })
	if len(events) > maxEvents {
		events = events[:maxEvents]
	}
	return CalendarData{Events: events}, nil
}

func propertyValue(property *ics.IANAProperty) string {
	if property == nil {
		return ""
	}
	return property.Value
}
