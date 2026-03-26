package fetcher

import (
	"fmt"
	"os/exec"
	"strings"
)

func fetchCurl(url string) (string, error) {
	cmd := exec.Command("curl",
		"--silent",
		"--location",
		"--max-time", "30",
		"--compressed",
		"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		"--header", "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"--header", "Accept-Language: en-US,en;q=0.9",
		"--header", "sec-fetch-dest: document",
		"--header", "sec-fetch-mode: navigate",
		"--header", "sec-fetch-site: none",
		"--header", "sec-fetch-user: ?1",
		"--write-out", "\n__STATUS__%{http_code}",
		url,
	)

	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return "", fmt.Errorf("curl not found in PATH")
		}
		return "", fmt.Errorf("curl exec: %w", err)
	}

	output := string(out)
	parts := strings.SplitN(output, "\n__STATUS__", 2)
	body := parts[0]
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "200" {
		return "", fmt.Errorf("curl: status %s", strings.TrimSpace(parts[1]))
	}

	return body, nil
}
