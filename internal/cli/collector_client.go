package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func fetchJSON[T any](collectorURL string, path string) (T, error) {
	var zero T
	endpoint := strings.TrimRight(collectorURL, "/") + path
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return zero, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("GET %s: status %d", endpoint, resp.StatusCode)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return out, nil
}

func formatAge(capturedAt time.Time) string {
	if capturedAt.IsZero() {
		return "-"
	}
	age := time.Since(capturedAt)
	if age < 0 {
		age = 0
	}
	age = age.Round(time.Second)
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}
