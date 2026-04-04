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
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
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

const flareSolverrRetries = 3

func fetchFlareSolverr(url, baseURL string) (string, error) {
	var lastErr error
	for i := range flareSolverrRetries {
		html, err := fetchFlareSolverrOnce(url, baseURL)
		if err == nil {
			return html, nil
		}
		lastErr = err
		if i < flareSolverrRetries-1 {
			time.Sleep(time.Duration(5*(i+1)) * time.Second)
		}
	}
	return "", lastErr
}

func fetchFlareSolverrOnce(url, baseURL string) (string, error) {
	payload, _ := json.Marshal(flareSolverrRequest{
		Cmd:        "request.get",
		URL:        url,
		MaxTimeout: 120000,
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
		preview := string(raw)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return "", fmt.Errorf("flaresolverr returned non-JSON (HTTP %d): %s", resp.StatusCode, preview)
	}
	if fsResp.Status != "ok" {
		return "", fmt.Errorf("flaresolverr error: %s", fsResp.Message)
	}
	if fsResp.Solution.Status != 200 {
		return "", fmt.Errorf("flaresolverr: target returned HTTP %d", fsResp.Solution.Status)
	}

	return fsResp.Solution.Response, nil
}
