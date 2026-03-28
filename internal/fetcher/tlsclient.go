package fetcher

import (
	"fmt"
	"io"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

var (
	tlsOnce   sync.Once
	tlsClient tls_client.HttpClient
	tlsErr    error
)

func fetchTLSClient(url string) (string, error) {
	tlsOnce.Do(func() {
		jar := tls_client.NewCookieJar()
		tlsClient, tlsErr = tls_client.NewHttpClient(tls_client.NewNoopLogger(),
			tls_client.WithTimeoutSeconds(30),
			tls_client.WithClientProfile(profiles.Chrome_133),
			tls_client.WithCookieJar(jar),
			tls_client.WithNotFollowRedirects(),
		)
	})
	if tlsErr != nil {
		return "", fmt.Errorf("tls-client init: %w", tlsErr)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	req.Header = http.Header{
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		"accept-language":           {"en-US,en;q=0.9"},
		"accept-encoding":           {"gzip, deflate, br"},
		"user-agent":                {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"},
		"sec-ch-ua":                 {`"Not(A:Brand";v="99", "Google Chrome";v="133", "Chromium";v="133"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"macOS"`},
		"sec-fetch-dest":            {"document"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-user":            {"?1"},
		"upgrade-insecure-requests": {"1"},
		http.HeaderOrderKey: {
			"accept", "accept-language", "accept-encoding", "user-agent",
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site", "sec-fetch-user",
			"upgrade-insecure-requests",
		},
	}

	time.Sleep(500 * time.Millisecond)

	resp, err := tlsClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tls-client request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("tls-client: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("tls-client read body: %w", err)
	}
	return string(body), nil
}
