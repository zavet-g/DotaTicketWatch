package flights

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiBase     = "https://api.travelpayouts.com/aviasales/v3/prices_for_dates"
	destination = "SHA"
)

type Client struct {
	token  string
	marker string
	http   *http.Client
}

func NewClient(token, marker string) *Client {
	return &Client{
		token:  token,
		marker: marker,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) FetchDirect(origin, departure, ret string) ([]Route, error) {
	params := url.Values{
		"origin":      {origin},
		"destination": {destination},
		"one_way":     {"false"},
		"direct":      {"true"},
		"currency":    {"rub"},
		"sorting":     {"price"},
		"limit":       {"10"},
		"token":       {c.token},
	}
	if departure != "" {
		params.Set("departure_at", departure)
	}
	if ret != "" {
		params.Set("return_at", ret)
	}

	resp, err := c.http.Get(apiBase + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	if !result.Success || len(result.Data) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)
	var routes []Route
	for _, t := range result.Data {
		if t.Transfers != 0 || t.ReturnTransfers != 0 {
			continue
		}

		key := t.FlightNumber
		if seen[key] {
			continue
		}
		seen[key] = true

		var link string
		if strings.HasPrefix(t.Link, "/search/") {
			link = "https://www.aviasales.ru" + t.Link
			if c.marker != "" {
				link += "&marker=" + c.marker
			}
		} else {
			link = c.FallbackLink(origin, departure, ret)
		}

		var depAt time.Time
		if parsed, err := time.Parse(time.RFC3339, t.DepartureAt); err == nil {
			depAt = parsed
		}

		routes = append(routes, Route{
			Origin:             t.Origin,
			OriginAirport:      t.OriginAirport,
			DestinationAirport: t.DestinationAirport,
			Price:              t.Price,
			Airline:            t.Airline,
			AirlineName:        airlineName(t.Airline),
			DurationTo:         t.DurationTo,
			DepartureAt:        depAt,
			Link:               link,
		})
	}
	return routes, nil
}

func (c *Client) FallbackLink(origin, departure, ret string) string {
	dep := mustFormatDDMM(departure)
	ret2 := mustFormatDDMM(ret)
	link := fmt.Sprintf("https://www.aviasales.ru/search/%s%s%s%s1", origin, dep, destination, ret2)
	if c.marker != "" {
		link += "?marker=" + c.marker
	}
	return link
}

func mustFormatDDMM(isoDate string) string {
	t, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return "0101"
	}
	return fmt.Sprintf("%02d%02d", t.Day(), t.Month())
}
