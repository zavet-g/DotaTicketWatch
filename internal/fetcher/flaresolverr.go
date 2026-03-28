package fetcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type flareSolverrRequest struct {
	Cmd               string `json:"cmd"`
	URL               string `json:"url"`
	MaxTimeout        int    `json:"maxTimeout"`
	Session           string `json:"session"`
	SessionTTLMinutes int    `json:"session_ttl_minutes"`
	DisableMedia      bool   `json:"disableMedia"`
}

type flareSolverrSolution struct {
	Status   int    `json:"status"`
	Response string `json:"response"`
}

type flareSolverrResponse struct {
	Solution flareSolverrSolution `json:"solution"`
	Status   string               `json:"status"`
	Message  string               `json:"message"`
}

var flareSolverrHTTPClient = &http.Client{Timeout: 150 * time.Second}

func fetchFlareSolverr(url, baseURL string) (string, error) {
	payload, _ := json.Marshal(flareSolverrRequest{
		Cmd:               "request.get",
		URL:               url,
		MaxTimeout:        120000,
		Session:           "axs-monitor",
		SessionTTLMinutes: 30,
		DisableMedia:      true,
	})

	resp, err := flareSolverrHTTPClient.Post(baseURL+"/v1", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("flaresolverr request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("flaresolverr read response: %w", err)
	}

	var fsResp flareSolverrResponse
	if err := json.Unmarshal(raw, &fsResp); err != nil {
		return "", fmt.Errorf("flaresolverr parse response: %w", err)
	}
	if fsResp.Status != "ok" {
		return "", fmt.Errorf("flaresolverr error: %s", fsResp.Message)
	}
	if fsResp.Solution.Status != 200 {
		return "", fmt.Errorf("flaresolverr: target returned HTTP %d", fsResp.Solution.Status)
	}

	return fsResp.Solution.Response, nil
}
