package fetcher

import (
	"fmt"
	"log/slog"
	"strings"
)

func Fetch(url, flareSolverrURL string) (string, error) {
	if html, err := FetchLevel1(url); err == nil {
		slog.Debug("fetcher: level1 ok", "url", url)
		return html, nil
	}
	slog.Debug("fetcher: level1 failed, trying curl")

	if html, err := FetchLevel2(url); err == nil {
		slog.Debug("fetcher: level2 ok", "url", url)
		return html, nil
	}
	slog.Debug("fetcher: level2 failed, trying flaresolverr")

	if flareSolverrURL == "" {
		return "", fmt.Errorf("all fetch levels failed and FlareSolverr not configured")
	}
	html, err := fetchFlareSolverr(url, flareSolverrURL)
	if err != nil {
		return "", fmt.Errorf("all fetch levels failed, flaresolverr: %w", err)
	}
	if !isRealHTML(html) {
		return "", fmt.Errorf("all fetch levels failed: flaresolverr returned challenge page")
	}
	slog.Debug("fetcher: level3 ok", "url", url)
	return html, nil
}

func FetchLevel1(url string) (string, error) {
	html, err := fetchTLSClient(url)
	if err != nil {
		return "", err
	}
	if !isRealHTML(html) {
		return "", fmt.Errorf("level1: got cloudflare challenge page")
	}
	return html, nil
}

func FetchLevel2(url string) (string, error) {
	html, err := fetchCurl(url)
	if err != nil {
		return "", err
	}
	if !isRealHTML(html) {
		return "", fmt.Errorf("level2: got cloudflare challenge page")
	}
	return html, nil
}

func isRealHTML(html string) bool {
	if len(html) < 500 {
		return false
	}
	lower := strings.ToLower(html)
	if strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf-browser-verification") ||
		(strings.Contains(lower, "cloudflare") && strings.Contains(lower, "checking your browser")) {
		return false
	}
	return true
}
