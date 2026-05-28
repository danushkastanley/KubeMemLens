package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

func collectorUnavailableError(opts client.Options, description string, err error) error {
	return client.ConnectionError(opts, description, err)
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
